package registry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloudless/internal/config"
)

func modelServer(status int, delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(status)
		io.WriteString(w, `{"data":[]}`)
	}))
}

// L1: healthy backends rank fastest-first; unhealthy ones stay listed as a
// last resort behind every healthy one.
func TestRankedHealthyFastestFirst(t *testing.T) {
	r := New([]config.Backend{{Name: "slow"}, {Name: "fast"}, {Name: "down"}}, time.Hour, nil)
	r.record("slow", true, 80)
	r.record("fast", true, 5)
	r.record("down", false, 0)

	got := r.Ranked()
	want := []string{"fast", "slow", "down"}
	for i, name := range want {
		if got[i].Backend.Name != name {
			t.Fatalf("rank %d = %s, want %s (full: %v)", i, got[i].Backend.Name, name, got)
		}
	}
}

// Probing a live backend records health and a real latency; a 5xx or a dead
// server marks it unhealthy and counts consecutive failures.
func TestProbeRecordsHealthAndFailures(t *testing.T) {
	good := modelServer(http.StatusOK, 0)
	defer good.Close()
	bad := modelServer(http.StatusInternalServerError, 0)
	defer bad.Close()
	dead := modelServer(http.StatusOK, 0)
	dead.Close() // connection refused

	r := New([]config.Backend{
		{Name: "good", BaseURL: good.URL},
		{Name: "bad", BaseURL: bad.URL},
		{Name: "dead", BaseURL: dead.URL},
	}, time.Hour, nil)
	r.probeAll(context.Background())
	r.probeAll(context.Background())

	states := map[string]BackendState{}
	for _, s := range r.Ranked() {
		states[s.Backend.Name] = s
	}
	if !states["good"].Healthy || states["good"].Failures != 0 {
		t.Fatalf("good backend: %+v", states["good"])
	}
	if states["bad"].Healthy {
		t.Fatalf("5xx backend must be unhealthy: %+v", states["bad"])
	}
	if states["dead"].Healthy || states["dead"].Failures != 2 {
		t.Fatalf("dead backend must count consecutive failures: %+v", states["dead"])
	}
}

// A backend that recovers is healthy again and its failure count resets.
func TestProbeRecovery(t *testing.T) {
	srv := modelServer(http.StatusOK, 0)
	defer srv.Close()
	r := New([]config.Backend{{Name: "n", BaseURL: srv.URL}}, time.Hour, nil)
	r.record("n", false, 0)
	r.record("n", false, 0)
	r.probeAll(context.Background())
	s := r.Ranked()[0]
	if !s.Healthy || s.Failures != 0 {
		t.Fatalf("recovered backend must reset failures: %+v", s)
	}
}

// Upsert updates a rejoining node's URL in place and adds unknown nodes;
// Remove drops a departed node from routing entirely.
func TestUpsertAndRemove(t *testing.T) {
	r := New([]config.Backend{{Name: "a", BaseURL: "http://old"}}, time.Hour, nil)
	r.record("a", true, 7)
	r.Upsert(config.Backend{Name: "a", BaseURL: "http://new"})
	r.Upsert(config.Backend{Name: "b", BaseURL: "http://b"})

	states := map[string]BackendState{}
	for _, s := range r.Ranked() {
		states[s.Backend.Name] = s
	}
	if states["a"].Backend.BaseURL != "http://new" {
		t.Fatalf("rejoined node must keep its state but update its URL: %+v", states["a"])
	}
	if !states["a"].Healthy {
		t.Fatal("upsert must not reset health state")
	}
	if _, ok := states["b"]; !ok {
		t.Fatal("gossip-discovered node missing")
	}
	r.Remove("a")
	for _, s := range r.Ranked() {
		if s.Backend.Name == "a" {
			t.Fatal("removed node still routable")
		}
	}
}

// The probe honors context cancellation — a wedged backend cannot hang Run.
func TestProbeContextCancel(t *testing.T) {
	slow := modelServer(http.StatusOK, 5*time.Second)
	defer slow.Close()
	r := New([]config.Backend{{Name: "slow", BaseURL: slow.URL}}, time.Hour, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { r.probeAll(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("probe ignored context cancellation")
	}
	if r.Ranked()[0].Healthy {
		t.Fatal("timed-out probe must mark the backend unhealthy")
	}
}
