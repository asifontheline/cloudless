package pki

import (
	"os"
	"path/filepath"
	"testing"
)

// L1 backfill: readPEM's error branches, SignPubKey's malformed-input
// branches, SaveNodeCreds' caDER-present path, and SelfIssue's idempotent
// second call had zero or near-zero coverage.

func TestReadPEMMissingFile(t *testing.T) {
	if _, err := readPEM(filepath.Join(t.TempDir(), "nope.pem")); err == nil {
		t.Fatal("reading a missing PEM file must error")
	}
}

func TestReadPEMNoBlock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(p, []byte("not a pem file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPEM(p); err == nil {
		t.Fatal("a file with no PEM block must error")
	}
}

func TestSignPubKeyMissingCACert(t *testing.T) {
	dir := t.TempDir() // no CA at all
	joiner := t.TempDir()
	pub, err := NewNodeKey(joiner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SignPubKey(dir, "n", pub); err == nil {
		t.Fatal("signing without a CA cert must error")
	}
}

func TestSignPubKeyCorruptCAKey(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCA(dir); err != nil {
		t.Fatal(err)
	}
	// Overwrite the CA key with a syntactically valid PEM block that isn't
	// a parseable EC key.
	if err := writePEM(filepath.Join(dir, caKeyFile), "EC PRIVATE KEY", []byte("not a real key"), 0o600); err != nil {
		t.Fatal(err)
	}
	joiner := t.TempDir()
	pub, _ := NewNodeKey(joiner)
	if _, err := SignPubKey(dir, "n", pub); err == nil {
		t.Fatal("signing with a corrupt CA key must error")
	}
}

func TestSignPubKeyBadPublicKey(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCA(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := SignPubKey(dir, "n", []byte("not a der public key")); err == nil {
		t.Fatal("signing a malformed public key must error")
	}
}

// SaveNodeCreds' caDER-present path (used by the CA-holding node's own
// self-issue-with-CA-copy case) writes both files.
func TestSaveNodeCredsWithCADER(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCA(dir); err != nil {
		t.Fatal(err)
	}
	joiner := t.TempDir()
	pub, _ := NewNodeKey(joiner)
	certDER, err := SignPubKey(dir, "n", pub)
	if err != nil {
		t.Fatal(err)
	}
	caDER, err := readPEM(filepath.Join(dir, caCertFile))
	if err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	if err := SaveNodeCreds(out, certDER, caDER); err != nil {
		t.Fatal(err)
	}
	if !HasCreds(out) {
		t.Fatal("SaveNodeCreds with a CA DER must leave usable node creds")
	}
	if _, err := os.Stat(filepath.Join(out, caCertFile)); err != nil {
		t.Fatalf("CA cert file must be written when caDER is provided: %v", err)
	}
}

// SelfIssue is idempotent: a second call on a node that already has a cert
// is a no-op, not a re-issue.
func TestSelfIssueIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCA(dir); err != nil {
		t.Fatal(err)
	}
	if err := SelfIssue(dir, "founder"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, nodeCertFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := SelfIssue(dir, "founder"); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(dir, nodeCertFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("SelfIssue must not re-issue a cert that already exists")
	}
}
