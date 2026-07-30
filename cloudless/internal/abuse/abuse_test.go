package abuse

import (
	"testing"
	"time"
)

func TestBlockedFalseBeforeThreshold(t *testing.T) {
	g := New(5, time.Minute, time.Minute)
	for i := 0; i < 4; i++ {
		g.RecordFailure("1.2.3.4")
	}
	if blocked, _ := g.Blocked("1.2.3.4"); blocked {
		t.Fatal("must not lock out before threshold is reached")
	}
}

func TestLocksOutAtThreshold(t *testing.T) {
	g := New(5, time.Minute, time.Hour)
	for i := 0; i < 5; i++ {
		g.RecordFailure("1.2.3.4")
	}
	blocked, remaining := g.Blocked("1.2.3.4")
	if !blocked {
		t.Fatal("must lock out once threshold is reached")
	}
	if remaining <= 0 || remaining > time.Hour {
		t.Fatalf("remaining = %v, want within (0, 1h]", remaining)
	}
}

// Failures outside the window don't accumulate toward the threshold — an
// occasional mistyped key over a long period isn't credential stuffing.
func TestOldFailuresOutsideWindowDontCount(t *testing.T) {
	g := New(3, 10*time.Millisecond, time.Minute)
	g.RecordFailure("1.2.3.4")
	g.RecordFailure("1.2.3.4")
	time.Sleep(20 * time.Millisecond) // both fall outside the window now
	g.RecordFailure("1.2.3.4")
	if blocked, _ := g.Blocked("1.2.3.4"); blocked {
		t.Fatal("stale failures outside the window must not count toward the threshold")
	}
}

// Different sources are tracked independently — one attacker's failures
// never lock out an unrelated, well-behaved source.
func TestSourcesAreIndependent(t *testing.T) {
	g := New(3, time.Minute, time.Minute)
	for i := 0; i < 5; i++ {
		g.RecordFailure("attacker")
	}
	if blocked, _ := g.Blocked("innocent"); blocked {
		t.Fatal("an unrelated source must not be locked out")
	}
	if blocked, _ := g.Blocked("attacker"); !blocked {
		t.Fatal("the actual offending source must be locked out")
	}
}

// A success clears prior failures — an honest typo followed by the
// correct key doesn't leave the source one failure away from lockout.
func TestSuccessClearsFailureHistory(t *testing.T) {
	g := New(3, time.Minute, time.Minute)
	g.RecordFailure("1.2.3.4")
	g.RecordFailure("1.2.3.4")
	g.RecordSuccess("1.2.3.4")
	g.RecordFailure("1.2.3.4")
	if blocked, _ := g.Blocked("1.2.3.4"); blocked {
		t.Fatal("a success must reset the failure count, not just delay lockout")
	}
}

// A lockout expires — this isn't a permanent ban.
func TestLockoutExpires(t *testing.T) {
	g := New(2, time.Minute, 10*time.Millisecond)
	g.RecordFailure("1.2.3.4")
	g.RecordFailure("1.2.3.4")
	if blocked, _ := g.Blocked("1.2.3.4"); !blocked {
		t.Fatal("must be locked out immediately after threshold")
	}
	time.Sleep(20 * time.Millisecond)
	if blocked, _ := g.Blocked("1.2.3.4"); blocked {
		t.Fatal("lockout must expire after its duration")
	}
}

// A nil Guard (no abuse protection configured) is always a safe no-op —
// gateways that don't opt in must not panic or behave as if locked.
func TestNilGuardIsSafeNoOp(t *testing.T) {
	var g *Guard
	g.RecordFailure("x")
	g.RecordSuccess("x")
	if blocked, _ := g.Blocked("x"); blocked {
		t.Fatal("nil guard must never report blocked")
	}
}
