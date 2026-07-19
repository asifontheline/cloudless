package share

import (
	"path/filepath"
	"testing"
)

func TestClampToCeiling(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "share.json"))
	cases := []struct{ in, want int }{
		{5, 5}, {70, 70}, {95, Ceiling}, {-3, 0}, {0, 0},
	}
	for _, c := range cases {
		got := s.Set(Limits{CPUPercent: c.in}).CPUPercent
		if got != c.want {
			t.Errorf("Set(%d%%) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestDefaultIsFivePercent(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "share.json"))
	if got := s.Get().CPUPercent; got != Default {
		t.Errorf("fresh default = %d%%, want %d%%", got, Default)
	}
}

func TestMaxProcsZeroWhenNotSharing(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "share.json"))
	s.Set(Limits{CPUPercent: 0})
	if n := s.MaxProcs(); n != 0 {
		t.Errorf("MaxProcs at 0%% = %d, want 0", n)
	}
}
