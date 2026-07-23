package registry

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
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
	self     string // this node's own hierarchical location (I4)
}

// New builds a registry; tlsCfg (may be nil) carries the node's client cert
// so health probes can reach peers' mutual-TLS relays.
func New(backends []config.Backend, interval time.Duration, tlsCfg *tls.Config) *Registry {
	r := &Registry{
		states:   make(map[string]*BackendState, len(backends)),
		interval: interval,
		client:   &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
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

// SetSelfLocation sets this node's own hierarchical location
// (continent/country/state/city/village), used to prefer routing to nearby
// backends (I4). Unset (the default) disables locality preference — pure
// health/latency ranking, unchanged from before I4.
func (r *Registry) SetSelfLocation(loc string) {
	r.mu.Lock()
	r.self = loc
	r.mu.Unlock()
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

// Ranked returns healthy backends nearest-and-fastest first, then unhealthy
// ones as a last resort so a request can still be attempted during probe
// gaps. "Nearest" (I4) means the backend's hierarchical location
// (continent/country/state/city/village) shares the longest leading prefix
// with this node's own — a same-city backend outranks a same-country one,
// which outranks a different-continent one, all else equal. Health always
// wins over locality; locality wins over raw latency. When this node's own
// location is unset, every backend has locality depth 0 and ranking is
// exactly the pre-I4 health/latency order.
func (r *Registry) Ranked() []BackendState {
	r.mu.RLock()
	self := r.self
	out := make([]BackendState, 0, len(r.states))
	for _, s := range r.states {
		out = append(out, *s)
	}
	r.mu.RUnlock()
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if less(out[j], out[i], self) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// localityDepth counts matching leading path segments between two
// hierarchical locations — how "near" b is to ref. 0 when either is unset
// or they share no common prefix (e.g. different continents).
func localityDepth(ref, loc string) int {
	if ref == "" || loc == "" {
		return 0
	}
	refParts := strings.Split(ref, "/")
	locParts := strings.Split(loc, "/")
	depth := 0
	for i := 0; i < len(refParts) && i < len(locParts); i++ {
		if refParts[i] != locParts[i] {
			break
		}
		depth++
	}
	return depth
}

func less(a, b BackendState, self string) bool {
	if a.Healthy != b.Healthy {
		return a.Healthy
	}
	if da, db := localityDepth(self, a.Backend.Location), localityDepth(self, b.Backend.Location); da != db {
		return da > db
	}
	return a.LatencyMS < b.LatencyMS
}
