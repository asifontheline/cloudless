package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloudless/internal/config"
	"cloudless/internal/registry"
)

// newTestGateway builds a gateway over the given backend URLs, no TLS.
func newTestGateway(t *testing.T, urls ...string) *Gateway {
	t.Helper()
	backends := make([]config.Backend, len(urls))
	for i, u := range urls {
		backends[i] = config.Backend{Name: string(rune('a' + i)), BaseURL: u}
	}
	return New(registry.New(backends, time.Hour, nil), "test-key", nil)
}

func proxyRequest(t *testing.T, g *Gateway, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

// A stream that dies before its first token must be retried on another
// backend; the client sees one clean, complete response (C5).
func TestStreamRetryBeforeFirstByte(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Commit headers, then drop the connection with zero body bytes.
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer dead.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: hello\n\ndata: [DONE]\n\n")
	}))
	defer good.Close()

	g := newTestGateway(t, dead.URL, good.URL)
	// Ranked order is not deterministic without probes; run several requests
	// so both orders occur. Every request must succeed regardless.
	for i := 0; i < 10; i++ {
		rec := proxyRequest(t, g, `{"stream":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, "data: hello") {
			t.Fatalf("request %d: incomplete stream: %q", i, body)
		}
	}
}

// When every backend dies pre-first-byte the client gets one clean 502 —
// never a half-committed response.
func TestStreamAllBackendsDead(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer dead.Close()

	g := newTestGateway(t, dead.URL)
	rec := proxyRequest(t, g, `{"stream":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
}

// A buffered (non-stream) body that fails mid-read is retried: nothing was
// committed to the client yet.
func TestBufferedRetryOnTruncatedBody(t *testing.T) {
	truncated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1000") // promise more than we send
		io.WriteString(w, `{"partial":`)
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer truncated.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`)
	}))
	defer good.Close()

	g := newTestGateway(t, truncated.URL, good.URL)
	for i := 0; i < 10; i++ {
		rec := proxyRequest(t, g, `{}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"choices"`) {
			t.Fatalf("request %d: wrong body: %q", i, rec.Body.String())
		}
	}
}

// Connection-refused backends keep failing over to a healthy one (existing
// behavior, pinned by a test case).
func TestConnectErrorFailover(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer good.Close()

	g := newTestGateway(t, "http://127.0.0.1:1", good.URL)
	for i := 0; i < 5; i++ {
		rec := proxyRequest(t, g, `{}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, rec.Code)
		}
	}
}

// /join-info reveals the mesh secret — it must demand the admin key and be
// absent on nodes that can't share it (E2).
func TestJoinInfoAdminGate(t *testing.T) {
	g := newTestGateway(t)
	g.JoinInfo = func() (string, string, string) { return "sec", "1.2.3.4:7946", "http://1.2.3.4:8080" }

	req := httptest.NewRequest(http.MethodGet, "/join-info", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("no admin key must be refused: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/join-info", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin key: status %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "1.2.3.4:7946") || !strings.Contains(body, `"secret":"sec"`) {
		t.Fatalf("join info body wrong: %s", body)
	}

	g2 := newTestGateway(t)
	req = httptest.NewRequest(http.MethodGet, "/join-info", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	g2.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unset JoinInfo: status %d, want 404", rec.Code)
	}
}

// O1: a batch fans out across backends concurrently, returns results in
// submission order, and spreads load over more than one node.
func TestBatchFanOut(t *testing.T) {
	var hitsA, hitsB atomic.Int64
	mk := func(hits *atomic.Int64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			var in struct {
				N int `json:"n"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"echo":%d,"usage":{"prompt_tokens":1,"completion_tokens":1}}`, in.N)
		}))
	}
	a, b := mk(&hitsA), mk(&hitsB)
	defer a.Close()
	defer b.Close()

	g := newTestGateway(t, a.URL, b.URL)
	items := make([]string, 12)
	for i := range items {
		items[i] = fmt.Sprintf(`{"n":%d}`, i)
	}
	body := `{"path":"/v1/chat/completions","requests":[` + strings.Join(items, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Results []struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 12 {
		t.Fatalf("want 12 results, got %d", len(out.Results))
	}
	for i, res := range out.Results {
		if res.Status != 200 {
			t.Fatalf("item %d status %d", i, res.Status)
		}
		var echo struct {
			Echo int `json:"echo"`
		}
		json.Unmarshal(res.Body, &echo)
		if echo.Echo != i {
			t.Fatalf("order broken: item %d echoed %d", i, echo.Echo)
		}
	}
	if hitsA.Load() == 0 || hitsB.Load() == 0 {
		t.Fatalf("fan-out must spread across nodes: a=%d b=%d", hitsA.Load(), hitsB.Load())
	}
}

// A batch item that hits a dead backend fails over like a single request.
func TestBatchItemFailover(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer good.Close()
	g := newTestGateway(t, "http://127.0.0.1:1", good.URL)
	body := `{"requests":[{},{},{},{}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	var out struct {
		Results []struct {
			Status int `json:"status"`
		} `json:"results"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	for i, res := range out.Results {
		if res.Status != 200 {
			t.Fatalf("item %d must fail over to the healthy node, got %d", i, res.Status)
		}
	}
}

// Batch size limits are enforced.
func TestBatchLimits(t *testing.T) {
	g := newTestGateway(t, "http://127.0.0.1:1")
	for _, body := range []string{`{"requests":[]}`, `{"requests":[` + strings.Repeat(`{},`, 64) + `{}]}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad batch size must 400, got %d", rec.Code)
		}
	}
}
