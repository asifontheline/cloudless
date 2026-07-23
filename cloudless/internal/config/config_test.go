package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsApplied(t *testing.T) {
	c, err := Load(write(t, `{"backends":[{"name":"n","base_url":"http://x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" {
		t.Fatalf("default listen = %q", c.Listen)
	}
	if c.HealthIntervalSeconds != 5 {
		t.Fatalf("default health interval = %d", c.HealthIntervalSeconds)
	}
}

func TestRejectsEmptyTopology(t *testing.T) {
	if _, err := Load(write(t, `{}`)); err == nil {
		t.Fatal("config without backends or gossip must be rejected")
	}
}

func TestRejectsMalformedJSON(t *testing.T) {
	if _, err := Load(write(t, `{not json`)); err == nil {
		t.Fatal("malformed config must be rejected")
	}
}

func TestMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("missing file must error")
	}
}

func TestConcurrencyAndQuotaBlocks(t *testing.T) {
	c, err := Load(write(t, `{"backends":[{"name":"n","base_url":"http://x"}],
		"concurrency":{"max_in_flight":4,"max_queue":8,"wait_seconds":2},
		"quotas":{"requests_per_minute":60,"tokens_per_day":1000}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Concurrency == nil || c.Concurrency.MaxInFlight != 4 || c.Concurrency.MaxQueue != 8 || c.Concurrency.WaitSeconds != 2 {
		t.Fatalf("concurrency block wrong: %+v", c.Concurrency)
	}
	if c.Quotas == nil || c.Quotas.RequestsPerMinute != 60 || c.Quotas.TokensPerDay != 1000 {
		t.Fatalf("quota block wrong: %+v", c.Quotas)
	}
}

func TestRuntimeBlock(t *testing.T) {
	c, err := Load(write(t, `{"backends":[{"name":"n","base_url":"http://x"}],
		"runtime":{"command":["ollama","serve"],"dir":"/opt/ollama"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Runtime == nil {
		t.Fatal("runtime block missing")
	}
	if len(c.Runtime.Command) != 2 || c.Runtime.Command[0] != "ollama" || c.Runtime.Command[1] != "serve" {
		t.Fatalf("runtime command wrong: %+v", c.Runtime.Command)
	}
	if c.Runtime.Dir != "/opt/ollama" {
		t.Fatalf("runtime dir = %q", c.Runtime.Dir)
	}
}

func TestRuntimeBlockAbsentByDefault(t *testing.T) {
	c, err := Load(write(t, `{"backends":[{"name":"n","base_url":"http://x"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Runtime != nil {
		t.Fatalf("runtime should be nil when not configured, got %+v", c.Runtime)
	}
}

func TestGossipDefaults(t *testing.T) {
	c, err := Load(write(t, `{"gossip":{"secret":"0123456789abcdef0123456789abcdef"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Gossip.Bind != "0.0.0.0:7946" {
		t.Fatalf("default gossip bind = %q", c.Gossip.Bind)
	}
	if c.Gossip.NodeName == "" {
		t.Fatal("node name must default to hostname")
	}
}
