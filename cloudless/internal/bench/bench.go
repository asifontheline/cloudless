// Package bench measures real latency and throughput against a running
// node — the "measured, never marketed" principle applied to performance
// claims (D2). It fires a configurable number of chat-completions requests
// at a configurable concurrency and reports p50/p95/p99 latency plus
// requests/sec and tokens/sec.
package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Result is one benchmark run's outcome.
type Result struct {
	N           int
	Concurrency int
	Duration    time.Duration
	Successes   int
	Failures    int
	Latencies   []time.Duration // successful requests only, unsorted as recorded

	PromptTokens     int64
	CompletionTokens int64
}

// Percentile returns the latency at percentile p (0-100) using nearest-rank
// on the sorted latencies. Zero when there are no successful requests.
func (r Result) Percentile(p float64) time.Duration {
	if len(r.Latencies) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), r.Latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(p/100*float64(len(sorted))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// RequestsPerSec is successful requests per second of wall-clock duration.
func (r Result) RequestsPerSec() float64 {
	if r.Duration <= 0 {
		return 0
	}
	return float64(r.Successes) / r.Duration.Seconds()
}

// TokensPerSec is total (prompt + completion) tokens per second of
// wall-clock duration.
func (r Result) TokensPerSec() float64 {
	if r.Duration <= 0 {
		return 0
	}
	return float64(r.PromptTokens+r.CompletionTokens) / r.Duration.Seconds()
}

type chatResponse struct {
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

// Run fires n requests (concurrency at a time) at url with the given body
// and Bearer apiKey, measuring wall-clock latency of each and summing
// reported token usage from successful (200) responses.
func Run(ctx context.Context, client *http.Client, url, apiKey, body string, n, concurrency int) Result {
	if concurrency < 1 {
		concurrency = 1
	}
	var mu sync.Mutex
	r := Result{N: n, Concurrency: concurrency}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			reqStart := time.Now()
			ok, promptTok, completionTok := doOne(ctx, client, url, apiKey, body)
			lat := time.Since(reqStart)

			mu.Lock()
			defer mu.Unlock()
			if ok {
				r.Successes++
				r.Latencies = append(r.Latencies, lat)
				r.PromptTokens += promptTok
				r.CompletionTokens += completionTok
			} else {
				r.Failures++
			}
		}()
	}
	wg.Wait()
	r.Duration = time.Since(start)
	return r
}

func doOne(ctx context.Context, client *http.Client, url, apiKey, body string) (ok bool, promptTok, completionTok int64) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte(body)))
	if err != nil {
		return false, 0, 0
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, 0, 0
	}
	var out chatResponse
	json.NewDecoder(resp.Body).Decode(&out) // best-effort; a bad body still counts as a success
	return true, out.Usage.PromptTokens, out.Usage.CompletionTokens
}
