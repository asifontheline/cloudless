package share

import (
	"encoding/json"
	"os"
	"runtime"
	"sync"
)

// Limits is a node's declaration of how much of the machine it will share.
// Safe default 5%, tunable up to a 70% ceiling (never 100% — headroom keeps
// the device responsive, cool, and safe). Applies to every node; on mobile a
// thermal/battery guard sits on top.

const (
	Default = 5  // percent
	Ceiling = 70 // percent — hard max, never 100
)

type Limits struct {
	CPUPercent  int    `json:"cpu_percent"`  // 0..70
	DiskGB      int    `json:"disk_gb"`      // 0 = unlimited beyond store use
	ShareWhen   string `json:"share_when"`   // "always" | "charging" | "idle"
	MeteredOK   bool   `json:"metered_ok"`   // contribute on metered networks?
}

func clamp(p int) int {
	if p < 0 {
		return 0
	}
	if p > Ceiling {
		return Ceiling
	}
	return p
}

func Defaults() Limits {
	return Limits{CPUPercent: Default, DiskGB: 0, ShareWhen: "charging", MeteredOK: false}
}

type Store struct {
	mu   sync.RWMutex
	path string
	lim  Limits
}

func Open(path string) *Store {
	s := &Store{path: path, lim: Defaults()}
	if raw, err := os.ReadFile(path); err == nil {
		var l Limits
		if json.Unmarshal(raw, &l) == nil {
			l.CPUPercent = clamp(l.CPUPercent)
			s.lim = l
		}
	}
	return s
}

func (s *Store) Get() Limits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lim
}

// Set applies new limits, clamping CPU into [0, Ceiling]. Returns the applied
// limits so callers can show exactly what took effect.
func (s *Store) Set(l Limits) Limits {
	l.CPUPercent = clamp(l.CPUPercent)
	if l.ShareWhen == "" {
		l.ShareWhen = "charging"
	}
	s.mu.Lock()
	s.lim = l
	raw, _ := json.MarshalIndent(l, "", " ")
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
	s.mu.Unlock()
	return l
}

// MaxProcs converts the CPU share percent into a concurrency budget: how many
// logical cores this node will let shared work occupy (at least 1 if sharing).
func (s *Store) MaxProcs() int {
	p := s.Get().CPUPercent
	if p <= 0 {
		return 0
	}
	n := runtime.NumCPU() * p / 100
	if n < 1 {
		n = 1
	}
	return n
}
