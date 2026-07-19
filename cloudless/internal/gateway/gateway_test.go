package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
