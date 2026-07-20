package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func raceRequest(t *testing.T, g *Gateway, k string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("X-Race", k)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

// O2: a raced request returns the fast backend's answer without waiting for
// the slow one, and the loser's work is cancelled.
func TestRaceFirstAnswerWins(t *testing.T) {
	var slowCancelled atomic.Bool
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"winner":"fast","usage":{"prompt_tokens":3,"completion_tokens":4}}`)
	}))
	defer fast.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain the body first: the server only watches for client
		// disconnect (context cancellation) once the request body is consumed.
		io.ReadAll(r.Body)
		select {
		case <-r.Context().Done():
			slowCancelled.Store(true)
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()

	g := newTestGateway(t, fast.URL, slow.URL)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- raceRequest(t, g, "2") }()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"winner":"fast"`) {
			t.Fatalf("wrong winner: %q", rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("raced request waited for the slow backend")
	}
	// Cancellation must reach the losing backend (no zombie compute).
	deadline := time.Now().Add(2 * time.Second)
	for !slowCancelled.Load() {
		if time.Now().After(deadline) {
			t.Fatal("losing backend was never cancelled")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A raced request where one backend 500s still gets the healthy answer.
func TestRaceSurvivesFailingBackend(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer good.Close()

	g := newTestGateway(t, bad.URL, good.URL)
	rec := raceRequest(t, g, "2")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("status %d body %q, want the healthy answer", rec.Code, rec.Body.String())
	}
}

// When every raced backend fails, the client gets one clean 502.
func TestRaceAllFail(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()

	g := newTestGateway(t, bad.URL)
	rec := raceRequest(t, g, "2")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
}

// The opt-in header is parsed defensively: absent or 1 means no race, junk
// means the default, huge values are clamped.
func TestRaceKParsing(t *testing.T) {
	cases := []struct {
		header string
		set    bool
		want   int
	}{
		{set: false, want: 1},
		{header: "0", set: true, want: 1},
		{header: "1", set: true, want: 1},
		{header: "2", set: true, want: 2},
		{header: "99", set: true, want: raceMaxK},
		{header: "on", set: true, want: raceDefaultK},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		if c.set {
			req.Header.Set("X-Race", c.header)
		}
		if got := raceK(req); got != c.want {
			t.Errorf("X-Race=%q (set=%v): got %d, want %d", c.header, c.set, got, c.want)
		}
	}
}

// /status reports the raced-vs-single latency comparison for the console.
func TestRaceStatsOnStatus(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer good.Close()

	g := newTestGateway(t, good.URL)
	if rec := raceRequest(t, g, "2"); rec.Code != http.StatusOK {
		t.Fatalf("raced request: status %d", rec.Code)
	}
	if rec := proxyRequest(t, g, `{}`); rec.Code != http.StatusOK {
		t.Fatalf("single request: status %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `"racing"`) || !strings.Contains(body, `"raced"`) || !strings.Contains(body, `"single"`) {
		t.Fatalf("/status missing racing stats: %q", body)
	}
}

// The race helper itself respects a k larger than the mesh.
func TestRaceKClampedToMeshSize(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer good.Close()

	g := newTestGateway(t, good.URL)
	a, raced := g.race(context.Background(), "/v1/chat/completions", []byte(`{}`), 4)
	if a.status != http.StatusOK || raced != 1 {
		t.Fatalf("status %d raced %d, want 200 with k clamped to 1", a.status, raced)
	}
}
