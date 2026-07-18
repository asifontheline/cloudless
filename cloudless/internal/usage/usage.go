package usage

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Store accumulates per-key, per-backend usage on the gateway node and
// persists it as JSON. Zero external dependencies by design; an embedded
// SQL store can replace the file when accounting grows richer.

type Entry struct {
	Requests         int64     `json:"requests"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	LastUsed         time.Time `json:"last_used"`
}

type Key struct {
	APIKey  string `json:"api_key"` // redacted form: first 8 chars
	Backend string `json:"backend"`
}

type Record struct {
	Key
	Entry
}

type Store struct {
	mu   sync.Mutex
	path string
	data map[Key]*Entry
	dirt bool
}

func Open(path string) *Store {
	s := &Store{path: path, data: make(map[Key]*Entry)}
	if raw, err := os.ReadFile(path); err == nil {
		var recs []Record
		if json.Unmarshal(raw, &recs) == nil {
			for i := range recs {
				e := recs[i].Entry
				s.data[recs[i].Key] = &e
			}
		}
	}
	go s.flushLoop()
	return s
}

// Redact shortens an API key for display and storage.
func Redact(key string) string {
	if len(key) > 8 {
		return key[:8] + "…"
	}
	return key
}

func (s *Store) Add(apiKey, backend string, requests, promptTokens, completionTokens int64) {
	if s == nil {
		return
	}
	k := Key{APIKey: Redact(apiKey), Backend: backend}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[k]
	if !ok {
		e = &Entry{}
		s.data[k] = e
	}
	e.Requests += requests
	e.PromptTokens += promptTokens
	e.CompletionTokens += completionTokens
	e.LastUsed = time.Now()
	s.dirt = true
}

// Snapshot returns all records, most recently used first.
func (s *Store) Snapshot() []Record {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.data))
	for k, e := range s.data {
		out = append(out, Record{Key: k, Entry: *e})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].LastUsed.After(out[i].LastUsed) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func (s *Store) flushLoop() {
	for range time.Tick(5 * time.Second) {
		s.mu.Lock()
		if !s.dirt {
			s.mu.Unlock()
			continue
		}
		recs := make([]Record, 0, len(s.data))
		for k, e := range s.data {
			recs = append(recs, Record{Key: k, Entry: *e})
		}
		s.dirt = false
		path := s.path
		s.mu.Unlock()
		if raw, err := json.MarshalIndent(recs, "", " "); err == nil {
			tmp := path + ".tmp"
			if os.WriteFile(tmp, raw, 0o600) == nil {
				os.Rename(tmp, path)
			}
		}
	}
}
