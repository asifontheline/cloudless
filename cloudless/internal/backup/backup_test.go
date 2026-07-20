package backup

import (
	"strings"
	"testing"

	"cloudless/internal/vault"
)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	v, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The worst case M5 exists for: every node gone, only the archive survives.
// A brand-new vault (new machine, new key) recovers every object readable.
func TestWholeMeshGoneRecovery(t *testing.T) {
	old := newVault(t)
	if _, err := old.Put("notes.txt", strings.NewReader("survives the fire")); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Put("plan.txt", strings.NewReader("second object")); err != nil {
		t.Fatal(err)
	}
	arch, err := Export(old, "old-node", []ModelRef{{Name: "m.gguf", SHA256: "abc"}}, "correct horse")
	if err != nil {
		t.Fatal(err)
	}

	fresh := newVault(t) // different key, empty history
	results, models, err := Import(fresh, arch, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %+v", results)
	}
	for _, r := range results {
		if r.Outcome != "restored" {
			t.Fatalf("object %s: %+v", r.Name, r)
		}
	}
	got, err := fresh.Get("notes.txt")
	if err != nil || string(got) != "survives the fire" {
		t.Fatalf("restored object unreadable on the new mesh: %v %q", err, got)
	}
	if len(models) != 1 || models[0].Name != "m.gguf" {
		t.Fatalf("model manifest lost: %+v", models)
	}
	// Restored objects are owned (re-sealed under the new key), not hosted.
	for _, e := range fresh.List() {
		if e.Sealed {
			t.Fatalf("imported object %s marked as key-less replica", e.Name)
		}
	}
}

// A wrong passphrase yields a clean error, not garbage.
func TestWrongPassphraseRejected(t *testing.T) {
	v := newVault(t)
	v.Put("a.txt", strings.NewReader("data"))
	arch, err := Export(v, "n", nil, "right")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Import(newVault(t), arch, "wrong"); err != ErrBadPassphrase {
		t.Fatalf("want ErrBadPassphrase, got %v", err)
	}
	if _, err := Export(v, "n", nil, ""); err == nil {
		t.Fatal("empty passphrase must be refused")
	}
}

// Hosted third-party ciphertext is not ours to export.
func TestHostedObjectsExcluded(t *testing.T) {
	owner := newVault(t)
	owner.Put("mine.txt", strings.NewReader("mine"))
	host := newVault(t)
	sealed, _ := owner.SealedBytes("mine.txt")
	host.AddSealed("theirs.txt", strings.NewReader(string(sealed)))
	host.Put("also-mine.txt", strings.NewReader("host's own"))

	arch, err := Export(host, "host", nil, "pw")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Open(arch, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Objects) != 1 || a.Objects[0].Name != "also-mine.txt" {
		t.Fatalf("archive must hold only owned objects, got %+v", a.Objects)
	}
}

// Re-importing over live data touches nothing that already exists.
func TestImportIdempotent(t *testing.T) {
	v := newVault(t)
	v.Put("a.txt", strings.NewReader("original"))
	arch, _ := Export(v, "n", nil, "pw")
	v.Put("a.txt", strings.NewReader("edited since the backup"))
	results, _, err := Import(v, arch, "pw")
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Outcome != "present" {
		t.Fatalf("existing object must be untouched: %+v", results)
	}
	got, _ := v.Get("a.txt")
	if string(got) != "edited since the backup" {
		t.Fatal("import overwrote newer local data")
	}
}

// A tampered archive is rejected outright.
func TestTamperedArchiveRejected(t *testing.T) {
	v := newVault(t)
	v.Put("a.txt", strings.NewReader("data"))
	arch, _ := Export(v, "n", nil, "pw")
	arch[len(arch)-1] ^= 0xff
	if _, _, err := Import(newVault(t), arch, "pw"); err != ErrBadPassphrase {
		t.Fatalf("tampered archive must be rejected, got %v", err)
	}
}
