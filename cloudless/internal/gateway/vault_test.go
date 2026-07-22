package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudless/internal/vault"
)

func vaultRequest(t *testing.T, g *Gateway, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

// M3 API: seal, list, read back, delete — admin key required for content.
func TestVaultEndpoints(t *testing.T) {
	g := newTestGateway(t)
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g.Vault = v

	if rec := vaultRequest(t, g, http.MethodPut, "/vault/notes.txt", "secret", "test-key"); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}
	// Listing is public and shows no plaintext.
	rec := vaultRequest(t, g, http.MethodGet, "/vault", "", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("list leaked content or failed: %d %s", rec.Code, rec.Body.String())
	}
	// Reading needs the admin key.
	if rec := vaultRequest(t, g, http.MethodGet, "/vault/notes.txt", "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated read got %d, want 403", rec.Code)
	}
	rec = vaultRequest(t, g, http.MethodGet, "/vault/notes.txt", "", "test-key")
	if rec.Code != http.StatusOK || rec.Body.String() != "secret" {
		t.Fatalf("owner read: %d %q", rec.Code, rec.Body.String())
	}
	if rec := vaultRequest(t, g, http.MethodDelete, "/vault/notes.txt", "", "test-key"); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := vaultRequest(t, g, http.MethodGet, "/vault/notes.txt", "", "test-key"); rec.Code != http.StatusNotFound {
		t.Fatalf("read after delete got %d, want 404", rec.Code)
	}
}

// A node without a vault reports it cleanly instead of 500ing.
func TestVaultDisabled(t *testing.T) {
	g := newTestGateway(t)
	if rec := vaultRequest(t, g, http.MethodGet, "/vault", "", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("disabled vault list: %d %s", rec.Code, rec.Body.String())
	}
	if rec := vaultRequest(t, g, http.MethodPut, "/vault/x", "data", "test-key"); rec.Code != http.StatusNotFound {
		t.Fatalf("put without vault got %d, want 404", rec.Code)
	}
	if rec := vaultRequest(t, g, http.MethodPost, "/vault/compact", "", "test-key"); rec.Code != http.StatusNotFound {
		t.Fatalf("compact without vault got %d, want 404", rec.Code)
	}
}

// M7: POST /vault/compact tiers storage by temperature, admin-gated, with a
// caller-tunable hot window.
func TestVaultCompactEndpoint(t *testing.T) {
	g := newTestGateway(t)
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g.Vault = v
	g.Audit = newTestAudit(t)

	content := strings.Repeat("compressible content for tiering ", 500)
	if rec := vaultRequest(t, g, http.MethodPut, "/vault/doc.txt", content, "test-key"); rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}

	if rec := vaultRequest(t, g, http.MethodPost, "/vault/compact", "", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("compact without admin key: %d, want 403", rec.Code)
	}

	rec := vaultRequest(t, g, http.MethodPost, "/vault/compact?hot_window_hours=0", "", "test-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("compact: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Compressed   int     `json:"compressed"`
		Decompressed int     `json:"decompressed"`
		HotWindow    float64 `json:"hot_window_hours"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Compressed != 1 || resp.Decompressed != 0 || resp.HotWindow != 0 {
		t.Fatalf("unexpected compact response: %+v", resp)
	}

	// The object is still readable, transparently decompressed.
	rec = vaultRequest(t, g, http.MethodGet, "/vault/doc.txt", "", "test-key")
	if rec.Code != http.StatusOK || rec.Body.String() != content {
		t.Fatalf("read after compaction: %d, len=%d want=%d", rec.Code, rec.Body.Len(), len(content))
	}

	if rec := vaultRequest(t, g, http.MethodPost, "/vault/compact?hot_window_hours=not-a-number", "", "test-key"); rec.Code != http.StatusBadRequest {
		t.Fatalf("compact with a bad hot_window_hours: %d, want 400", rec.Code)
	}
}
