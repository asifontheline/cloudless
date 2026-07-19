package store

import (
	"strings"
	"testing"
)

func TestSafeFormatAllowlist(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Valid GGUF (magic bytes) is admitted.
	if _, err := s.Add("model.gguf", strings.NewReader("GGUF\x03\x00\x00\x00payload")); err != nil {
		t.Errorf("valid GGUF rejected: %v", err)
	}
	// Pickle-based / unknown extensions are rejected.
	for _, name := range []string{"weights.bin", "model.pkl", "model.pt"} {
		if _, err := s.Add(name, strings.NewReader("\x80\x04payload")); err == nil {
			t.Errorf("%s should have been rejected by the safe-format allowlist", name)
		}
	}
	// A .gguf without the magic bytes is rejected.
	if _, err := s.Add("fake.gguf", strings.NewReader("not-gguf-data")); err == nil {
		t.Error("fake .gguf without magic bytes should be rejected")
	}
}

func TestHashVerification(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("m.gguf", strings.NewReader("GGUF\x03\x00\x00\x00abc")); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Verify("m.gguf")
	if err != nil || !ok {
		t.Errorf("freshly added artifact failed verification: ok=%v err=%v", ok, err)
	}
}
