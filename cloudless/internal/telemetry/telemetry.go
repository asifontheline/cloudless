// Package telemetry renders node health, routing, and usage state as
// Prometheus-compatible text exposition format (D3) — any standard scraper
// can pull it straight from GET /metrics, no proprietary agent required.
package telemetry

import (
	"fmt"
	"sort"
	"strings"

	"cloudless/internal/registry"
	"cloudless/internal/usage"
)

// Load carries the gateway's current concurrency stats.
type Load struct {
	Inflight      int64
	Waiting       int64
	MaxConcurrent int64
}

func escape(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

// Render produces the full text-exposition-format snapshot for one node.
func Render(backends []registry.BackendState, usageRecs []usage.Record, load Load) []byte {
	var b strings.Builder

	writeMetric(&b, "cloudless_backend_healthy", "1 if the backend's last health probe succeeded", "gauge")
	for _, s := range sortedBackends(backends) {
		v := 0
		if s.Healthy {
			v = 1
		}
		fmt.Fprintf(&b, "cloudless_backend_healthy{backend=\"%s\"} %d\n", escape(s.Backend.Name), v)
	}

	writeMetric(&b, "cloudless_backend_latency_ms", "last successful health-probe latency in milliseconds", "gauge")
	for _, s := range sortedBackends(backends) {
		fmt.Fprintf(&b, "cloudless_backend_latency_ms{backend=\"%s\"} %d\n", escape(s.Backend.Name), s.LatencyMS)
	}

	writeMetric(&b, "cloudless_backend_failures_total", "consecutive failed health probes since last success", "gauge")
	for _, s := range sortedBackends(backends) {
		fmt.Fprintf(&b, "cloudless_backend_failures_total{backend=\"%s\"} %d\n", escape(s.Backend.Name), s.Failures)
	}

	writeMetric(&b, "cloudless_usage_requests_total", "requests served, by redacted API key and backend", "counter")
	for _, r := range usageRecs {
		fmt.Fprintf(&b, "cloudless_usage_requests_total{key=\"%s\",backend=\"%s\"} %d\n", escape(r.APIKey), escape(r.Backend), r.Requests)
	}

	writeMetric(&b, "cloudless_usage_prompt_tokens_total", "prompt tokens consumed, by redacted API key and backend", "counter")
	for _, r := range usageRecs {
		fmt.Fprintf(&b, "cloudless_usage_prompt_tokens_total{key=\"%s\",backend=\"%s\"} %d\n", escape(r.APIKey), escape(r.Backend), r.PromptTokens)
	}

	writeMetric(&b, "cloudless_usage_completion_tokens_total", "completion tokens generated, by redacted API key and backend", "counter")
	for _, r := range usageRecs {
		fmt.Fprintf(&b, "cloudless_usage_completion_tokens_total{key=\"%s\",backend=\"%s\"} %d\n", escape(r.APIKey), escape(r.Backend), r.CompletionTokens)
	}

	writeMetric(&b, "cloudless_inflight_requests", "requests currently being served", "gauge")
	fmt.Fprintf(&b, "cloudless_inflight_requests %d\n", load.Inflight)

	writeMetric(&b, "cloudless_waiting_requests", "requests queued behind the concurrency limiter", "gauge")
	fmt.Fprintf(&b, "cloudless_waiting_requests %d\n", load.Waiting)

	writeMetric(&b, "cloudless_max_concurrent_requests", "configured concurrency ceiling", "gauge")
	fmt.Fprintf(&b, "cloudless_max_concurrent_requests %d\n", load.MaxConcurrent)

	return []byte(b.String())
}

func writeMetric(b *strings.Builder, name, help, typ string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func sortedBackends(backends []registry.BackendState) []registry.BackendState {
	out := make([]registry.BackendState, len(backends))
	copy(out, backends)
	sort.Slice(out, func(i, j int) bool { return out[i].Backend.Name < out[j].Backend.Name })
	return out
}
