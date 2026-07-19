package inflight

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestUnlimited(t *testing.T) {
	l := New(0, 0, time.Second)
	rel, ok := l.Acquire(context.Background())
	if !ok {
		t.Fatal("unlimited limiter must always admit")
	}
	rel()
}

func TestConcurrencyBounded(t *testing.T) {
	l := New(2, 0, 50*time.Millisecond) // 2 in-flight, no queue
	r1, ok1 := l.Acquire(context.Background())
	r2, ok2 := l.Acquire(context.Background())
	if !ok1 || !ok2 {
		t.Fatal("first two acquires should succeed")
	}
	// Third has no slot and no queue room -> rejected quickly.
	_, ok3 := l.Acquire(context.Background())
	if ok3 {
		t.Fatal("third acquire should be rejected (queue full)")
	}
	if inf, _ := l.Stats(); inf != 2 {
		t.Errorf("expected 2 in-flight, got %d", inf)
	}
	r1()
	// Now a slot frees; next acquire succeeds.
	r3, ok := l.Acquire(context.Background())
	if !ok {
		t.Fatal("acquire should succeed after a release")
	}
	r2()
	r3()
	if inf, wait := l.Stats(); inf != 0 || wait != 0 {
		t.Errorf("expected 0/0 after release, got %d/%d", inf, wait)
	}
}

func TestQueueWaitThenAdmit(t *testing.T) {
	l := New(1, 4, 500*time.Millisecond) // 1 in-flight, up to 4 waiting
	r1, _ := l.Acquire(context.Background())
	var wg sync.WaitGroup
	var admitted int32
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		rel, ok := l.Acquire(context.Background()) // waits for r1 to release
		if ok {
			mu.Lock()
			admitted++
			mu.Unlock()
			rel()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	r1() // free the slot; the waiter should now be admitted
	wg.Wait()
	if admitted != 1 {
		t.Errorf("queued waiter should have been admitted after release, admitted=%d", admitted)
	}
}
