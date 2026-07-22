package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// E2: join links/QR from console. POST /join-link mints a fresh token and
// combines it with this node's join info into one ready-to-run command
// plus a QR code of that same command — one call instead of the console
// stitching together two separate endpoints by hand.

func TestJoinLinkComposesCommandAndQR(t *testing.T) {
	g := newTestGateway(t)
	g.Audit = newTestAudit(t)
	var mintedTTL time.Duration
	g.MintJoinToken = func(ttl time.Duration) (string, time.Time, error) {
		mintedTTL = ttl
		return "tok-abc123", time.Now().Add(ttl), nil
	}
	g.JoinInfo = func() (string, string, string) {
		return "cluster-secret", "1.2.3.4:7946", "http://1.2.3.4:8080"
	}

	req := httptest.NewRequest(http.MethodPost, "/join-link", strings.NewReader(`{"ttl_minutes":30}`))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if mintedTTL != 30*time.Minute {
		t.Fatalf("ttl_minutes not forwarded to MintJoinToken: got %v", mintedTTL)
	}

	var resp struct {
		Command     string `json:"command"`
		Expires     string `json:"expires"`
		QRPNGBase64 string `json:"qr_png_base64"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	want := "cloudless up -join cluster-secret@1.2.3.4:7946 -seed-api http://1.2.3.4:8080 -join-token tok-abc123"
	if resp.Command != want {
		t.Fatalf("command = %q, want %q", resp.Command, want)
	}
	if resp.Expires == "" {
		t.Fatal("expires must be set")
	}

	// The QR must actually be a valid, decodable PNG (not just non-empty
	// bytes) — an invite nobody can scan is worse than no QR at all.
	raw, err := base64.StdEncoding.DecodeString(resp.QRPNGBase64)
	if err != nil {
		t.Fatalf("qr_png_base64 is not valid base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("qr_png_base64 does not decode as PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() < 100 || b.Dy() < 100 {
		t.Fatalf("QR image implausibly small: %v", b)
	}
}

func TestJoinLinkRequiresAdminKey(t *testing.T) {
	g := newTestGateway(t)
	g.MintJoinToken = func(time.Duration) (string, time.Time, error) { return "t", time.Now(), nil }
	g.JoinInfo = func() (string, string, string) { return "s", "a", "u" }

	req := httptest.NewRequest(http.MethodPost, "/join-link", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no admin key: status %d, want 403", rec.Code)
	}
}

// A non-founding node (no CA, so no MintJoinToken/JoinInfo wired) reports
// this cleanly instead of panicking on a nil func call.
func TestJoinLinkUnavailableOnNonFoundingNode(t *testing.T) {
	g := newTestGateway(t)
	req := httptest.NewRequest(http.MethodPost, "/join-link", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestJoinLinkMintFailure(t *testing.T) {
	g := newTestGateway(t)
	g.MintJoinToken = func(time.Duration) (string, time.Time, error) {
		return "", time.Time{}, errors.New("mint failed")
	}
	g.JoinInfo = func() (string, string, string) { return "s", "a", "u" }

	req := httptest.NewRequest(http.MethodPost, "/join-link", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
}
