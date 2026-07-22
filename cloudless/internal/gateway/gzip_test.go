package gateway

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudless/internal/vault"
)

// O7: transfer compression. JSON/text API responses are gzipped when the
// client asks for it; binary and streaming endpoints are never touched.

func TestGzipCompressesWhenRequested(t *testing.T) {
	g := newTestGateway(t)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", rec.Header().Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	defer gr.Close()
	plain, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.Unmarshal(plain, &status); err != nil {
		t.Fatalf("decompressed body is not valid JSON: %v (%q)", err, plain)
	}
}

func TestGzipSkippedWithoutAcceptEncoding(t *testing.T) {
	g := newTestGateway(t)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("must not compress when the client didn't ask for it")
	}
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("plain body is not valid JSON: %v", err)
	}
}

// The vault holds a secret; its content must never be gzipped regardless of
// Accept-Encoding, and must round-trip byte for byte.
func TestGzipNeverAppliesToVaultSecret(t *testing.T) {
	g := newTestGateway(t)
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g.Vault = v
	if _, err := v.Put("notes.txt", strings.NewReader("secret content")); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/vault/notes.txt", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("vault plaintext must never be gzip-encoded")
	}
	if rec.Body.String() != "secret content" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "secret content")
	}
}

// Streaming inference must never be gzip-wrapped — buffering through gzip
// would defeat the whole point of real-time token flushing.
func TestGzipNeverAppliesToStreamingProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: hello\n\ndata: [DONE]\n\n")
	}))
	defer backend.Close()
	g := newTestGateway(t, backend.URL)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("streaming responses must never be gzip-encoded")
	}
	if !strings.Contains(rec.Body.String(), "data: hello") {
		t.Fatalf("stream body missing expected content: %q", rec.Body.String())
	}
}
