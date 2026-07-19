package revoke

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Set is the mesh's revocation list: node names whose certificates are no
// longer trusted. A revoked node is refused at every relay (mutual-TLS
// verification consults this set) and dropped from routing. The set is
// persisted and propagated across the mesh by gossip.

type Record struct {
	Name    string    `json:"name"`
	Revoked time.Time `json:"revoked"`
}

type Set struct {
	mu   sync.RWMutex
	path string
	m    map[string]Record
}

func Open(path string) *Set {
	s := &Set{path: path, m: map[string]Record{}}
	if raw, err := os.ReadFile(path); err == nil {
		var list []Record
		if json.Unmarshal(raw, &list) == nil {
			for _, r := range list {
				s.m[r.Name] = r
			}
		}
	}
	return s
}

// Add records a revocation; returns false if it was already present.
func (s *Set) Add(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[name]; ok {
		return false
	}
	s.m[name] = Record{Name: name, Revoked: time.Now()}
	s.persist()
	return true
}

// Has reports whether a node name is revoked.
func (s *Set) Has(name string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[name]
	return ok
}

func (s *Set) List() []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Record, 0, len(s.m))
	for _, r := range s.m {
		out = append(out, r)
	}
	return out
}

func (s *Set) persist() {
	list := make([]Record, 0, len(s.m))
	for _, r := range s.m {
		list = append(list, r)
	}
	raw, _ := json.MarshalIndent(list, "", " ")
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
}
