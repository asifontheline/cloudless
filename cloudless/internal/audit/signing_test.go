package audit

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
)

// A5: signed audit log. Hash-chaining alone only proves internal
// self-consistency — anyone with write access to the file can regenerate a
// whole new chain and Verify would call it intact. A Signer binds entries to
// a real key pair so a forged replacement log fails unless the forger also
// holds the private key.

// testSigner is a minimal ECDSA signer for tests, independent of the pki
// package (which is a separate module boundary — this proves the Signer
// interface itself, pki.NodeSigner is tested in its own package).
type testSigner struct{ priv *ecdsa.PrivateKey }

func newTestSigner(t *testing.T) *testSigner {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &testSigner{priv: priv}
}

func (s *testSigner) Sign(data []byte) ([]byte, error) {
	h := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, s.priv, h[:])
}

func (s *testSigner) Verify(data, sig []byte) bool {
	h := sha256.Sum256(data)
	return ecdsa.VerifyASN1(&s.priv.PublicKey, h[:], sig)
}

func TestSignedEntriesVerify(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	l := Open(p)
	l.SetSigner(newTestSigner(t))
	l.Append("cluster", "keys.create", "alice", "")
	l.Append("cluster", "revoke", "node-b", "")

	if !l.Signed() {
		t.Fatal("Signed() must report true once a signer is set")
	}
	entries := l.List(0)
	for _, e := range entries {
		if e.Sig == "" {
			t.Fatalf("entry seq %d has no signature", e.Seq)
		}
	}
	if ok, at := l.Verify(); !ok {
		t.Fatalf("signed chain should verify, broke at %d", at)
	}
}

// The core guarantee: an attacker with write access to the log file cannot
// forge a whole new self-consistent chain, because they don't hold the
// signing key — Verify catches it even though the hash chain itself is
// perfectly consistent.
func TestForgedChainFailsWithoutSigningKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	genuine := Open(p)
	genuine.SetSigner(newTestSigner(t))
	genuine.Append("cluster", "keys.create", "alice", "")
	genuine.Append("cluster", "revoke", "bob", "")

	// The attacker doesn't have the real signer, so they append with their
	// own (or no) signer — simulating a wholly rewritten log using a
	// different key.
	forgerPath := filepath.Join(t.TempDir(), "forged.log")
	forger := Open(forgerPath)
	forger.SetSigner(newTestSigner(t)) // different key pair
	forger.Append("cluster", "keys.create", "alice", "")
	forger.Append("cluster", "revoke", "bob", "")

	// The forged log is internally hash-consistent...
	if ok, at := forger.Verify(); !ok {
		t.Fatalf("forger's own chain should be self-consistent (bad test setup), broke at %d", at)
	}

	// ...but fails against the genuine node's verifier, because the
	// signatures don't belong to the genuine key.
	checker := Open(forgerPath)
	checker.SetSigner(genuine.signer)
	if ok, _ := checker.Verify(); ok {
		t.Fatal("a log signed by a different key must NOT verify against the genuine signer")
	}
}

// Entries appended before a signer is attached (or under no signer at all)
// have no Sig; once a signer is set, Verify demands one from every entry —
// a signing-capable node must never silently accept unsigned entries.
func TestVerifyRequiresSignatureOnceSignerIsSet(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	l := Open(p)
	l.Append("cluster", "keys.create", "alice", "") // unsigned — no signer yet

	l.SetSigner(newTestSigner(t))
	if ok, at := l.Verify(); ok {
		t.Fatalf("a signing-capable node must reject unsigned entries in its own chain, got ok with broke-at %d", at)
	}
}

// A corrupted signature on disk (one hex character flipped) is caught even
// though the hash chain itself is untouched.
func TestTamperedSignatureDetected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	l := Open(p)
	l.SetSigner(newTestSigner(t))
	l.Append("cluster", "revoke", "bob", "secret-reason")
	l.Append("cluster", "keys.create", "alice", "")

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	idx := bytesIndex(raw, []byte(`"sig":"`))
	if idx < 0 {
		t.Fatal("could not find a sig field to tamper")
	}
	flipAt := idx + len(`"sig":"`)
	tampered := append([]byte{}, raw...)
	if tampered[flipAt] == 'a' {
		tampered[flipAt] = 'b'
	} else {
		tampered[flipAt] = 'a'
	}
	if err := os.WriteFile(p, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	l2 := Open(p)
	l2.SetSigner(l.signer)
	if ok, _ := l2.Verify(); ok {
		t.Fatal("a tampered signature must not verify")
	}
}

func bytesIndex(data, sub []byte) int {
	for i := 0; i+len(sub) <= len(data); i++ {
		match := true
		for j := range sub {
			if data[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func TestUnsignedLogUnaffected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	l := Open(p)
	l.Append("cluster", "keys.create", "alice", "")
	if l.Signed() {
		t.Fatal("a log with no signer must report Signed() == false")
	}
	entries := l.List(0)
	if entries[0].Sig != "" {
		t.Fatal("entries must have no signature when no signer is set")
	}
	if ok, at := l.Verify(); !ok {
		t.Fatalf("unsigned chain must still verify on hash-chain integrity alone, broke at %d", at)
	}
}
