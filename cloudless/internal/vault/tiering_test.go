package vault

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

// M7: temperature-tiered storage. A fresh write is hot (uncompressed) for
// fast access; Compact demotes objects untouched past hotWindow to a
// compressed cold tier, and promotes a compressed object back to hot the
// moment it's read again within the window.

// compressible is large and repetitive so gzip visibly shrinks it — a
// realistic stand-in for the kind of document this tier targets.
func compressible() string {
	return strings.Repeat("the quick brown fox jumps over the lazy dog. ", 2000)
}

func TestFreshWriteIsHot(t *testing.T) {
	v := mustOpen(t)
	e, err := v.Put("a.txt", strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Compressed {
		t.Fatal("a freshly written object must start hot (uncompressed)")
	}
	if e.LastAccessed.IsZero() {
		t.Fatal("a freshly written object must have a non-zero LastAccessed")
	}
}

func TestCompactDemotesUntouchedObjects(t *testing.T) {
	v := mustOpen(t)
	content := compressible()
	if _, err := v.Put("cold.txt", strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}

	compressed, decompressed, err := v.Compact(0) // hotWindow=0: everything is cold
	if err != nil {
		t.Fatal(err)
	}
	if compressed != 1 || decompressed != 0 {
		t.Fatalf("compact = %d compressed, %d decompressed; want 1, 0", compressed, decompressed)
	}

	list := v.List()
	if len(list) != 1 || !list[0].Compressed {
		t.Fatalf("object must be marked compressed after demotion: %+v", list)
	}

	got, err := v.Get("cold.txt")
	if err != nil || string(got) != content {
		t.Fatalf("compressed object must still read back correctly: err=%v", err)
	}
}

// Storage actually shrinks for compressible content — the point of the
// feature, not just a metadata flag flip.
func TestCompactionShrinksStorage(t *testing.T) {
	v := mustOpen(t)
	content := compressible()
	e, err := v.Put("big.txt", strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	hotSize := e.Size

	if _, _, err := v.Compact(0); err != nil {
		t.Fatal(err)
	}
	list := v.List()
	if len(list) != 1 {
		t.Fatal("expected one entry")
	}
	if list[0].Size >= hotSize {
		t.Fatalf("compressed size %d must be smaller than hot size %d for compressible content", list[0].Size, hotSize)
	}
}

// Reading a compressed object marks it accessed; the next Compact call
// promotes it back to hot instead of leaving it compressed forever.
func TestCompactPromotesRecentlyReadObjects(t *testing.T) {
	v := mustOpen(t)
	content := compressible()
	if _, err := v.Put("obj.txt", strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Compact(0); err != nil {
		t.Fatal(err)
	}
	if !v.List()[0].Compressed {
		t.Fatal("setup: object should be compressed before this test's assertion")
	}

	if _, err := v.Get("obj.txt"); err != nil {
		t.Fatal(err)
	}
	// Now hot again (just accessed); a generous hot window promotes it back.
	compressed, decompressed, err := v.Compact(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if decompressed != 1 || compressed != 0 {
		t.Fatalf("compact = %d compressed, %d decompressed; want 0, 1", compressed, decompressed)
	}
	if v.List()[0].Compressed {
		t.Fatal("object must be promoted back to hot after being read within the hot window")
	}
	got, err := v.Get("obj.txt")
	if err != nil || string(got) != content {
		t.Fatalf("promoted object must still read back correctly: err=%v", err)
	}
}

// An object accessed within the hot window is left alone — Compact is not
// supposed to touch things that are already correctly tiered.
func TestCompactLeavesHotObjectsAlone(t *testing.T) {
	v := mustOpen(t)
	if _, err := v.Put("hot.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	compressed, decompressed, err := v.Compact(time.Hour) // just written == hot
	if err != nil {
		t.Fatal(err)
	}
	if compressed != 0 || decompressed != 0 {
		t.Fatalf("compact touched an already-hot object: %d/%d", compressed, decompressed)
	}
}

// Replica (key-less) objects are never touched by Compact — this node
// cannot decrypt a peer's ciphertext to retier it.
func TestCompactSkipsReplicas(t *testing.T) {
	owner := mustOpen(t)
	host := mustOpen(t)
	if _, err := owner.Put("mine.txt", strings.NewReader(compressible())); err != nil {
		t.Fatal(err)
	}
	p, _ := owner.Path("mine.txt")
	sealed, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.AddSealed("mine.txt", bytes.NewReader(sealed)); err != nil {
		t.Fatal(err)
	}

	compressed, decompressed, err := host.Compact(0)
	if err != nil {
		t.Fatal(err)
	}
	if compressed != 0 || decompressed != 0 {
		t.Fatalf("compact must skip replica objects entirely, got %d/%d", compressed, decompressed)
	}
	if host.List()[0].Compressed {
		t.Fatal("a replica must never be marked compressed by a host that can't decrypt it")
	}
}

func TestCompactEmptyVault(t *testing.T) {
	v := mustOpen(t)
	compressed, decompressed, err := v.Compact(time.Hour)
	if err != nil || compressed != 0 || decompressed != 0 {
		t.Fatalf("compact on an empty vault: %d/%d/%v", compressed, decompressed, err)
	}
}
