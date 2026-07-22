package inflight

import (
	"context"
	"testing"
	"time"
)

// L1 backfill: RetryAfter, Capacity, and the nil-receiver paths had zero
// coverage. A nil *Limiter is a real, intentional configuration (an
// unconfigured gateway field), not just a defensive nil check.

func TestNilLimiterIsUnlimited(t *testing.T) {
	var l *Limiter
	rel, ok := l.Acquire(context.Background())
	if !ok {
		t.Fatal("nil limiter must always admit")
	}
	rel() // must not panic on a nil receiver

	if got := l.RetryAfter(); got != 0 {
		t.Fatalf("nil limiter RetryAfter = %v, want 0", got)
	}
	if got := l.Capacity(); got != 0 {
		t.Fatalf("nil limiter Capacity = %d, want 0", got)
	}
	if inf, wait := l.Stats(); inf != 0 || wait != 0 {
		t.Fatalf("nil limiter Stats = %d/%d, want 0/0", inf, wait)
	}
}

func TestCapacityReflectsConfiguredLimit(t *testing.T) {
	l := New(5, 0, time.Second)
	if got := l.Capacity(); got != 5 {
		t.Fatalf("Capacity = %d, want 5", got)
	}
	if got := New(0, 0, time.Second).Capacity(); got != 0 {
		t.Fatalf("unlimited limiter Capacity = %d, want 0", got)
	}
}

func TestRetryAfterReflectsConfiguredWait(t *testing.T) {
	l := New(1, 1, 250*time.Millisecond)
	if got := l.RetryAfter(); got != 250*time.Millisecond {
		t.Fatalf("RetryAfter = %v, want 250ms", got)
	}
}

// A queued caller that waits past the configured timeout is rejected, not
// left hanging.
func TestQueueWaitTimesOut(t *testing.T) {
	l := New(1, 1, 50*time.Millisecond)
	rel, ok := l.Acquire(context.Background())
	if !ok {
		t.Fatal("first acquire should succeed")
	}
	defer rel()

	start := time.Now()
	_, ok = l.Acquire(context.Background())
	if ok {
		t.Fatal("acquire should time out while the only slot stays held")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("acquire returned before the configured wait elapsed: %v", elapsed)
	}
}

// A caller's context cancellation ends the wait early, even if the
// configured timeout hasn't elapsed yet.
func TestQueueWaitCanceledByContext(t *testing.T) {
	l := New(1, 1, 10*time.Second) // long wait — ctx must win the race
	rel, ok := l.Acquire(context.Background())
	if !ok {
		t.Fatal("first acquire should succeed")
	}
	defer rel()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		_, ok := l.Acquire(ctx)
		done <- ok
	}()
	time.Sleep(20 * time.Millisecond) // let the goroutine join the queue
	start := time.Now()
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("acquire must fail once its context is canceled")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("cancellation took too long to be observed: %v", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire did not return after context cancellation")
	}
}
