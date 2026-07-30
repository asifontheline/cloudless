package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudless/internal/abuse"
)

// S2: repeated wrong admin-key attempts from one source lock that source
// out — rate-limited harder than a single 403, not merely counted.
func TestAdminOnlyLocksOutAfterRepeatedFailures(t *testing.T) {
	g := newTestGateway(t)
	g.Abuse = abuse.New(3, time.Minute, time.Hour)

	wrongKey := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/keys", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		req.RemoteAddr = "203.0.113.9:5555"
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < 3; i++ {
		rec := wrongKey()
		if rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d: status %d, want 403 (not yet locked out)", i, rec.Code)
		}
	}

	// The threshold is now crossed — even the *correct* key must be
	// rejected while locked out, because the lockout is about the
	// source, not any one credential.
	req := httptest.NewRequest(http.MethodGet, "/keys", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.RemoteAddr = "203.0.113.9:5555"
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked-out source with correct key: status %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("locked-out response should carry Retry-After")
	}
}

// A different source is never affected by another source's lockout.
func TestAdminOnlyLockoutIsPerSource(t *testing.T) {
	g := newTestGateway(t)
	g.Abuse = abuse.New(2, time.Minute, time.Hour)

	attacker := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/keys", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		req.RemoteAddr = "203.0.113.9:5555"
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		return rec
	}
	attacker()
	attacker() // crosses threshold=2, locks out 203.0.113.9

	req := httptest.NewRequest(http.MethodGet, "/keys", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	req.RemoteAddr = "198.51.100.4:1111" // a different source entirely
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unrelated source: status %d, want 200", rec.Code)
	}
}

// The per-user-key auth() path (not just adminOnly) gets the same
// protection — /v1/* is exactly what a credential-stuffing scan targets.
func TestAuthLocksOutAfterRepeatedFailures(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()
	g := newTestGateway(t, backend.URL)
	g.Abuse = abuse.New(3, time.Minute, time.Hour)

	attempt := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+key)
		req.RemoteAddr = "203.0.113.9:5555"
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 3; i++ {
		if rec := attempt("wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401", i, rec.Code)
		}
	}
	if rec := attempt("test-key"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked-out source with valid key: status %d, want 429", rec.Code)
	}
}

// A source that eventually succeeds isn't left one mistake away from
// lockout — a genuine typo doesn't accumulate toward a false positive.
func TestAuthSuccessResetsFailureCount(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()
	g := newTestGateway(t, backend.URL)
	g.Abuse = abuse.New(3, time.Minute, time.Hour)

	attempt := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+key)
		req.RemoteAddr = "203.0.113.9:5555"
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		return rec
	}
	attempt("wrong")
	attempt("wrong")
	if rec := attempt("test-key"); rec.Code != http.StatusOK {
		t.Fatalf("valid key before lockout: status %d, want 200", rec.Code)
	}
	// Two more failures — if the earlier success hadn't reset the count,
	// this would already be at 4 total failures and locked out.
	attempt("wrong")
	if rec := attempt("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401 (not yet locked out — success reset the count)", rec.Code)
	}
}

// No Abuse guard configured (nil) must never lock anyone out — this is
// opt-in behavior, not a hidden requirement.
func TestNoAbuseGuardConfiguredNeverLocksOut(t *testing.T) {
	g := newTestGateway(t)
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/keys", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		req.RemoteAddr = "203.0.113.9:5555"
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("attempt %d: status %d, want 403 (no lockout without a configured guard)", i, rec.Code)
		}
	}
}
