package vault

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustOpen(t *testing.T) *Vault {
	t.Helper()
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Round trip on the owner's node: seal, reopen, read back.
func TestPutGetRoundTrip(t *testing.T) {
	v := mustOpen(t)
	secret := "the group's private notes"
	if _, err := v.Put("notes.txt", strings.NewReader(secret)); err != nil {
		t.Fatal(err)
	}
	got, err := v.Get("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != secret {
		t.Fatalf("got %q, want %q", got, secret)
	}
}

// M3 acceptance: a foreign node hosting a replica cannot read it. Its disk
// holds no plaintext and its own key fails to open the ciphertext.
func TestHostCannotReadHostedData(t *testing.T) {
	owner := mustOpen(t)
	host := mustOpen(t) // different machine — different key
	secret := "medical records of a member"
	if _, err := owner.Put("records.txt", strings.NewReader(secret)); err != nil {
		t.Fatal(err)
	}
	// Replicate the way the mesh does: ship the sealed bytes.
	p, _ := owner.Path("records.txt")
	sealed, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.AddSealed("records.txt", bytes.NewReader(sealed)); err != nil {
		t.Fatal(err)
	}
	// The hosting operator tries everything they legitimately have:
	if _, err := host.Get("records.txt"); err == nil {
		t.Fatal("host node decrypted third-party data with its own key")
	}
	// Nothing on the host's disk contains the plaintext.
	entries, err := os.ReadDir(hostDir(host))
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range entries {
		raw, err := os.ReadFile(filepath.Join(hostDir(host), de.Name()))
		if err != nil {
			continue
		}
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("plaintext found on host disk in %s", de.Name())
		}
	}
}

func hostDir(v *Vault) string { return v.dir }

// The sealing key never travels with the blob: the replica flow only moves
// ciphertext, and the ciphertext hash verifies without any key.
func TestReplicaVerifiableWithoutKey(t *testing.T) {
	owner := mustOpen(t)
	host := mustOpen(t)
	e, err := owner.Put("doc.txt", strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := owner.Path("doc.txt")
	sealed, _ := os.ReadFile(p)
	got, err := host.AddSealed("doc.txt", bytes.NewReader(sealed))
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != e.SHA256 {
		t.Fatalf("ciphertext hash changed in transit: %s vs %s", got.SHA256, e.SHA256)
	}
	if !got.Sealed {
		t.Fatal("replica entry must be marked sealed (key-less)")
	}
}

// Binding the name as AAD: ciphertext moved under another name fails to open.
func TestRenamedCiphertextFailsToOpen(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("a.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	p, _ := v.Path("a.txt")
	sealed, _ := os.ReadFile(p)
	if _, err := v.AddSealed("b.txt", bytes.NewReader(sealed)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("b.txt"); err == nil {
		t.Fatal("ciphertext under a swapped name must not open")
	}
}

// Tampered ciphertext is rejected, not decrypted to garbage.
func TestTamperedCiphertextRejected(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("a.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	p, _ := v.Path("a.txt")
	sealed, _ := os.ReadFile(p)
	sealed[len(sealed)-1] ^= 0xff
	if _, err := v.AddSealed("a.txt", bytes.NewReader(sealed)); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get("a.txt"); err == nil {
		t.Fatal("tampered ciphertext must be rejected")
	}
}

// The key survives reopen and is created 0600.
func TestKeyPersistsAndIsPrivate(t *testing.T) {
	dir := t.TempDir()
	v1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1.Put("a.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	v2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := v2.Get("a.txt"); err != nil || string(got) != "data" {
		t.Fatalf("reopened vault failed to decrypt: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, "vault.key"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %o, want 0600", fi.Mode().Perm())
	}
}

// Deleting removes the object; overwriting drops the orphaned old blob.
func TestDeleteAndOverwrite(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("a.txt", strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	old, _ := v.Path("a.txt")
	if _, err := v.Put("a.txt", strings.NewReader("v2")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("orphaned old ciphertext left on disk")
	}
	if !v.Delete("a.txt") {
		t.Fatal("delete failed")
	}
	if _, err := v.Get("a.txt"); err == nil {
		t.Fatal("deleted object still readable")
	}
}
