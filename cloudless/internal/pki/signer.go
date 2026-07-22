package pki

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"path/filepath"
)

// NodeSigner signs and verifies with this node's own enrolled key pair — the
// same key used for mutual TLS. It satisfies audit.Signer structurally (the
// audit package has no dependency on pki), binding audit log entries to the
// node's PKI identity rather than just an internally self-consistent hash
// chain (A5).
type NodeSigner struct {
	priv *ecdsa.PrivateKey
}

// LoadNodeSigner reads this node's private key from dir. Call after
// EnsureCA/SelfIssue or Enroll have run — it fails if no node key exists yet.
func LoadNodeSigner(dir string) (*NodeSigner, error) {
	keyDER, err := readPEM(filepath.Join(dir, nodeKeyFile))
	if err != nil {
		return nil, err
	}
	priv, err := x509.ParseECPrivateKey(keyDER)
	if err != nil {
		return nil, err
	}
	return &NodeSigner{priv: priv}, nil
}

func (s *NodeSigner) Sign(data []byte) ([]byte, error) {
	h := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, s.priv, h[:])
}

func (s *NodeSigner) Verify(data, sig []byte) bool {
	h := sha256.Sum256(data)
	return ecdsa.VerifyASN1(&s.priv.PublicKey, h[:], sig)
}
