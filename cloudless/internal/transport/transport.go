// Package transport is the pluggable seam for how cloudless nodes actually
// carry bytes to each other — both gossip membership traffic and the
// mutual-TLS relay/gateway peer connections. Every node today talks over
// raw TCP, and that stays the default, but neither the gossip nor the
// relay/gateway code needs to hardcode that: they dial and listen through
// this interface, so a future medium (Bluetooth/BLE, LoRa, USB/serial — see
// BACKLOG T2/T3/T5) can implement Transport and be swapped in via Default
// without touching mesh membership or routing logic (T1, #141).
package transport

import (
	"context"
	"net"
)

// Dialer opens an outbound connection to addr over network (e.g. "tcp").
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Listener accepts inbound connections on addr over network.
type Listener interface {
	Listen(network, addr string) (net.Listener, error)
}

// Transport is the full pluggable medium: both directions of a link.
type Transport interface {
	Dialer
	Listener
}

// TCP is the transport every node uses today — a thin pass-through to the
// standard library, so wiring it in changes nothing about current behavior.
type TCP struct {
	Dialer net.Dialer
}

func (t TCP) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return t.Dialer.DialContext(ctx, network, addr)
}

func (TCP) Listen(network, addr string) (net.Listener, error) {
	return net.Listen(network, addr)
}

// Default is the transport used unless a caller overrides it — the actual
// plug-in point for an alternate medium.
var Default Transport = TCP{}
