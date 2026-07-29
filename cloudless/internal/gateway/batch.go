package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"cloudless/internal/usage"
)

// Parallel fan-out (O1): a batch of independent requests is divided across
// the mesh's healthy backends, processed concurrently, and returned in the
// order submitted. Each item keeps the gateway's normal semantics — retry on
// the next backend for any failure before a complete response, backpressure
// via the limiter, quota and usage accounting per item.

const (
	batchMaxItems   = 64
	batchMaxWorkers = 8
)

type batchResult struct {
	Status     int             `json:"status"`
	Backend    string          `json:"backend"`
	Body       json.RawMessage `json:"body"`
	DurationMS int64           `json:"duration_ms"`
}

// forwardBuffered tries ranked backends starting at offset `start` (so
// concurrent batch items spread across the mesh instead of piling onto the
// fastest node) until one returns a complete, non-5xx response.
func (g *Gateway) forwardBuffered(ctx context.Context, path string, body []byte, start int) (status int, respBody []byte, backend string, attempts int) {
	backends := g.reg.Ranked()
	n := len(backends)
	if n == 0 {
		return http.StatusServiceUnavailable, []byte(`{"error":"no backends configured"}`), "-", 0
	}
	for i := 0; i < n; i++ {
		b := backends[(start+i)%n]
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.Backend.BaseURL+trimV1(path), bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := g.client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			continue // incomplete body — same pre-commit retry rule as single requests
		}
		return resp.StatusCode, data, b.Backend.Name, i
	}
	return http.StatusBadGateway, []byte(`{"error":"all backends unavailable"}`), "-", n
}

