package keys

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// Store manages per-user API keys. The cluster key (from config) remains the
// admin credential; user keys authenticate service calls and are what usage,
// quotas, and the ledger attribute to.

type Record struct {
	Name    string    `json:"name"`
	Key     string    `json:"key"` // full secret; only ever returned at creation
	Created time.Time `json:"created"`
	Revoked bool      `json:"revoked"`
}

type Public struct {
	Name    string    `json:"name"`
	Key     string    `json:"key"` // redacted
	Created time.Time `json:"created"`
	Revoked bool      `json:"revoked"`
}

type Store struct {
	mu   sync.Mutex
	path string
	recs []Record
}

func Open(path string) *Store {
	s := &Store{path: path}
	if raw, err := os.ReadFile(path); err == nil {
		json.Unmarshal(raw, &s.recs)
	}
	return s
}

func redact(k string) string {
	if len(k) > 8 {
		return k[:8] + "…"
	}
	return k
}

func (s *Store) persist() {
	raw, err := json.MarshalIndent(s.recs, "", " ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
}

// Create mints a key for name and returns the full secret (shown once).
func (s *Store) Create(name string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, Record{Name: name, Key: key, Created: time.Now()})
	s.persist()
	return key, nil
}

// Revoke disables every key whose redacted prefix matches.
func (s *Store) Revoke(prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "…")
	s.mu.Lock()
	defer s.mu.Unlock()
	hit := false
	for i := range s.recs {
		if !s.recs[i].Revoked && strings.HasPrefix(s.recs[i].Key, prefix) && prefix != "" {
			s.recs[i].Revoked = true
			hit = true
		}
	}
	if hit {
		s.persist()
	}
	return hit
}

// Active reports whether the full key authenticates.
func (s *Store) Active(key string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.recs {
		if !s.recs[i].Revoked && s.recs[i].Key == key {
			return true
		}
	}
	return false
}

// List returns redacted records for display.
func (s *Store) List() []Public {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Public, 0, len(s.recs))
	for _, r := range s.recs {
		out = append(out, Public{Name: r.Name, Key: redact(r.Key), Created: r.Created, Revoked: r.Revoked})
	}
	return out
}
