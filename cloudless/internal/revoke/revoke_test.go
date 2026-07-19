package revoke

import (
	"path/filepath"
	"testing"
)

func TestAddHasList(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "revoked.json"))
	if s.Has("n1") {
		t.Fatal("fresh set must be empty")
	}
	if !s.Add("n1") {
		t.Fatal("first add must report true")
	}
	if s.Add("n1") {
		t.Fatal("duplicate add must report false")
	}
	if !s.Has("n1") {
		t.Fatal("added name must be present")
	}
	if got := len(s.List()); got != 1 {
		t.Fatalf("list length = %d, want 1", got)
	}
}

func TestRevocationSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revoked.json")
	Open(path).Add("gone-node")
	if !Open(path).Has("gone-node") {
		t.Fatal("revocation must persist across reopen — an evicted node must stay evicted")
	}
}
