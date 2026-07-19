package relay

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"cloudless/internal/jointoken"
	"cloudless/internal/pki"
)

func enrollServer(t *testing.T, secret []byte) (*httptest.Server, func(string) error) {
	t.Helper()
	pkiDir := t.TempDir()
	if err := pki.EnsureCA(pkiDir); err != nil {
		t.Fatal(err)
	}
	used := jointoken.OpenUsed(filepath.Join(pkiDir, "used.json"))
	check := func(tok string) error {
		id, exp, err := jointoken.Parse(secret, tok)
		if err != nil {
			return err
		}
		return used.Burn(id, exp)
	}
	return httptest.NewServer(EnrollHandler(pkiDir, secret, check)), check
}

func postEnroll(t *testing.T, url string, req EnrollRequest) int {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// A valid token enrolls once; the same token is rejected on second use, and
// an expired token never works (A2 acceptance criteria).
func TestJoinTokenSingleUseAndTTL(t *testing.T) {
	secret := []byte("cluster-secret")
	srv, _ := enrollServer(t, secret)
	defer srv.Close()

	pub := []byte("fake-public-key-der")
	mkReq := func(name, tok string) EnrollRequest {
		return EnrollRequest{
			Name: name, Pub: base64.StdEncoding.EncodeToString(pub),
			MAC: Sign(secret, name, pub), Token: tok,
		}
	}

	tok, _, _ := jointoken.New(secret, time.Minute)
	// First use: token accepted (enrollment proceeds past the token gate —
	// signing may still fail on the fake key, so anything but 401 is a pass).
	if code := postEnroll(t, srv.URL, mkReq("n1", tok)); code == http.StatusUnauthorized {
		t.Fatalf("fresh token must not be rejected, got %d", code)
	}
	// Second use: burned token rejected outright.
	if code := postEnroll(t, srv.URL, mkReq("n2", tok)); code != http.StatusUnauthorized {
		t.Fatalf("reused token must be rejected with 401, got %d", code)
	}
	// Expired token rejected.
	expired, _, _ := jointoken.New(secret, time.Nanosecond)
	time.Sleep(1100 * time.Millisecond)
	if code := postEnroll(t, srv.URL, mkReq("n3", expired)); code != http.StatusUnauthorized {
		t.Fatalf("expired token must be rejected with 401, got %d", code)
	}
	// Token minted with the wrong secret rejected.
	forged, _, _ := jointoken.New([]byte("other-secret"), time.Minute)
	if code := postEnroll(t, srv.URL, mkReq("n4", forged)); code != http.StatusUnauthorized {
		t.Fatalf("forged token must be rejected with 401, got %d", code)
	}
}

// The HMAC binding over name|pub still rejects tampering regardless of token.
func TestEnrollRejectsBadMAC(t *testing.T) {
	secret := []byte("cluster-secret")
	srv, _ := enrollServer(t, secret)
	defer srv.Close()
	pub := []byte("key")
	req := EnrollRequest{Name: "evil", Pub: base64.StdEncoding.EncodeToString(pub), MAC: "bogus"}
	if code := postEnroll(t, srv.URL, req); code != http.StatusUnauthorized {
		t.Fatalf("bad MAC must be rejected with 401, got %d", code)
	}
}
