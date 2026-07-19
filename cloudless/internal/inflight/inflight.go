// Package inflight provides gateway backpressure: it bounds the number of
// concurrent in-flight requests and lets a limited number wait briefly for a
// slot. When both are full, callers are told to retry instead of piling onto
// overloaded backends.
package inflight

import (
	"context"
	"sync/atomic"
	"time"
)

type Limiter struct {
	sem      chan struct{} // capacity = max concurrent in-flight
	maxQueue int           // max callers allowed to wait for a slot
	wait     time.Duration // how long a caller waits before giving up
	waiting  atomic.Int64
	inflight atomic.Int64
}

// New builds a limiter. maxConcurrent <= 0 means unlimited (a no-op limiter).
func New(maxConcurrent, maxQueue int, wait time.Duration) *Limiter {
	l := &Limiter{maxQueue: maxQueue, wait: wait}
	if maxConcurrent > 0 {
		l.sem = make(chan struct{}, maxConcurrent)
	}
	return l
}

// Acquire tries to reserve a slot. It returns a release func and true on
// success; false means the caller should back off (queue full or wait timed
// out). RetryAfter suggests how long to wait before retrying.
func (l *Limiter) Acquire(ctx context.Context) (release func(), ok bool) {
	if l == nil || l.sem == nil {
		return func() {}, true // unlimited
	}
	rel := func() { <-l.sem; l.inflight.Add(-1) }
	// Fast path: grab a free slot without joining the wait queue.
	select {
	case l.sem <- struct{}{}:
		l.inflight.Add(1)
		return rel, true
	default:
	}
	// Slow path: no slot free — join the bounded wait queue.
	q := l.waiting.Add(1)
	if int(q) > l.maxQueue {
		l.waiting.Add(-1)
		return nil, false // queue full — back off
	}
	defer l.waiting.Add(-1)
	timer := time.NewTimer(l.wait)
	defer timer.Stop()
	select {
	case l.sem <- struct{}{}:
		l.inflight.Add(1)
		return rel, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

func (l *Limiter) RetryAfter() time.Duration {
	if l == nil {
		return 0
	}
	return l.wait
}

// Stats reports the current in-flight and waiting counts.
func (l *Limiter) Stats() (inflight, waiting int64) {
	if l == nil {
		return 0, 0
	}
	return l.inflight.Load(), l.waiting.Load()
}

// Capacity returns the configured max concurrent (0 = unlimited).
func (l *Limiter) Capacity() int {
	if l == nil || l.sem == nil {
		return 0
	}
	return cap(l.sem)
}
