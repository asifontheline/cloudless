package quota

import (
	"sync"
	"time"
)

// Limiter enforces the group's fair-use agreement at the gateway: a
// per-key request rate (sliding one-minute window) and a per-key daily
// token budget. Zero limits mean unlimited.

type Limits struct {
	RequestsPerMinute int   `json:"requests_per_minute"`
	TokensPerDay      int64 `json:"tokens_per_day"`
}

type Status struct {
	Key               string `json:"key"` // redacted
	RequestsLastMin   int    `json:"requests_last_min"`
	TokensToday       int64  `json:"tokens_today"`
	RequestsPerMinute int    `json:"requests_per_minute"`
	TokensPerDay      int64  `json:"tokens_per_day"`
}

type dayCount struct {
	day    string
	tokens int64
}

type Limiter struct {
	mu     sync.Mutex
	limits Limits
	reqs   map[string][]time.Time
	tokens map[string]*dayCount
}

func New(l Limits) *Limiter {
	return &Limiter{limits: l, reqs: make(map[string][]time.Time), tokens: make(map[string]*dayCount)}
}

func today() string { return time.Now().Format("2006-01-02") }

// Allow reports whether a request under this key may proceed and, if not,
// how long the caller should wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()

	if l.limits.TokensPerDay > 0 {
		if d, ok := l.tokens[key]; ok && d.day == today() && d.tokens >= l.limits.TokensPerDay {
			return false, time.Until(now.Truncate(24 * time.Hour).Add(24 * time.Hour))
		}
	}
	if l.limits.RequestsPerMinute > 0 {
		cutoff := now.Add(-time.Minute)
		times := l.reqs[key]
		kept := times[:0]
		for _, t := range times {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		l.reqs[key] = kept
		if len(kept) >= l.limits.RequestsPerMinute {
			return false, time.Until(kept[0].Add(time.Minute))
		}
		l.reqs[key] = append(kept, now)
	}
	return true, 0
}

// AddTokens charges completed usage against the key's daily budget.
func (l *Limiter) AddTokens(key string, n int64) {
	if l == nil || n <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	d, ok := l.tokens[key]
	if !ok || d.day != today() {
		d = &dayCount{day: today()}
		l.tokens[key] = d
	}
	d.tokens += n
}

// Snapshot returns limits plus per-key consumption for the console/CLI.
func (l *Limiter) Snapshot() (Limits, []Status) {
	if l == nil {
		return Limits{}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-time.Minute)
	keys := make(map[string]bool)
	for k := range l.reqs {
		keys[k] = true
	}
	for k := range l.tokens {
		keys[k] = true
	}
	out := make([]Status, 0, len(keys))
	for k := range keys {
		n := 0
		for _, t := range l.reqs[k] {
			if t.After(cutoff) {
				n++
			}
		}
		var tok int64
		if d, ok := l.tokens[k]; ok && d.day == today() {
			tok = d.tokens
		}
		out = append(out, Status{Key: k, RequestsLastMin: n, TokensToday: tok,
			RequestsPerMinute: l.limits.RequestsPerMinute, TokensPerDay: l.limits.TokensPerDay})
	}
	return l.limits, out
}
