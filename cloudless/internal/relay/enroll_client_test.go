package relay

import (
	"strings"
	"testing"

	"cloudless/internal/pki"
)

// L1 backfill: Enroll (the joiner side) and enrollError were entirely
// untested — only the server side (EnrollHandler) had coverage.

func TestEnrollJoinerRoundTrip(t *testing.T) {
	secret := []byte("cluster-secret")
	srv, _ := enrollServer(t, secret)
	defer srv.Close()

	joinerDir := t.TempDir()
	if err := Enroll(srv.URL, joinerDir, "joiner", secret, ""); err != nil {
		t.Fatalf("Enroll failed: %v", err)
	}
	// A successful enroll leaves usable node credentials behind.
	if _, err := pki.ClientTLS(joinerDir, nil); err != nil {
		t.Fatalf("joiner has no usable TLS creds after enroll: %v", err)
	}
}

func TestEnrollRejectedByServerReturnsEnrollError(t *testing.T) {
	// A server with a different secret rejects the HMAC.
	srv, _ := enrollServer(t, []byte("cluster-secret"))
	defer srv.Close()

	err := Enroll(srv.URL, t.TempDir(), "joiner", []byte("wrong-secret"), "")
	if err == nil {
		t.Fatal("enroll with the wrong secret must fail")
	}
	if !strings.Contains(err.Error(), "enroll failed") {
		t.Fatalf("error should be an *enrollError with server body, got: %v", err)
	}
}

func TestEnrollUnreachableServer(t *testing.T) {
	if err := Enroll("http://127.0.0.1:1", t.TempDir(), "joiner", []byte("s"), ""); err == nil {
		t.Fatal("enroll against an unreachable server must fail")
	}
}
