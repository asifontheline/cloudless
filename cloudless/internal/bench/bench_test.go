package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPercentileNearestRank(t *testing.T) {
	r := Result{Latencies: []time.Duration{
		10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond,
		40 * time.Millisecond, 50 * time.Millisecond, 60 * time.Millisecond,
		70 * time.Millisecond, 80 * time.Millisecond, 90 * time.Millisecond,
		100 * time.Millisecond,
	}}
	if got := r.Percentile(50); got != 50*time.Millisecond {
		t.Errorf("p50 = %v, want 50ms", got)
	}
	if got := r.Percentile(90); got != 90*time.Millisecond {
		t.Errorf("p90 = %v, want 90ms", got)
	}
	if got := r.Percentile(100); got != 100*time.Millisecond {
		t.Errorf("p100 = %v, want 100ms", got)
	}
}

func TestPercentileEmpty(t *testing.T) {
	var r Result
	if got := r.Percentile(50); got != 0 {
		t.Errorf("percentile of empty result = %v, want 0", got)
	}
}

func TestThroughputCalculations(t *testing.T) {
	r := Result{
		Successes: 10, Duration: 2 * time.Second,
		PromptTokens: 100, CompletionTokens: 200,
	}
	if got := r.RequestsPerSec(); got != 5 {
		t.Errorf("RequestsPerSec = %v, want 5", got)
	}
	if got := r.TokensPerSec(); got != 150 {
		t.Errorf("TokensPerSec = %v, want 150", got)
	}
}

func TestThroughputZeroDuration(t *testing.T) {
	var r Result
	if r.RequestsPerSec() != 0 || r.TokensPerSec() != 0 {
		t.Fatal("zero-duration result must report zero throughput, not divide by zero / Inf")
	}
}

// Run against a real HTTP server: every request succeeds, usage is summed,
// and the exact request count is made.
func TestRunAllSuccess(t *testing.T) {
	var count int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&count, 1)
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer srv.Close()

	r := Run(context.Background(), srv.Client(), srv.URL, "test-key", `{}`, 20, 4)
	if r.Successes != 20 || r.Failures != 0 {
		t.Fatalf("want 20 successes 0 failures, got %d/%d", r.Successes, r.Failures)
	}
	if atomic.LoadInt64(&count) != 20 {
		t.Fatalf("server saw %d requests, want 20", count)
	}
	if r.PromptTokens != 200 || r.CompletionTokens != 100 {
		t.Fatalf("token sums wrong: prompt=%d completion=%d", r.PromptTokens, r.CompletionTokens)
	}
	if len(r.Latencies) != 20 {
		t.Fatalf("want 20 recorded latencies, got %d", len(r.Latencies))
	}
}

// A non-200 response counts as a failure, not a success with zero tokens.
func TestRunMixedSuccessFailure(t *testing.T) {
	var n int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := atomic.AddInt64(&n, 1)
		if i%2 == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1}})
	}))
	defer srv.Close()

	r := Run(context.Background(), srv.Client(), srv.URL, "k", `{}`, 10, 2)
	if r.Successes != 5 || r.Failures != 5 {
		t.Fatalf("want 5/5 split, got %d successes %d failures", r.Successes, r.Failures)
	}
}

// Concurrency is actually bounded, not just cosmetic — a semaphore-style
// gate verifies no more than `concurrency` requests are in flight at once.
func TestRunRespectsConcurrency(t *testing.T) {
	const concurrency = 3
	var inFlight, maxSeen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cur := atomic.AddInt64(&inFlight, 1)
		defer atomic.AddInt64(&inFlight, -1)
		for {
			m := atomic.LoadInt64(&maxSeen)
			if cur <= m || atomic.CompareAndSwapInt64(&maxSeen, m, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"usage": map[string]any{}})
	}))
	defer srv.Close()

	Run(context.Background(), srv.Client(), srv.URL, "k", `{}`, 12, concurrency)
	if maxSeen > concurrency {
		t.Fatalf("max concurrent in-flight = %d, want <= %d", maxSeen, concurrency)
	}
	if maxSeen < concurrency {
		t.Errorf("never reached the configured concurrency (saw max %d) — test may not be exercising it", maxSeen)
	}
}

// A connection failure (server down) is a clean failure, not a panic or hang.
func TestRunConnectionFailure(t *testing.T) {
	r := Run(context.Background(), http.DefaultClient, "http://127.0.0.1:1", "k", `{}`, 3, 1)
	if r.Successes != 0 || r.Failures != 3 {
		t.Fatalf("want 0 successes 3 failures against a dead server, got %d/%d", r.Successes, r.Failures)
	}
}

// concurrency < 1 is treated as 1, not zero (which would deadlock the
// semaphore forever).
func TestRunConcurrencyFloor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"usage": map[string]any{}})
	}))
	defer srv.Close()
	done := make(chan Result, 1)
	go func() { done <- Run(context.Background(), srv.Client(), srv.URL, "k", `{}`, 3, 0) }()
	select {
	case r := <-done:
		if r.Successes != 3 {
			t.Fatalf("want 3 successes, got %d", r.Successes)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run with concurrency=0 deadlocked instead of flooring to 1")
	}
}

func TestRunReportString(t *testing.T) {
	// Sanity: a Result with real numbers formats without panicking, for
	// callers building CLI/log output around Percentile/throughput.
	r := Result{Successes: 3, Duration: time.Second, Latencies: []time.Duration{
		10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond,
	}}
	s := fmt.Sprintf("p50=%v p95=%v rps=%.1f", r.Percentile(50), r.Percentile(95), r.RequestsPerSec())
	if s == "" {
		t.Fatal("unexpected empty report string")
	}
}
