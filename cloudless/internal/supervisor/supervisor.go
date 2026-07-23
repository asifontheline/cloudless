// Package supervisor keeps a locally configured inference runtime (e.g. an
// ollama or llama.cpp server) alive: it launches the process and restarts it
// on unexpected exit with exponential backoff, so an operator doesn't have
// to babysit a crashed backend by hand (B5).
package supervisor

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// Status is a point-in-time snapshot of the supervised process.
type Status struct {
	Running    bool      `json:"running"`
	PID        int       `json:"pid,omitempty"`
	Restarts   int       `json:"restarts"`
	LastExit   string    `json:"last_exit,omitempty"`
	LastExitAt time.Time `json:"last_exit_at,omitempty"`
}

// Supervisor launches Command and keeps it running until Run's context is
// cancelled. Backoff fields have sane production defaults from New but are
// exported so tests (and unusually impatient/patient deployments) can tune
// them.
type Supervisor struct {
	Command []string
	Dir     string

	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// StableAfter is how long a process must run before a subsequent crash
	// resets the backoff to InitialBackoff instead of continuing to grow —
	// a long-lived process that eventually crashes once shouldn't be
	// punished with the same backoff as one crash-looping on startup.
	StableAfter time.Duration

	mu     sync.Mutex
	status Status
}

// New builds a supervisor for command (argv[0] plus args), run with working
// directory dir (empty = inherit).
func New(command []string, dir string) *Supervisor {
	return &Supervisor{
		Command:        command,
		Dir:            dir,
		InitialBackoff: time.Second,
		MaxBackoff:     30 * time.Second,
		StableAfter:    10 * time.Second,
	}
}

// Status returns a snapshot safe to read concurrently with Run.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Run launches Command and supervises it until ctx is done: on unexpected
// exit it restarts with exponential backoff, and on ctx cancellation it
// terminates the child and returns promptly rather than waiting out any
// pending backoff.
func (s *Supervisor) Run(ctx context.Context) {
	backoff := s.InitialBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		started := time.Now()
		exitErr := s.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}

		s.mu.Lock()
		s.status.Running = false
		s.status.Restarts++
		s.status.LastExitAt = time.Now()
		if exitErr != nil {
			s.status.LastExit = exitErr.Error()
		} else {
			s.status.LastExit = "exited 0"
		}
		s.mu.Unlock()

		if time.Since(started) >= s.StableAfter {
			backoff = s.InitialBackoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > s.MaxBackoff {
			backoff = s.MaxBackoff
		}
	}
}

// runOnce starts the process and blocks until it exits or ctx is cancelled
// (in which case the process is killed).
func (s *Supervisor) runOnce(ctx context.Context) error {
	cmd := exec.Command(s.Command[0], s.Command[1:]...)
	cmd.Dir = s.Dir
	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		s.status.Running = false
		s.status.LastExit = err.Error()
		s.status.LastExitAt = time.Now()
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	s.status.Running = true
	s.status.PID = cmd.Process.Pid
	s.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		s.mu.Lock()
		s.status.Running = false
		s.mu.Unlock()
		return ctx.Err()
	}
}
