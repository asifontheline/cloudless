// Package jointoken mints and verifies single-use, expiring join tokens
// (A2). A token is self-authenticating — HMAC over its id and expiry with
// the cluster secret — so the CA node can verify it without shared state.
// Single-use is enforced by a small persisted used-id set on the CA node,
// so a token stays burned across restarts.
package jointoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultTTL = 15 * time.Minute

var (
	ErrMalformed = errors.New("join token malformed")
	ErrBadMAC    = errors.New("join token rejected")
	ErrExpired   = errors.New("join token expired")
	ErrUsed      = errors.New("join token already used")
)

func mac(secret []byte, id string, exp int64) string {
	m := hmac.New(sha256.New, secret)
	fmt.Fprintf(m, "%s|%d", id, exp)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// New mints a token valid for ttl (DefaultTTL when ttl <= 0).
// Format: <id>.<unix-expiry>.<mac>, URL-safe.
func New(secret []byte, ttl time.Duration) (token string, expires time.Time, err error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	raw := make([]byte, 9)
	if _, err = rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(raw)
	expires = time.Now().Add(ttl)
	return fmt.Sprintf("%s.%d.%s", id, expires.Unix(), mac(secret, id, expires.Unix())), expires, nil
}

// Parse verifies authenticity and expiry, returning the token id and expiry.
// It does not consult the used set — see Used.Burn.
func Parse(secret []byte, token string) (id string, exp int64, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", 0, ErrMalformed
	}
	exp, perr := strconv.ParseInt(parts[1], 10, 64)
	if perr != nil {
		return "", 0, ErrMalformed
	}
	if !hmac.Equal([]byte(parts[2]), []byte(mac(secret, parts[0], exp))) {
		return "", 0, ErrBadMAC
	}
	if time.Now().Unix() > exp {
		return "", 0, ErrExpired
	}
	return parts[0], exp, nil
}

// Used is the persisted set of burned token ids, kept on the CA node.
type Used struct {
	mu   sync.Mutex
	path string
	ids  map[string]int64 // id -> expiry, for pruning
}

// OpenUsed loads (or starts) the used set persisted at path.
func OpenUsed(path string) *Used {
	u := &Used{path: path, ids: map[string]int64{}}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &u.ids)
	}
	return u
}

// Burn marks the id used. It returns ErrUsed when the id was already burned.
func (u *Used) Burn(id string, exp int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, dup := u.ids[id]; dup {
		return ErrUsed
	}
	now := time.Now().Unix()
	for k, e := range u.ids { // prune long-expired ids so the set stays small
		if e < now {
			delete(u.ids, k)
		}
	}
	u.ids[id] = exp
	data, _ := json.Marshal(u.ids)
	return os.WriteFile(u.path, data, 0o600)
}
