package pki

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestCAIssueAndVerify(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureCA(dir); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCA(dir); err != nil {
		t.Fatalf("EnsureCA must be idempotent: %v", err)
	}

	// A joiner mints a keypair; the CA signs it; the chain must verify.
	joiner := t.TempDir()
	pub, err := NewNodeKey(joiner)
	if err != nil {
		t.Fatal(err)
	}
	certDER, err := SignPubKey(dir, "node-1", pub)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "node-1" {
		t.Fatalf("CN = %q, want node-1", cert.Subject.CommonName)
	}
	caPEM, err := CACertPEM(dir)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(caPEM)
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		t.Fatalf("issued cert must verify against its CA: %v", err)
	}
}

func TestSelfIssueAndHasCreds(t *testing.T) {
	dir := t.TempDir()
	if HasCreds(dir) {
		t.Fatal("empty dir must have no creds")
	}
	if err := EnsureCA(dir); err != nil {
		t.Fatal(err)
	}
	if err := SelfIssue(dir, "founder"); err != nil {
		t.Fatal(err)
	}
	if !HasCreds(dir) {
		t.Fatal("after self-issue the node must have creds")
	}
}

func TestSignRequiresCA(t *testing.T) {
	joiner := t.TempDir()
	pub, _ := NewNodeKey(joiner)
	if _, err := SignPubKey(t.TempDir(), "n", pub); err == nil {
		t.Fatal("signing without a CA must fail")
	}
}
