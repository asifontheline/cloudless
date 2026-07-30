// Package abuse tracks attack-shaped behavior at the gateway (S2): mass
// authentication failures from one source look like credential stuffing
// or endpoint scanning, not an honest typo. A source that crosses a
// failure threshold within a window is locked out — rate-limited harder
// than the standard per-key quota, not merely counted.
package abuse

import (
	"sync"
	"time"
)

// Guard tracks recent auth failures per source (typically remote IP) and
// reports whether a source is currently locked out.
type Guard struct {
	mu        sync.Mutex
	fails     map[string][]time.Time
	lockedTil map[string]time.Time
	threshold int
	window    time.Duration
	lockout   time.Duration
}

// New builds a Guard: a source is locked out for lockout once it records
// threshold failures within window.
func New(threshold int, window, lockout time.Duration) *Guard {
	return &Guard{
		fails:     make(map[string][]time.Time),
		lockedTil: make(map[string]time.Time),
		threshold: threshold,
		window:    window,
		lockout:   lockout,
	}
}

// Blocked reports whether source is currently locked out, and if so, how
// much longer.
func (g *Guard) Blocked(source string) (bool, time.Duration) {
	if g == nil {
		return false, 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	until, ok := g.lockedTil[source]
	if !ok {
		return false, 0
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		delete(g.lockedTil, source)
		return false, 0
	}
	return true, remaining
}

// RecordFailure logs a failed auth attempt from source, pruning failures
// older than window, and locks the source out once threshold is crossed.
func (g *Guard) RecordFailure(source string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-g.window)
	kept := g.fails[source][:0]
	for _, t := range g.fails[source] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	g.fails[source] = kept
	if len(kept) >= g.threshold {
		g.lockedTil[source] = now.Add(g.lockout)
		delete(g.fails, source) // lockout imposed; start clean once it expires
	}
}

// RecordSuccess clears a source's failure history — a legitimate request
// isn't punished for an earlier honest typo once it actually succeeds.
func (g *Guard) RecordSuccess(source string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, source)
}