// handleBatch serves POST /v1/batch: {"path": "/v1/chat/completions",
// "requests": [ {...}, ... ]} → {"results": [ {status, backend, body}, ... ]}
// in submission order.
func (g *Gateway) handleBatch(w http.ResponseWriter, r *http.Request) {
	key := usage.Redact(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	var req struct {
		Path     string            `json:"path"`
		Requests []json.RawMessage `json:"requests"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		req.Path = "/v1/chat/completions"
	}
	if len(req.Requests) == 0 || len(req.Requests) > batchMaxItems {
		http.Error(w, `{"error":"batch must contain 1..64 requests"}`, http.StatusBadRequest)
		return
	}
	results, elapsedMS := g.runFanOut(r.Context(), key, req.Path, req.Requests)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"results": results, "timing": batchTiming(results, elapsedMS)})
}

// Divide-and-conquer (O5): one large task — a document, a long
// transcript — split into chunks, each processed against the same prompt
// template concurrently across the mesh (reusing O1's fan-out), then
// optionally merged into a single result. "Map" from the pattern this
// mirrors: apply one operation across many pieces, reduce them together.
func (g *Gateway) handleMap(w http.ResponseWriter, r *http.Request) {
	key := usage.Redact(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	var req struct {
		Path     string   `json:"path"`
		Template string   `json:"template"` // must contain {{chunk}}
		Chunks   []string `json:"chunks"`
		Merge    string   `json:"merge"` // "concat" (default) or "none"
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 10<<20)).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		req.Path = "/v1/chat/completions"
	}
	if !strings.Contains(req.Template, "{{chunk}}") {
		http.Error(w, `{"error":"template must contain {{chunk}}"}`, http.StatusBadRequest)
		return
	}
	if len(req.Chunks) == 0 || len(req.Chunks) > batchMaxItems {
		http.Error(w, `{"error":"map must contain 1..64 chunks"}`, http.StatusBadRequest)
		return
	}
	if req.Merge == "" {
		req.Merge = "concat"
	}
	if req.Merge != "concat" && req.Merge != "none" {
		http.Error(w, `{"error":"merge must be \"concat\" or \"none\""}`, http.StatusBadRequest)
		return
	}

	requests := make([]json.RawMessage, len(req.Chunks))
	for i, c := range req.Chunks {
		prompt := strings.ReplaceAll(req.Template, "{{chunk}}", c)
		body, _ := json.Marshal(map[string]any{
			"messages": []map[string]string{{"role": "user", "content": prompt}},
		})
		requests[i] = body
	}

	results, elapsedMS := g.runFanOut(r.Context(), key, req.Path, requests)

	out := map[string]any{"results": results, "timing": batchTiming(results, elapsedMS)}
	if req.Merge == "concat" {
		out["merged"] = mergeMapResults(results)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// mergeMapResults concatenates each chunk's completion content in
// submission order, skipping failed chunks — a partial merge, clearly
// short of every chunk, beats silently dropping the whole response.
func mergeMapResults(results []batchResult) string {
	var sb strings.Builder
	wrote := false
	for _, r := range results {
		if r.Status >= 400 {
			continue
		}
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(r.Body, &parsed) != nil || len(parsed.Choices) == 0 {
			continue
		}
		if wrote {
			sb.WriteString("\n\n")
		}
		sb.WriteString(parsed.Choices[0].Message.Content)
		wrote = true
	}
	return sb.String()
}

// batchTiming reports the fan-out's honest, measured speedup (O6): wall
// clock time for the whole batch versus the sum of each item's own
// duration — what running them one at a time on a single backend would
// have cost. Never a modeled or claimed number, only what was measured.
func batchTiming(results []batchResult, elapsedMS int64) map[string]any {
	var sequentialMS int64
	for _, r := range results {
		sequentialMS += r.DurationMS
	}
	speedup := 1.0
	if elapsedMS > 0 {
		speedup = float64(sequentialMS) / float64(elapsedMS)
	}
	return map[string]any{
		"elapsed_ms":    elapsedMS,
		"sequential_ms": sequentialMS,
		"speedup_x":     speedup,
	}
}

// runFanOut is the concurrent worker pool shared by /v1/batch (O1) and
// /v1/map (O5) — independent requests fanned out across the mesh, each
// keeping single-request semantics via batchOne, returning results in
// submission order plus the wall-clock time for the whole fan-out.
func (g *Gateway) runFanOut(ctx context.Context, key, path string, requests []json.RawMessage) ([]batchResult, int64) {
	results := make([]batchResult, len(requests))
	workers := batchMaxWorkers
	if len(requests) < workers {
		workers = len(requests)
	}
	start := time.Now()
	var wg sync.WaitGroup
	idx := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				results[i] = g.batchOne(ctx, key, path, requests[i], i)
			}
		}()
	}
	for i := range requests {
		idx <- i
	}
	close(idx)
	wg.Wait()
	return results, time.Since(start).Milliseconds()
}

func (g *Gateway) batchOne(ctx context.Context, key, path string, body json.RawMessage, i int) batchResult {
	start := time.Now()
	if ok, _ := g.Quota.Allow(key); !ok {
		return batchResult{Status: http.StatusTooManyRequests, Backend: "-",
			Body: json.RawMessage(`{"error":"quota exceeded — group fair-use limit reached"}`), DurationMS: time.Since(start).Milliseconds()}
	}
	if g.Limiter != nil {
		release, ok := g.Limiter.Acquire(ctx)
		if !ok {
			return batchResult{Status: http.StatusServiceUnavailable, Backend: "-",
				Body: json.RawMessage(`{"error":"node busy — retry shortly (backpressure)"}`), DurationMS: time.Since(start).Milliseconds()}
		}
		defer release()
	}
	status, respBody, backend, attempts := g.forwardBuffered(ctx, path, body, i)
	g.logRoute(path+" [batch]", backend, status, attempts)
	if status < 400 {
		var parsed struct {
			Usage struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
			} `json:"usage"`
		}
		json.Unmarshal(respBody, &parsed)
		g.Usage.Add(key, backend, 1, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)
		g.Quota.AddTokens(key, parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens)
	}
	if !json.Valid(respBody) {
		quoted, _ := json.Marshal(string(respBody))
		respBody = quoted
	}
	return batchResult{Status: status, Backend: backend, Body: respBody, DurationMS: time.Since(start).Milliseconds()}
}
