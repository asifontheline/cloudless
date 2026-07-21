package relay

import (
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"time"

	"cloudless/internal/pki"
)

// L7 backfill: revoked identity is refused by the relay's own mutual-TLS
// serving stack (relay.ListenAndServe wires pki.ServerTLS + relay.Handler),
// not just at the pki primitive level.

func enrollRelayNode(t *testing.T, caDir, nodeDir, name string) {
	t.Helper()
	pub, err := pki.NewNodeKey(nodeDir)
	if err != nil {
		t.Fatal(err)
	}
	certDER, err := pki.SignPubKey(caDir, name, pub)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := pki.CACertPEM(caDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := pki.SaveNodeCredsWithCA(nodeDir, certDER, caPEM); err != nil {
		t.Fatal(err)
	}
}

// serveRelay starts the relay's real Handler behind mutual TLS built the
// same way ListenAndServe builds it, on an OS-assigned port, and returns its
// address.
func serveRelay(t *testing.T, pkiDir, backendURL string, revoked pki.RevokedFn) string {
	t.Helper()
	tlsCfg, err := pki.ServerTLS(pkiDir, revoked)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	srv := &http.Server{Handler: NewServer(backendURL, nil, nil, func() int { return 1 }).Handler()}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

// dialRelay must exchange bytes, not just complete Dial: with TLS 1.3 the
// client's handshake can succeed locally before the server's rejection of a
// revoked client certificate surfaces, so the failure only appears on the
// first read/write.
func dialRelay(addr string, clientTLS *tls.Config) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr, clientTLS)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET / HTTP/1.0\r\n\r\n")); err != nil {
		return err
	}
	buf := make([]byte, 64)
	_, err = conn.Read(buf)
	return err
}

// A node whose certificate is revoked cannot complete the mutual-TLS
// handshake against the relay, even though its certificate is otherwise
// validly signed by the cluster CA.
func TestRelayRejectsRevokedPeer(t *testing.T) {
	caDir := t.TempDir()
	if err := pki.EnsureCA(caDir); err != nil {
		t.Fatal(err)
	}

	serverDir := t.TempDir()
	enrollRelayNode(t, caDir, serverDir, "relay-server")

	goodDir := t.TempDir()
	enrollRelayNode(t, caDir, goodDir, "good-peer")

	evictedDir := t.TempDir()
	enrollRelayNode(t, caDir, evictedDir, "evicted-peer")

	revoked := func(cn string) bool { return cn == "evicted-peer" }
	addr := serveRelay(t, serverDir, "http://unused.invalid", revoked)

	goodTLS, err := pki.ClientTLS(goodDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := dialRelay(addr, goodTLS); err != nil {
		t.Fatalf("a non-revoked peer must complete the handshake, got: %v", err)
	}

	evictedTLS, err := pki.ClientTLS(evictedDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := dialRelay(addr, evictedTLS); err == nil {
		t.Fatal("a revoked peer's certificate must be rejected by the relay")
	}
}

// Revocation is enforced from the moment the relay starts serving — a
// revoked node was never able to reach the proxy handler in the first
// place, not merely turned away after the fact.
func TestRelayRevokedPeerNeverReachesHandler(t *testing.T) {
	caDir := t.TempDir()
	if err := pki.EnsureCA(caDir); err != nil {
		t.Fatal(err)
	}

	serverDir := t.TempDir()
	enrollRelayNode(t, caDir, serverDir, "relay-server")

	evictedDir := t.TempDir()
	enrollRelayNode(t, caDir, evictedDir, "evicted-peer")

	revoked := func(cn string) bool { return cn == "evicted-peer" }
	addr := serveRelay(t, serverDir, "http://unused.invalid", revoked)

	evictedTLS, err := pki.ClientTLS(evictedDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: evictedTLS},
		Timeout:   3 * time.Second,
	}
	resp, err := client.Get("https://" + addr + "/v1/models")
	if err == nil {
		resp.Body.Close()
		t.Fatal("a revoked peer's request must never reach the relay handler")
	}
}
