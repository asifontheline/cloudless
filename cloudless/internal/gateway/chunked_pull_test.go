package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloudless/internal/config"
	"cloudless/internal/registry"
	"cloudless/internal/store"
)

// blobServer serves one file at /blob?name=<name> via http.ServeFile, so
// Range requests are honored exactly like the real relay (which also uses
// http.ServeFile) — no need to fake range handling in the test itself.
func blobServer(t *testing.T, name string, content []byte) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != name {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	}))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A single source, or a small artifact, takes the plain whole-file path.
func TestFetchChunkedSingleSource(t *testing.T) {
	content := []byte("small artifact content")
	srv := blobServer(t, "m.gguf", content)
	defer srv.Close()

	path, err := fetchChunked(context.Background(), srv.Client(),
		[]chunkSource{{Base: srv.URL, Name: "m.gguf"}}, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if got := readFile(t, path); !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q want %q", got, content)
	}
}

// The actual point of O4: a large artifact with multiple sources is split
// into range requests spread across all of them and reassembled correctly.
func TestFetchChunkedMultiSourceReassemblesCorrectly(t *testing.T) {
	content := make([]byte, 3*chunkSize+12345) // spans several chunk boundaries
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	srvA := blobServer(t, "big.gguf", content)
	defer srvA.Close()
	srvB := blobServer(t, "big.gguf", content)
	defer srvB.Close()

	sources := []chunkSource{{Base: srvA.URL, Name: "big.gguf"}, {Base: srvB.URL, Name: "big.gguf"}}
	path, err := fetchChunked(context.Background(), srvA.Client(), sources, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	got := readFile(t, path)
	if !bytes.Equal(got, content) {
		t.Fatalf("reassembled content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
}

// If any one source fails a chunk, the whole pull fails rather than
// silently storing a corrupt/incomplete artifact.
func TestFetchChunkedOneBadSourceFailsWhole(t *testing.T) {
	content := make([]byte, 3*chunkSize)
	good := blobServer(t, "big.gguf", content)
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer bad.Close()

	sources := []chunkSource{{Base: good.URL, Name: "big.gguf"}, {Base: bad.URL, Name: "big.gguf"}}
	_, err := fetchChunked(context.Background(), good.Client(), sources, int64(len(content)))
	if err == nil {
		t.Fatal("expected an error when one source fails a chunk, got nil")
	}
}

// No sources at all is a clean error, not a panic or an empty file.
func TestFetchChunkedNoSources(t *testing.T) {
	if _, err := fetchChunked(context.Background(), http.DefaultClient, nil, 100); err == nil {
		t.Fatal("expected an error with no sources")
	}
}

// relayPeer fakes a mutual-TLS relay node: it serves /store (the artifact
// listing) and /blob (Range-aware, like the real relay) so the full
// production path — handleStorePull's peer discovery through
// fetchChunked's reassembly — can be exercised end to end, not just the
// chunking helper in isolation.
func relayPeer(t *testing.T, name string, content []byte) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "blob")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /store", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"artifacts": []store.Entry{{Name: name, Size: int64(len(content))}},
		})
	})
	mux.HandleFunc("GET /blob", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != name {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	})
	return httptest.NewTLSServer(mux)
}

// The full production path — GET /store/pull discovering multiple https
// peers and splitting a large artifact across them — reassembles correctly
// and the result passes Store.Add's own hash verification (O4).
func TestHandleStorePullChunksAcrossRealPeers(t *testing.T) {
	content := make([]byte, 3*chunkSize+777)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	copy(content, []byte("GGUF")) // satisfies the store's safe-format check
	peerA := relayPeer(t, "big.gguf", content)
	defer peerA.Close()
	peerB := relayPeer(t, "big.gguf", content)
	defer peerB.Close()

	backends := []config.Backend{
		{Name: "peer-a", BaseURL: peerA.URL + "/v1"},
		{Name: "peer-b", BaseURL: peerB.URL + "/v1"},
	}
	g := New(registry.New(backends, time.Hour, nil), "test-key", nil)
	g.Models = mustStore(t)
	// Both fake peers use self-signed certs from their own httptest.Server;
	// trust whichever one's transport (they're independent, so borrow A's
	// and make it accept B's cert too by trusting both via a shared pool).
	g.client = peerA.Client()
	g.client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true

	req := httptest.NewRequest(http.MethodPost, "/store/pull?name=big.gguf", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Pulled    bool   `json:"pulled"`
		FromPeers int    `json:"from_peers"`
		SHA256    string `json:"sha256"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Pulled || out.FromPeers != 2 {
		t.Fatalf("expected a pull across 2 peers, got %+v", out)
	}
	p, ok := g.Models.Path("big.gguf")
	if !ok {
		t.Fatal("artifact not present in store after pull")
	}
	got := readFile(t, p)
	if !bytes.Equal(got, content) {
		t.Fatalf("stored content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
	if ok, err := g.Models.Verify("big.gguf"); err != nil || !ok {
		t.Fatalf("Store.Verify failed after chunked pull: ok=%v err=%v", ok, err)
	}
}
