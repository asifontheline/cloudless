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

// R1: `cloudless join <link>` accepts the console's literal, copy-pasteable
// "cloudless up -join ..." command text (see internal/gateway's
// handleJoinLink), whether it arrives as one quoted arg (shell hands us a
// single field) or unquoted (shell already split it into multiple fields).

func TestParseJoinLinkQuotedFullCommand(t *testing.T) {
	got, err := parseJoinLink([]string{
		"cloudless up -join secret123@10.0.0.5:8080 -seed-api http://10.0.0.5:8080 -join-token tok1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"-join", "secret123@10.0.0.5:8080", "-seed-api", "http://10.0.0.5:8080", "-join-token", "tok1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseJoinLinkUnquotedFields(t *testing.T) {
	got, err := parseJoinLink([]string{
		"cloudless", "up", "-join", "secret123@10.0.0.5:8080", "-seed-api", "http://10.0.0.5:8080",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"-join", "secret123@10.0.0.5:8080", "-seed-api", "http://10.0.0.5:8080"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseJoinLinkWithoutLeadingCloudlessUp(t *testing.T) {
	got, err := parseJoinLink([]string{"-join", "secret123@10.0.0.5:8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"-join", "secret123@10.0.0.5:8080"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseJoinLinkMissingJoinFlagErrors(t *testing.T) {
	_, err := parseJoinLink([]string{"cloudless", "up", "-backend", "http://127.0.0.1:11434"})
	if err == nil {
		t.Fatal("expected an error for a link with no -join flag, got nil")
	}
}

func TestParseJoinLinkEqualsFormFlag(t *testing.T) {
	got, err := parseJoinLink([]string{"-join=secret123@10.0.0.5:8080"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(got, " ") != "-join=secret123@10.0.0.5:8080" {
		t.Errorf("got %v", got)
	}
}

// R6: `cloudless up` with no flags, a real terminal, and no existing config
// prompts step-by-step instead of silently guessing. The wizard's stdin/
// stdout are injected so these tests don't touch a real terminal, and
// `detect` is injected so they don't depend on a local inference runtime
// actually being present on the machine running the tests.

func TestFirstRunWizardJoinWithFullLink(t *testing.T) {
	in := strings.NewReader("join\ncloudless up -join abc123@10.0.0.5:8080 -seed-api http://10.0.0.5:8080\n")
	var out strings.Builder
	got := runFirstRunWizard(in, &out, func() string { return "" })
	want := []string{"-join", "abc123@10.0.0.5:8080", "-seed-api", "http://10.0.0.5:8080"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFirstRunWizardJoinWithBareSecretHost(t *testing.T) {
	in := strings.NewReader("j\nabc123@10.0.0.5:8080\n")
	var out strings.Builder
	got := runFirstRunWizard(in, &out, func() string { return "" })
	want := []string{"-join", "abc123@10.0.0.5:8080"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFirstRunWizardStartWithDetectedBackendSkipsPrompt(t *testing.T) {
	in := strings.NewReader("start\n")
	var out strings.Builder
	got := runFirstRunWizard(in, &out, func() string { return "http://127.0.0.1:11434/v1" })
	if got != nil {
		t.Errorf("expected no extra args when a backend was auto-detected, got %v", got)
	}
	if strings.Contains(out.String(), "No local inference runtime detected") {
		t.Error("should not ask for a backend when one was already detected")
	}
}

func TestFirstRunWizardStartNoBackendDetectedPromptsAndUsesInput(t *testing.T) {
	in := strings.NewReader("start\nhttp://10.0.0.9:9000\n")
	var out strings.Builder
	got := runFirstRunWizard(in, &out, func() string { return "" })
	want := []string{"-backend", "http://10.0.0.9:9000"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
	if !strings.Contains(out.String(), "No local inference runtime detected") {
		t.Error("expected the wizard to explain why it's asking for a backend")
	}
}

func TestFirstRunWizardStartNoBackendEmptyInputMeansRoutingOnly(t *testing.T) {
	in := strings.NewReader("start\n\n")
	var out strings.Builder
	got := runFirstRunWizard(in, &out, func() string { return "" })
	if got != nil {
		t.Errorf("expected no args (routing-only node) for an empty backend answer, got %v", got)
	}
}

func TestFirstRunWizardEmptyChoiceDefaultsToStart(t *testing.T) {
	in := strings.NewReader("\n")
	var out strings.Builder
	got := runFirstRunWizard(in, &out, func() string { return "http://127.0.0.1:11434/v1" })
	if got != nil {
		t.Errorf("empty choice should default to start with a detected backend, got %v", got)
	}
}
