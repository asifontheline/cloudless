package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store is the content-addressed model store: every artifact is identified
// by its SHA-256, verified on write and on demand, and only safe tensor-data
// formats are admitted. Pickle-based files — the model ecosystem's main
// malware vector — are rejected outright.

type Entry struct {
	Name   string    `json:"name"`
	SHA256 string    `json:"sha256"`
	Size   int64     `json:"size"`
	Format string    `json:"format"`
	Added  time.Time `json:"added"`
}

type Store struct {
	mu    sync.Mutex
	dir   string
	index map[string]Entry // by name
}

var ErrBadFormat = errors.New("format not allowed: only .gguf, .safetensors, .onnx tensor formats are accepted; pickle-based model files can execute code and are rejected")

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, index: map[string]Entry{}}
	if raw, err := os.ReadFile(filepath.Join(dir, "index.json")); err == nil {
		var list []Entry
		if json.Unmarshal(raw, &list) == nil {
			for _, e := range list {
				s.index[e.Name] = e
			}
		}
	}
	return s, nil
}

func (s *Store) persist() {
	list := make([]Entry, 0, len(s.index))
	for _, e := range s.index {
		list = append(list, e)
	}
	raw, _ := json.MarshalIndent(list, "", " ")
	tmp := filepath.Join(s.dir, "index.json.tmp")
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, filepath.Join(s.dir, "index.json"))
	}
}

// checkFormat enforces the allowlist by extension AND magic bytes.
func checkFormat(name string, head []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".gguf":
		if len(head) >= 4 && string(head[:4]) == "GGUF" {
			return "gguf", nil
		}
		return "", fmt.Errorf("%w (file does not have GGUF magic)", ErrBadFormat)
	case ".safetensors":
		// 8-byte little-endian header length followed by a JSON object.
		if len(head) >= 9 && head[8] == '{' {
			return "safetensors", nil
		}
		return "", fmt.Errorf("%w (file does not look like safetensors)", ErrBadFormat)
	case ".onnx":
		// Protobuf; reject if it starts with a pickle opcode.
		if len(head) >= 2 && head[0] == 0x80 {
			return "", fmt.Errorf("%w (pickle signature found)", ErrBadFormat)
		}
		return "onnx", nil
	default:
		return "", fmt.Errorf("%w (extension %q)", ErrBadFormat, ext)
	}
}

// Add streams an artifact in, enforcing the allowlist and recording its hash.
func (s *Store) Add(name string, r io.Reader) (Entry, error) {
	head := make([]byte, 16)
	n, _ := io.ReadFull(r, head)
	format, err := checkFormat(name, head[:n])
	if err != nil {
		return Entry{}, err
	}
	tmp, err := os.CreateTemp(s.dir, "incoming-*")
	if err != nil {
		return Entry{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	w := io.MultiWriter(tmp, h)
	if _, err := w.Write(head[:n]); err != nil {
		tmp.Close()
		return Entry{}, err
	}
	size, err := io.Copy(w, r)
	tmp.Close()
	if err != nil {
		return Entry{}, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err := os.Rename(tmp.Name(), filepath.Join(s.dir, sum)); err != nil {
		return Entry{}, err
	}
	e := Entry{Name: name, SHA256: sum, Size: size + int64(n), Format: format, Added: time.Now()}
	s.mu.Lock()
	s.index[name] = e
	s.persist()
	s.mu.Unlock()
	return e, nil
}

func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, 0, len(s.index))
	for _, e := range s.index {
		out = append(out, e)
	}
	return out
}

// Verify re-hashes the blob on disk and compares with the recorded hash —
// a poisoned or corrupted replica is detected, not served.
func (s *Store) Verify(name string) (bool, error) {
	s.mu.Lock()
	e, ok := s.index[name]
	s.mu.Unlock()
	if !ok {
		return false, fmt.Errorf("unknown artifact %q", name)
	}
	f, err := os.Open(filepath.Join(s.dir, e.SHA256))
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == e.SHA256, nil
}

// Path returns the on-disk blob path for a stored artifact.
func (s *Store) Path(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.index[name]
	if !ok {
		return "", false
	}
	return filepath.Join(s.dir, e.SHA256), true
}

func (s *Store) Delete(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.index[name]
	if !ok {
		return false
	}
	delete(s.index, name)
	// Remove the blob only if no other name references it.
	shared := false
	for _, other := range s.index {
		if other.SHA256 == e.SHA256 {
			shared = true
			break
		}
	}
	if !shared {
		os.Remove(filepath.Join(s.dir, e.SHA256))
	}
	s.persist()
	return true
}
