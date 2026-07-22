// Package vault is the owner-encrypted data store (M3): objects are sealed
// with AES-256-GCM on the owner's machine before they replicate, so foreign
// nodes hold ciphertext they cannot read. The sealing key is generated
// locally, stored 0600, and never leaves the owner's node — a breached,
// curious, or hostile host yields nothing but random-looking bytes.
//
// This is layered defense with a minimized blast radius, not a silver
// bullet: the owner's own node still holds the key, and metadata (object
// names, sizes, timing) remains visible to hosts.
package vault

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// MaxObject bounds one sealed object; vault objects are documents and
// backups, not model weights.
const MaxObject = 64 << 20

// Entry describes one sealed object. SHA256 is the hash of the ciphertext,
// so replicas can be verified by any node without the key.
type Entry struct {
	Name         string    `json:"name"`
	SHA256       string    `json:"sha256"` // ciphertext hash — verifiable key-free
	Size         int64     `json:"size"`   // ciphertext bytes
	Sealed       bool      `json:"sealed"` // true when this node holds only ciphertext (replica)
	Added        time.Time `json:"added"`
	Compressed   bool      `json:"compressed"`    // M7: cold tier — plaintext was gzipped before sealing
	LastAccessed time.Time `json:"last_accessed"` // M7: drives hot/cold tiering
}

type Vault struct {
	mu    sync.Mutex
	dir   string
	key   []byte // 32-byte sealing key; nil on key-less replica holders
	index map[string]Entry
}

var ErrNoKey = errors.New("this node does not hold the sealing key for that object")

// Open loads (or creates) the vault. A sealing key is generated on first
// open and stored 0600 next to the blobs — it never leaves this machine.
func Open(dir string) (*Vault, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	v := &Vault{dir: dir, index: map[string]Entry{}}
	keyPath := filepath.Join(dir, "vault.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(keyPath, key, 0o600); err != nil {
			return nil, err
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("vault key at %s is corrupt (%d bytes, want 32)", keyPath, len(key))
	}
	v.key = key
	if raw, err := os.ReadFile(filepath.Join(dir, "index.json")); err == nil {
		var list []Entry
		if json.Unmarshal(raw, &list) == nil {
			for _, e := range list {
				v.index[e.Name] = e
			}
		}
	}
	return v, nil
}

func (v *Vault) persist() {
	list := make([]Entry, 0, len(v.index))
	for _, e := range v.index {
		list = append(list, e)
	}
	raw, _ := json.MarshalIndent(list, "", " ")
	tmp := filepath.Join(v.dir, "index.json.tmp")
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, filepath.Join(v.dir, "index.json"))
	}
}

func (v *Vault) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts plain under the vault's key, name bound as AAD. The wire
// format is nonce || AES-256-GCM(plain, aad=name), so a blob swapped under
// a different name fails to open.
func (v *Vault) seal(name string, plain []byte) ([]byte, error) {
	gcm, err := v.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plain, []byte(name))...), nil
}

// Put seals plaintext on this machine and stores the ciphertext.
func (v *Vault) Put(name string, r io.Reader) (Entry, error) {
	if name == "" {
		return Entry{}, errors.New("name required")
	}
	plain, err := io.ReadAll(io.LimitReader(r, MaxObject+1))
	if err != nil {
		return Entry{}, err
	}
	if len(plain) > MaxObject {
		return Entry{}, fmt.Errorf("object exceeds %d bytes", MaxObject)
	}
	sealed, err := v.seal(name, plain)
	if err != nil {
		return Entry{}, err
	}
	return v.store(name, sealed, false, false, time.Now())
}

// AddSealed accepts an already-sealed replica pushed by a peer. No key is
// needed — the ciphertext hash is the integrity check.
func (v *Vault) AddSealed(name string, r io.Reader) (Entry, error) {
	if name == "" {
		return Entry{}, errors.New("name required")
	}
	sealed, err := io.ReadAll(io.LimitReader(r, MaxObject+1))
	if err != nil {
		return Entry{}, err
	}
	if len(sealed) > MaxObject {
		return Entry{}, fmt.Errorf("object exceeds %d bytes", MaxObject)
	}
	return v.store(name, sealed, true, false, time.Now())
}

