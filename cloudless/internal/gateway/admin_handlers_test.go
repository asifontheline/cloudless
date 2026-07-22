package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloudless/internal/keys"
	"cloudless/internal/store"
	"cloudless/internal/usage"
)

// L1 backfill: the admin-gated model store, per-user key management, and
// cooperative-accounting endpoints (savings/capacity/ledger) had zero
// handler coverage — handleKeysCreate/handleKeysRevoke in particular guard
// C1's per-user API keys, a security-relevant admin surface.

const validGGUF = "GGUF\x03\x00\x00\x00payload"

func doAdmin(t *testing.T, g *Gateway, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

func doAnon(t *testing.T, g *Gateway, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

func mustStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// PUT/GET/DELETE /store round trip, admin-gated, safe-format enforced.
func TestStoreEndpointsRoundTrip(t *testing.T) {
	g := newTestGateway(t)
	g.Models = mustStore(t)

	if rec := doAnon(t, g, http.MethodPut, "/store?name=m.gguf", validGGUF); rec.Code != http.StatusForbidden {
		t.Fatalf("PUT /store without admin key: status %d, want 403", rec.Code)
	}
	if rec := doAdmin(t, g, http.MethodPut, "/store", validGGUF); rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /store without name: status %d, want 400", rec.Code)
	}
	if rec := doAdmin(t, g, http.MethodPut, "/store?name=bad.pkl", "\x80\x04payload"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PUT /store unsafe format: status %d, want 422", rec.Code)
	}

	rec := doAdmin(t, g, http.MethodPut, "/store?name=m.gguf", validGGUF)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /store valid gguf: status %d, body %s", rec.Code, rec.Body)
	}

	rec = doAnon(t, g, http.MethodGet, "/store", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "m.gguf") {
		t.Fatalf("GET /store missing added artifact: %d %s", rec.Code, rec.Body)
	}

	rec = doAnon(t, g, http.MethodGet, "/store/verify?name=m.gguf", "")
	var vr struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &vr); err != nil || !vr.OK {
		t.Fatalf("GET /store/verify: %d %s", rec.Code, rec.Body)
	}

	if rec := doAnon(t, g, http.MethodDelete, "/store/m.gguf", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE /store without admin key: status %d, want 403", rec.Code)
	}
	if rec := doAdmin(t, g, http.MethodDelete, "/store/m.gguf", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /store: status %d, want 204", rec.Code)
	}
	if rec := doAdmin(t, g, http.MethodDelete, "/store/m.gguf", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /store on already-deleted: status %d, want 404", rec.Code)
	}
}

