// Package replicate keeps every stored artifact on N nodes across distinct
// failure domains (M1). Placement is rendezvous hashing over the artifact
// hash and node name — every node computes the same desired holder set with
// no coordinator — greedily diversified so copies land in different
// locations (different homes/networks) whenever the mesh allows it.
//
// Repair is push-based: the periodic scan surveys peers' stores over the
// mutual-TLS relay and pushes missing copies to the desired holders, so a
// lost node's artifacts are restored within one scan interval. Writes call
// AckWrite, which pushes synchronously and reports how many replicas exist
// before the write is acknowledged.
package replicate

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"cloudless/internal/store"
)

const DefaultFactor = 3

// Peer is a mesh node reachable over the mutual-TLS relay.
type Peer struct {
	Name     string
	BaseURL  string // relay base, possibly ending in /v1 like registry backends
	Location string
}

// Manager surveys and repairs replication for the local store.
type Manager struct {
	Target   int    // replication factor (N)
	Self     string // this node's name
	Location string
	Store    *store.Store
	Peers    func() []Peer // healthy peers, excluding self
	Client   *http.Client  // carries the node's mTLS client cert

	mu      sync.Mutex
	status  []ObjectStatus
	scanned time.Time
}

// ObjectStatus is one artifact's replication health for the console.
type ObjectStatus struct {
	Name     string   `json:"name"`
	SHA256   string   `json:"sha256"`
	Replicas int      `json:"replicas"`
	Target   int      `json:"target"`
	Holders  []string `json:"holders"`
	Domains  int      `json:"domains"` // distinct failure domains among holders
	Healthy  bool     `json:"healthy"` // replicas meet the reachable target
}

func (m *Manager) target() int {
	if m.Target > 0 {
		return m.Target
	}
	return DefaultFactor
}

func base(u string) string { return strings.TrimSuffix(u, "/v1") }

// survey asks every peer what it holds. Returns name → holder peer names.
func (m *Manager) survey(ctx context.Context, peers []Peer) map[string][]string {
	holders := map[string][]string{}
	for _, e := range m.Store.List() {
		holders[e.Name] = append(holders[e.Name], m.Self)
	}
	for _, p := range peers {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base(p.BaseURL)+"/store", nil)
		if err != nil {
			continue
		}
		resp, err := m.Client.Do(req)
		if err != nil {
			continue
		}
		var lr struct {
			Artifacts []store.Entry `json:"artifacts"`
		}
		json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&lr)
		resp.Body.Close()
		for _, a := range lr.Artifacts {
			holders[a.Name] = append(holders[a.Name], p.Name)
		}
	}
	return holders
}

// desired returns the ideal holder set for an artifact: rendezvous-hash
// ranking diversified across failure domains — walk the ranking preferring
// nodes in locations not yet holding a copy, then fill remaining slots by
// rank. Every node computes the same answer from the same membership.
func desired(sha string, cands []Peer, n int) []Peer {
	ranked := make([]Peer, len(cands))
	copy(ranked, cands)
	score := func(p Peer) uint64 {
		h := fnv.New64a()
		h.Write([]byte(sha))
		h.Write([]byte("|"))
		h.Write([]byte(p.Name))
		return h.Sum64()
	}
	sort.Slice(ranked, func(i, j int) bool {
		si, sj := score(ranked[i]), score(ranked[j])
		if si != sj {
			return si > sj
		}
		return ranked[i].Name < ranked[j].Name
	})
	if n > len(ranked) {
		n = len(ranked)
	}
	out := make([]Peer, 0, n)
	seen := map[string]bool{}
	taken := map[string]bool{}
	// First pass: one copy per distinct location (empty = always distinct).
	for _, p := range ranked {
		if len(out) == n {
			return out
		}
		if p.Location != "" && seen[p.Location] {
			continue
		}
		seen[p.Location] = true
		taken[p.Name] = true
		out = append(out, p)
	}
	// Second pass: mesh has fewer domains than n — fill by rank.
	for _, p := range ranked {
		if len(out) == n {
			break
		}
		if !taken[p.Name] {
			taken[p.Name] = true
			out = append(out, p)
		}
	}
	return out
}

