// Package ext is the polyglot extension model (K4): new services and
// workloads in any language, without touching the Go core. An extension is
// an ordinary HTTP service — Python, JS, Rust, a shell script behind a
// server, anything — that registers itself with the node over the open API
// and is then reachable through the gateway at /x/<name>/... with the same
// bearer authentication as inference.
//
// Registration is admin-only and audited; the node health-checks each
// extension and the console lists them. Extensions run as their own
// processes under their own permissions — the node proxies to them, it does
// not execute their code.
package ext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Extension is one registered service.
type Extension struct {
	Name        string    `json:"name"`        // path segment under /x/
	BaseURL     string    `json:"base_url"`    // where the service listens, e.g. http://127.0.0.1:9090
	Description string    `json:"description"` // shown on the console catalog
	Runtime     string    `json:"runtime"`     // free-form: python, node, rust, ... (informational)
	Added       time.Time `json:"added"`
	Healthy     bool      `json:"healthy"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
}

type Registry struct {
	mu   sync.Mutex
	path string
	exts map[string]*Extension
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Open loads the extension registry (persisted so registrations survive
// node restarts).
func Open(path string) *Registry {
	r := &Registry{path: path, exts: map[string]*Extension{}}
	if raw, err := os.ReadFile(path); err == nil {
		var list []Extension
		if json.Unmarshal(raw, &list) == nil {
			for i := range list {
				e := list[i]
				r.exts[e.Name] = &e
			}
		}
	}
	return r
}

func (r *Registry) persist() {
	list := r.snapshot()
	raw, _ := json.MarshalIndent(list, "", " ")
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, r.path)
	}
}

func (r *Registry) snapshot() []Extension {
	out := make([]Extension, 0, len(r.exts))
	for _, e := range r.exts {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Register admits an extension after validating its name and URL. The URL
// must parse and use http(s); loopback is the recommended deployment (the
// service runs next to the node), but LAN services are allowed — the mesh
// operator decides what runs where.
func (r *Registry) Register(e Extension) (Extension, error) {
	if !nameRe.MatchString(e.Name) {
		return Extension{}, errors.New("name must be lowercase letters, digits, hyphens (max 64)")
	}
	u, err := url.Parse(e.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Extension{}, errors.New("base_url must be an absolute http(s) URL")
	}
	e.BaseURL = strings.TrimSuffix(e.BaseURL, "/")
	e.Added = time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.exts[e.Name]; exists {
		return Extension{}, fmt.Errorf("extension %q already registered — remove it first", e.Name)
	}
	r.exts[e.Name] = &e
	r.persist()
	return e, nil
}

// Remove deregisters an extension.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.exts[name]; !ok {
		return false
	}
	delete(r.exts, name)
	r.persist()
	return true
}

// List returns registered extensions, name-sorted.
func (r *Registry) List() []Extension {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot()
}

// Get resolves one extension by name.
func (r *Registry) Get(name string) (Extension, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.exts[name]
	if !ok {
		return Extension{}, false
	}
	return *e, true
}

// Probe health-checks every extension: GET <base>/healthz, anything below
// 500 counts as alive (an extension without /healthz typically 404s, which
// still proves the process is up and answering).
func (r *Registry) Probe(ctx context.Context, client *http.Client) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	r.mu.Lock()
	names := make([]string, 0, len(r.exts))
	for n := range r.exts {
		names = append(names, n)
	}
	r.mu.Unlock()
	for _, name := range names {
		e, ok := r.Get(name)
		if !ok {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.BaseURL+"/healthz", nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		healthy := false
		if err == nil {
			resp.Body.Close()
			healthy = resp.StatusCode < 500
		}
		r.mu.Lock()
		if cur, ok := r.exts[name]; ok {
			cur.Healthy = healthy
			if healthy {
				cur.LastSeen = time.Now()
			}
		}
		r.mu.Unlock()
	}
}

// Run probes on an interval until ctx ends.
func (r *Registry) Run(ctx context.Context, interval time.Duration, client *http.Client) {
	t := time.NewTicker(interval)
	defer t.Stop()
	r.Probe(ctx, client)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Probe(ctx, client)
		}
	}
}

// extDir returns the default registry path beside other node state.
func DefaultPath(stateDir string) string {
	return filepath.Join(stateDir, "extensions.json")
}
