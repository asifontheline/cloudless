package quota

import (
	"testing"
	"time"
)

func TestRequestRateLimit(t *testing.T) {
	l := New(Limits{RequestsPerMinute: 2})
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("first request must pass")
	}
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("second request must pass")
	}
	ok, retry := l.Allow("k")
	if ok {
		t.Fatal("third request within the minute must be refused")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry hint out of range: %v", retry)
	}
	// A different key is unaffected — limits are per key.
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("independent key must not be throttled")
	}
}

func TestDailyTokenBudget(t *testing.T) {
	l := New(Limits{TokensPerDay: 100})
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("under budget must pass")
	}
	l.AddTokens("k", 100)
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("at/over budget must be refused")
	}
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("independent key keeps its own budget")
	}
}

func TestZeroMeansUnlimited(t *testing.T) {
	l := New(Limits{})
	for i := 0; i < 1000; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatal("zero limits must never throttle")
		}
	}
	l.AddTokens("k", 1<<40)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("zero token limit must never refuse")
	}
}

func TestNilLimiterIsOpen(t *testing.T) {
	var l *Limiter
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("nil limiter must admit")
	}
	l.AddTokens("k", 10) // must not panic
}

func TestSnapshotReflectsUsage(t *testing.T) {
	l := New(Limits{RequestsPerMinute: 10, TokensPerDay: 50})
	l.Allow("k")
	l.AddTokens("k", 7)
	limits, sts := l.Snapshot()
	if limits.RequestsPerMinute != 10 || limits.TokensPerDay != 50 {
		t.Fatalf("limits echoed wrong: %+v", limits)
	}
	if len(sts) != 1 || sts[0].TokensToday != 7 || sts[0].RequestsLastMin != 1 {
		t.Fatalf("status wrong: %+v", sts)
	}
}
