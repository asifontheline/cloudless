package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// B5: runtime supervisor. Tests exercise real subprocesses (/bin/sh), not
// mocks — the whole point is process lifecycle, which a fake can't prove.

func fastSupervisor(command []string) *Supervisor {
	s := New(command, "")
	s.InitialBackoff = 10 * time.Millisecond
	s.MaxBackoff = 50 * time.Millisecond
	s.StableAfter = 200 * time.Millisecond
	return s
}

func TestSupervisorReportsRunningWithPID(t *testing.T) {
	s := fastSupervisor([]string{"/bin/sh", "-c", "sleep 2"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := s.Status()
		if st.Running && st.PID > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("supervisor never reported Running with a PID")
}

func TestSupervisorRestartsOnCrash(t *testing.T) {
	s := fastSupervisor([]string{"/bin/sh", "-c", "exit 3"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Status().Restarts >= 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected at least 3 restarts, got %d", s.Status().Restarts)
}

func TestSupervisorRecordsExitReason(t *testing.T) {
	s := fastSupervisor([]string{"/bin/sh", "-c", "exit 7"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.Status(); st.Restarts >= 1 && st.LastExit != "" {
			if st.Running {
				t.Skip("caught mid-restart; exit info present is what matters")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected a recorded exit reason after a crash")
}

// Cancelling the context must kill the child and return promptly — not wait
// out a long-running process or a pending backoff sleep.
func TestSupervisorStopsPromptlyOnCancel(t *testing.T) {
	s := fastSupervisor([]string{"/bin/sh", "-c", "sleep 30"})
	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !s.Status().Running {
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Status().Running {
		t.Fatal("process never started")
	}

	done := make(chan struct{})
	go func() {
		cancel()
		// Run itself has no completion signal exposed; poll Status instead.
		for i := 0; i < 100; i++ {
			if !s.Status().Running {
				close(done)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop the child within 2s of cancellation")
	}
}

// A process that fails immediately on start (bad binary) is reported, not
// left silently retrying forever without any visible status.
func TestSupervisorHandlesStartFailure(t *testing.T) {
	s := fastSupervisor([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.Status(); st.LastExit != "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected a recorded failure for a nonexistent binary")
}

func TestSupervisorRunsInConfiguredDir(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "here.txt")
	s := New([]string{"/bin/sh", "-c", "pwd > " + marker}, dir)
	s.InitialBackoff = time.Hour // don't restart during the test
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("command never ran in the configured dir: %v", err)
	}
	// Resolve symlinks (e.g. /tmp -> /private/tmp on macOS) before comparing.
	wantDir, _ := filepath.EvalSymlinks(dir)
	gotDir, _ := filepath.EvalSymlinks(string(trimNewline(got)))
	if gotDir != wantDir {
		t.Fatalf("ran in %q, want %q", gotDir, wantDir)
	}
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
