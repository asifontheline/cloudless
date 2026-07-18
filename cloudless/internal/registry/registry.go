package registry

import (
	"context"
	"net/http"
	"sync"
	"time"

	"cloudless/internal/config"
)

// BackendState is the registry's view of one backend: reachability and
// the latency of its last successful health probe, used for routing order.
type BackendState struct {
	Backend   config.Backend
	Healthy   bool
	LatencyMS int64
	LastSeen  time.Time
	Failures  int
}

type Registry struct {
	mu       sync.RWMutex
	states   map[string]*BackendState
	interval time.Duration
	client   *http.Client
}

func New(backends []config.Backend, interval time.Duration) *Registry {
	r := &Registry{
		states:   make(map[string]*BackendState, len(backends)),
		interval: interval,
		client:   &http.Client{Timeout: 3 * time.Second},
	}
	for _, b := range backends {
		r.states[b.Name] = &BackendState{Backend: b}
	}
	return r
}

// Run probes every backend's /models endpoint on a fixed interval until ctx ends.
func (r *Registry) Run(ctx context.Context) {
	r.probeAll(ctx)
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.probeAll(ctx)
		}
	}
}

func (r *Registry) probeAll(ctx context.Context) {
	var wg sync.WaitGroup
	r.mu.RLock()
	names := make([]string, 0, len(r.states))
	for name := range r.states {
		names = append(names, name)
	}
	r.mu.RUnlock()
	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			r.probe(ctx, name)
		}(name)
	}
	wg.Wait()
}

func (r *Registry) probe(ctx context.Context, name string) {
	r.mu.RLock()
	s, ok := r.states[name]
	r.mu.RUnlock()
	if !ok {
		return
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Backend.BaseURL+"/models", nil)
	if err != nil {
		r.record(name, false, 0)
		return
	}
	resp, err := r.client.Do(req)
	if err != nil {
		r.record(name, false, 0)
		return
	}
	resp.Body.Close()
	r.record(name, resp.StatusCode < 500, time.Since(start).Milliseconds())
}

func (r *Registry) record(name string, healthy bool, latencyMS int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.states[name]
	s.Healthy = healthy
	if healthy {
		s.LatencyMS = latencyMS
		s.LastSeen = time.Now()
		s.Failures = 0
	} else {
		s.Failures++
	}
}

// Upsert adds a backend discovered at runtime (e.g. via gossip) or updates
// its URL if the node rejoined with a new address.
func (r *Registry) Upsert(b config.Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.states[b.Name]; ok {
		s.Backend = b
		return
	}
	r.states[b.Name] = &BackendState{Backend: b}
}

// Remove drops a backend whose node left the mesh.
func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, name)
}

// Ranked returns healthy backends fastest-first, then unhealthy ones as a
// last resort so a request can still be attempted during probe gaps.
func (r *Registry) Ranked() []BackendState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BackendState, 0, len(r.states))
	for _, s := range r.states {
		out = append(out, *s)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if less(out[j], out[i]) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func less(a, b BackendState) bool {
	if a.Healthy != b.Healthy {
		return a.Healthy
	}
	return a.LatencyMS < b.LatencyMS
}
