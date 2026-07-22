package backup

import (
	"os"
	"strings"
	"testing"
)

// L1 backfill: Open's malformed-input guards, Export's read-failure path,
// and Import's per-object failure handling had zero coverage — the exact
// edges of M5's "worst case, mesh gone" escape hatch.

func TestOpenRejectsTruncatedData(t *testing.T) {
	if _, err := Open([]byte("too short"), "pw"); err != ErrBadPassphrase {
		t.Fatalf("data shorter than salt+tag must be ErrBadPassphrase, got %v", err)
	}
	// Long enough to pass the first length guard but too short once the
	// salt is stripped off to leave a full nonce.
	if _, err := Open(make([]byte, saltLen+13), "pw"); err != ErrBadPassphrase {
		t.Fatalf("data with no room for a nonce after the salt must be ErrBadPassphrase, got %v", err)
	}
}

// An archive sealed under a future/incompatible layout is reported as an
// unsupported version, not silently misread.
func TestOpenRejectsUnsupportedVersion(t *testing.T) {
	a := &Archive{Version: Version + 1, NodeName: "n"}
	arch, err := Seal(a, "pw")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(arch, "pw")
	if err == nil || err == ErrBadPassphrase {
		t.Fatalf("unsupported version must be its own error, not ErrBadPassphrase or nil, got %v", err)
	}
	if !strings.Contains(err.Error(), "unsupported archive version") {
		t.Fatalf("error should name the version problem, got: %v", err)
	}
}

// A blob that vanishes between listing and reading fails the export with a
// clear error instead of silently omitting the object.
func TestExportSurfacesReadFailure(t *testing.T) {
	v := newVault(t)
	if _, err := v.Put("a.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	p, ok := v.Path("a.txt")
	if !ok {
		t.Fatal("expected a path for a.txt")
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Export(v, "n", nil, "pw"); err == nil {
		t.Fatal("export must fail when a listed object's blob is missing")
	}
}

// One object corrupted independently of the archive envelope is reported
// as a per-object failure — it does not block restoring everything else.
func TestImportReportsPerObjectFailureWithoutAbortingOthers(t *testing.T) {
	v := newVault(t)
	if _, err := v.Put("good.txt", strings.NewReader("fine")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Put("bad.txt", strings.NewReader("will be corrupted")); err != nil {
		t.Fatal(err)
	}
	arch, err := Export(v, "n", nil, "pw")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Open(arch, "pw")
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.Objects {
		if a.Objects[i].Name == "bad.txt" {
			a.Objects[i].Sealed[len(a.Objects[i].Sealed)-1] ^= 0xff
		}
	}
	reArch, err := Seal(a, "pw")
	if err != nil {
		t.Fatal(err)
	}

	fresh := newVault(t)
	results, _, err := Import(fresh, reArch, "pw")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ImportResult{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if byName["good.txt"].Outcome != "restored" {
		t.Fatalf("uncorrupted object must still restore, got %+v", byName["good.txt"])
	}
	if byName["bad.txt"].Outcome != "failed" || byName["bad.txt"].Error == "" {
		t.Fatalf("corrupted object must report a per-object failure, got %+v", byName["bad.txt"])
	}
	if _, err := fresh.Get("good.txt"); err != nil {
		t.Fatalf("good.txt should be readable after import: %v", err)
	}
}
