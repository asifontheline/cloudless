package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// L1 backfill: Open's reload-from-disk path and flushLoop's actual write to
// disk had zero coverage — only the in-memory Add/Snapshot path was tested.

// Usage survives a restart: data written by one Store is loaded by the next
// Open at the same path, once the background flush has run.
func TestPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	s1 := Open(path)
	s1.Add("secret-key-1", "node-a", 3, 100, 200)

	waitForFile(t, path, 6*time.Second)

	s2 := Open(path)
	snap := s2.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("reopened store has %d records, want 1", len(snap))
	}
	rec := snap[0]
	if rec.Backend != "node-a" || rec.Requests != 3 || rec.PromptTokens != 100 || rec.CompletionTokens != 200 {
		t.Fatalf("reloaded record wrong: %+v", rec)
	}
	if rec.APIKey != Redact("secret-key-1") {
		t.Fatalf("reloaded record must keep the redacted key, got %q", rec.APIKey)
	}
}

// A missing or corrupt usage file is not fatal — Open starts empty instead
// of failing the node boot.
func TestOpenMissingFileStartsEmpty(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if got := s.Snapshot(); len(got) != 0 {
		t.Fatalf("fresh store should start empty, got %d records", len(got))
	}
}

func TestOpenCorruptFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(path)
	if got := s.Snapshot(); len(got) != 0 {
		t.Fatalf("corrupt file should start empty, got %d records", len(got))
	}
	// Still usable after a corrupt reload.
	s.Add("k", "b", 1, 1, 1)
	if got := s.Snapshot(); len(got) != 1 {
		t.Fatalf("store must remain usable after a corrupt reload, got %d records", len(got))
	}
}

// A clean store (no Add since the last flush) does not rewrite the file —
// flushLoop's dirty-flag short circuit.
func TestFlushLoopSkipsWhenClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	s := Open(path)
	s.Add("k", "b", 1, 1, 1)
	waitForFile(t, path, 6*time.Second)

	fi1, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5500 * time.Millisecond) // one more flush tick with nothing dirty
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fi2.ModTime().Equal(fi1.ModTime()) {
		t.Fatalf("file was rewritten with nothing dirty: mtime %v -> %v", fi1.ModTime(), fi2.ModTime())
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s was never written within %s", path, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
