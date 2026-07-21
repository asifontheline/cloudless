package pki

import (
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"
)

// L1 backfill: the mutual-TLS handshake itself — the security-critical
// surface that was previously untested (ServerTLS/ClientTLS/revocation).

// enrollNode mints a fresh keypair and CA-signed cert for a node under a
// shared CA directory, mirroring what SelfIssue/enrollment do in practice.
func enrollNode(t *testing.T, caDir, nodeDir, name string) {
	t.Helper()
	pub, err := NewNodeKey(nodeDir)
	if err != nil {
		t.Fatal(err)
	}
	certDER, err := SignPubKey(caDir, name, pub)
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := CACertPEM(caDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveNodeCredsWithCA(nodeDir, certDER, caPEM); err != nil {
		t.Fatal(err)
	}
}

func echoServer(t *testing.T, tlsCfg *tls.Config) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 64)
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				conn.Write(buf[:n])
			}()
		}
	}()
	return ln.Addr().String()
}

func dial(addr string, tlsCfg *tls.Config) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		return err
	}
	buf := make([]byte, 64)
	_, err = conn.Read(buf)
	return err
}

// Two nodes enrolled under the same CA complete a mutual-TLS handshake.
func TestMutualTLSHandshakeSucceeds(t *testing.T) {
	caDir := t.TempDir()
	if err := EnsureCA(caDir); err != nil {
		t.Fatal(err)
	}
	serverDir, clientDir := t.TempDir(), t.TempDir()
	enrollNode(t, caDir, serverDir, "server-node")
	enrollNode(t, caDir, clientDir, "client-node")

	serverTLS, err := ServerTLS(serverDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS, err := ClientTLS(clientDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	addr := echoServer(t, serverTLS)
	if err := dial(addr, clientTLS); err != nil {
		t.Fatalf("handshake between two CA-enrolled nodes must succeed: %v", err)
	}
}

// A client presenting a cert from a different (unrelated) CA is rejected.
func TestForeignCARejected(t *testing.T) {
	caDir := t.TempDir()
	EnsureCA(caDir)
	serverDir := t.TempDir()
	enrollNode(t, caDir, serverDir, "server-node")

	foreignCA := t.TempDir()
	EnsureCA(foreignCA)
	foreignClient := t.TempDir()
	enrollNode(t, foreignCA, foreignClient, "impostor")

	serverTLS, _ := ServerTLS(serverDir, nil)
	clientTLS, _ := ClientTLS(foreignClient, nil)
	addr := echoServer(t, serverTLS)
	if err := dial(addr, clientTLS); err == nil {
		t.Fatal("a cert from a different cluster CA must be rejected")
	}
}

// A revoked node's certificate is refused even though it is otherwise valid
// and signed by the same CA — A4's enforcement point.
func TestRevokedNodeRejected(t *testing.T) {
	caDir := t.TempDir()
	EnsureCA(caDir)
	serverDir, clientDir := t.TempDir(), t.TempDir()
	enrollNode(t, caDir, serverDir, "server-node")
	enrollNode(t, caDir, clientDir, "evicted-node")

	revoked := func(cn string) bool { return cn == "evicted-node" }
	serverTLS, _ := ServerTLS(serverDir, revoked)
	clientTLS, _ := ClientTLS(clientDir, nil)
	addr := echoServer(t, serverTLS)
	if err := dial(addr, clientTLS); err == nil {
		t.Fatal("a revoked node's certificate must be rejected by the server")
	}
}

// A server presenting a revoked cert is refused by a client that checks
// revocation too — enforcement works in both directions.
func TestClientRejectsRevokedServer(t *testing.T) {
	caDir := t.TempDir()
	EnsureCA(caDir)
	serverDir, clientDir := t.TempDir(), t.TempDir()
	enrollNode(t, caDir, serverDir, "evicted-server")
	enrollNode(t, caDir, clientDir, "client-node")

	revoked := func(cn string) bool { return cn == "evicted-server" }
	serverTLS, _ := ServerTLS(serverDir, nil)
	clientTLS, _ := ClientTLS(clientDir, revoked)
	addr := echoServer(t, serverTLS)
	if err := dial(addr, clientTLS); err == nil {
		t.Fatal("a client must refuse a revoked peer's server certificate")
	}
}

// A client presenting no certificate at all is refused (mutual TLS is
// mandatory, not opportunistic).
func TestNoClientCertRejected(t *testing.T) {
	caDir := t.TempDir()
	EnsureCA(caDir)
	serverDir := t.TempDir()
	enrollNode(t, caDir, serverDir, "server-node")
	serverTLS, _ := ServerTLS(serverDir, nil)
	addr := echoServer(t, serverTLS)

	bare := &tls.Config{InsecureSkipVerify: true} // no client cert presented
	if err := dial(addr, bare); err == nil {
		t.Fatal("connection without any client certificate must be rejected")
	}
}

// ServerTLS/ClientTLS fail cleanly when node credentials don't exist yet.
func TestTLSConfigMissingCreds(t *testing.T) {
	if _, err := ServerTLS(t.TempDir(), nil); err == nil {
		t.Fatal("ServerTLS without creds must error")
	}
	if _, err := ClientTLS(t.TempDir(), nil); err == nil {
		t.Fatal("ClientTLS without creds must error")
	}
}

// caPool errors cleanly on a corrupt CA file instead of a nil-pointer panic.
func TestCAPoolBadCert(t *testing.T) {
	dir := t.TempDir()
	if err := writePEMFile(t, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := caPool(dir); err == nil {
		t.Fatal("garbage CA PEM must be rejected")
	}
}

func writePEMFile(t *testing.T, dir string) error {
	t.Helper()
	return writePEM(dir+"/"+caCertFile, "CERTIFICATE", []byte("not a real cert"), 0o644)
}

// Ensure a stream survives past the handshake — the echo round-trips.
func TestHandshakeStreamWorks(t *testing.T) {
	caDir := t.TempDir()
	EnsureCA(caDir)
	serverDir, clientDir := t.TempDir(), t.TempDir()
	enrollNode(t, caDir, serverDir, "s")
	enrollNode(t, caDir, clientDir, "c")
	serverTLS, _ := ServerTLS(serverDir, nil)
	clientTLS, _ := ClientTLS(clientDir, nil)
	addr := echoServer(t, serverTLS)

	conn, err := tls.Dial("tcp", addr, clientTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("hello"))
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "hello" {
		t.Fatalf("echo mismatch: %q err=%v", buf, err)
	}
}
