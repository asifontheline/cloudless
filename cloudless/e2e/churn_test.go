package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// D1: churn test harness. TestTwoNodeMeshJoinAndChurn (multinode_test.go)
// proves the mesh survives one kill event. A real harness needs to prove it
// survives repeated churn — nodes leaving and new ones joining, over
// several rounds — without ever losing the ability to serve requests or
// accumulating stale registry entries.

// churnMesh holds a live seed plus whatever peer nodes are currently
// joined, so rounds can add and kill nodes freely.
type churnMesh struct {
	seedBase   string
	gossipAddr string
	peers      map[string]*node
	backends   map[string]*httptest.Server
}

// launchSeed starts the seed node directly (not via startMeshNode, which
// doesn't expose the gossip address callers need to hand to joiners) and
// waits for it to come up.
func launchSeed(t *testing.T, gossipPort, apiPort int, backendURL string) string {
	t.Helper()
	cfg := map[string]any{
		"listen": fmt.Sprintf("127.0.0.1:%d", apiPort), "api_key": "e2e-key",
		"health_interval_seconds": 1,
		"gossip": map[string]any{
			"node_name": "seed", "bind": fmt.Sprintf("127.0.0.1:%d", gossipPort),
			"backend_url": backendURL, "secret": meshSecret,
		},
	}
	dir := t.TempDir()
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binPath, "serve", "-config", cfgPath)
	cmd.Dir = dir
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		t.Logf("seed output:\n%s", out.String())
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	for i := 0; i < 100; i++ {
		if r, err := http.Get(base + "/healthz"); err == nil {
			r.Body.Close()
			return base
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("seed did not come up")
	return ""
}

// waitForConverge polls /status until the backend count (and, if wantHealthy
// >= 0, the healthy count) matches, or fails the test after timeout.
func waitForConverge(t *testing.T, base string, wantTotal, wantHealthy int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		total, healthy, names := healthyBackends(t, base)
		totalOK := total == wantTotal
		healthyOK := wantHealthy < 0 || healthy == wantHealthy
		if totalOK && healthyOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("mesh did not converge: total=%d (want %d) healthy=%d (want %d) names=%v",
				total, wantTotal, healthy, wantHealthy, names)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// churnRound joins a fresh node, confirms the mesh absorbs it and keeps
// serving, then kills that same node and confirms the mesh converges back
// down and keeps serving through the loss too.
func churnRound(t *testing.T, cm *churnMesh, roundNum int) {
	t.Helper()

	joinName := fmt.Sprintf("churn-%d", roundNum)
	be := stubBackend(joinName)
	t.Cleanup(be.Close)
	n := startMeshNode(t, joinName, be.URL, cm.gossipAddr)
	cm.peers[joinName] = n
	cm.backends[joinName] = be

	waitForConverge(t, cm.seedBase, len(cm.peers)+1, len(cm.peers)+1, 15*time.Second)
	if code, _ := chat(t, cm.seedBase, "e2e-key"); code != http.StatusOK {
		t.Fatalf("round %d: mesh must serve after join, got %d", roundNum, code)
	}

	// SIGTERM (not Kill) so the leaving node runs its graceful shutdown —
	// including the deferred mesh.Leave() broadcast — instead of forcing
	// the rest of the mesh to fall back to slower SWIM failure detection.
	// A hard-crash scenario is already covered by
	// TestTwoNodeMeshJoinAndChurn; this harness is about churn — nodes
	// coming and going, including clean departures — across many rounds.
	n.cmd.Process.Signal(syscall.SIGTERM)
	n.cmd.Wait()
	be.Close()
	delete(cm.peers, joinName)
	delete(cm.backends, joinName)

	waitForConverge(t, cm.seedBase, len(cm.peers)+1, -1, 20*time.Second)
	if code, _ := chat(t, cm.seedBase, "e2e-key"); code != http.StatusOK {
		t.Fatalf("round %d: mesh must keep serving after churn, got %d", roundNum, code)
	}
}

// The mesh survives four full rounds of churn — a new node joining and then
// dying, back to back — without ever failing a request from the seed, and
// without accumulating stale entries in the registry once everything that
// joined has since left.
func TestChurnHarnessMultiRound(t *testing.T) {
	seedBackend := stubBackend("seed")
	defer seedBackend.Close()

	gossipPort, apiPort := freePort(t), freePort(t)
	seedBase := launchSeed(t, gossipPort, apiPort, seedBackend.URL)

	cm := &churnMesh{
		seedBase:   seedBase,
		gossipAddr: fmt.Sprintf("127.0.0.1:%d", gossipPort),
		peers:      map[string]*node{},
		backends:   map[string]*httptest.Server{},
	}

	const rounds = 4
	for i := 1; i <= rounds; i++ {
		churnRound(t, cm, i)
	}

	// Back to exactly the seed alone — no ghost entries left behind.
	waitForConverge(t, seedBase, 1, 1, 10*time.Second)

	// And still fully functional: a batch of requests all succeed.
	items := strings.TrimSuffix(strings.Repeat(`{"messages":[]},`, 6), ",")
	req, _ := http.NewRequest(http.MethodPost, seedBase+"/v1/batch",
		strings.NewReader(`{"requests":[`+items+`]}`))
	req.Header.Set("Authorization", "Bearer e2e-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Results []struct {
			Status int `json:"status"`
		} `json:"results"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Results) != 6 {
		t.Fatalf("want 6 batch results after churn, got %d", len(out.Results))
	}
	for i, r := range out.Results {
		if r.Status != http.StatusOK {
			t.Fatalf("post-churn batch item %d: status %d", i, r.Status)
		}
	}
}
