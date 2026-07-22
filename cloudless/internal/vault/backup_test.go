package vault

import (
	"os"
	"strings"
	"testing"
)

// L1 backfill: the M5 off-mesh backup escape hatch (KeyCopy, SealedBytes,
// PutBytes, OpenSealed) and integrity checking (Verify, List) had zero
// coverage — the export/import path is exactly where a bug would silently
// corrupt or leak a backup.

// KeyCopy is the one sanctioned way the sealing key leaves the machine; the
// caller must get an independent copy, not a view into the vault's own key.
func TestKeyCopyIsIndependentCopy(t *testing.T) {
	v := mustOpen(t)
	k1 := v.KeyCopy()
	k1[0] ^= 0xff // mutate the copy

	if _, err := v.Put("a.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	got, err := v.Get("a.txt")
	if err != nil || string(got) != "data" {
		t.Fatalf("mutating a KeyCopy must not affect the vault's own key: %v", err)
	}

	k2 := v.KeyCopy()
	if k1[0] == k2[0] {
		t.Fatal("KeyCopy must return a fresh copy each time, not a shared slice")
	}
}

// SealedBytes exports the on-disk ciphertext; OpenSealed with the matching
// key (as archived alongside a backup) recovers the original plaintext.
func TestSealedBytesAndOpenSealedRoundTrip(t *testing.T) {
	v := mustOpen(t)
	secret := "backup archive contents"
	if _, err := v.Put("doc.txt", strings.NewReader(secret)); err != nil {
		t.Fatal(err)
	}
	key := v.KeyCopy()

	sealed, err := v.SealedBytes("doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := OpenSealed(key, "doc.txt", sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != secret {
		t.Fatalf("got %q, want %q", plain, secret)
	}
}

func TestSealedBytesUnknownName(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.SealedBytes("ghost.txt"); err == nil {
		t.Fatal("SealedBytes on an unknown name must error")
	}
}

// OpenSealed is name-bound (AAD) exactly like Get: the wrong key or a
// renamed blob must not decrypt.
func TestOpenSealedWrongKeyFails(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("doc.txt", strings.NewReader("secret")); err != nil {
		t.Fatal(err)
	}
	sealed, err := v.SealedBytes("doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	other := mustOpen(t) // a different, unrelated key
	if _, err := OpenSealed(other.KeyCopy(), "doc.txt", sealed); err == nil {
		t.Fatal("OpenSealed with the wrong key must fail")
	}
}

func TestOpenSealedWrongNameFails(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("doc.txt", strings.NewReader("secret")); err != nil {
		t.Fatal(err)
	}
	sealed, err := v.SealedBytes("doc.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSealed(v.KeyCopy(), "renamed.txt", sealed); err == nil {
		t.Fatal("OpenSealed with a name that doesn't match the sealed AAD must fail")
	}
}

func TestOpenSealedTruncatedInput(t *testing.T) {
	v := mustOpen(t)
	if _, err := OpenSealed(v.KeyCopy(), "x", []byte("short")); err == nil {
		t.Fatal("OpenSealed on input shorter than a nonce must fail, not panic")
	}
}

// PutBytes (the backup-import path) must be equivalent to Put for an
// in-memory plaintext buffer.
func TestPutBytesRoundTrip(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.PutBytes("restored.txt", []byte("recovered data")); err != nil {
		t.Fatal(err)
	}
	got, err := v.Get("restored.txt")
	if err != nil || string(got) != "recovered data" {
		t.Fatalf("PutBytes round trip failed: %v, %q", err, got)
	}
}

// Verify re-hashes the ciphertext on disk; it must catch corruption and
// missing blobs without needing the sealing key.
func TestVerifyDetectsCorruption(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("a.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	ok, err := v.Verify("a.txt")
	if err != nil || !ok {
		t.Fatalf("freshly written object must verify clean: ok=%v err=%v", ok, err)
	}

	p, _ := v.Path("a.txt")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] ^= 0xff
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	ok, err = v.Verify("a.txt")
	if err != nil {
		t.Fatalf("Verify on a corrupted blob should report false, not error: %v", err)
	}
	if ok {
		t.Fatal("Verify must detect on-disk corruption")
	}
}

func TestVerifyMissingBlob(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("a.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	p, _ := v.Path("a.txt")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify("a.txt"); err == nil {
		t.Fatal("Verify on a missing blob must error")
	}
}

func TestVerifyUnknownName(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Verify("ghost.txt"); err == nil {
		t.Fatal("Verify on an unknown name must error")
	}
}

func TestListReturnsAllEntries(t *testing.T) {
	v := mustOpen(t)
	names := []string{"a.txt", "b.txt", "c.txt"}
	for _, n := range names {
		if _, err := v.Put(n, strings.NewReader(n)); err != nil {
			t.Fatal(err)
		}
	}
	got := v.List()
	if len(got) != len(names) {
		t.Fatalf("List returned %d entries, want %d", len(got), len(names))
	}
	seen := map[string]bool{}
	for _, e := range got {
		seen[e.Name] = true
	}
	for _, n := range names {
		if !seen[n] {
			t.Fatalf("List missing entry %q", n)
		}
	}
}

func TestListEmptyVault(t *testing.T) {
	v := mustOpen(t)
	if got := v.List(); len(got) != 0 {
		t.Fatalf("List on an empty vault must return empty, got %d entries", len(got))
	}
}
