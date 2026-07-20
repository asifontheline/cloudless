package store

import (
	"os"
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

// L1 backfill: a corrupted blob is detected, not served as intact.
func TestVerifyDetectsCorruption(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("m.gguf", strings.NewReader("GGUF\x03\x00\x00\x00abc")); err != nil {
		t.Fatal(err)
	}
	p, ok := s.Path("m.gguf")
	if !ok {
		t.Fatal("path missing")
	}
	if err := os.WriteFile(p, []byte("GGUF-corrupted-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Verify("m.gguf"); ok {
		t.Fatal("corrupted blob passed verification")
	}
	if _, err := s.Verify("ghost.gguf"); err == nil {
		t.Fatal("unknown artifact must error, not report ok")
	}
}

// The index survives reopen: entries, hashes, and blob paths intact.
func TestIndexPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s1.Add("m.gguf", strings.NewReader("GGUF\x03\x00\x00\x00abc"))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	list := s2.List()
	if len(list) != 1 || list[0].SHA256 != e.SHA256 || list[0].Format != "gguf" {
		t.Fatalf("reopened index wrong: %+v", list)
	}
	if ok, err := s2.Verify("m.gguf"); err != nil || !ok {
		t.Fatalf("reopened store failed verification: %v", err)
	}
}

// Two names for identical bytes share one blob; deleting one name keeps the
// blob until the last reference goes.
func TestDeleteKeepsSharedBlob(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const data = "GGUF\x03\x00\x00\x00same-bytes"
	if _, err := s.Add("a.gguf", strings.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("b.gguf", strings.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if !s.Delete("a.gguf") {
		t.Fatal("delete a failed")
	}
	if ok, err := s.Verify("b.gguf"); err != nil || !ok {
		t.Fatalf("shared blob must survive deleting one name: %v", err)
	}
	p, _ := s.Path("b.gguf")
	if !s.Delete("b.gguf") {
		t.Fatal("delete b failed")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("last delete must remove the blob from disk")
	}
	if s.Delete("b.gguf") {
		t.Fatal("double delete must report false")
	}
}

// The other two allowed formats are admitted by magic, not just extension.
func TestSafetensorsAndOnnxAdmitted(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// safetensors: 8-byte little-endian header length then a JSON object.
	st := "\x02\x00\x00\x00\x00\x00\x00\x00{}tensor"
	if _, err := s.Add("m.safetensors", strings.NewReader(st)); err != nil {
		t.Errorf("valid safetensors rejected: %v", err)
	}
	if _, err := s.Add("bad.safetensors", strings.NewReader("\x02\x00\x00\x00\x00\x00\x00\x00XX")); err == nil {
		t.Error("safetensors without JSON header admitted")
	}
	if _, err := s.Add("m.onnx", strings.NewReader("\x08\x01protobufish")); err != nil {
		t.Errorf("valid onnx rejected: %v", err)
	}
	// An .onnx that is actually a pickle is the attack this guard exists for.
	if _, err := s.Add("evil.onnx", strings.NewReader("\x80\x04pickle")); err == nil {
		t.Error("pickle disguised as onnx admitted")
	}
}
