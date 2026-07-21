package gossip

import (
	"encoding/json"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/memberlist"

	"cloudless/internal/config"
	"cloudless/internal/registry"
)

// L1 backfill: internal/gossip had no tests at all. The delegate/events glue
// is what actually wires memberlist into the mesh's routing table and
// revocation propagation (A4) — it's unit-tested here directly, plus a real
// two-node Join() over loopback to prove peers converge on each other.

func TestDelegateNotifyMsgAppliesRevoke(t *testing.T) {
	var got string
	d := &delegate{onApply: func(name string) { got = name }}

	msg, _ := json.Marshal(revokeMsg{Type: "revoke", Name: "bad-node"})
	d.NotifyMsg(msg)

	if got != "bad-node" {
		t.Fatalf("onApply not called with revoked name, got %q", got)
	}
}

func TestDelegateNotifyMsgIgnoresGarbage(t *testing.T) {
	called := false
	d := &delegate{onApply: func(string) { called = true }}

	d.NotifyMsg(nil)
	d.NotifyMsg([]byte("not json"))
	d.NotifyMsg([]byte(`{"type":"not-revoke","name":"x"}`))
	d.NotifyMsg([]byte(`{"type":"revoke","name":""}`))

	if called {
		t.Fatal("onApply must not fire on malformed or non-revoke messages")
	}
}

func TestDelegateNodeMetaRespectsLimit(t *testing.T) {
	d := &delegate{meta: []byte(`{"backend_url":"http://x"}`)}

	if got := d.NodeMeta(1000); string(got) != string(d.meta) {
		t.Fatalf("meta under limit should be returned as-is, got %q", got)
	}
	if got := d.NodeMeta(2); got != nil {
		t.Fatalf("meta over limit must be dropped, got %q", got)
	}
}

func TestDelegateGetBroadcastsNilQueue(t *testing.T) {
	d := &delegate{}
	if got := d.GetBroadcasts(0, 100); got != nil {
		t.Fatalf("nil broadcast queue must yield no broadcasts, got %v", got)
	}
}

func TestEventsNotifyJoinUpsertsBackend(t *testing.T) {
	reg := registry.New(nil, time.Hour, nil)
	e := &events{reg: reg, self: "self"}

	meta, _ := json.Marshal(Meta{BackendURL: "http://peer:8080", Location: "us/ca"})
	e.NotifyJoin(&memberlist.Node{Name: "peer", Meta: meta})

	found := false
	for _, s := range reg.Ranked() {
		if s.Backend.Name == "peer" && s.Backend.BaseURL == "http://peer:8080" && s.Backend.Location == "us/ca" {
			found = true
		}
	}
	if !found {
		t.Fatal("peer with valid metadata must be upserted into the registry")
	}
}

func TestEventsNotifyJoinIgnoresSelf(t *testing.T) {
	reg := registry.New(nil, time.Hour, nil)
	e := &events{reg: reg, self: "self"}

	meta, _ := json.Marshal(Meta{BackendURL: "http://self:8080"})
	e.NotifyJoin(&memberlist.Node{Name: "self", Meta: meta})

	if len(reg.Ranked()) != 0 {
		t.Fatal("a node must not add itself to its own registry")
	}
}

func TestEventsNotifyJoinIgnoresUnusableMeta(t *testing.T) {
	reg := registry.New(nil, time.Hour, nil)
	e := &events{reg: reg, self: "self"}

	e.NotifyJoin(&memberlist.Node{Name: "peer", Meta: []byte("not json")})
	e.NotifyJoin(&memberlist.Node{Name: "peer2", Meta: nil})

	if len(reg.Ranked()) != 0 {
		t.Fatal("peers without a usable backend URL must not be added")
	}
}

func TestEventsNotifyLeaveRemovesBackend(t *testing.T) {
	reg := registry.New([]config.Backend{{Name: "peer", BaseURL: "http://peer:8080"}}, time.Hour, nil)
	e := &events{reg: reg, self: "self"}

	e.NotifyLeave(&memberlist.Node{Name: "peer"})

	for _, s := range reg.Ranked() {
		if s.Backend.Name == "peer" {
			t.Fatal("peer must be removed from the registry on leave")
		}
	}
}