// store writes the ciphertext blob and updates the index. compressed and
// lastAccessed are recorded as given rather than always reset, so a
// tiering pass (retier) can rewrite an object's storage form without it
// looking like a fresh write or a fresh access.
func (v *Vault) store(name string, sealed []byte, replica, compressed bool, lastAccessed time.Time) (Entry, error) {
	sum := sha256.Sum256(sealed)
	hexSum := hex.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(v.dir, hexSum), sealed, 0o600); err != nil {
		return Entry{}, err
	}
	e := Entry{Name: name, SHA256: hexSum, Size: int64(len(sealed)), Sealed: replica, Added: time.Now(),
		Compressed: compressed, LastAccessed: lastAccessed}
	v.mu.Lock()
	old, had := v.index[name]
	v.index[name] = e
	v.persist()
	if had && old.SHA256 != hexSum {
		v.removeBlobIfUnreferenced(old.SHA256)
	}
	v.mu.Unlock()
	return e, nil
}

// openPlain decrypts a stored entry and, if it was tiered cold, decompresses
// it back to its original plaintext.
func (v *Vault) openPlain(name string, e Entry) ([]byte, error) {
	sealed, err := os.ReadFile(filepath.Join(v.dir, e.SHA256))
	if err != nil {
		return nil, err
	}
	gcm, err := v.aead()
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ErrNoKey
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(name))
	if err != nil {
		return nil, ErrNoKey // wrong key or tampered ciphertext — indistinguishable by design
	}
	if e.Compressed {
		gz, err := gzip.NewReader(bytes.NewReader(plain))
		if err != nil {
			return nil, fmt.Errorf("corrupt compressed object %q: %w", name, err)
		}
		defer gz.Close()
		plain, err = io.ReadAll(gz)
		if err != nil {
			return nil, fmt.Errorf("corrupt compressed object %q: %w", name, err)
		}
	}
	return plain, nil
}

// Get opens a sealed object. Only the owner's node — the one holding the
// sealing key that produced it — can succeed; a replica holder gets ErrNoKey
// behavior via authentication failure of the ciphertext. A successful read
// marks the object as recently accessed, so the next Compact pass (M7)
// promotes it back to the fast tier if it had been compressed.
func (v *Vault) Get(name string) ([]byte, error) {
	v.mu.Lock()
	e, ok := v.index[name]
	v.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown object %q", name)
	}
	plain, err := v.openPlain(name, e)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	if cur, ok := v.index[name]; ok && cur.SHA256 == e.SHA256 {
		cur.LastAccessed = time.Now()
		v.index[name] = cur
		v.persist()
	}
	v.mu.Unlock()
	return plain, nil
}

// retier re-seals name's plaintext with the requested compression state,
// preserving LastAccessed — retiering itself must never look like a fresh
// access or it would fight the next Compact pass.
func (v *Vault) retier(name string, wantCompressed bool) error {
	v.mu.Lock()
	e, ok := v.index[name]
	v.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown object %q", name)
	}
	plain, err := v.openPlain(name, e)
	if err != nil {
		return err
	}
	toSeal := plain
	if wantCompressed {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(plain); err != nil {
			return err
		}
		if err := gz.Close(); err != nil {
			return err
		}
		toSeal = buf.Bytes()
	}
	sealed, err := v.seal(name, toSeal)
	if err != nil {
		return err
	}
	_, err = v.store(name, sealed, e.Sealed, wantCompressed, e.LastAccessed)
	return err
}

