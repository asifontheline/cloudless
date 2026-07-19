package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Log is a tamper-evident, hash-chained record of administrative actions.
// Each entry embeds the SHA-256 of the previous entry, so altering or
// removing any past entry breaks the chain and is detectable by Verify.
// Append-only on disk (one JSON object per line).

type Entry struct {
	Seq      int64     `json:"seq"`
	Time     time.Time `json:"time"`
	Actor    string    `json:"actor"`  // redacted key or "cluster"
	Action   string    `json:"action"` // e.g. "revoke", "keys.create", "share.set"
	Target   string    `json:"target"`
	Detail   string    `json:"detail,omitempty"`
	PrevHash string    `json:"prev_hash"`
	Hash     string    `json:"hash"`
}

type Log struct {
	mu   sync.Mutex
	path string
	last string // hash of the most recent entry
	seq  int64
}

const genesis = "genesis"

// hashEntry computes the entry hash over its content plus the previous hash.
func hashEntry(e Entry) string {
	h := sha256.New()
	// Deterministic field order; Hash itself is excluded.
	enc, _ := json.Marshal([]any{e.Seq, e.Time.UnixNano(), e.Actor, e.Action, e.Target, e.Detail, e.PrevHash})
	h.Write(enc)
	return hex.EncodeToString(h.Sum(nil))
}

func Open(path string) *Log {
	l := &Log{path: path, last: genesis}
	entries := readAll(path)
	if n := len(entries); n > 0 {
		l.last = entries[n-1].Hash
		l.seq = entries[n-1].Seq
	}
	return l
}

func readAll(path string) []Entry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// Append records an action and extends the hash chain.
func (l *Log) Append(actor, action, target, detail string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e := Entry{
		Seq: l.seq, Time: time.Now(), Actor: actor, Action: action,
		Target: target, Detail: detail, PrevHash: l.last,
	}
	e.Hash = hashEntry(e)
	l.last = e.Hash
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(e)
	f.Write(append(line, '\n'))
}

// List returns entries, most recent first, up to limit (0 = all).
func (l *Log) List(limit int) []Entry {
	entries := readAll(l.path)
	// reverse
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

// Verify walks the chain from genesis and reports whether it is intact.
// Returns (ok, brokenAtSeq). brokenAtSeq is 0 when ok.
func (l *Log) Verify() (bool, int64) {
	entries := readAll(l.path)
	prev := genesis
	for _, e := range entries {
		if e.PrevHash != prev {
			return false, e.Seq
		}
		want := hashEntry(e)
		if e.Hash != want {
			return false, e.Seq
		}
		prev = e.Hash
	}
	return true, 0
}
