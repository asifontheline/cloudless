package main

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

// R2: raw Go network errors from a failed join attempt get a plain-language
// diagnosis instead of surfacing straight to a first-time user.

func TestDiagnoseJoinErrorTimeout(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "http://seed:8080/enroll", Err: &timeoutError{}}
	got := diagnoseJoinError(err, "http://seed:8080")
	if !strings.Contains(got, "timed out") {
		t.Errorf("want a timeout diagnosis, got %q", got)
	}
}

func TestDiagnoseJoinErrorDNS(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "http://bogus.invalid:8080/enroll",
		Err: &net.DNSError{Err: "no such host", Name: "bogus.invalid"}}
	got := diagnoseJoinError(err, "http://bogus.invalid:8080")
	if !strings.Contains(got, "resolve") || !strings.Contains(got, "bogus.invalid") {
		t.Errorf("want a DNS diagnosis naming the host, got %q", got)
	}
}

func TestDiagnoseJoinErrorConnectionRefused(t *testing.T) {
	err := &url.Error{Op: "Post", URL: "http://127.0.0.1:1/enroll",
		Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}
	got := diagnoseJoinError(err, "http://127.0.0.1:1")
	if !strings.Contains(got, "refused") {
		t.Errorf("want a connection-refused diagnosis, got %q", got)
	}
}

// A non-network error (e.g. the seed rejected the join token, or the
// secret was wrong) already carries its own clear message and must pass
// through unmodified, not get mangled into a generic network message.
func TestDiagnoseJoinErrorPassesThroughNonNetworkErrors(t *testing.T) {
	err := errors.New("enroll failed: join token rejected — mint a fresh one")
	got := diagnoseJoinError(err, "http://seed:8080")
	if got != err.Error() {
		t.Errorf("non-network error should pass through unmodified, got %q", got)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
