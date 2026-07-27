package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cloudless/internal/ext"
)

func extRequest(t *testing.T, g *Gateway, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

// K4: register a service in any language, reach it through /x/<name>/...,
// with mesh credentials stripped before the extension sees the request.
func TestExtensionRegisterProxyRemove(t *testing.T) {
	var sawAuth, sawPath, sawQuery string
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		sawQuery = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"echo":`+string(b)+`}`)
	}))
	defer svc.Close()

	g := newTestGateway(t)
	g.Ext = ext.Open(filepath.Join(t.TempDir(), "extensions.json"))

	// Registration is admin-only.
	if rec := extRequest(t, g, http.MethodPost, "/extensions",
		`{"name":"summarize","base_url":"`+svc.URL+`"}`, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated register got %d, want 403", rec.Code)
	}
	rec := extRequest(t, g, http.MethodPost, "/extensions",
		`{"name":"summarize","base_url":"`+svc.URL+`","runtime":"python"}`, "test-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}

	// Proxy through with auth enforced at the gateway...
	if rec := extRequest(t, g, http.MethodPost, "/x/summarize/run?fast=1", `{"text":"hi"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated proxy got %d, want 401", rec.Code)
	}
	rec = extRequest(t, g, http.MethodPost, "/x/summarize/run?fast=1", `{"text":"hi"}`, "test-key")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"echo":{"text":"hi"}`) {
		t.Fatalf("proxy: %d %s", rec.Code, rec.Body.String())
	}
	// ...but credentials never reach the extension, and path/query survive.
	if sawAuth != "" {
		t.Fatalf("extension saw mesh credentials: %q", sawAuth)
	}
	if sawPath != "/run" || sawQuery != "fast=1" {
		t.Fatalf("path/query mangled: %q %q", sawPath, sawQuery)
	}

	// Listing is public; removal is admin and takes the route away.
	var list struct {
		Extensions []ext.Extension `json:"extensions"`
	}
	json.NewDecoder(extRequest(t, g, http.MethodGet, "/extensions", "", "").Body).Decode(&list)
	if len(list.Extensions) != 1 || list.Extensions[0].Name != "summarize" {
		t.Fatalf("list: %+v", list.Extensions)
	}
	if rec := extRequest(t, g, http.MethodDelete, "/extensions/summarize", "", "test-key"); rec.Code != http.StatusNoContent {
		t.Fatalf("remove: %d", rec.Code)
	}
	if rec := extRequest(t, g, http.MethodPost, "/x/summarize/run", `{}`, "test-key"); rec.Code != http.StatusNotFound {
		t.Fatalf("removed extension still routed: %d", rec.Code)
	}
}

// Bad registrations are rejected with reasons: bad names, bad URLs, dupes.
func TestExtensionRegistrationValidation(t *testing.T) {
	g := newTestGateway(t)
	g.Ext = ext.Open(filepath.Join(t.TempDir(), "extensions.json"))
	for _, body := range []string{
		`{"name":"Bad Name","base_url":"http://127.0.0.1:1"}`,
		`{"name":"ok","base_url":"ftp://x"}`,
		`{"name":"ok","base_url":""}`,
	} {
		if rec := extRequest(t, g, http.MethodPost, "/extensions", body, "test-key"); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s: got %d, want 422", body, rec.Code)
		}
	}
	if rec := extRequest(t, g, http.MethodPost, "/extensions",
		`{"name":"dup","base_url":"http://127.0.0.1:1"}`, "test-key"); rec.Code != http.StatusOK {
		t.Fatalf("first dup register: %d", rec.Code)
	}
	if rec := extRequest(t, g, http.MethodPost, "/extensions",
		`{"name":"dup","base_url":"http://127.0.0.1:2"}`, "test-key"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("duplicate name must 422, got %d", rec.Code)
	}
}

// K5: an extension registered with inference:true joins the routing pool
// and actually serves /v1/chat/completions traffic — not just /x/... — and
// leaves the pool again on removal.
func TestExtensionInferenceJoinsAndLeavesRouting(t *testing.T) {
	var hits int
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	}))
	defer svc.Close()

	g := newTestGateway(t) // no static backends — the extension must be the only route
	g.Ext = ext.Open(filepath.Join(t.TempDir(), "extensions.json"))

	body := `{"name":"pyinfer","base_url":"` + svc.URL + `","inference":true}`
	if rec := extRequest(t, g, http.MethodPost, "/extensions", body, "test-key"); rec.Code != http.StatusOK {
		t.Fatalf("register: status %d, body %s", rec.Code, rec.Body)
	}

	rec := proxyRequest(t, g, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("inference extension not routed to: status %d, body %s", rec.Code, rec.Body)
	}
	if hits != 1 {
		t.Fatalf("extension backend hit %d times, want 1", hits)
	}

	if rec := extRequest(t, g, http.MethodDelete, "/extensions/pyinfer", "", "test-key"); rec.Code != http.StatusNoContent {
		t.Fatalf("remove: status %d", rec.Code)
	}
	rec = proxyRequest(t, g, `{}`)
	if rec.Code == http.StatusOK {
		t.Fatal("removed inference extension must no longer be routable")
	}
}

// A plain extension (inference:false, the default) never joins routing —
// it stays proxy-only at /x/..., preserving today's behavior exactly.
func TestExtensionWithoutInferenceStaysProxyOnly(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer svc.Close()

	g := newTestGateway(t)
	g.Ext = ext.Open(filepath.Join(t.TempDir(), "extensions.json"))
	body := `{"name":"sidecar","base_url":"` + svc.URL + `"}`
	if rec := extRequest(t, g, http.MethodPost, "/extensions", body, "test-key"); rec.Code != http.StatusOK {
		t.Fatalf("register: status %d, body %s", rec.Code, rec.Body)
	}
	rec := proxyRequest(t, g, `{}`)
	if rec.Code == http.StatusOK {
		t.Fatal("non-inference extension must not be routable at /v1/*")
	}
}

// Registrations survive a node restart.
func TestExtensionPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extensions.json")
	r1 := ext.Open(path)
	if _, err := r1.Register(ext.Extension{Name: "keep", BaseURL: "http://127.0.0.1:9"}); err != nil {
		t.Fatal(err)
	}
	r2 := ext.Open(path)
	if _, ok := r2.Get("keep"); !ok {
		t.Fatal("registration lost across restart")
	}
}
