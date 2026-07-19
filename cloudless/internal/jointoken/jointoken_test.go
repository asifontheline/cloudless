package jointoken

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMintVerifyBurn(t *testing.T) {
	secret := []byte("s3cret")
	tok, _, err := New(secret, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	id, exp, err := Parse(secret, tok)
	if err != nil {
		t.Fatalf("fresh token must verify: %v", err)
	}
	u := OpenUsed(filepath.Join(t.TempDir(), "used.json"))
	if err := u.Burn(id, exp); err != nil {
		t.Fatalf("first use must succeed: %v", err)
	}
	if err := u.Burn(id, exp); err != ErrUsed {
		t.Fatalf("second use must be rejected, got %v", err)
	}
}

func TestExpiry(t *testing.T) {
	secret := []byte("s3cret")
	expired, _, _ := New(secret, time.Nanosecond)
	time.Sleep(1100 * time.Millisecond) // expiry has 1s granularity
	if _, _, err := Parse(secret, expired); err != ErrExpired {
		t.Fatalf("expired token must be rejected, got %v", err)
	}
}

func TestTamperAndWrongSecret(t *testing.T) {
	tok, _, _ := New([]byte("right"), time.Minute)
	if _, _, err := Parse([]byte("wrong"), tok); err != ErrBadMAC {
		t.Fatalf("wrong secret must fail MAC, got %v", err)
	}
	if _, _, err := Parse([]byte("right"), tok+"x"); err == nil {
		t.Fatal("tampered token must be rejected")
	}
	if _, _, err := Parse([]byte("right"), "not-a-token"); err != ErrMalformed {
		t.Fatalf("malformed token, got %v", err)
	}
}

func TestUsedPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "used.json")
	secret := []byte("s")
	tok, _, _ := New(secret, time.Minute)
	id, exp, _ := Parse(secret, tok)
	if err := OpenUsed(path).Burn(id, exp); err != nil {
		t.Fatal(err)
	}
	// A fresh open (simulating CA node restart) must still reject reuse.
	if err := OpenUsed(path).Burn(id, exp); err != ErrUsed {
		t.Fatalf("burned id must survive restart, got %v", err)
	}
}
