package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// L1 backfill: the relay proxy is the owner's share-limit enforcement point
// — peer-served work may only occupy the declared budget.

func proxyTo(t *testing.T, backendURL string, slots func() int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewServer(backendURL, nil, nil, slots).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// A node sharing nothing refuses peer work outright.
func TestNotSharingRefusesWork(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()
	srv := proxyTo(t, backend.URL, func() int { return 0 })
	resp := post(t, srv.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("0-slot node must 503, got %d", resp.StatusCode)
	}
}

// Peer work beyond the shared budget is turned away with Retry-After while
// work within the budget proceeds.
func TestShareBudgetEnforced(t *testing.T) {
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Done()
		<-release
		io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()
	srv := proxyTo(t, backend.URL, func() int { return 1 })

	first := make(chan *http.Response)
	go func() {
		resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
		if err == nil {
			first <- resp
		}
	}()
	started.Wait() // the single slot is now occupied
	resp := post(t, srv.URL)
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over-budget request must 503 with Retry-After, got %d %q",
			resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	resp.Body.Close()
	close(release)
	r1 := <-first
	defer r1.Body.Close()
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("in-budget request must succeed, got %d", r1.StatusCode)
	}
}

// The slot frees after a request completes — the budget is per-in-flight,
// not a lifetime counter.
func TestSlotFreedAfterCompletion(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()
	srv := proxyTo(t, backend.URL, func() int { return 1 })
	for i := 0; i < 3; i++ {
		resp := post(t, srv.URL)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sequential request %d got %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// A node with no local runtime says so instead of hanging.
func TestNoRuntime(t *testing.T) {
	srv := proxyTo(t, "", func() int { return 4 })
	resp := post(t, srv.URL)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("runtime-less node must 503, got %d", resp.StatusCode)
	}
}

// The proxy relays the backend's response body and status through.
func TestProxyRelaysBackend(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("backend saw path %q, want /chat/completions", r.URL.Path)
		}
		w.WriteHeader(http.StatusTeapot)
		io.WriteString(w, `{"relayed":true}`)
	}))
	defer backend.Close()
	srv := proxyTo(t, backend.URL, nil)
	resp := post(t, srv.URL)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusTeapot || !strings.Contains(string(body), `"relayed":true`) {
		t.Fatalf("proxy must relay status and body: %d %s", resp.StatusCode, body)
	}
}
