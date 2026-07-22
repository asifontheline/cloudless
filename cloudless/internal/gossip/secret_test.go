package gossip

import (
	"strconv"
	"testing"
	"time"

	"cloudless/internal/registry"
)

// Reconciled from PR #135 (an earlier, unmerged pass at this same package):
// A3's encrypted-mesh guarantee had no coverage — a node with the wrong
// cluster secret must be unable to join at all, not just be treated as an
// ordinary unreachable peer.

const testSecret = "0123456789abcdef0123456789abcdef" // 32 bytes, test-only

func TestWrongSecretCannotJoin(t *testing.T) {
	seedReg := registry.New(nil, time.Hour, nil)
	seedPort := freePort(t)
	seed, err := Start(Options{
		NodeName: "seed", BindAddr: "127.0.0.1:" + strconv.Itoa(seedPort),
		BackendURL: "http://seed-backend", Secret: []byte(testSecret),
	}, seedReg)
	if err != nil {
		t.Fatal(err)
	}
	defer seed.Leave()

	impostorReg := registry.New(nil, time.Hour, nil)
	impostorPort := freePort(t)
	impostor, err := Start(Options{
		NodeName: "impostor", BindAddr: "127.0.0.1:" + strconv.Itoa(impostorPort),
		BackendURL: "http://impostor-backend", Secret: []byte("ffffffffffffffffffffffffffffffff"),
		Join: []string{"127.0.0.1:" + strconv.Itoa(seedPort)},
	}, impostorReg)
	if err != nil {
		t.Fatal(err)
	}
	defer impostor.Leave()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range seedReg.Ranked() {
			if s.Backend.Name == "impostor" {
				t.Fatal("a node with the wrong cluster secret must not join the mesh")
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
