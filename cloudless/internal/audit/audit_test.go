package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChainVerifies(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	l := Open(p)
	l.Append("cluster", "keys.create", "alice", "")
	l.Append("cluster", "share.set", "node-a", "cpu=40")
	l.Append("cluster", "revoke", "node-b", "")

	if ok, at := l.Verify(); !ok {
		t.Fatalf("fresh chain should verify, broke at %d", at)
	}
	if got := len(l.List(0)); got != 3 {
		t.Fatalf("expected 3 entries, got %d", got)
	}
	// A reopened log continues the chain intact.
	l2 := Open(p)
	l2.Append("cluster", "revoke", "node-c", "")
	if ok, _ := l2.Verify(); !ok {
		t.Fatal("chain should still verify after reopen + append")
	}
}

func TestTamperDetected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	l := Open(p)
	l.Append("cluster", "keys.create", "alice", "")
	l.Append("cluster", "revoke", "bob", "secret-reason")
	l.Append("cluster", "share.set", "node-a", "cpu=70")

	// Tamper: flip a byte in the middle entry's detail on disk.
	raw, _ := os.ReadFile(p)
	tampered := []byte(string(raw))
	idx := -1
	for i := 0; i+13 < len(tampered); i++ {
		if string(tampered[i:i+13]) == "secret-reason" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("could not find entry to tamper")
	}
	tampered[idx] = 'X'
	os.WriteFile(p, tampered, 0o600)

	l2 := Open(p)
	ok, at := l2.Verify()
	if ok {
		t.Fatal("tampered chain must NOT verify")
	}
	if at != 2 {
		t.Errorf("expected break at seq 2, got %d", at)
	}
}
