package gossip

import (
	"net"
	"strconv"
	"testing"
	"time"

	"cloudless/internal/registry"
)

// L1 backfill: real memberlist nodes over loopback — the encrypted gossip
// membership layer had zero test coverage despite backing A3 (encrypted
// mesh) and every node-discovery path the registry depends on.

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

const secret = "0123456789abcdef0123456789abcdef" // 32 bytes, test-only

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("condition never became true")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A joining node's backend is discovered by the seed's registry, and vice
// versa — the mesh discovery path Ranked()/routing depends on.
func TestJoinPopulatesRegistry(t *testing.T) {
	seedReg := registry.New(nil, time.Hour, nil)
	seedPort := freePort(t)
	seed, err := Start(Options{
		NodeName: "seed", BindAddr: "127.0.0.1:" + strconv.Itoa(seedPort),
		BackendURL: "http://seed-backend", Secret: []byte(secret),
	}, seedReg)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Leave()

	joinerReg := registry.New(nil, time.Hour, nil)
	joinerPort := freePort(t)
	joiner, err := Start(Options{
		NodeName: "joiner", BindAddr: "127.0.0.1:" + strconv.Itoa(joinerPort),
		BackendURL: "http://joiner-backend", Secret: []byte(secret),
		Join: []string{"127.0.0.1:" + strconv.Itoa(seedPort)},
	}, joinerReg)
	if err != nil {
		t.Fatal(err)
	}
	defer joiner.Leave()

	waitFor(t, 10*time.Second, func() bool {
		for _, s := range seedReg.Ranked() {
			if s.Backend.Name == "joiner" && s.Backend.BaseURL == "http://joiner-backend" {
				return true
			}
		}
		return false
	})
	waitFor(t, 10*time.Second, func() bool {
		for _, s := range joinerReg.Ranked() {
			if s.Backend.Name == "seed" {
				return true
			}
		}
		return false
	})
}

// A node with the wrong secret cannot join the encrypted mesh (A3): the
// seed's registry never learns about it.
func TestWrongSecretCannotJoin(t *testing.T) {
	seedReg := registry.New(nil, time.Hour, nil)
	seedPort := freePort(t)
	seed, err := Start(Options{
		NodeName: "seed", BindAddr: "127.0.0.1:" + strconv.Itoa(seedPort),
		BackendURL: "http://seed-backend", Secret: []byte(secret),
	}, seedReg)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Leave()

	wrongReg := registry.New(nil, time.Hour, nil)
	wrongPort := freePort(t)
	wrong, err := Start(Options{
		NodeName: "impostor", BindAddr: "127.0.0.1:" + strconv.Itoa(wrongPort),
		BackendURL: "http://impostor-backend", Secret: []byte("ffffffffffffffffffffffffffffffff"),
		Join: []string{"127.0.0.1:" + strconv.Itoa(seedPort)},
	}, wrongReg)
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Leave()

	time.Sleep(1 * time.Second) // let any (failed) join attempt settle
	for _, s := range seedReg.Ranked() {
		if s.Backend.Name == "impostor" {
			t.Fatal("a node with the wrong cluster secret must not join the mesh")
		}
	}
}

// A node that leaves cleanly is dropped from peers' registries.
func TestLeaveRemovesFromRegistry(t *testing.T) {
	seedReg := registry.New(nil, time.Hour, nil)
	seedPort := freePort(t)
	seed, err := Start(Options{
		NodeName: "seed", BindAddr: "127.0.0.1:" + strconv.Itoa(seedPort),
		BackendURL: "http://seed-backend", Secret: []byte(secret),
	}, seedReg)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Leave()

	joinerReg := registry.New(nil, time.Hour, nil)
	joinerPort := freePort(t)
	joiner, err := Start(Options{
		NodeName: "joiner", BindAddr: "127.0.0.1:" + strconv.Itoa(joinerPort),
		BackendURL: "http://joiner-backend", Secret: []byte(secret),
		Join: []string{"127.0.0.1:" + strconv.Itoa(seedPort)},
	}, joinerReg)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, 10*time.Second, func() bool {
		for _, s := range seedReg.Ranked() {
			if s.Backend.Name == "joiner" {
				return true
			}
		}
		return false
	})
	joiner.Leave()
	waitFor(t, 10*time.Second, func() bool {
		for _, s := range seedReg.Ranked() {
			if s.Backend.Name == "joiner" {
				return false
			}
		}
		return true
	})
}

// A revocation broadcast from one node reaches peers and invokes their
// OnRevoke callback — A4's mesh-wide propagation.
func TestBroadcastRevokeReachesPeers(t *testing.T) {
	seedReg := registry.New(nil, time.Hour, nil)
	seedPort := freePort(t)
	seed, err := Start(Options{
		NodeName: "seed", BindAddr: "127.0.0.1:" + strconv.Itoa(seedPort),
		BackendURL: "http://seed-backend", Secret: []byte(secret),
	}, seedReg)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Leave()

	joinerReg := registry.New(nil, time.Hour, nil)
	joinerPort := freePort(t)
	revoked := make(chan string, 1)
	joiner, err := Start(Options{
		NodeName: "joiner", BindAddr: "127.0.0.1:" + strconv.Itoa(joinerPort),
		BackendURL: "http://joiner-backend", Secret: []byte(secret),
		Join:     []string{"127.0.0.1:" + strconv.Itoa(seedPort)},
		OnRevoke: func(name string) { revoked <- name },
	}, joinerReg)
	if err != nil {
		t.Fatal(err)
	}
	defer joiner.Leave()

	waitFor(t, 10*time.Second, func() bool {
		for _, s := range joinerReg.Ranked() {
			if s.Backend.Name == "seed" {
				return true
			}
		}
		return false
	})

	seed.BroadcastRevoke("bad-node")
	select {
	case name := <-revoked:
		if name != "bad-node" {
			t.Fatalf("revoked name = %q, want bad-node", name)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("revocation broadcast never reached the peer")
	}
}

// A node joining without valid metadata (Meta unmarshal failure or empty
// BackendURL) is not upserted into the registry — a malformed peer cannot
// silently take a routing slot.
func TestJoinWithoutBackendURLIgnored(t *testing.T) {
	seedReg := registry.New(nil, time.Hour, nil)
	seedPort := freePort(t)
	seed, err := Start(Options{
		NodeName: "seed", BindAddr: "127.0.0.1:" + strconv.Itoa(seedPort),
		BackendURL: "http://seed-backend", Secret: []byte(secret),
	}, seedReg)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Leave()

	joinerReg := registry.New(nil, time.Hour, nil)
	joinerPort := freePort(t)
	joiner, err := Start(Options{
		NodeName: "no-backend", BindAddr: "127.0.0.1:" + strconv.Itoa(joinerPort),
		BackendURL: "", // no advertised endpoint
		Secret:     []byte(secret),
		Join:       []string{"127.0.0.1:" + strconv.Itoa(seedPort)},
	}, joinerReg)
	if err != nil {
		t.Fatal(err)
	}
	defer joiner.Leave()

	time.Sleep(1 * time.Second)
	for _, s := range seedReg.Ranked() {
		if s.Backend.Name == "no-backend" {
			t.Fatal("a node without a backend URL must not be upserted into routing")
		}
	}
}
