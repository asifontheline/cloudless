package gateway

import (
	"bytes"
	"crypto/subtle"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"cloudless/internal/keys"
	"cloudless/internal/quota"
	"cloudless/internal/registry"
	"cloudless/internal/usage"
)

//go:embed ui/index.html
var consoleHTML []byte

type RouteEntry struct {
	Time    time.Time `json:"time"`
	Path    string    `json:"path"`
	Backend string    `json:"backend"`
	Status  int       `json:"status"`
	Retries int       `json:"retries"`
}

type Gateway struct {
	reg    *registry.Registry
	apiKey string
	client *http.Client

	mu     sync.Mutex
	routes []RouteEntry // ring buffer of recent routing decisions

	// EnrollHandler, when set (CA-holding node), serves POST /enroll.
	EnrollHandler http.HandlerFunc

	// Usage, when set, accumulates per-key/backend accounting.
	Usage *usage.Store

	// Quota, when set, enforces per-key fair-use limits.
	Quota *quota.Limiter

	// Keys, when set, holds per-user API keys (cluster key stays admin).
	Keys *keys.Store
}

const routeLogSize = 20

// New builds the gateway; tlsCfg (may be nil) carries the node's client cert
// for proxying to peers' mutual-TLS relays.
func New(reg *registry.Registry, apiKey string, tlsCfg *tls.Config) *Gateway {
	return &Gateway{
		reg:    reg,
		apiKey: apiKey,
		// No overall timeout: chat completions stream for minutes.
		client: &http.Client{Timeout: 0, Transport: &http.Transport{TLSClientConfig: tlsCfg}},
	}
}

func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /status", g.handleStatus)
	if g.EnrollHandler != nil {
		mux.HandleFunc("POST /enroll", g.EnrollHandler)
	}
	mux.HandleFunc("GET /ledger", g.handleLedger)
	mux.HandleFunc("GET /keys", g.adminOnly(g.handleKeysList))
	mux.HandleFunc("POST /keys", g.adminOnly(g.handleKeysCreate))
	mux.HandleFunc("DELETE /keys/{prefix}", g.adminOnly(g.handleKeysRevoke))
	mux.HandleFunc("GET /usage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		limits, quotas := g.Quota.Snapshot()
		json.NewEncoder(w).Encode(map[string]any{
			"usage": g.Usage.Snapshot(), "limits": limits, "quotas": quotas,
		})
	})
	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(consoleHTML)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/v1/", g.auth(g.handleProxy))
	return mux
}

