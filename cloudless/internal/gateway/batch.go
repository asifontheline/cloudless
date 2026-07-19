package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

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
	Status  int             `json:"status"`
	Backend string          `json:"backend"`
	Body    json.RawMessage `json:"body"`
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
	results := make([]batchResult, len(req.Requests))
	workers := batchMaxWorkers
	if len(req.Requests) < workers {
		workers = len(req.Requests)
	}
	var wg sync.WaitGroup
	idx := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				results[i] = g.batchOne(r.Context(), key, req.Path, req.Requests[i], i)
			}
		}()
	}
	for i := range req.Requests {
		idx <- i
	}
	close(idx)
	wg.Wait()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"results": results})
}

func (g *Gateway) batchOne(ctx context.Context, key, path string, body json.RawMessage, i int) batchResult {
	if ok, _ := g.Quota.Allow(key); !ok {
		return batchResult{Status: http.StatusTooManyRequests, Backend: "-",
			Body: json.RawMessage(`{"error":"quota exceeded — group fair-use limit reached"}`)}
	}
	if g.Limiter != nil {
		release, ok := g.Limiter.Acquire(ctx)
		if !ok {
			return batchResult{Status: http.StatusServiceUnavailable, Backend: "-",
				Body: json.RawMessage(`{"error":"node busy — retry shortly (backpressure)"}`)}
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
	return batchResult{Status: status, Backend: backend, Body: respBody}
}
