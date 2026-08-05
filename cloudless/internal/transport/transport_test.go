package transport

import (
	"context"
	"io"
	"testing"
)

// T1: the default TCP transport must behave exactly like plain net.Listen/
// net.Dialer.DialContext — it's a pass-through, not a reimplementation, so
// switching every caller onto it must not change observable behavior.

func TestTCPListenAndDialRoundTrip(t *testing.T) {
	tr := TCP{}
	ln, err := tr.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	accepted := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("hello"))
		close(accepted)
	}()

	conn, err := tr.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("got %q, want %q", buf, "hello")
	}
	<-accepted
}

func TestDefaultIsTCP(t *testing.T) {
	if _, ok := Default.(TCP); !ok {
		t.Errorf("Default should be TCP unless overridden, got %T", Default)
	}
}

// A fake transport, standing in for a future alternate medium (T2/T3/T5),
// proves Transport is a real interface seam — anything satisfying Dialer +
// Listener can be assigned to Default without cloudless/internal/gateway,
// internal/registry, or internal/relay changing a line.
type fakeTransport struct{ TCP }

func TestAlternateTransportSatisfiesInterface(t *testing.T) {
	var tr Transport = fakeTransport{}
	if tr == nil {
		t.Fatal("fakeTransport should satisfy Transport")
	}
}
