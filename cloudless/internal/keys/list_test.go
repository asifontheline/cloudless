package keys

import (
	"path/filepath"
	"strings"
	"testing"
)

// L1 backfill: List (the console/API surface for key management) had zero
// coverage — the exact place a redaction bug would leak a full secret.

func TestListRedactsAndReflectsState(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "keys.json"))
	k1, err := s.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := s.Create("bob")
	if err != nil {
		t.Fatal(err)
	}
	s.Revoke(k2[:8])

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(list))
	}
	byName := map[string]Public{}
	for _, p := range list {
		byName[p.Name] = p
	}

	alice, bob := byName["alice"], byName["bob"]
	if alice.Key == k1 || bob.Key == k2 {
		t.Fatalf("List must never return the full secret: alice=%q bob=%q", alice.Key, bob.Key)
	}
	if alice.Key != redact(k1) || bob.Key != redact(k2) {
		t.Fatalf("List must return the redacted form: alice=%q bob=%q", alice.Key, bob.Key)
	}
	if alice.Revoked {
		t.Fatal("alice's key was never revoked")
	}
	if !bob.Revoked {
		t.Fatal("bob's key must show as revoked")
	}
}

func TestListEmptyStore(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "keys.json"))
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List on an empty store returned %d entries, want 0", len(got))
	}
}

func TestListNilStore(t *testing.T) {
	var s *Store
	if got := s.List(); got != nil {
		t.Fatalf("List on a nil store must return nil, got %v", got)
	}
}

func TestRedactShortKeyUnchanged(t *testing.T) {
	if got := redact("short"); got != "short" {
		t.Fatalf("redact of a short key should be returned unchanged, got %q", got)
	}
}

func TestRedactLongKeyTruncated(t *testing.T) {
	got := redact("0123456789abcdef")
	if !strings.HasPrefix(got, "01234567") || !strings.HasSuffix(got, "…") {
		t.Fatalf("redact of a long key = %q, want an 8-char prefix + ellipsis", got)
	}
}