// Compact tiers storage by temperature (M7): objects untouched for
// hotWindow are gzip-compressed before their ciphertext to shrink them
// ("cold, small"); objects that were compressed but have been read again
// within hotWindow are decompressed back to their fast form ("hot, fast").
// Compression is applied to plaintext before sealing — encrypted bytes are
// already pseudorandom and would not meaningfully compress.
//
// Only objects this node holds the key for are eligible; a replica holder
// cannot decrypt a peer's ciphertext to retier it.
func (v *Vault) Compact(hotWindow time.Duration) (compressed, decompressed int, err error) {
	v.mu.Lock()
	names := make([]string, 0, len(v.index))
	for name, e := range v.index {
		if !e.Sealed {
			names = append(names, name)
		}
	}
	v.mu.Unlock()

	cutoff := time.Now().Add(-hotWindow)
	for _, name := range names {
		v.mu.Lock()
		e, ok := v.index[name]
		v.mu.Unlock()
		if !ok {
			continue
		}
		hot := e.LastAccessed.After(cutoff)
		switch {
		case !e.Compressed && !hot:
			if rerr := v.retier(name, true); rerr != nil {
				return compressed, decompressed, fmt.Errorf("compress %q: %w", name, rerr)
			}
			compressed++
		case e.Compressed && hot:
			if rerr := v.retier(name, false); rerr != nil {
				return compressed, decompressed, fmt.Errorf("decompress %q: %w", name, rerr)
			}
			decompressed++
		}
	}
	return compressed, decompressed, nil
}

// KeyCopy returns a copy of the sealing key for the off-mesh backup
// archive (M5) — the one sanctioned way the key leaves this machine, and
// only inside a passphrase-encrypted export.
func (v *Vault) KeyCopy() []byte {
	out := make([]byte, len(v.key))
	copy(out, v.key)
	return out
}

// SealedBytes returns an object's stored ciphertext for export.
func (v *Vault) SealedBytes(name string) ([]byte, error) {
	p, ok := v.Path(name)
	if !ok {
		return nil, fmt.Errorf("unknown object %q", name)
	}
	return os.ReadFile(p)
}

// PutBytes seals plaintext already held in memory (backup import path).
func (v *Vault) PutBytes(name string, plain []byte) (Entry, error) {
	return v.Put(name, bytes.NewReader(plain))
}

// OpenSealed decrypts one sealed blob with an explicit key — used by backup
// import to recover objects archived under a previous node's key. The name
// is bound as AAD exactly as in Get.
func OpenSealed(key []byte, name string, sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ErrNoKey
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(name))
	if err != nil {
		return nil, ErrNoKey
	}
	return plain, nil
}

// Verify re-hashes the ciphertext on disk against the recorded hash — a
// corrupted or missing blob is detected without needing the sealing key.
func (v *Vault) Verify(name string) (bool, error) {
	v.mu.Lock()
	e, ok := v.index[name]
	v.mu.Unlock()
	if !ok {
		return false, fmt.Errorf("unknown object %q", name)
	}
	raw, err := os.ReadFile(filepath.Join(v.dir, e.SHA256))
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]) == e.SHA256, nil
}

func (v *Vault) List() []Entry {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]Entry, 0, len(v.index))
	for _, e := range v.index {
		out = append(out, e)
	}
	return out
}

// Path returns the on-disk ciphertext path, for serving replicas to peers.
func (v *Vault) Path(name string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.index[name]
	if !ok {
		return "", false
	}
	return filepath.Join(v.dir, e.SHA256), true
}

func (v *Vault) Delete(name string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.index[name]
	if !ok {
		return false
	}
	delete(v.index, name)
	v.removeBlobIfUnreferenced(e.SHA256)
	v.persist()
	return true
}

// removeBlobIfUnreferenced deletes a ciphertext blob no index entry uses.
// Callers hold v.mu.
func (v *Vault) removeBlobIfUnreferenced(sha string) {
	for _, other := range v.index {
		if other.SHA256 == sha {
			return
		}
	}
	os.Remove(filepath.Join(v.dir, sha))
}
