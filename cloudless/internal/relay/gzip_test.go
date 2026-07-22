package relay

import (
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// O7: relay's JSON listings compress on request; binary blob serving never
// does, regardless of what the client asks for.

func TestRelayListCompressesWhenRequested(t *testing.T) {
	models := &BlobSet{List: func() []storeEntry {
		return []storeEntry{{Name: "m.gguf", SHA256: "abc", Size: 3, Format: "gguf"}}
	}}
	srv := blobRelay(t, models, nil)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/store", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", resp.Header.Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	defer gr.Close()
	plain, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), "m.gguf") {
		t.Fatalf("decompressed body missing expected content: %q", plain)
	}
}

func TestRelayBlobNeverCompressed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(p, []byte("raw model bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	models := &BlobSet{Path: func(name string) (string, bool) { return p, true }}
	srv := blobRelay(t, models, nil)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/blob?name=x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") == "gzip" {
		t.Fatal("blob serving must never be gzip-encoded")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "raw model bytes" {
		t.Fatalf("body = %q, want raw content", body)
	}
}