// POST /store/pull: already-present short-circuits without touching the
// mesh; no peer holding the artifact yields a clean 404 (fallback signal).
func TestStorePullShortCircuitsAndMisses(t *testing.T) {
	g := newTestGateway(t) // no backends
	g.Models = mustStore(t)
	if _, err := g.Models.Add("m.gguf", strings.NewReader(validGGUF)); err != nil {
		t.Fatal(err)
	}

	rec := doAdmin(t, g, http.MethodPost, "/store/pull?name=m.gguf", "")
	var pr struct {
		Pulled bool   `json:"pulled"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil || pr.Pulled || pr.Reason == "" {
		t.Fatalf("pull of an already-present artifact must short-circuit: %d %s", rec.Code, rec.Body)
	}

	rec = doAdmin(t, g, http.MethodPost, "/store/pull?name=missing.gguf", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("pull with no candidate peer: status %d, want 404", rec.Code)
	}
}

// Key management is admin-only; the full secret is returned exactly once,
// list output is redacted, and revoke removes it from routing eligibility.
func TestKeysEndpointsRoundTrip(t *testing.T) {
	g := newTestGateway(t)
	g.Keys = keys.Open(filepath.Join(t.TempDir(), "keys.json"))

	if rec := doAnon(t, g, http.MethodPost, "/keys", `{"name":"alice"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /keys without admin key: status %d, want 403", rec.Code)
	}
	if rec := doAdmin(t, g, http.MethodPost, "/keys", `{"name":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /keys empty name: status %d, want 400", rec.Code)
	}

	rec := doAdmin(t, g, http.MethodPost, "/keys", `{"name":"alice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /keys: status %d, body %s", rec.Code, rec.Body)
	}
	var created struct{ Name, Key string }
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Key == "" {
		t.Fatalf("created key response missing secret: %s", rec.Body)
	}

	rec = doAdmin(t, g, http.MethodGet, "/keys", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), created.Key) {
		t.Fatalf("GET /keys must not leak the full secret: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "alice") {
		t.Fatalf("GET /keys missing created key's name: %s", rec.Body)
	}

	prefix := created.Key[:8]
	if rec := doAnon(t, g, http.MethodDelete, "/keys/"+prefix, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE /keys without admin key: status %d, want 403", rec.Code)
	}
	if rec := doAdmin(t, g, http.MethodDelete, "/keys/"+prefix, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /keys/%s: status %d, want 204", prefix, rec.Code)
	}
	if rec := doAdmin(t, g, http.MethodDelete, "/keys/doesnotexist", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /keys on unknown prefix: status %d, want 404", rec.Code)
	}
}

// /savings computes the hosted-API comparison from usage, honoring
// caller-supplied reference rates.
func TestHandleSavings(t *testing.T) {
	g := newTestGateway(t)
	g.Usage = usage.Open(filepath.Join(t.TempDir(), "usage.json"))
	g.Usage.Add("key1", "a", 1, 1_000_000, 2_000_000)

	rec := doAnon(t, g, http.MethodGet, "/savings?prompt_per_1m=1&completion_per_1m=2", "")
	var sr struct {
		HostedEquivalentUSD float64 `json:"hosted_equivalent_usd"`
		PromptTokens        int64   `json:"prompt_tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	if sr.PromptTokens != 1_000_000 {
		t.Fatalf("prompt tokens = %d, want 1_000_000", sr.PromptTokens)
	}
	want := 1.0*1 + 2.0*2 // 1M prompt @ $1/1M + 2M completion @ $2/1M
	if sr.HostedEquivalentUSD != want {
		t.Fatalf("hosted_equivalent_usd = %v, want %v", sr.HostedEquivalentUSD, want)
	}
}

// /capacity flags healthy nodes that have never served as idle, and
// recently-served healthy nodes as not idle.
func TestHandleCapacityIdleDetection(t *testing.T) {
	active := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer active.Close()
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer idle.Close()

	g := newTestGateway(t, active.URL, idle.URL) // names "a" (active), "b" (idle)
	g.Usage = usage.Open(filepath.Join(t.TempDir(), "usage.json"))
	g.Usage.Add("key1", "a", 5, 100, 100)

	// Probe once synchronously so both backends are marked healthy, the way
	// the registry's background loop would after node startup.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { g.reg.Run(ctx); close(done) }()
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	rec := doAnon(t, g, http.MethodGet, "/capacity", "")
	var cr struct {
		IdleNodes int `json:"idle_nodes"`
		Nodes     []struct {
			Node string `json:"node"`
			Idle bool   `json:"idle"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cr); err != nil {
		t.Fatal(err)
	}
	idleByName := map[string]bool{}
	for _, n := range cr.Nodes {
		idleByName[n.Node] = n.Idle
	}
	if idleByName["a"] {
		t.Fatal("recently-served node must not be reported idle")
	}
	if !idleByName["b"] {
		t.Fatal("healthy node that never served must be reported idle")
	}
	if cr.IdleNodes != 1 {
		t.Fatalf("idle_nodes = %d, want 1", cr.IdleNodes)
	}
}

// /ledger aggregates usage into per-node (contribution) and per-key
// (consumption) shares that sum to 100%.
func TestHandleLedgerShares(t *testing.T) {
	g := newTestGateway(t)
	g.Usage = usage.Open(filepath.Join(t.TempDir(), "usage.json"))
	g.Usage.Add("key1", "a", 1, 300, 0)
	g.Usage.Add("key2", "a", 1, 100, 0)

	rec := doAnon(t, g, http.MethodGet, "/ledger", "")
	var lr struct {
		Contributed []LedgerLine `json:"contributed"`
		Consumed    []LedgerLine `json:"consumed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Contributed) != 1 || lr.Contributed[0].Party != "a" || lr.Contributed[0].Share != 100 {
		t.Fatalf("expected node a at 100%% share, got %+v", lr.Contributed)
	}
	if len(lr.Consumed) != 2 {
		t.Fatalf("expected 2 consumers, got %d", len(lr.Consumed))
	}
	var total float64
	for _, c := range lr.Consumed {
		total += c.Share
	}
	if total < 99.99 || total > 100.01 {
		t.Fatalf("consumer shares must sum to ~100%%, got %v", total)
	}
}