// push copies a local artifact to one peer via the relay's PUT /store.
// The receiver re-verifies format and hash on write.
func (m *Manager) push(ctx context.Context, p Peer, name string) error {
	path, ok := m.Store.Path(name)
	if !ok {
		return fmt.Errorf("artifact %q not held locally", name)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		base(p.BaseURL)+"/store?name="+url.QueryEscape(name), f)
	if err != nil {
		return err
	}
	resp, err := m.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("peer %s rejected push: %d", p.Name, resp.StatusCode)
	}
	return nil
}

// AckWrite replicates a freshly written artifact to its desired holders
// before the write is acknowledged (M1: durability target met at ack).
// Returns the replica count achieved (including this node) and the holders.
func (m *Manager) AckWrite(ctx context.Context, name string) (int, []string) {
	e, ok := m.entry(name)
	if !ok {
		return 0, nil
	}
	peers := m.Peers()
	self := Peer{Name: m.Self, Location: m.Location}
	want := desired(e.SHA256, append(peers, self), m.target())
	achieved := 1
	holders := []string{m.Self}
	for _, p := range want {
		if p.Name == m.Self {
			continue
		}
		if err := m.push(ctx, p, name); err == nil {
			achieved++
			holders = append(holders, p.Name)
		}
	}
	return achieved, holders
}

func (m *Manager) entry(name string) (store.Entry, bool) {
	for _, e := range m.Store.List() {
		if e.Name == name {
			return e, true
		}
	}
	return store.Entry{}, false
}

// Scan surveys the mesh and repairs: any artifact this node holds that is
// below target is pushed to the highest-ranked desired holders missing it.
// Under-replicated objects are repaired first.
func (m *Manager) Scan(ctx context.Context) []ObjectStatus {
	peers := m.Peers()
	holders := m.survey(ctx, peers)
	self := Peer{Name: m.Self, Location: m.Location}
	all := append(append([]Peer{}, peers...), self)
	byName := map[string]Peer{}
	for _, p := range all {
		byName[p.Name] = p
	}

	type job struct {
		name string
		e    store.Entry
		have map[string]bool
	}
	var jobs []job
	for _, e := range m.Store.List() {
		have := map[string]bool{}
		for _, h := range holders[e.Name] {
			have[h] = true
		}
		jobs = append(jobs, job{name: e.Name, e: e, have: have})
	}
	// Furthest below target first (M2: repair priority).
	sort.Slice(jobs, func(i, j int) bool { return len(jobs[i].have) < len(jobs[j].have) })

	n := m.target()
	for _, j := range jobs {
		if len(j.have) >= n {
			continue
		}
		for _, p := range desired(j.e.SHA256, all, n) {
			if len(j.have) >= n || p.Name == m.Self || j.have[p.Name] {
				continue
			}
			if err := m.push(ctx, p, j.name); err == nil {
				j.have[p.Name] = true
				holders[j.name] = append(holders[j.name], p.Name)
			}
		}
	}

	// Status covers every artifact seen anywhere in the mesh.
	names := make([]string, 0, len(holders))
	for name := range holders {
		names = append(names, name)
	}
	sort.Strings(names)
	reachable := n
	if len(all) < reachable {
		reachable = len(all)
	}
	out := make([]ObjectStatus, 0, len(names))
	for _, name := range names {
		hs := holders[name]
		sort.Strings(hs)
		domains := map[string]int{}
		distinct := 0
		for _, h := range hs {
			loc := byName[h].Location
			if loc == "" || domains[loc] == 0 {
				distinct++
			}
			domains[loc]++
		}
		sha := ""
		if e, ok := m.entry(name); ok {
			sha = e.SHA256
		}
		out = append(out, ObjectStatus{
			Name: name, SHA256: sha, Replicas: len(hs), Target: n,
			Holders: hs, Domains: distinct, Healthy: len(hs) >= reachable,
		})
	}
	m.mu.Lock()
	m.status = out
	m.scanned = time.Now()
	m.mu.Unlock()
	return out
}

// Run scans on a fixed interval until ctx ends — the self-healing loop.
func (m *Manager) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	m.Scan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Scan(ctx)
		}
	}
}

// Status is the console view: last scan's per-object replication health.
func (m *Manager) Status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]any{
		"target":  m.target(),
		"objects": m.status,
		"scanned": m.scanned,
	}
}
