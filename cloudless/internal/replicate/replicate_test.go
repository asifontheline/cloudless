package replicate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudless/internal/relay"
	"cloudless/internal/store"
)

// fakePeer is a real store behind a real relay handler — what a mesh peer
// actually serves, minus the mutual TLS.
type fakePeer struct {
	name string
	st   *store.Store
	srv  *httptest.Server
}

func newFakePeer(t *testing.T, name string) *fakePeer {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	list := func() []relay.Entry {
		out := []relay.Entry{}
		for _, e := range st.List() {
			out = append(out, relay.Entry{Name: e.Name, SHA256: e.SHA256, Size: e.Size, Format: e.Format})
		}
		return out
	}
	add := func(n string, r io.Reader) error { _, err := st.Add(n, r); return err }
	srv := httptest.NewServer(relay.NewServer("", list, st.Path, add, nil).Handler())
	t.Cleanup(srv.Close)
	return &fakePeer{name: name, st: st, srv: srv}
}

func (p *fakePeer) peer(loc string) Peer {
	return Peer{Name: p.name, BaseURL: p.srv.URL, Location: loc}
}

const gguf = "GGUF\x00\x00\x00\x00tensor-bytes"

func newManager(t *testing.T, peers ...Peer) *Manager {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{
		Target: 3, Self: "self", Location: "eu/de/berlin",
		Store:  st,
		Client: &http.Client{Timeout: 5 * time.Second},
		Peers:  func() []Peer { return peers },
	}
}

// M1: a write is replicated to the desired holders before it is acknowledged.
func TestAckWriteMeetsDurabilityTarget(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	p2 := newFakePeer(t, "p2")
	m := newManager(t, p1.peer("eu/fr/paris"), p2.peer("us/ny/nyc"))
	if _, err := m.Store.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	replicas, holders := m.AckWrite(context.Background(), "m.gguf")
	if replicas != 3 {
		t.Fatalf("replicas = %d, want 3 (holders %v)", replicas, holders)
	}
	for _, p := range []*fakePeer{p1, p2} {
		if _, ok := p.st.Path("m.gguf"); !ok {
			t.Fatalf("peer %s did not receive the replica", p.name)
		}
		if ok, err := p.st.Verify("m.gguf"); err != nil || !ok {
			t.Fatalf("peer %s replica failed verification: %v", p.name, err)
		}
	}
}

// Self-healing: a scan pushes under-replicated artifacts to fresh nodes.
func TestScanRepairsUnderReplication(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	m := newManager(t, p1.peer("eu/fr/paris"))
	if _, err := m.Store.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	status := m.Scan(context.Background())
	if len(status) != 1 {
		t.Fatalf("status entries = %d, want 1", len(status))
	}
	s := status[0]
	// Mesh of 2 (self + p1): reachable target is 2, and repair got us there.
	if s.Replicas != 2 || !s.Healthy {
		t.Fatalf("replicas=%d healthy=%v, want 2/true: %+v", s.Replicas, s.Healthy, s)
	}
	if _, ok := p1.st.Path("m.gguf"); !ok {
		t.Fatal("scan did not push the missing replica")
	}
}

// A peer that already holds the artifact is not pushed to again; the survey
// counts it as a holder.
func TestScanCountsExistingReplicas(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	if _, err := p1.st.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	m := newManager(t, p1.peer("eu/fr/paris"))
	if _, err := m.Store.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	s := m.Scan(context.Background())[0]
	if s.Replicas != 2 || s.Domains != 2 {
		t.Fatalf("replicas=%d domains=%d, want 2/2", s.Replicas, s.Domains)
	}
}

// Placement prefers distinct failure domains when the mesh allows it.
func TestDesiredSpreadsAcrossDomains(t *testing.T) {
	cands := []Peer{
		{Name: "a1", Location: "eu/de/berlin"},
		{Name: "a2", Location: "eu/de/berlin"},
		{Name: "b1", Location: "eu/fr/paris"},
		{Name: "c1", Location: "us/ny/nyc"},
	}
	got := desired("somesha", cands, 3)
	locs := map[string]bool{}
	for _, p := range got {
		if locs[p.Location] {
			t.Fatalf("duplicate domain %q in %v despite 3 distinct domains available", p.Location, got)
		}
		locs[p.Location] = true
	}
}

// When the mesh has fewer domains than the target, remaining slots still fill.
func TestDesiredFillsWhenDomainsScarce(t *testing.T) {
	cands := []Peer{
		{Name: "a1", Location: "eu/de/berlin"},
		{Name: "a2", Location: "eu/de/berlin"},
		{Name: "a3", Location: "eu/de/berlin"},
	}
	if got := desired("somesha", cands, 3); len(got) != 3 {
		t.Fatalf("got %d holders, want 3", len(got))
	}
}

// Every node must compute the same desired set (no coordinator).
func TestDesiredDeterministic(t *testing.T) {
	cands := []Peer{
		{Name: "a", Location: "x"}, {Name: "b", Location: "y"},
		{Name: "c", Location: "z"}, {Name: "d", Location: "x"},
	}
	shuffled := []Peer{cands[2], cands[0], cands[3], cands[1]}
	a := desired("sha", cands, 3)
	b := desired("sha", shuffled, 3)
	for i := range a {
		if a[i].Name != b[i].Name {
			t.Fatalf("order-dependent placement: %v vs %v", a, b)
		}
	}
}

// The relay's push endpoint enforces the store's format allowlist: a peer
// cannot plant a pickle-based file.
func TestPushRejectedByFormatAllowlist(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	req, _ := http.NewRequest(http.MethodPut, p1.srv.URL+"/store?name=evil.pkl", strings.NewReader("\x80\x04pickle"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("pickle push got %d, want 422", resp.StatusCode)
	}
	if _, ok := p1.st.Path("evil.pkl"); ok {
		t.Fatal("rejected artifact must not be stored")
	}
}
