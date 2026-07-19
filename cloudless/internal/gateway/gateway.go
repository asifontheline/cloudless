package gateway

import (
	"bytes"
	"crypto/subtle"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloudless/internal/audit"
	"cloudless/internal/inflight"
	"cloudless/internal/keys"
	"cloudless/internal/quota"
	"cloudless/internal/registry"
	"cloudless/internal/revoke"
	"cloudless/internal/share"
	"cloudless/internal/store"
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

	// Models, when set, is the content-addressed model store.
	Models *store.Store

	// Share, when set, holds this node's resource-sharing limits.
	Share *share.Store

	// Audit, when set, records administrative actions in a hash-chained log.
	Audit *audit.Log

	// Revoke, when set, evicts a node from the mesh (persist + broadcast +
	// drop from routing). RevokedList lists current revocations.
	Revoke      func(name string) bool
	RevokedList func() []revoke.Record

	// Limiter, when set, applies backpressure to inference requests.
	Limiter *inflight.Limiter

	// MintJoinToken, when set (CA-holding node), mints a single-use
	// expiring join token for enrolling a new node (A2).
	MintJoinToken func(ttl time.Duration) (token string, expires time.Time, err error)
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
	mux.HandleFunc("GET /savings", g.handleSavings)
	mux.HandleFunc("GET /capacity", g.handleCapacity)
	mux.HandleFunc("GET /audit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ok, at := true, int64(0)
		var entries []audit.Entry
		if g.Audit != nil {
			entries = g.Audit.List(200)
			ok, at = g.Audit.Verify()
		}
		json.NewEncoder(w).Encode(map[string]any{"entries": entries, "intact": ok, "broken_at": at})
	})
	mux.HandleFunc("GET /revocations", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var list []revoke.Record
		if g.RevokedList != nil {
			list = g.RevokedList()
		}
		json.NewEncoder(w).Encode(map[string]any{"revoked": list})
	})
	mux.HandleFunc("POST /revoke/{name}", g.adminOnly(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if g.Revoke == nil || !g.Revoke(name) {
			http.Error(w, `{"error":"already revoked or unknown"}`, http.StatusConflict)
			return
		}
		g.Audit.Append("cluster", "node.revoke", name, "")
		log.Printf("revoke: node %s evicted from the mesh", name)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /join-tokens", g.adminOnly(func(w http.ResponseWriter, r *http.Request) {
		if g.MintJoinToken == nil {
			http.Error(w, `{"error":"this node does not hold the cluster CA"}`, http.StatusNotFound)
			return
		}
		var body struct {
			TTLMinutes int `json:"ttl_minutes"`
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&body) // empty body = default TTL
		tok, exp, err := g.MintJoinToken(time.Duration(body.TTLMinutes) * time.Minute)
		if err != nil {
			http.Error(w, `{"error":"mint failed"}`, http.StatusInternalServerError)
			return
		}
		g.Audit.Append("cluster", "join-token.mint", "", "expires "+exp.UTC().Format(time.RFC3339))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"token": tok, "expires": exp.UTC()})
	}))
	mux.HandleFunc("GET /share", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"limits": g.Share.Get(), "ceiling": share.Ceiling, "default": share.Default,
			"shared_cores": g.Share.MaxProcs(),
		})
	})
	mux.HandleFunc("PUT /share", g.adminOnly(func(w http.ResponseWriter, r *http.Request) {
		// Partial update: only fields present in the body change; omitted
		// fields keep their current value (so setting CPU alone doesn't
		// silently zero everything else).
		var body struct {
			CPUPercent *int    `json:"cpu_percent"`
			DiskGB     *int    `json:"disk_gb"`
			ShareWhen  *string `json:"share_when"`
			MeteredOK  *bool   `json:"metered_ok"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		cur := g.Share.Get()
		if body.CPUPercent != nil {
			cur.CPUPercent = *body.CPUPercent
		}
		if body.DiskGB != nil {
			cur.DiskGB = *body.DiskGB
		}
		if body.ShareWhen != nil {
			cur.ShareWhen = *body.ShareWhen
		}
		if body.MeteredOK != nil {
			cur.MeteredOK = *body.MeteredOK
		}
		applied := g.Share.Set(cur)
		g.Audit.Append("cluster", "share.set", "this-node", fmt.Sprintf("cpu=%d%% when=%s", applied.CPUPercent, applied.ShareWhen))
		log.Printf("share: limits set to %d%% CPU (ceiling %d%%), when=%s", applied.CPUPercent, share.Ceiling, applied.ShareWhen)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"limits": applied, "ceiling": share.Ceiling, "shared_cores": g.Share.MaxProcs()})
	}))
	mux.HandleFunc("GET /store", g.handleStoreList)
	mux.HandleFunc("PUT /store", g.adminOnly(g.handleStoreAdd))
	mux.HandleFunc("POST /store/pull", g.adminOnly(g.handleStorePull))
	mux.HandleFunc("GET /store/verify", g.handleStoreVerify)
	mux.HandleFunc("DELETE /store/{name}", g.adminOnly(g.handleStoreDelete))
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
	// Backpressure: bound concurrent in-flight requests. When saturated,
	// tell the caller to retry instead of piling onto overloaded backends.
	if g.Limiter != nil {
		release, ok := g.Limiter.Acquire(r.Context())
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(g.Limiter.RetryAfter().Seconds())+1))
			http.Error(w, `{"error":"node busy — retry shortly (backpressure)"}`, http.StatusServiceUnavailable)
			return
		}
		defer release()
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
		// Mid-stream failover (C5): deliver reports false when the backend
		// died before the first byte was committed to the client — safe to
		// retry on the next backend. After the first byte, no retry.
		if !g.deliver(w, r, resp, b.Backend.Name) {
			lastErr = nil
			log.Printf("backend %s failed before first byte, trying next", b.Backend.Name)
			continue
		}
		g.logRoute(r.URL.Path, b.Backend.Name, resp.StatusCode, i)
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
//
// It returns false when the backend failed before anything was committed to
// the client — the caller may then retry the next backend. Once the first
// byte has been written, failures can only truncate the response.
func (g *Gateway) deliver(w http.ResponseWriter, r *http.Request, resp *http.Response, backendName string) bool {
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		// Read the first chunk before committing: a stream that dies before
		// its first token is retried on the next backend.
		buf := make([]byte, 32*1024)
		n, err := resp.Body.Read(buf)
		for n == 0 && err == nil {
			n, err = resp.Body.Read(buf)
		}
		if n == 0 {
			resp.Body.Close()
			return false
		}
		g.Usage.Add(key, backendName, 1, 0, 0)
		copyResponse(w, resp, buf[:n])
		return true
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return false // nothing written yet — caller retries the next backend
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
	return true
}

// copyResponse streams the response through, starting with the already-read
// first chunk (may be empty). From here on the response is committed.
func copyResponse(w http.ResponseWriter, resp *http.Response, first []byte) {
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if len(first) > 0 {
		if _, werr := w.Write(first); werr != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
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

func (g *Gateway) handleStoreList(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"artifacts": g.Models.List()})
}

func (g *Gateway) handleStoreAdd(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, `{"error":"name query parameter required"}`, http.StatusBadRequest)
		return
	}
	e, err := g.Models.Add(name, r.Body)
	if err != nil {
		log.Printf("store: rejected %q: %v", name, err)
		http.Error(w, `{"error":`+strconv.Quote(err.Error())+`}`, http.StatusUnprocessableEntity)
		return
	}
	log.Printf("store: added %s (%s, %d bytes, sha256 %s…)", e.Name, e.Format, e.Size, e.SHA256[:12])
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(e)
}

// handleStorePull fetches a model from the mesh: it asks each peer's relay
// what it holds and, on the first peer that has the artifact, streams the
// blob through the mutual-TLS relay into the local store (hash + format
// verified on write). Public repositories are only a fallback the caller
// reaches for if this returns not-found.
func (g *Gateway) handleStorePull(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	if _, ok := g.Models.Path(name); ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"pulled": false, "reason": "already present"})
		return
	}
	for _, b := range g.reg.Ranked() {
		base := strings.TrimSuffix(b.Backend.BaseURL, "/v1")
		if !strings.HasPrefix(base, "https://") {
			continue // only mutual-TLS relays serve blobs
		}
		list, err := g.client.Get(base + "/store")
		if err != nil {
			continue
		}
		var lr struct {
			Artifacts []store.Entry `json:"artifacts"`
		}
		json.NewDecoder(list.Body).Decode(&lr)
		list.Body.Close()
		has := false
		for _, a := range lr.Artifacts {
			if a.Name == name {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		blob, err := g.client.Get(base + "/blob?name=" + name)
		if err != nil || blob.StatusCode != http.StatusOK {
			if blob != nil {
				blob.Body.Close()
			}
			continue
		}
		e, err := g.Models.Add(name, blob.Body)
		blob.Body.Close()
		if err != nil {
			log.Printf("store: pull of %q from %s failed verification: %v", name, b.Backend.Name, err)
			continue
		}
		log.Printf("store: pulled %s from %s (sha256 %s…)", name, b.Backend.Name, e.SHA256[:12])
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"pulled": true, "from": b.Backend.Name, "sha256": e.SHA256})
		return
	}
	http.Error(w, `{"error":"no peer in the mesh has this artifact — fall back to a public repository"}`, http.StatusNotFound)
}

func (g *Gateway) handleStoreVerify(w http.ResponseWriter, r *http.Request) {
	ok, err := g.Models.Verify(r.URL.Query().Get("name"))
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"ok": ok})
}

func (g *Gateway) handleStoreDelete(w http.ResponseWriter, r *http.Request) {
	if !g.Models.Delete(r.PathValue("name")) {
		http.Error(w, `{"error":"unknown artifact"}`, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	g.Audit.Append("cluster", "keys.create", req.Name, "")
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
	g.Audit.Append("cluster", "keys.revoke", prefix+"…", "")
	log.Printf("keys: revoked %s…", prefix)
	w.WriteHeader(http.StatusNoContent)
}

// handleSavings estimates what the mesh's served tokens would have cost on a
// metered hosted API. Reference rates are generic (USD per 1M tokens),
// overridable via query params — no provider is named or implied.
func (g *Gateway) handleSavings(w http.ResponseWriter, r *http.Request) {
	promptRate, completionRate := 0.50, 1.50 // typical hosted small-model class, USD/1M tokens
	if v, err := strconv.ParseFloat(r.URL.Query().Get("prompt_per_1m"), 64); err == nil && v >= 0 {
		promptRate = v
	}
	if v, err := strconv.ParseFloat(r.URL.Query().Get("completion_per_1m"), 64); err == nil && v >= 0 {
		completionRate = v
	}
	var prompt, completion, requests int64
	for _, rec := range g.Usage.Snapshot() {
		prompt += rec.PromptTokens
		completion += rec.CompletionTokens
		requests += rec.Requests
	}
	hosted := float64(prompt)/1e6*promptRate + float64(completion)/1e6*completionRate
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"requests":              requests,
		"prompt_tokens":         prompt,
		"completion_tokens":     completion,
		"reference_rates_usd":   map[string]float64{"prompt_per_1m": promptRate, "completion_per_1m": completionRate},
		"hosted_equivalent_usd": hosted,
		"mesh_marginal_usd":     0.0,
		"note":                  "Mesh marginal cost is zero by design; real costs are electricity and owned hardware. Reference rates are generic hosted-API figures — adjust to your comparison point.",
	})
}

// handleCapacity surfaces idle capacity: healthy nodes that have served
// nothing recently are flagged so the group uses hardware it already owns
// instead of letting it sit — the co-op answer to zombie cloud spend.
func (g *Gateway) handleCapacity(w http.ResponseWriter, _ *http.Request) {
	type nodeCap struct {
		Node        string     `json:"node"`
		Healthy     bool       `json:"healthy"`
		LatencyMS   int64      `json:"latency_ms"`
		Requests    int64      `json:"requests"`
		LastServed  *time.Time `json:"last_served,omitempty"`
		Idle        bool       `json:"idle"`
		IdleSeconds int64      `json:"idle_seconds"`
	}
	served := map[string]struct {
		reqs int64
		last time.Time
	}{}
	for _, rec := range g.Usage.Snapshot() {
		s := served[rec.Backend]
		s.reqs += rec.Requests
		if rec.LastUsed.After(s.last) {
			s.last = rec.LastUsed
		}
		served[rec.Backend] = s
	}
	const idleAfter = 10 * time.Minute
	now := time.Now()
	out := []nodeCap{}
	var idleCount int
	for _, b := range g.reg.Ranked() {
		n := nodeCap{Node: b.Backend.Name, Healthy: b.Healthy, LatencyMS: b.LatencyMS}
		if s, ok := served[b.Backend.Name]; ok && s.reqs > 0 {
			n.Requests = s.reqs
			t := s.last
			n.LastServed = &t
			n.IdleSeconds = int64(now.Sub(t).Seconds())
			n.Idle = b.Healthy && now.Sub(t) > idleAfter
		} else {
			n.Idle = b.Healthy
			n.IdleSeconds = -1 // never served
		}
		if n.Idle {
			idleCount++
		}
		out = append(out, n)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"nodes": out, "idle_nodes": idleCount,
		"note": "Idle = healthy but nothing served in 10+ minutes. This capacity is already paid for — point work at it.",
	})
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
	inflightN, waiting := g.Limiter.Stats()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"backends": g.reg.Ranked(),
		"routes":   routes,
		"load": map[string]any{
			"inflight": inflightN, "waiting": waiting, "max_concurrent": g.Limiter.Capacity(),
		},
	})
}
