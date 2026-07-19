package usage

import (
	"path/filepath"
	"testing"
)

func TestAddAggregatesPerKeyBackend(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "usage.json"))
	s.Add("keykeykey123", "node-a", 1, 10, 20)
	s.Add("keykeykey123", "node-a", 1, 5, 5)
	s.Add("keykeykey123", "node-b", 1, 1, 1)
	recs := s.Snapshot()
	if len(recs) != 2 {
		t.Fatalf("want 2 aggregated records, got %d", len(recs))
	}
	var a *Record
	for i := range recs {
		if recs[i].Backend == "node-a" {
			a = &recs[i]
		}
	}
	if a == nil || a.Requests != 2 || a.PromptTokens != 15 || a.CompletionTokens != 25 {
		t.Fatalf("node-a aggregate wrong: %+v", a)
	}
	if a.APIKey != Redact("keykeykey123") {
		t.Fatalf("stored key must be redacted, got %q", a.APIKey)
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("abcdefghij"); got != "abcdefgh…" {
		t.Fatalf("Redact = %q", got)
	}
	if got := Redact("short"); got != "short" {
		t.Fatalf("short keys pass through, got %q", got)
	}
}

func TestNilStoreIsSafe(t *testing.T) {
	var s *Store
	s.Add("k", "b", 1, 1, 1) // must not panic
	if s.Snapshot() != nil {
		t.Fatal("nil store snapshot must be empty")
	}
}
