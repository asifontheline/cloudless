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
	srv := httptest.NewServer(relay.NewServer("", &relay.BlobSet{List: list, Path: st.Path, Add: add}, nil, nil).Handler())
	t.Cleanup(srv.Close)
	return &fakePeer{name: name, st: st, srv: srv}
}

func (p *fakePeer) peer(loc string) Peer {
	return Peer{Name: p.name, BaseURL: p.srv.URL, Location: loc}
}

const gguf = "GGUF\x00\x00\x00\x00tensor-bytes"

func newManager(t *testing.T, peers ...Peer) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	list := func() []Blob {
		out := []Blob{}
		for _, e := range st.List() {
			out = append(out, Blob{Name: e.Name, SHA256: e.SHA256})
		}
		return out
	}
	return &Manager{
		Target: 3, Self: "self", Location: "eu/de/berlin",
		List: list, Path: st.Path,
		Client: &http.Client{Timeout: 5 * time.Second},
		Peers:  func() []Peer { return peers },
	}, st
}

// M1: a write is replicated to the desired holders before it is acknowledged.
func TestAckWriteMeetsDurabilityTarget(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	p2 := newFakePeer(t, "p2")
	m, st := newManager(t, p1.peer("eu/fr/paris"), p2.peer("us/ny/nyc"))
	if _, err := st.Add("m.gguf", strings.NewReader(gguf)); err != nil {
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
	m, st := newManager(t, p1.peer("eu/fr/paris"))
	if _, err := st.Add("m.gguf", strings.NewReader(gguf)); err != nil {
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
	m, st := newManager(t, p1.peer("eu/fr/paris"))
	if _, err := st.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	s := m.Scan(context.Background())[0]
	if s.Replicas != 2 || s.Domains != 2 {
		t.Fatalf("replicas=%d domains=%d, want 2/2", s.Replicas, s.Domains)
	}
}

// M2 acceptance: kill a node holding replicas; the next scan restores the
// replication factor on the surviving mesh.
func TestKillNodeFullReReplication(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	p2 := newFakePeer(t, "p2")
	m, st := newManager(t, p1.peer("eu/fr/paris"), p2.peer("us/ny/nyc"))
	m.Target = 2
	if _, err := st.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	// First scan reaches the target of 2 somewhere in the mesh.
	first := m.Scan(context.Background())[0]
	if first.Replicas < 2 {
		t.Fatalf("initial replication never reached target: %+v", first)
	}
	// Kill whichever peer holds the second copy; the mesh shrinks to the
	// other one. (If both held copies, kill p1.)
	var dead, alive *fakePeer = p1, p2
	if _, ok := p1.st.Path("m.gguf"); !ok {
		dead, alive = p2, p1
	}
	dead.srv.Close()
	if _, held := alive.st.Path("m.gguf"); held {
		t.Skip("both peers already hold copies — loss cannot be simulated")
	}
	m.Peers = func() []Peer { return []Peer{alive.peer("somewhere/else")} }

	// Self-healing: the next scan must restore N=2 on the survivors.
	s := m.Scan(context.Background())[0]
	if s.Replicas < 2 || !s.Healthy {
		t.Fatalf("re-replication after node loss failed: %+v", s)
	}
	if _, ok := alive.st.Path("m.gguf"); !ok {
		t.Fatal("surviving peer never received the repaired copy")
	}
	// The console's repair feed recorded the action.
	status := m.Status()
	if repairs, ok := status["repairs"].([]RepairAction); !ok || len(repairs) == 0 {
		t.Fatalf("repair activity missing from status: %+v", status["repairs"])
	}
}

// M4: an owner whose node lost its data rebuilds it from surviving
// replicas, hash-verified; what nobody holds is reported irrecoverable.
func TestRestoreFromSurvivingReplicas(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	if _, err := p1.st.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	m, st := newManager(t, p1.peer("eu/fr/paris"))
	m.Add = func(name string, r io.Reader) (string, error) {
		e, err := st.Add(name, r)
		return e.SHA256, err
	}
	m.Verify = func(name string) bool {
		ok, err := st.Verify(name)
		return err == nil && ok
	}
	got := m.Restore(context.Background(), nil)
	if len(got) != 1 || got[0].Outcome != "restored" || got[0].From != "p1" {
		t.Fatalf("restore = %+v, want m.gguf restored from p1", got)
	}
	if ok, err := st.Verify("m.gguf"); err != nil || !ok {
		t.Fatalf("restored blob failed hash verification: %v", err)
	}
	// A second run reports the object present, not re-fetched.
	if got := m.Restore(context.Background(), nil); got[0].Outcome != "present" {
		t.Fatalf("second restore = %+v, want present", got)
	}
	// An object nobody holds is explicitly irrecoverable.
	got = m.Restore(context.Background(), []string{"gone.gguf"})
	if len(got) != 1 || got[0].Outcome != "irrecoverable" {
		t.Fatalf("lost object = %+v, want explicit irrecoverable", got)
	}
}

// M6: durability is measured from live state — survives-k is the weakest
// object's spare copies, and repair timings appear once a repair happened.
func TestDurabilityMeasured(t *testing.T) {
	p1 := newFakePeer(t, "p1")
	m, st := newManager(t, p1.peer("eu/fr/paris"))
	m.Target = 2
	if _, err := st.Add("m.gguf", strings.NewReader(gguf)); err != nil {
		t.Fatal(err)
	}
	m.Scan(context.Background()) // repairs to p1
	d, ok := m.Status()["durability"].(map[string]any)
	if !ok {
		t.Fatal("durability block missing from status")
	}
	if d["objects"] != 1 || d["at_target"] != 1 {
		t.Fatalf("objects/at_target wrong: %+v", d)
	}
	if d["survives_loss"] != 1 {
		t.Fatalf("2 replicas must survive 1 loss, got %v", d["survives_loss"])
	}
	if _, ok := d["repair_median_ms"]; !ok {
		t.Fatalf("a repair ran but no observed repair time reported: %+v", d)
	}
	// No objects → nothing to claim.
	empty, _ := newManager(t)
	empty.Scan(context.Background())
	de := empty.Status()["durability"].(map[string]any)
	if de["objects"] != 0 || de["survives_loss"] != 0 {
		t.Fatalf("empty mesh must claim nothing: %+v", de)
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