func (g *Gateway) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.apiKey != "" {
			got := r.Header.Get("Authorization")
			want := "Bearer " + g.apiKey
			admin := subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
			if !admin && !g.Keys.Active(strings.TrimPrefix(got, "Bearer ")) {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

// handleProxy forwards an OpenAI-style request to the best backend,
// failing over to the next-ranked backend on errors that occur before
// any response byte has been written to the client.
func (g *Gateway) handleProxy(w http.ResponseWriter, r *http.Request) {
	key := usage.Redact(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if ok, retry := g.Quota.Allow(key); !ok {
		w.Header().Set("Retry-After", retry.Round(time.Second).String())
		http.Error(w, `{"error":"quota exceeded — group fair-use limit reached"}`, http.StatusTooManyRequests)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	backends := g.reg.Ranked()
	if len(backends) == 0 {
		http.Error(w, `{"error":"no backends configured"}`, http.StatusServiceUnavailable)
		return
	}

	var lastErr error
	for i, b := range backends {
		req, err := http.NewRequestWithContext(r.Context(), r.Method, b.Backend.BaseURL+trimV1(r.URL.Path), bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := g.client.Do(req)
		if err != nil {
			lastErr = err
			log.Printf("backend %s failed (%v), trying next", b.Backend.Name, err)
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = nil
			log.Printf("backend %s returned %d, trying next", b.Backend.Name, resp.StatusCode)
			continue
		}
		g.logRoute(r.URL.Path, b.Backend.Name, resp.StatusCode, i)
		g.deliver(w, r, resp, b.Backend.Name)
		return
	}
	g.logRoute(r.URL.Path, "-", http.StatusBadGateway, len(backends))
	if lastErr != nil {
		log.Printf("all backends failed: %v", lastErr)
	}
	http.Error(w, `{"error":"all backends unavailable"}`, http.StatusBadGateway)
}

// trimV1 maps the gateway path onto the backend base URL, which already
// ends in /v1 — /v1/chat/completions becomes /chat/completions.
func trimV1(path string) string {
	const p = "/v1"
	if len(path) >= len(p) && path[:len(p)] == p {
		return path[len(p):]
	}
	return path
}

// deliver relays the backend response to the client. Non-streaming JSON is
// buffered so token usage can be read from the body; streams pass through
// untouched and count requests only.
func (g *Gateway) deliver(w http.ResponseWriter, r *http.Request, resp *http.Response, backendName string) {
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		g.Usage.Add(key, backendName, 1, 0, 0)
		copyResponse(w, resp)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		http.Error(w, `{"error":"upstream read"}`, http.StatusBadGateway)
		return
	}
	var parsed struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	json.Unmarshal(body, &parsed)
	g.Usage.Add(key, backendName, 1, parsed.Usage.PromptTokens, parsed.Usage.CompletionTokens)
	g.Quota.AddTokens(usage.Redact(key), parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens)
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func copyResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func (g *Gateway) logRoute(path, backend string, status, retries int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.routes = append(g.routes, RouteEntry{Time: time.Now(), Path: path, Backend: backend, Status: status, Retries: retries})
	if len(g.routes) > routeLogSize {
		g.routes = g.routes[len(g.routes)-routeLogSize:]
	}
}

// adminOnly gates key management behind the cluster (admin) key.
func (g *Gateway) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := "Bearer " + g.apiKey
		if g.apiKey == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(want)) != 1 {
			http.Error(w, `{"error":"admin key required"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (g *Gateway) handleKeysList(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"keys": g.Keys.List()})
}

func (g *Gateway) handleKeysCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	key, err := g.Keys.Create(strings.TrimSpace(req.Name))
	if err != nil {
		http.Error(w, `{"error":"key generation failed"}`, http.StatusInternalServerError)
		return
	}
	log.Printf("keys: created key for %q", req.Name)
	w.Header().Set("Content-Type", "application/json")
	// The full secret is returned exactly once, at creation.
	json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "key": key})
}

func (g *Gateway) handleKeysRevoke(w http.ResponseWriter, r *http.Request) {
	prefix := r.PathValue("prefix")
	if !g.Keys.Revoke(prefix) {
		http.Error(w, `{"error":"no matching active key"}`, http.StatusNotFound)
		return
	}
	log.Printf("keys: revoked %s…", prefix)
	w.WriteHeader(http.StatusNoContent)
}

// LedgerLine is one party's side of the cooperative ledger.
type LedgerLine struct {
	Party    string  `json:"party"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Share    float64 `json:"share_pct"`
}

// handleLedger aggregates usage into the fairness view: contribution by
// serving node, consumption by API key — the seed of cooperative credits.
func (g *Gateway) handleLedger(w http.ResponseWriter, _ *http.Request) {
	recs := g.Usage.Snapshot()
	nodes := map[string]*LedgerLine{}
	consumers := map[string]*LedgerLine{}
	var totalTokens int64
	for _, r := range recs {
		tok := r.PromptTokens + r.CompletionTokens
		totalTokens += tok
		n, ok := nodes[r.Backend]
		if !ok {
			n = &LedgerLine{Party: r.Backend}
			nodes[r.Backend] = n
		}
		n.Requests += r.Requests
		n.Tokens += tok
		c, ok := consumers[r.APIKey]
		if !ok {
			c = &LedgerLine{Party: r.APIKey}
			consumers[r.APIKey] = c
		}
		c.Requests += r.Requests
		c.Tokens += tok
	}
	toSorted := func(m map[string]*LedgerLine) []LedgerLine {
		out := make([]LedgerLine, 0, len(m))
		for _, l := range m {
			if totalTokens > 0 {
				l.Share = float64(l.Tokens) * 100 / float64(totalTokens)
			}
			out = append(out, *l)
		}
		for i := 0; i < len(out); i++ {
			for j := i + 1; j < len(out); j++ {
				if out[j].Tokens > out[i].Tokens {
					out[i], out[j] = out[j], out[i]
				}
			}
		}
		return out
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"total_tokens": totalTokens,
		"contributed":  toSorted(nodes),
		"consumed":     toSorted(consumers),
	})
}

func (g *Gateway) handleStatus(w http.ResponseWriter, _ *http.Request) {
	g.mu.Lock()
	routes := make([]RouteEntry, len(g.routes))
	copy(routes, g.routes)
	g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"backends": g.reg.Ranked(),
		"routes":   routes,
	})
}
