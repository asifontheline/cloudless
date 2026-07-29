package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloudless/internal/audit"
	"cloudless/internal/config"
	"cloudless/internal/ext"
	"cloudless/internal/registry"
)

// newTestGateway builds a gateway over the given backend URLs, no TLS.
func newTestGateway(t *testing.T, urls ...string) *Gateway {
	t.Helper()
	backends := make([]config.Backend, len(urls))
	for i, u := range urls {
		backends[i] = config.Backend{Name: string(rune('a' + i)), BaseURL: u}
	}
	return New(registry.New(backends, time.Hour, nil), "test-key", nil)
}

// newTestAudit opens an audit log backed by a fresh temp file.
func newTestAudit(t *testing.T) *audit.Log {
	t.Helper()
	return audit.Open(filepath.Join(t.TempDir(), "audit.log"))
}

func proxyRequest(t *testing.T, g *Gateway, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	return rec
}

// A stream that dies before its first token must be retried on another
// backend; the client sees one clean, complete response (C5).
func TestStreamRetryBeforeFirstByte(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Commit headers, then drop the connection with zero body bytes.
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer dead.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: hello\n\ndata: [DONE]\n\n")
	}))
	defer good.Close()

	g := newTestGateway(t, dead.URL, good.URL)
	// Ranked order is not deterministic without probes; run several requests
	// so both orders occur. Every request must succeed regardless.
	for i := 0; i < 10; i++ {
		rec := proxyRequest(t, g, `{"stream":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, "data: hello") {
			t.Fatalf("request %d: incomplete stream: %q", i, body)
		}
	}
}

// When every backend dies pre-first-byte the client gets one clean 502 —
// never a half-committed response.
func TestStreamAllBackendsDead(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer dead.Close()

	g := newTestGateway(t, dead.URL)
	rec := proxyRequest(t, g, `{"stream":true}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502", rec.Code)
	}
}

// A buffered (non-stream) body that fails mid-read is retried: nothing was
// committed to the client yet.
func TestBufferedRetryOnTruncatedBody(t *testing.T) {
	truncated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1000") // promise more than we send
		io.WriteString(w, `{"partial":`)
		conn, _, _ := w.(http.Hijacker).Hijack()
		conn.Close()
	}))
	defer truncated.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`)
	}))
	defer good.Close()

	g := newTestGateway(t, truncated.URL, good.URL)
	for i := 0; i < 10; i++ {
		rec := proxyRequest(t, g, `{}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"choices"`) {
			t.Fatalf("request %d: wrong body: %q", i, rec.Body.String())
		}
	}
}

// Connection-refused backends keep failing over to a healthy one (existing
// behavior, pinned by a test case).
func TestConnectErrorFailover(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer good.Close()

	g := newTestGateway(t, "http://127.0.0.1:1", good.URL)
	for i := 0; i < 5; i++ {
		rec := proxyRequest(t, g, `{}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, rec.Code)
		}
	}
}

// /join-info reveals the mesh secret — it must demand the admin key and be
// absent on nodes that can't share it (E2).
func TestJoinInfoAdminGate(t *testing.T) {
	g := newTestGateway(t)
	g.JoinInfo = func() (string, string, string) { return "sec", "1.2.3.4:7946", "http://1.2.3.4:8080" }

	req := httptest.NewRequest(http.MethodGet, "/join-info", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("no admin key must be refused: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/join-info", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin key: status %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "1.2.3.4:7946") || !strings.Contains(body, `"secret":"sec"`) {
		t.Fatalf("join info body wrong: %s", body)
	}

	g2 := newTestGateway(t)
	req = httptest.NewRequest(http.MethodGet, "/join-info", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	g2.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unset JoinInfo: status %d, want 404", rec.Code)
	}
}

// O1: a batch fans out across backends concurrently, returns results in
// submission order, and spreads load over more than one node.
func TestBatchFanOut(t *testing.T) {
	var hitsA, hitsB atomic.Int64
	mk := func(hits *atomic.Int64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			var in struct {
				N int `json:"n"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"echo":%d,"usage":{"prompt_tokens":1,"completion_tokens":1}}`, in.N)
		}))
	}
	a, b := mk(&hitsA), mk(&hitsB)
	defer a.Close()
	defer b.Close()

	g := newTestGateway(t, a.URL, b.URL)
	items := make([]string, 12)
	for i := range items {
		items[i] = fmt.Sprintf(`{"n":%d}`, i)
	}
	body := `{"path":"/v1/chat/completions","requests":[` + strings.Join(items, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Results []struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 12 {
		t.Fatalf("want 12 results, got %d", len(out.Results))
	}
	for i, res := range out.Results {
		if res.Status != 200 {
			t.Fatalf("item %d status %d", i, res.Status)
		}
		var echo struct {
			Echo int `json:"echo"`
		}
		json.Unmarshal(res.Body, &echo)
		if echo.Echo != i {
			t.Fatalf("order broken: item %d echoed %d", i, echo.Echo)
		}
	}
	if hitsA.Load() == 0 || hitsB.Load() == 0 {
		t.Fatalf("fan-out must spread across nodes: a=%d b=%d", hitsA.Load(), hitsB.Load())
	}
}