func TestEventsNotifyLeaveIgnoresSelf(t *testing.T) {
	reg := registry.New([]config.Backend{{Name: "self", BaseURL: "http://self:8080"}}, time.Hour, nil)
	e := &events{reg: reg, self: "self"}

	e.NotifyLeave(&memberlist.Node{Name: "self"})

	found := false
	for _, s := range reg.Ranked() {
		if s.Backend.Name == "self" {
			found = true
		}
	}
	if !found {
		t.Fatal("a node must not remove itself from its own registry on its own leave notification")
	}
}

// freePort finds an available loopback port for a gossip bind address.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// Two real nodes joining over loopback converge: each learns of the other
// and upserts it into its registry via gossip, not manual wiring.
func TestTwoNodesConverge(t *testing.T) {
	pa, pb := freePort(t), freePort(t)
	addrA := "127.0.0.1:" + strconv.Itoa(pa)
	addrB := "127.0.0.1:" + strconv.Itoa(pb)

	regA := registry.New(nil, time.Hour, nil)
	meshA, err := Start(Options{
		NodeName:   "a",
		BindAddr:   addrA,
		BackendURL: "http://backend-a:8080",
	}, regA)
	if err != nil {
		t.Fatalf("start a: %v", err)
	}
	defer meshA.Leave()

	regB := registry.New(nil, time.Hour, nil)
	meshB, err := Start(Options{
		NodeName:   "b",
		BindAddr:   addrB,
		Join:       []string{addrA},
		BackendURL: "http://backend-b:8080",
	}, regB)
	if err != nil {
		t.Fatalf("start b: %v", err)
	}
	defer meshB.Leave()

	deadline := time.Now().Add(5 * time.Second)
	for {
		aKnowsB, bKnowsA := false, false
		for _, s := range regA.Ranked() {
			if s.Backend.Name == "b" && s.Backend.BaseURL == "http://backend-b:8080" {
				aKnowsB = true
			}
		}
		for _, s := range regB.Ranked() {
			if s.Backend.Name == "a" && s.Backend.BaseURL == "http://backend-a:8080" {
				bKnowsA = true
			}
		}
		if aKnowsB && bKnowsA {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("nodes did not converge: a knows b=%v, b knows a=%v", aKnowsB, bKnowsA)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A revocation gossiped from one node is applied by its peer — the
// propagation half of A4 (the handshake-refusal half is covered in pki).
func TestBroadcastRevokePropagates(t *testing.T) {
	pa, pb := freePort(t), freePort(t)
	addrA := "127.0.0.1:" + strconv.Itoa(pa)
	addrB := "127.0.0.1:" + strconv.Itoa(pb)

	var revokedOnB string
	meshA, err := Start(Options{
		NodeName:   "a",
		BindAddr:   addrA,
		BackendURL: "http://backend-a:8080",
	}, registry.New(nil, time.Hour, nil))
	if err != nil {
		t.Fatalf("start a: %v", err)
	}
	defer meshA.Leave()

	meshB, err := Start(Options{
		NodeName:   "b",
		BindAddr:   addrB,
		Join:       []string{addrA},
		BackendURL: "http://backend-b:8080",
		OnRevoke:   func(name string) { revokedOnB = name },
	}, registry.New(nil, time.Hour, nil))
	if err != nil {
		t.Fatalf("start b: %v", err)
	}
	defer meshB.Leave()

	// Wait for the two-node membership to settle before broadcasting.
	deadline := time.Now().Add(5 * time.Second)
	for meshA.list.NumMembers() < 2 || meshB.list.NumMembers() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("membership did not settle before broadcast")
		}
		time.Sleep(50 * time.Millisecond)
	}

	meshA.BroadcastRevoke("c")

	deadline = time.Now().Add(5 * time.Second)
	for revokedOnB != "c" {
		if time.Now().After(deadline) {
			t.Fatalf("revocation did not propagate, got %q", revokedOnB)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestStartInvalidBindAddr(t *testing.T) {
	if _, err := Start(Options{NodeName: "x", BindAddr: "not-a-host-port"}, registry.New(nil, time.Hour, nil)); err == nil {
		t.Fatal("malformed bind address must fail to start")
	}
	if _, err := Start(Options{NodeName: "x", BindAddr: "127.0.0.1:not-a-port"}, registry.New(nil, time.Hour, nil)); err == nil {
		t.Fatal("non-numeric port must fail to start")
	}
}
