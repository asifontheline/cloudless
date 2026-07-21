package ext

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// L1 backfill: the ext package standalone (gateway tests cover it through
// HTTP; this pins the registry's own contract).

func TestRegisterValidation(t *testing.T) {
	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	cases := []struct {
		name string
		e    Extension
	}{
		{"bad name (uppercase)", Extension{Name: "Bad", BaseURL: "http://127.0.0.1:1"}},
		{"bad name (empty)", Extension{Name: "", BaseURL: "http://127.0.0.1:1"}},
		{"bad name (too long)", Extension{Name: string(make([]byte, 65)), BaseURL: "http://127.0.0.1:1"}},
		{"non-http scheme", Extension{Name: "ok", BaseURL: "ftp://127.0.0.1:1"}},
		{"unparseable URL", Extension{Name: "ok", BaseURL: "://bad"}},
		{"no host", Extension{Name: "ok", BaseURL: "http://"}},
	}
	for _, c := range cases {
		if _, err := r.Register(c.e); err == nil {
			t.Errorf("%s: expected rejection", c.name)
		}
	}
}

func TestRegisterTrimsTrailingSlash(t *testing.T) {
	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	got, err := r.Register(Extension{Name: "svc", BaseURL: "http://127.0.0.1:9090/"})
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "http://127.0.0.1:9090" {
		t.Fatalf("trailing slash not trimmed: %q", got.BaseURL)
	}
}

func TestDuplicateNameRejected(t *testing.T) {
	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	if _, err := r.Register(Extension{Name: "svc", BaseURL: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(Extension{Name: "svc", BaseURL: "http://127.0.0.1:2"}); err == nil {
		t.Fatal("duplicate name must be rejected — a route cannot be silently hijacked")
	}
	// Original registration is untouched by the rejected attempt.
	got, _ := r.Get("svc")
	if got.BaseURL != "http://127.0.0.1:1" {
		t.Fatalf("rejected re-registration mutated the original: %+v", got)
	}
}

func TestRemoveAndList(t *testing.T) {
	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	r.Register(Extension{Name: "b", BaseURL: "http://127.0.0.1:2"})
	r.Register(Extension{Name: "a", BaseURL: "http://127.0.0.1:1"})
	list := r.List()
	if len(list) != 2 || list[0].Name != "a" || list[1].Name != "b" {
		t.Fatalf("list must be name-sorted: %+v", list)
	}
	if !r.Remove("a") {
		t.Fatal("remove of existing extension failed")
	}
	if r.Remove("a") {
		t.Fatal("double remove must report false")
	}
	if len(r.List()) != 1 {
		t.Fatalf("removed extension still listed: %+v", r.List())
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extensions.json")
	r1 := Open(path)
	r1.Register(Extension{Name: "svc", BaseURL: "http://127.0.0.1:1", Runtime: "python"})
	r2 := Open(path)
	got, ok := r2.Get("svc")
	if !ok || got.Runtime != "python" {
		t.Fatalf("registration lost across reopen: %+v ok=%v", got, ok)
	}
}

// Probe marks a responding service healthy and an unreachable one unhealthy,
// and only updates LastSeen when healthy.
func TestProbeHealthAndLastSeen(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer down.Close()

	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	r.Register(Extension{Name: "up", BaseURL: up.URL})
	r.Register(Extension{Name: "down", BaseURL: down.URL})
	r.Probe(context.Background(), &http.Client{Timeout: time.Second})

	gotUp, _ := r.Get("up")
	gotDown, _ := r.Get("down")
	if !gotUp.Healthy || gotUp.LastSeen.IsZero() {
		t.Fatalf("responding service must be healthy with LastSeen set: %+v", gotUp)
	}
	if gotDown.Healthy {
		t.Fatalf("5xx service must be marked unhealthy: %+v", gotDown)
	}
}

// A 404 (no /healthz handler) still proves the process is alive.
func TestProbe404StillCountsAsAlive(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer svc.Close()
	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	r.Register(Extension{Name: "svc", BaseURL: svc.URL})
	r.Probe(context.Background(), &http.Client{Timeout: time.Second})
	got, _ := r.Get("svc")
	if !got.Healthy {
		t.Fatalf("404 (process alive, no /healthz) must still count healthy: %+v", got)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx, 10*time.Millisecond, nil); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestGetUnknown(t *testing.T) {
	r := Open(filepath.Join(t.TempDir(), "extensions.json"))
	if _, ok := r.Get("nope"); ok {
		t.Fatal("Get on unregistered name must report false")
	}
}