// A batch's honest speedup (O6): fanning N slow items across backends
// should measure faster than their summed individual durations, and the
// response must report both numbers, not just a claimed multiplier.
// Divide-and-conquer (O5): a template applied across many chunks, fanned
// out concurrently, and merged into one result in submission order.
func TestMapAppliesTemplateAndMerges(t *testing.T) {
	mk := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"[%s]"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
				in.Messages[0].Content)
		}))
	}
	a, b := mk(), mk()
	defer a.Close()
	defer b.Close()

	g := newTestGateway(t, a.URL, b.URL)
	body := `{"template":"summarize: {{chunk}}","chunks":["one","two","three"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/map", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Results []struct {
			Status int `json:"status"`
		} `json:"results"`
		Merged string `json:"merged"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 3 {
		t.Fatalf("want 3 results, got %d", len(out.Results))
	}
	want := "[summarize: one]\n\n[summarize: two]\n\n[summarize: three]"
	if out.Merged != want {
		t.Fatalf("merged = %q, want %q", out.Merged, want)
	}
}

// A template missing {{chunk}} or an empty chunk list are rejected before
// any request is fanned out, not silently applied verbatim to every chunk.
func TestMapShapeValidation(t *testing.T) {
	g := newTestGateway(t, "http://127.0.0.1:1")
	for _, body := range []string{
		`not json`,
		`{"template":"no placeholder here","chunks":["a"]}`,
		`{"template":"has {{chunk}}","chunks":[]}`,
		`{"template":"has {{chunk}}","chunks":["a"],"merge":"bogus"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/map", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: got %d, want 400", body, rec.Code)
		}
	}
}

// merge:"none" skips the concatenation — the caller wants per-chunk
// results only, not a merged blob it didn't ask for.
func TestMapMergeNoneOmitsMerged(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"x"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer svc.Close()

	g := newTestGateway(t, svc.URL)
	body := `{"template":"{{chunk}}","chunks":["a"],"merge":"none"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/map", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"merged"`) {
		t.Fatalf("merge:none must omit the merged field, got: %s", rec.Body.String())
	}
}

func TestBatchReportsHonestSpeedup(t *testing.T) {
	mk := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(80 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
		}))
	}
	a, b := mk(), mk()
	defer a.Close()
	defer b.Close()

	g := newTestGateway(t, a.URL, b.URL)
	items := make([]string, 8)
	for i := range items {
		items[i] = `{"n":1}`
	}
	body := `{"path":"/v1/chat/completions","requests":[` + strings.Join(items, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Results []struct {
			DurationMS int64 `json:"duration_ms"`
		} `json:"results"`
		Timing struct {
			ElapsedMS    int64   `json:"elapsed_ms"`
			SequentialMS int64   `json:"sequential_ms"`
			SpeedupX     float64 `json:"speedup_x"`
		} `json:"timing"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for i, r := range out.Results {
		if r.DurationMS <= 0 {
			t.Errorf("item %d has no measured duration", i)
		}
	}
	if out.Timing.ElapsedMS <= 0 || out.Timing.SequentialMS <= 0 {
		t.Fatalf("timing summary missing measured values: %+v", out.Timing)
	}
	if out.Timing.SpeedupX <= 1.0 {
		t.Fatalf("fan-out across 2 backends should measure a speedup > 1x, got %.2f (elapsed=%dms sequential=%dms)",
			out.Timing.SpeedupX, out.Timing.ElapsedMS, out.Timing.SequentialMS)
	}
}

// A batch item that hits a dead backend fails over like a single request.
func TestBatchItemFailover(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer good.Close()
	g := newTestGateway(t, "http://127.0.0.1:1", good.URL)
	body := `{"requests":[{},{},{},{}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	var out struct {
		Results []struct {
			Status int `json:"status"`
		} `json:"results"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	for i, res := range out.Results {
		if res.Status != 200 {
			t.Fatalf("item %d must fail over to the healthy node, got %d", i, res.Status)
		}
	}
}

// Batch size limits are enforced.
func TestBatchLimits(t *testing.T) {
	g := newTestGateway(t, "http://127.0.0.1:1")
	for _, body := range []string{`{"requests":[]}`, `{"requests":[` + strings.Repeat(`{},`, 64) + `{}]}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/batch", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		g.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad batch size must 400, got %d", rec.Code)
		}
	}
}

