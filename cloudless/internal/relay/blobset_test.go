package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L1 backfill: the model-store and vault blob endpoints (listOf/blobOf/putOf)
// had almost no coverage despite being the wire format peers use to pull
// artifacts and push replicas (M1).

func blobRelay(t *testing.T, models, vault *BlobSet) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(NewServer("", models, vault, nil).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestListOfReturnsArtifacts(t *testing.T) {
	models := &BlobSet{List: func() []storeEntry {
		return []storeEntry{{Name: "m.gguf", SHA256: "abc", Size: 3, Format: "gguf"}}
	}}
	srv := blobRelay(t, models, nil)

	resp, err := http.Get(srv.URL + "/store")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "m.gguf") {
		t.Fatalf("GET /store missing artifact: %s", body)
	}
}

func TestListOfNilListIsEmpty(t *testing.T) {
	srv := blobRelay(t, &BlobSet{}, nil)
	resp, err := http.Get(srv.URL + "/store")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"artifacts":null`) && !strings.Contains(string(body), `"artifacts":[]`) {
		t.Fatalf("GET /store with no List func should report an empty set: %s", body)
	}
}

func TestBlobOfServesFileAndMisses(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(p, []byte("GGUF-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	models := &BlobSet{Path: func(name string) (string, bool) {
		if name == "m.gguf" {
			return p, true
		}
		return "", false
	}}
	srv := blobRelay(t, models, nil)

	resp, err := http.Get(srv.URL + "/blob?name=m.gguf")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "GGUF-bytes" {
		t.Fatalf("GET /blob: status %d, body %q", resp.StatusCode, body)
	}

	resp, err = http.Get(srv.URL + "/blob?name=missing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /blob unknown name: status %d, want 404", resp.StatusCode)
	}
}

func TestBlobOfNoStoreConfigured(t *testing.T) {
	srv := blobRelay(t, &BlobSet{}, nil)
	resp, err := http.Get(srv.URL + "/blob?name=x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET /blob with no Path func: status %d, want 503", resp.StatusCode)
	}
}

// putOf is how peers push replicas (M1); the receiving side re-verifies
// through the store's own Add rather than trusting the sender.
func TestPutOfAcceptsRejectsAndGates(t *testing.T) {
	var added map[string]string
	models := &BlobSet{Add: func(name string, r io.Reader) error {
		if added == nil {
			added = map[string]string{}
		}
		if name == "reject-me" {
			return io.ErrUnexpectedEOF
		}
		b, _ := io.ReadAll(r)
		added[name] = string(b)
		return nil
	}}
	srv := blobRelay(t, models, nil)

	resp, err := http.NewRequest(http.MethodPut, srv.URL+"/store?name=m.gguf", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := http.DefaultClient.Do(resp)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK || added["m.gguf"] != "payload" {
		t.Fatalf("PUT /store: status %d, stored %q", r.StatusCode, added["m.gguf"])
	}

	req2, _ := http.NewRequest(http.MethodPut, srv.URL+"/store", strings.NewReader("x"))
	r2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT /store without name: status %d, want 400", r2.StatusCode)
	}

	req3, _ := http.NewRequest(http.MethodPut, srv.URL+"/store?name=reject-me", strings.NewReader("x"))
	r3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	r3.Body.Close()
	if r3.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("PUT /store rejected by store: status %d, want 422", r3.StatusCode)
	}

	// No Add wired at all (e.g. a node with no vault) must 503, not panic.
	srv2 := blobRelay(t, &BlobSet{}, nil)
	req4, _ := http.NewRequest(http.MethodPut, srv2.URL+"/store?name=x", strings.NewReader("x"))
	r4, err := http.DefaultClient.Do(req4)
	if err != nil {
		t.Fatal(err)
	}
	r4.Body.Close()
	if r4.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("PUT /store with no store configured: status %d, want 503", r4.StatusCode)
	}
}

// The vault route wiring (/vault, /vault-blob) is a separate BlobSet from
// the model store — verify it's actually wired, not just the model side.
func TestVaultRouteWiredSeparatelyFromStore(t *testing.T) {
	models := &BlobSet{List: func() []storeEntry { return []storeEntry{{Name: "model-only"}} }}
	vault := &BlobSet{List: func() []storeEntry { return []storeEntry{{Name: "vault-only"}} }}
	srv := blobRelay(t, models, vault)

	resp, err := http.Get(srv.URL + "/store")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "model-only") || strings.Contains(string(body), "vault-only") {
		t.Fatalf("GET /store leaked vault contents or missed model contents: %s", body)
	}

	resp, err = http.Get(srv.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "vault-only") || strings.Contains(string(body), "model-only") {
		t.Fatalf("GET /vault leaked model contents or missed vault contents: %s", body)
	}
}
