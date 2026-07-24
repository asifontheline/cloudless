package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const meshSecret = "0123456789abcdef0123456789abcdef" // 32 bytes, test-only

type node struct {
	base string
	cmd  *exec.Cmd
}

// startMeshNode launches the binary with a gossip section (no PKI — peers
// advertise their backend directly), optionally joining a seed.
func startMeshNode(t *testing.T, name, backendURL string, join ...string) *node {
	t.Helper()
	apiPort, gossipPort := freePort(t), freePort(t)
	cfg := map[string]any{
		"listen": fmt.Sprintf("127.0.0.1:%d", apiPort), "api_key": "e2e-key",
		"health_interval_seconds": 1,
		"gossip": map[string]any{
			"node_name": name, "bind": fmt.Sprintf("127.0.0.1:%d", gossipPort),
			"backend_url": backendURL, "secret": meshSecret, "join": join,
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
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	n := &node{base: fmt.Sprintf("http://127.0.0.1:%d", apiPort), cmd: cmd}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for i := 0; i < 300; i++ {
		if r, err := http.Get(n.base + "/healthz"); err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				return n
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("node %s did not come up", name)
	return nil
}

func healthyBackends(t *testing.T, base string) (total, healthy int, names []string) {
	t.Helper()
	resp, err := http.Get(base + "/status")
	if err != nil {
		return 0, 0, nil
	}
	defer resp.Body.Close()
	var st struct {
		Backends []struct {
			Backend struct {
				Name string `json:"name"`
			} `json:"Backend"`
			Healthy bool `json:"Healthy"`
		} `json:"backends"`
	}
	json.NewDecoder(resp.Body).Decode(&st)
	for _, b := range st.Backends {
		names = append(names, b.Backend.Name)
		if b.Healthy {
			healthy++
		}
	}
	return len(st.Backends), healthy, names
}

// Two real node processes form a mesh over encrypted gossip on localhost:
// the seed learns the joiner's backend, work spreads across both, and when
// the joiner process is killed, the seed drops it and keeps serving (L2/D1).
func TestTwoNodeMeshJoinAndChurn(t *testing.T) {
	stubA, stubB := stubBackend("a"), stubBackend("b")
	defer stubA.Close()
	defer stubB.Close()

	gossipPort := freePort(t)
	// Seed with an explicit gossip bind we can hand to the joiner.
	apiPort := freePort(t)
	dir := t.TempDir()
	cfg := map[string]any{
		"listen": fmt.Sprintf("127.0.0.1:%d", apiPort), "api_key": "e2e-key",
		"health_interval_seconds": 1,
		"gossip": map[string]any{
			"node_name": "seed", "bind": fmt.Sprintf("127.0.0.1:%d", gossipPort),
			"backend_url": stubA.URL, "secret": meshSecret,
		},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	seedCmd := exec.Command(binPath, "serve", "-config", cfgPath)
	seedCmd.Dir = dir
	if err := seedCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { seedCmd.Process.Kill(); seedCmd.Wait() })
	seedBase := fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	for i := 0; i < 300; i++ {
		if r, err := http.Get(seedBase + "/healthz"); err == nil {
			r.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	joiner := startMeshNode(t, "joiner", stubB.URL, fmt.Sprintf("127.0.0.1:%d", gossipPort))

	// Membership: the seed must learn the joiner's backend and see it healthy.
	deadline := time.Now().Add(15 * time.Second)
	for {
		total, healthy, names := healthyBackends(t, seedBase)
		if total == 2 && healthy == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("seed never saw 2 healthy backends: total=%d healthy=%d names=%v", total, healthy, names)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Work spreads: a batch through the seed reaches both nodes' backends.
	items := strings.TrimSuffix(strings.Repeat(`{"messages":[]},`, 8), ",")
	req, _ := http.NewRequest(http.MethodPost, seedBase+"/v1/batch",
		strings.NewReader(`{"requests":[`+items+`]}`))
	req.Header.Set("Authorization", "Bearer e2e-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Results []struct {
			Body json.RawMessage `json:"body"`
		} `json:"results"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	served := map[string]int{}
	for _, res := range out.Results {
		var by struct {
			ServedBy string `json:"served_by"`
		}
		json.Unmarshal(res.Body, &by)
		served[by.ServedBy]++
	}
	if served["a"] == 0 || served["b"] == 0 {
		t.Fatalf("batch through the seed must reach both nodes, got %v", served)
	}

	// Churn: kill the joiner process; the seed must keep serving and
	// eventually drop or mark the lost peer unhealthy.
	joiner.cmd.Process.Kill()
	stubB.Close() // its backend dies with it
	deadline = time.Now().Add(20 * time.Second)
	for {
		if code, servedBy := chat(t, seedBase, "e2e-key"); code == http.StatusOK && servedBy != "a" {
			t.Fatalf("request routed to a dead node's backend: %q", servedBy)
		}
		total, healthy, _ := healthyBackends(t, seedBase)
		if total == 1 || healthy == 1 {
			break // lost peer dropped (leave) or marked unhealthy (probe)
		}
		if time.Now().After(deadline) {
			t.Fatalf("seed never converged after churn: total=%d healthy=%d", total, healthy)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if code, _ := chat(t, seedBase, "e2e-key"); code != http.StatusOK {
		t.Fatalf("mesh must keep serving after churn, got %d", code)
	}
}
