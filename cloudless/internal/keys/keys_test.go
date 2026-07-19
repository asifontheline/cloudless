package keys

import (
	"path/filepath"
	"testing"
)

func TestCreateActivateRevoke(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "keys.json"))
	key, err := s.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Active(key) {
		t.Fatal("freshly created key must authenticate")
	}
	if s.Active("not-a-key") {
		t.Fatal("unknown key must not authenticate")
	}
	if !s.Revoke(key[:8]) {
		t.Fatal("revoke by redacted prefix must hit")
	}
	if s.Active(key) {
		t.Fatal("revoked key must not authenticate")
	}
	if s.Revoke("zzzz") {
		t.Fatal("unknown prefix must not report a hit")
	}
}

func TestEmptyPrefixRevokesNothing(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "keys.json"))
	key, _ := s.Create("bob")
	if s.Revoke("") || s.Revoke("…") {
		t.Fatal("empty prefix must never revoke")
	}
	if !s.Active(key) {
		t.Fatal("key must survive empty-prefix revoke")
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	key, _ := Open(path).Create("carol")
	s2 := Open(path)
	if !s2.Active(key) {
		t.Fatal("key must survive reopen")
	}
	s2.Revoke(key[:8])
	if Open(path).Active(key) {
		t.Fatal("revocation must survive reopen")
	}
}

func TestNilStore(t *testing.T) {
	var s *Store
	if s.Active("anything") {
		t.Fatal("nil store must reject all keys")
	}
}
