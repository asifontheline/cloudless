package registry

import (
	"testing"
	"time"

	"cloudless/internal/config"
)

// I4: locality-aware routing. Among equally healthy backends, prefer ones
// whose hierarchical location shares more of this node's own prefix —
// health always wins over locality, locality wins over raw latency.

func TestLocalityDepth(t *testing.T) {
	cases := []struct {
		ref, loc string
		want     int
	}{
		{"na/us/ca/sf/village-a", "na/us/ca/sf/village-a", 5},
		{"na/us/ca/sf/village-a", "na/us/ca/sf/village-b", 4},
		{"na/us/ca/sf/village-a", "na/us/ca/la/village-c", 3},
		{"na/us/ca/sf/village-a", "na/us/ny/nyc/village-d", 2},
		{"na/us/ca/sf/village-a", "na/eu/de/berlin/village-e", 1},
		{"na/us/ca/sf/village-a", "sa/br/sp/village-f", 0},
		{"", "na/us/ca/sf/village-a", 0},
		{"na/us/ca/sf/village-a", "", 0},
		{"", "", 0},
	}
	for _, c := range cases {
		if got := localityDepth(c.ref, c.loc); got != c.want {
			t.Errorf("localityDepth(%q, %q) = %d, want %d", c.ref, c.loc, got, c.want)
		}
	}
}

// A same-region backend outranks a far-away one, even at higher latency —
// as long as both are healthy.
func TestRankedPrefersNearbyOverFarWhenHealthy(t *testing.T) {
	r := New([]config.Backend{
		{Name: "near", Location: "na/us/ca/sf/village-a"},
		{Name: "far", Location: "na/eu/de/berlin/village-e"},
	}, time.Hour, nil)
	r.SetSelfLocation("na/us/ca/sf/village-a")
	r.record("near", true, 200) // slower...
	r.record("far", true, 5)    // ...but far still loses to locality

	got := r.Ranked()
	if got[0].Backend.Name != "near" {
		t.Fatalf("nearby backend should rank first despite higher latency, got %+v", got)
	}
}

// Health still beats locality: an unhealthy nearby node never outranks a
// healthy far one.
func TestRankedHealthBeatsLocality(t *testing.T) {
	r := New([]config.Backend{
		{Name: "near-down", Location: "na/us/ca/sf/village-a"},
		{Name: "far-up", Location: "na/eu/de/berlin/village-e"},
	}, time.Hour, nil)
	r.SetSelfLocation("na/us/ca/sf/village-a")
	r.record("near-down", false, 0)
	r.record("far-up", true, 500)

	got := r.Ranked()
	if got[0].Backend.Name != "far-up" {
		t.Fatalf("healthy backend must outrank an unhealthy nearby one, got %+v", got)
	}
}

// Within the same locality depth, latency still breaks the tie.
func TestRankedLatencyTiebreakWithinSameLocality(t *testing.T) {
	r := New([]config.Backend{
		{Name: "a", Location: "na/us/ca/sf/village-a"},
		{Name: "b", Location: "na/us/ca/sf/village-b"},
	}, time.Hour, nil)
	r.SetSelfLocation("na/us/ny/nyc/village-z") // equidistant from both (depth 2 for both)
	r.record("a", true, 50)
	r.record("b", true, 10)

	got := r.Ranked()
	if got[0].Backend.Name != "b" {
		t.Fatalf("equal locality depth should fall back to latency, got %+v", got)
	}
}

// Unset self-location disables locality preference entirely — pure
// health/latency ranking, matching pre-I4 behavior exactly.
func TestRankedUnsetSelfLocationIsPureLatency(t *testing.T) {
	r := New([]config.Backend{
		{Name: "near", Location: "na/us/ca/sf/village-a"},
		{Name: "far", Location: "na/eu/de/berlin/village-e"},
	}, time.Hour, nil)
	// No SetSelfLocation call.
	r.record("near", true, 200)
	r.record("far", true, 5)

	got := r.Ranked()
	if got[0].Backend.Name != "far" {
		t.Fatalf("with no self-location, ranking must be pure latency, got %+v", got)
	}
}
