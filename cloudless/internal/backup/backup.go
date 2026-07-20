// Package backup is the off-mesh escape hatch (M5): the owner exports an
// encrypted archive of their vault to local disk and can re-import it later
// — into the same mesh or a brand-new one. The archive covers the worst
// case, the whole mesh gone: it carries the sealing key and every owned
// object, protected by a passphrase-derived key (PBKDF2-SHA256, AES-256-GCM).
//
// Import never transplants the old key: each object is opened with the
// archived key in memory and re-sealed under the importing node's own key,
// so a restored mesh continues with exactly one live key per node.
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cloudless/internal/vault"
)

const (
	// Version tags the archive layout for forward compatibility.
	Version = 1
	// pbkdf2Iters follows current OWASP guidance for PBKDF2-SHA256.
	pbkdf2Iters = 600_000
	saltLen     = 16
)

// Archive is the plaintext layout sealed inside an export.
type Archive struct {
	Version   int        `json:"version"`
	Created   time.Time  `json:"created"`
	NodeName  string     `json:"node_name"`
	VaultKey  []byte     `json:"vault_key"` // the exporting node's sealing key
	Objects   []Object   `json:"objects"`   // owned objects, as stored ciphertext
	ModelRefs []ModelRef `json:"model_refs,omitempty"`
}

// Object is one vault object: its entry plus the sealed bytes exactly as
// stored (ciphertext under VaultKey, name bound as AAD).
type Object struct {
	Name   string `json:"name"`
	Sealed []byte `json:"sealed"`
}

// ModelRef records a model by name and hash — models are large and
// re-pullable from the mesh or public repositories, so the archive carries
// the manifest, not the weights.
type ModelRef struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

var ErrBadPassphrase = errors.New("wrong passphrase or corrupted archive")

func derive(passphrase string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, pbkdf2Iters, 32)
}

// Seal encrypts a marshalled archive: salt || nonce || AES-256-GCM(json).
func Seal(a *Archive, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("a passphrase is required — the archive leaves the mesh")
	}
	plain, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := derive(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(salt, nonce...)
	return append(out, gcm.Seal(nil, nonce, plain, nil)...), nil
}

// Open decrypts an exported archive with the owner's passphrase.
func Open(data []byte, passphrase string) (*Archive, error) {
	if len(data) < saltLen+13 {
		return nil, ErrBadPassphrase
	}
	key, err := derive(passphrase, data[:saltLen])
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	rest := data[saltLen:]
	if len(rest) < gcm.NonceSize() {
		return nil, ErrBadPassphrase
	}
	plain, err := gcm.Open(nil, rest[:gcm.NonceSize()], rest[gcm.NonceSize():], nil)
	if err != nil {
		return nil, ErrBadPassphrase
	}
	var a Archive
	if err := json.Unmarshal(plain, &a); err != nil {
		return nil, ErrBadPassphrase
	}
	if a.Version != Version {
		return nil, fmt.Errorf("unsupported archive version %d", a.Version)
	}
	return &a, nil
}

// Export assembles this node's archive: every owned (non-replica) vault
// object as stored, the sealing key, and the model manifest.
func Export(v *vault.Vault, nodeName string, models []ModelRef, passphrase string) ([]byte, error) {
	a := &Archive{Version: Version, Created: time.Now().UTC(), NodeName: nodeName,
		VaultKey: v.KeyCopy(), ModelRefs: models}
	for _, e := range v.List() {
		if e.Sealed {
			continue // hosted third-party ciphertext is not ours to export
		}
		raw, err := v.SealedBytes(e.Name)
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", e.Name, err)
		}
		a.Objects = append(a.Objects, Object{Name: e.Name, Sealed: raw})
	}
	return Seal(a, passphrase)
}

// ImportResult is one object's outcome from a re-import.
type ImportResult struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"` // restored | present | failed
	Error   string `json:"error,omitempty"`
}

// Import restores an archive into the given vault: each object is opened
// with the archived key in memory and re-sealed under this node's own key.
// Objects already present locally are left untouched.
func Import(v *vault.Vault, data []byte, passphrase string) ([]ImportResult, []ModelRef, error) {
	a, err := Open(data, passphrase)
	if err != nil {
		return nil, nil, err
	}
	existing := map[string]bool{}
	for _, e := range v.List() {
		if !e.Sealed {
			existing[e.Name] = true
		}
	}
	out := make([]ImportResult, 0, len(a.Objects))
	for _, o := range a.Objects {
		if existing[o.Name] {
			out = append(out, ImportResult{Name: o.Name, Outcome: "present"})
			continue
		}
		plain, err := vault.OpenSealed(a.VaultKey, o.Name, o.Sealed)
		if err != nil {
			out = append(out, ImportResult{Name: o.Name, Outcome: "failed", Error: err.Error()})
			continue
		}
		if _, err := v.PutBytes(o.Name, plain); err != nil {
			out = append(out, ImportResult{Name: o.Name, Outcome: "failed", Error: err.Error()})
			continue
		}
		out = append(out, ImportResult{Name: o.Name, Outcome: "restored"})
	}
	return out, a.ModelRefs, nil
}
