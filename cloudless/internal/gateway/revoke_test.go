package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// L7 backfill: a revoked identity is refused by the gateway — the admin
// endpoint requires the admin key, and once a node is revoked it is dropped
// from routing so no further requests reach it.

// POST /revoke/{name} is admin-gated like the other cluster-management
// endpoints.
func TestRevokeRequiresAdminKey(t *testing.T) {
	g := newTestGateway(t, "http://unused.invalid")
	g.Revoke = func(string) bool { return true }

	req := httptest.NewRequest(http.MethodPost, "/revoke/a", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no admin key must be refused: status %d", rec.Code)
	}
}

// Revoking a node evicts it from routing: once /revoke/{name} succeeds, the
// proxy never sends another request to that backend, even under retries.
func TestRevokedNodeDroppedFromRouting(t *testing.T) {
	var aHits, bHits int
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		aHits++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		bHits++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer b.Close()

	g := newTestGateway(t, a.URL, b.URL) // names "a", "b" per newTestGateway
	g.Audit = newTestAudit(t)
	g.Revoke = func(name string) bool {
		g.reg.Remove(name)
		return true
	}

	req := httptest.NewRequest(http.MethodPost, "/revoke/b", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke of a known node must succeed: status %d, body %s", rec.Code, rec.Body)
	}

	for i := 0; i < 10; i++ {
		proxyRequest(t, g, `{}`)
	}
	if bHits != 0 {
		t.Fatalf("revoked backend must never receive traffic, got %d hits", bHits)
	}
	if aHits == 0 {
		t.Fatal("the surviving backend should still serve requests")
	}
}

// Revoking an unknown node is reported, not silently accepted.
func TestRevokeUnknownNodeConflict(t *testing.T) {
	g := newTestGateway(t, "http://unused.invalid")
	g.Audit = newTestAudit(t)
	g.Revoke = func(string) bool { return false }

	req := httptest.NewRequest(http.MethodPost, "/revoke/ghost", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("revoking an unknown/already-revoked node must 409, got %d", rec.Code)
	}
}