// /names lists every backend the registry knows about, resolvable by name
// without hardcoding an address (E3).
func TestNamesListsBackends(t *testing.T) {
	g := newTestGateway(t, "http://backend-a:1", "http://backend-b:2")
	req := httptest.NewRequest(http.MethodGet, "/names", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var out struct {
		Names []NameEntry `json:"names"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Names) != 2 {
		t.Fatalf("want 2 names, got %d: %+v", len(out.Names), out.Names)
	}
	for _, e := range out.Names {
		if e.Kind != "node" {
			t.Errorf("want kind=node, got %q", e.Kind)
		}
	}
}

// /names/{name} resolves one entry directly, the CLI's `resolve` path.
func TestNamesResolveByName(t *testing.T) {
	g := newTestGateway(t, "http://backend-a:1")
	req := httptest.NewRequest(http.MethodGet, "/names/a", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var e NameEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Name != "a" || e.Address != "http://backend-a:1" {
		t.Fatalf("wrong entry: %+v", e)
	}
}

// An unknown name resolves to 404, not a silent empty result.
func TestNamesResolveUnknown(t *testing.T) {
	g := newTestGateway(t, "http://backend-a:1")
	req := httptest.NewRequest(http.MethodGet, "/names/does-not-exist", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

// Registered extensions (K4) appear in the same directory as nodes, so
// callers resolve either kind through one interface.
func TestNamesIncludesExtensions(t *testing.T) {
	g := newTestGateway(t, "http://backend-a:1")
	g.Ext = ext.Open(filepath.Join(t.TempDir(), "ext.json"))
	if _, err := g.Ext.Register(ext.Extension{Name: "embeddings", BaseURL: "http://127.0.0.1:9090"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/names", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	var out struct {
		Names []NameEntry `json:"names"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	found := false
	for _, e := range out.Names {
		if e.Kind == "extension" && e.Name == "embeddings" && e.Address == "http://127.0.0.1:9090" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extension not in names directory: %+v", out.Names)
	}
}

// GET /metrics serves Prometheus-compatible text exposition, reflecting
// real backend state (D3). L1 backfill: this handler had zero coverage —
// only the underlying telemetry.Render() was tested.
func TestHandleMetrics(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	g := newTestGateway(t, backend.URL)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain prefix", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# HELP cloudless_backend_healthy",
		"cloudless_backend_healthy{backend=\"a\"}",
		"cloudless_inflight_requests",
		"cloudless_max_concurrent_requests",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics output missing %q, got:\n%s", want, body)
		}
	}
}

// The formal API spec is served by every node and stays valid YAML (K1).
func TestOpenAPIServed(t *testing.T) {
	g := newTestGateway(t)
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	g.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"openapi:", "/v1/chat/completions", "/v1/batch", "/enroll", "/join-tokens"} {
		if !strings.Contains(body, want) {
			t.Fatalf("spec missing %q", want)
		}
	}
}
