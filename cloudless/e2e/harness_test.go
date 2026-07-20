// Package e2e drives the real cloudless binary as a black box: build it,
// start a node against stub backends, and prove the promises hold end to
// end — routing, failover when a backend dies mid-run, batch fan-out, and
// status reporting. This is the reusable harness for L2 (#85); multi-node
// gossip/churn scenarios extend it.
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cloudless-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "cloudless")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/cloudless")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// stubBackend is a minimal chat-completions endpoint whose availability the
// test controls.
func stubBackend(name string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /models", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"data":[]}`)
	})
	mux.HandleFunc("POST /chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"served_by":%q,"usage":{"prompt_tokens":1,"completion_tokens":1}}`, name)
	})
	return httptest.NewServer(mux)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startNode launches the binary with a config over the given backends and
// waits for the gateway to come up.
func startNode(t *testing.T, apiKey string, backendURLs ...string) (base string) {
	t.Helper()
	port := freePort(t)
	type backend struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	}
	backends := make([]backend, len(backendURLs))
	for i, u := range backendURLs {
		backends[i] = backend{Name: fmt.Sprintf("stub-%d", i), BaseURL: u}
	}
	cfg := map[string]any{
		"listen": fmt.Sprintf("127.0.0.1:%d", port), "api_key": apiKey,
		"health_interval_seconds": 1, "backends": backends,
	}
	dir := t.TempDir()
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binPath, "serve", "-config", cfgPath)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	base = fmt.Sprintf("http://127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		if r, err := http.Get(base + "/healthz"); err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				return base
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("node did not come up")
	return ""
}

func chat(t *testing.T, base, apiKey string) (status int, servedBy string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		ServedBy string `json:"served_by"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.ServedBy
}

// The core promise, end to end on the real binary: requests keep succeeding
// when the serving backend dies — another takes over.
func TestFailoverWhenBackendDies(t *testing.T) {
	a, b := stubBackend("a"), stubBackend("b")
	defer b.Close()
	base := startNode(t, "e2e-key", a.URL, b.URL)

	if code, _ := chat(t, base, "e2e-key"); code != http.StatusOK {
		t.Fatalf("initial request failed: %d", code)
	}
	a.Close() // kill one backend mid-run
	deadline := time.Now().Add(10 * time.Second)
	for {
		code, served := chat(t, base, "e2e-key")
		if code == http.StatusOK && served == "b" {
			break // failover complete
		}
		if time.Now().After(deadline) {
			t.Fatalf("failover did not complete: last code %d served_by %q", code, served)
		}
		time.Sleep(200 * time.Millisecond)
	}
	// /status must reflect reality: one healthy, one not (after a probe cycle).
	statusDeadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/status")
		if err != nil {
			t.Fatal(err)
		}
		var st struct {
			Backends []struct {
				Healthy bool `json:"Healthy"`
			} `json:"backends"`
		}
		json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		healthy := 0
		for _, s := range st.Backends {
			if s.Healthy {
				healthy++
			}
		}
		if len(st.Backends) == 2 && healthy == 1 {
			break
		}
		if time.Now().After(statusDeadline) {
			t.Fatalf("status never showed 1/2 healthy: %+v", st.Backends)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Batch fan-out works through the real binary and spreads across backends.
func TestBatchAcrossNodes(t *testing.T) {
	a, b := stubBackend("a"), stubBackend("b")
	defer a.Close()
	defer b.Close()
	base := startNode(t, "e2e-key", a.URL, b.URL)

	items := make([]string, 8)
	for i := range items {
		items[i] = `{"messages":[]}`
	}
	body := `{"requests":[` + strings.Join(items, ",") + `]}`
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/batch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer e2e-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Results []struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 8 {
		t.Fatalf("want 8 results, got %d", len(out.Results))
	}
	served := map[string]int{}
	for i, res := range out.Results {
		if res.Status != http.StatusOK {
			t.Fatalf("item %d: status %d", i, res.Status)
		}
		var by struct {
			ServedBy string `json:"served_by"`
		}
		json.Unmarshal(res.Body, &by)
		served[by.ServedBy]++
	}
	if len(served) < 2 {
		t.Fatalf("batch must spread across both backends, got %v", served)
	}
}

// Auth is enforced end to end: wrong key refused, admin key accepted.
func TestAuthEndToEnd(t *testing.T) {
	a := stubBackend("a")
	defer a.Close()
	base := startNode(t, "right-key", a.URL)
	if code, _ := chat(t, base, "wrong-key"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key must 401, got %d", code)
	}
	if code, _ := chat(t, base, "right-key"); code != http.StatusOK {
		t.Fatalf("admin key must pass, got %d", code)
	}
}
