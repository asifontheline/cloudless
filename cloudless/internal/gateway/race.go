package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Speculative racing (O2): for latency-sensitive single requests the caller
// opts in with an X-Race header naming how many of the fastest healthy
// backends should run the same request at once. The first complete, non-5xx
// answer wins and is returned; the shared context is cancelled so the losing
// backends' connections are torn down and their work stops. Duplicate work is
// bounded (k capped at raceMaxK and at the mesh size) and the winner alone is
// metered on the ledger — the caller pays for one answer, not k.

const (
	raceDefaultK = 2
	raceMaxK     = 4
	latSample    = 256 // ring of recent latencies, raced vs single, for the console
)

// raceK reads the opt-in: absent/0/1 means no racing, "on"/"" with the header
// present means the default, anything else is clamped to [2, raceMaxK].
func raceK(r *http.Request) int {
	v, ok := r.Header[http.CanonicalHeaderKey("X-Race")]
	if !ok || len(v) == 0 {
		return 1
	}
	k, err := strconv.Atoi(v[0])
	if err != nil {
		return raceDefaultK
	}
	if k <= 1 {
		return 1
	}
	if k > raceMaxK {
		return raceMaxK
	}
	return k
}

type raceAnswer struct {
	status  int
	body    []byte
	backend string
}

// race sends the same buffered request to the k fastest healthy backends and
// returns the first complete, non-5xx answer, cancelling the rest. Racing is
// for buffered JSON responses; streaming requests take the normal path.
func (g *Gateway) race(ctx context.Context, path string, body []byte, k int) (raceAnswer, int) {
	backends := g.reg.Ranked()
	if len(backends) < k {
		k = len(backends)
	}
	if k == 0 {
		return raceAnswer{status: http.StatusServiceUnavailable, backend: "-",
			body: []byte(`{"error":"no backends configured"}`)}, 0
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // the winner is buffered; losers stop the moment we return

	wins := make(chan raceAnswer, k)
	var wg sync.WaitGroup
	for i := 0; i < k; i++ {
		wg.Add(1)
		go func(b string, baseURL string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+trimV1(path), bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := g.client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 500 {
				return
			}
			data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			if err != nil {
				return // cancelled mid-read (lost the race) or truncated
			}
			wins <- raceAnswer{status: resp.StatusCode, body: data, backend: b}
		}(backends[i].Backend.Name, backends[i].Backend.BaseURL)
	}
	go func() { wg.Wait(); close(wins) }()

	if a, ok := <-wins; ok {
		return a, k
	}
	return raceAnswer{status: http.StatusBadGateway, backend: "-",
		body: []byte(`{"error":"all raced backends unavailable"}`)}, k
}

// recordLatency files one request's wall time under raced or single, keeping
// the last latSample of each for the console's p50/p95 comparison.
func (g *Gateway) recordLatency(raced bool, d time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if raced {
		g.racedLat = appendSample(g.racedLat, d)
	} else {
		g.singleLat = appendSample(g.singleLat, d)
	}
}

func appendSample(s []time.Duration, d time.Duration) []time.Duration {
	s = append(s, d)
	if len(s) > latSample {
		s = s[len(s)-latSample:]
	}
	return s
}

// latencyStats summarizes one sample set as count/p50/p95 in milliseconds.
func latencyStats(s []time.Duration) map[string]any {
	if len(s) == 0 {
		return map[string]any{"count": 0}
	}
	sorted := make([]time.Duration, len(s))
	copy(sorted, s)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	pct := func(p float64) int64 {
		i := int(p * float64(len(sorted)-1))
		return sorted[i].Milliseconds()
	}
	return map[string]any{"count": len(s), "p50_ms": pct(0.50), "p95_ms": pct(0.95)}
}

// raceStats is the console's measured answer to "is racing worth it": recent
// p50/p95 with and without racing, side by side.
func (g *Gateway) raceStats() map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return map[string]any{
		"raced":  latencyStats(g.racedLat),
		"single": latencyStats(g.singleLat),
	}
}
