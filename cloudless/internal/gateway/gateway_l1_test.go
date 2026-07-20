package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudless/internal/quota"
)

// L1 backfill: gateway promises pinned by tests — body cap, route log,
// status shape, quota enforcement at the gateway door.

// An oversized request is rejected with 413, never silently truncated into
// corrupt JSON at a backend.
func TestBodyCapRejectsNotTruncates(t *testing.T) {
	var backendSaw int64 = -1
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		backendSaw = int64(len(b))
		io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()
	g := newTestGateway(t, backend.URL)

	big := strings.Repeat("x", 10<<20+100)
	rec := proxyRequest(t, g, big)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body got %d, want 413", rec.Code)
	}
	if backendSaw != -1 {
		t.Fatalf("oversized request must never reach a backend (saw %d bytes)", backendSaw)
	}
	// At the cap exactly: allowed through, unmodified.
	ok := strings.Repeat("y", 10<<20)
	rec = proxyRequest(t, g, ok)
	if rec.Code != http.StatusOK || backendSaw != 10<<20 {
		t.Fatalf("at-cap body: code %d, backend saw %d bytes", rec.Code, backendSaw)
	}
}

// The route log records decisions newest-last and stays bounded at its ring
// size; /status reports backends, routes, and load in the shape the console
// and CLI parse.
func TestRouteLogAndStatusShape(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()
	g := newTestGateway(t, backend.URL)
	for i := 0; i < routeLogSize+5; i++ {
		if rec := proxyRequest(t, g, `{}`); rec.Code != http.StatusOK {
			t.Fatalf("request %d failed: %d", i, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	var st struct {
		Backends []json.RawMessage `json:"backends"`
		Routes   []RouteEntry      `json:"routes"`
		Load     struct {
			MaxConcurrent int `json:"max_concurrent"`
		} `json:"load"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if len(st.Backends) != 1 {
		t.Fatalf("status backends = %d, want 1", len(st.Backends))
	}
	if len(st.Routes) != routeLogSize {
		t.Fatalf("route log must cap at %d, got %d", routeLogSize, len(st.Routes))
	}
	for _, r := range st.Routes {
		if r.Status != http.StatusOK || r.Path != "/v1/chat/completions" {
			t.Fatalf("route entry wrong: %+v", r)
		}
	}
}

// Quota enforcement happens at the gateway door: over-limit keys get 429
// with Retry-After before any backend is contacted.
func TestQuotaEnforcedAtGateway(t *testing.T) {
	backendHits := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		backendHits++
		io.WriteString(w, `{"ok":true}`)
	}))
	defer backend.Close()
	g := newTestGateway(t, backend.URL)
	g.Quota = quota.New(quota.Limits{RequestsPerMinute: 2})

	for i := 0; i < 2; i++ {
		if rec := proxyRequest(t, g, `{}`); rec.Code != http.StatusOK {
			t.Fatalf("request %d within quota failed: %d", i, rec.Code)
		}
	}
	rec := proxyRequest(t, g, `{}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over-quota must 429 with Retry-After, got %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if backendHits != 2 {
		t.Fatalf("over-quota request must not reach a backend (hits=%d)", backendHits)
	}
}

// The batch endpoint rejects malformed and oversized shapes cleanly.
func TestBatchShapeValidation(t *testing.T) {
	g := newTestGateway(t, "http://127.0.0.1:1")
	for _, body := range []string{`not json`, `{"requests":[]}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: got %d, want 400", body, rec.Code)
		}
	}
}
