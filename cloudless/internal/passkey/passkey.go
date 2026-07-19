// Package passkey implements passwordless sign-in for the console using
// passkeys (WebAuthn/FIDO2). Credentials never leave the user's device; the
// server stores only public keys. On successful login a short-lived session
// token is issued, which the console uses in place of a pasted admin key.
package passkey

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// user implements webauthn.User. IDs and credentials are persisted.
type user struct {
	ID    []byte                `json:"id"`
	Name  string                `json:"name"`
	Creds []webauthn.Credential `json:"creds"`
}

func (u *user) WebAuthnID() []byte                         { return u.ID }
func (u *user) WebAuthnName() string                       { return u.Name }
func (u *user) WebAuthnDisplayName() string                { return u.Name }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.Creds }

type session struct {
	token   string
	name    string
	expires time.Time
}

// Manager holds the WebAuthn engine, the user store, in-flight registration/
// login challenges, and issued session tokens.
type Manager struct {
	mu       sync.Mutex
	path     string
	wa       *webauthn.WebAuthn
	users    map[string]*user
	pending  map[string]*webauthn.SessionData // keyed by username
	sessions map[string]session               // token -> session
}

// New builds a Manager. rpID is the registrable domain (e.g. "localhost");
// origins are the full origins the console is served from.
func New(path, rpID string, origins []string) (*Manager, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "Cloudless",
		RPID:          rpID,
		RPOrigins:     origins,
	})
	if err != nil {
		return nil, err
	}
	m := &Manager{
		path: path, wa: wa,
		users:    map[string]*user{},
		pending:  map[string]*webauthn.SessionData{},
		sessions: map[string]session{},
	}
	m.load()
	return m, nil
}

func (m *Manager) load() {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var list []*user
	if json.Unmarshal(data, &list) == nil {
		for _, u := range list {
			m.users[u.Name] = u
		}
	}
}

func (m *Manager) persist() {
	list := make([]*user, 0, len(m.users))
	for _, u := range m.users {
		list = append(list, u)
	}
	raw, _ := json.MarshalIndent(list, "", " ")
	tmp := m.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, m.path)
	}
}

func randID(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func token() string {
	return hex.EncodeToString(randID(24))
}

// HasUsers reports whether anyone has registered yet (first-run onboarding).
func (m *Manager) HasUsers() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.users) > 0
}

// BeginRegister starts registration for name, returning the browser options.
func (m *Manager) BeginRegister(name string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[name]
	if !ok {
		u = &user{ID: randID(16), Name: name}
		m.users[name] = u
	}
	opts, sess, err := m.wa.BeginRegistration(u)
	if err != nil {
		return nil, err
	}
	m.pending[name] = sess
	return opts, nil
}

// FinishRegister completes registration from the authenticator's response.
func (m *Manager) FinishRegister(name string, r *http.Request) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[name]
	sess := m.pending[name]
	if !ok || sess == nil {
		return errors.New("no registration in progress")
	}
	cred, err := m.wa.FinishRegistration(u, *sess, r)
	if err != nil {
		return err
	}
	u.Creds = append(u.Creds, *cred)
	delete(m.pending, name)
	m.persist()
	return nil
}

// BeginLogin starts a login for name.
func (m *Manager) BeginLogin(name string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[name]
	if !ok || len(u.Creds) == 0 {
		return nil, errors.New("unknown user or no passkey registered")
	}
	opts, sess, err := m.wa.BeginLogin(u)
	if err != nil {
		return nil, err
	}
	m.pending[name] = sess
	return opts, nil
}

// FinishLogin verifies the assertion and returns a session token.
func (m *Manager) FinishLogin(name string, r *http.Request) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[name]
	sess := m.pending[name]
	if !ok || sess == nil {
		return "", errors.New("no login in progress")
	}
	if _, err := m.wa.FinishLogin(u, *sess, r); err != nil {
		return "", err
	}
	delete(m.pending, name)
	t := token()
	m.sessions[t] = session{token: t, name: name, expires: time.Now().Add(12 * time.Hour)}
	m.persist()
	return t, nil
}

// Session returns the username for a valid, unexpired token (or "").
func (m *Manager) Session(tok string) string {
	if tok == "" {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[tok]
	if !ok || time.Now().After(s.expires) {
		delete(m.sessions, tok)
		return ""
	}
	return s.name
}
