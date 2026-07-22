package pki

import (
	"testing"
)

// A5: NodeSigner binds audit entries to this node's own PKI key pair.

func TestNodeSignerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewNodeKey(dir); err != nil {
		t.Fatal(err)
	}
	s, err := LoadNodeSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("audit entry hash")
	sig, err := s.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Verify(msg, sig) {
		t.Fatal("a signature must verify against its own message")
	}
	if s.Verify([]byte("different message"), sig) {
		t.Fatal("a signature must not verify against a different message")
	}
}

func TestNodeSignerDifferentNodesDontCrossVerify(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	if _, err := NewNodeKey(dirA); err != nil {
		t.Fatal(err)
	}
	if _, err := NewNodeKey(dirB); err != nil {
		t.Fatal(err)
	}
	a, err := LoadNodeSigner(dirA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadNodeSigner(dirB)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("audit entry hash")
	sig, err := a.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	if b.Verify(msg, sig) {
		t.Fatal("node B must not be able to verify a signature made by node A's key")
	}
}

func TestLoadNodeSignerMissingKey(t *testing.T) {
	if _, err := LoadNodeSigner(t.TempDir()); err == nil {
		t.Fatal("loading a signer with no node key on disk must error")
	}
}
