package share

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// L1 backfill: Open's reload-from-disk path and MaxProcs' positive-share
// branch had almost no coverage — only the fresh-default path was tested.

func TestPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share.json")
	s1 := Open(path)
	s1.Set(Limits{CPUPercent: 30, DiskGB: 10, ShareWhen: "always", MeteredOK: true})

	s2 := Open(path)
	got := s2.Get()
	if got.CPUPercent != 30 || got.DiskGB != 10 || got.ShareWhen != "always" || !got.MeteredOK {
		t.Fatalf("reopened store lost limits: %+v", got)
	}
}

// A stored value beyond the ceiling (e.g. from an older build without the
// clamp, or a hand-edited file) is clamped on load, not trusted as-is.
func TestOpenClampsStoredValueOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share.json")
	if err := os.WriteFile(path, []byte(`{"cpu_percent":95,"share_when":"idle"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Open(path).Get()
	if got.CPUPercent != Ceiling {
		t.Fatalf("loaded CPUPercent = %d, want clamped to %d", got.CPUPercent, Ceiling)
	}
}

func TestOpenCorruptFileFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Open(path).Get()
	if got != Defaults() {
		t.Fatalf("corrupt file should fall back to defaults, got %+v", got)
	}
}

func TestOpenMissingFileUsesDefaults(t *testing.T) {
	got := Open(filepath.Join(t.TempDir(), "does-not-exist.json")).Get()
	if got != Defaults() {
		t.Fatalf("missing file should use defaults, got %+v", got)
	}
}

// Sharing at a positive percent budgets at least one core, scaled by
// available CPUs.
func TestMaxProcsPositiveShare(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "share.json"))
	s.Set(Limits{CPUPercent: 50})
	want := runtime.NumCPU() * 50 / 100
	if want < 1 {
		want = 1
	}
	if got := s.MaxProcs(); got != want {
		t.Fatalf("MaxProcs at 50%% = %d, want %d", got, want)
	}
}

// A small percentage that would floor to 0 cores is still guaranteed 1 —
// "sharing something" must mean at least one core is actually offered.
func TestMaxProcsFloorsToAtLeastOne(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "share.json"))
	s.Set(Limits{CPUPercent: 1})
	if got := s.MaxProcs(); got < 1 {
		t.Fatalf("MaxProcs at 1%% = %d, want at least 1", got)
	}
}

// An empty ShareWhen defaults to "charging" rather than persisting blank.
func TestSetEmptyShareWhenDefaultsToCharging(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "share.json"))
	got := s.Set(Limits{CPUPercent: 10, ShareWhen: ""})
	if got.ShareWhen != "charging" {
		t.Fatalf("empty ShareWhen = %q, want default \"charging\"", got.ShareWhen)
	}
}
