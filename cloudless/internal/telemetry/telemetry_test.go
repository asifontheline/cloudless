package telemetry

import (
	"strings"
	"testing"

	"cloudless/internal/config"
	"cloudless/internal/registry"
	"cloudless/internal/usage"
)

func TestRenderIncludesBackendMetrics(t *testing.T) {
	backends := []registry.BackendState{
		{Backend: config.Backend{Name: "node-a"}, Healthy: true, LatencyMS: 12, Failures: 0},
		{Backend: config.Backend{Name: "node-b"}, Healthy: false, LatencyMS: 0, Failures: 3},
	}
	out := string(Render(backends, nil, Load{}))

	if !strings.Contains(out, `cloudless_backend_healthy{backend="node-a"} 1`) {
		t.Errorf("missing healthy node-a metric, got:\n%s", out)
	}
	if !strings.Contains(out, `cloudless_backend_healthy{backend="node-b"} 0`) {
		t.Errorf("missing unhealthy node-b metric, got:\n%s", out)
	}
	if !strings.Contains(out, `cloudless_backend_latency_ms{backend="node-a"} 12`) {
		t.Errorf("missing latency metric, got:\n%s", out)
	}
	if !strings.Contains(out, `cloudless_backend_failures_total{backend="node-b"} 3`) {
		t.Errorf("missing failures metric, got:\n%s", out)
	}
}

func TestRenderIncludesUsageMetrics(t *testing.T) {
	recs := []usage.Record{
		{Key: usage.Key{APIKey: "abcd1234…", Backend: "node-a"}, Entry: usage.Entry{Requests: 5, PromptTokens: 100, CompletionTokens: 200}},
	}
	out := string(Render(nil, recs, Load{}))

	if !strings.Contains(out, `cloudless_usage_requests_total{key="abcd1234…",backend="node-a"} 5`) {
		t.Errorf("missing requests metric, got:\n%s", out)
	}
	if !strings.Contains(out, `cloudless_usage_prompt_tokens_total{key="abcd1234…",backend="node-a"} 100`) {
		t.Errorf("missing prompt tokens metric, got:\n%s", out)
	}
	if !strings.Contains(out, `cloudless_usage_completion_tokens_total{key="abcd1234…",backend="node-a"} 200`) {
		t.Errorf("missing completion tokens metric, got:\n%s", out)
	}
}

func TestRenderIncludesLoadMetrics(t *testing.T) {
	out := string(Render(nil, nil, Load{Inflight: 2, Waiting: 1, MaxConcurrent: 10}))

	if !strings.Contains(out, "cloudless_inflight_requests 2") {
		t.Errorf("missing inflight metric, got:\n%s", out)
	}
	if !strings.Contains(out, "cloudless_waiting_requests 1") {
		t.Errorf("missing waiting metric, got:\n%s", out)
	}
	if !strings.Contains(out, "cloudless_max_concurrent_requests 10") {
		t.Errorf("missing max concurrent metric, got:\n%s", out)
	}
}

func TestRenderEmptyIsStillValid(t *testing.T) {
	out := string(Render(nil, nil, Load{}))
	if !strings.Contains(out, "# HELP cloudless_backend_healthy") {
		t.Errorf("expected HELP/TYPE headers even with no data, got:\n%s", out)
	}
}

func TestRenderEscapesSpecialCharacters(t *testing.T) {
	recs := []usage.Record{
		{Key: usage.Key{APIKey: `a"b\c`, Backend: "node-a"}, Entry: usage.Entry{Requests: 1}},
	}
	out := string(Render(nil, recs, Load{}))
	if !strings.Contains(out, `key="a\"b\\c"`) {
		t.Errorf("expected escaped key, got:\n%s", out)
	}
}

func TestRenderSortsBackendsDeterministically(t *testing.T) {
	backends := []registry.BackendState{
		{Backend: config.Backend{Name: "zzz"}, Healthy: true},
		{Backend: config.Backend{Name: "aaa"}, Healthy: true},
	}
	out := string(Render(backends, nil, Load{}))
	aIdx := strings.Index(out, `backend="aaa"`)
	zIdx := strings.Index(out, `backend="zzz"`)
	if aIdx == -1 || zIdx == -1 || aIdx > zIdx {
		t.Errorf("expected aaa before zzz, got:\n%s", out)
	}
}
