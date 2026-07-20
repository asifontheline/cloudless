package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"cloudless/internal/audit"
	"cloudless/internal/config"
	"cloudless/internal/gateway"
	"cloudless/internal/gossip"
	"cloudless/internal/inflight"
	"cloudless/internal/jointoken"
	"cloudless/internal/keys"
	"cloudless/internal/pki"
	"cloudless/internal/quota"
	"cloudless/internal/registry"
	"cloudless/internal/relay"
	"cloudless/internal/replicate"
	"cloudless/internal/revoke"
	"cloudless/internal/share"
	"cloudless/internal/store"
	"cloudless/internal/usage"
	"cloudless/internal/vault"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "up":
		up(os.Args[2:])
	case "serve":
		serve(os.Args[2:])
	case "status":
		status(os.Args[2:])
	case "usage":
		usageCmd(os.Args[2:])
	case "ledger":
		ledgerCmd(os.Args[2:])
	case "keys":
		keysCmd(os.Args[2:])
	case "savings":
		savingsCmd(os.Args[2:])
	case "capacity":
		capacityCmd(os.Args[2:])
	case "models":
		modelsCmd(os.Args[2:])
	case "share":
		shareCmd(os.Args[2:])
	case "nodes":
		nodesCmd(os.Args[2:])
	case "audit":
		auditCmd(os.Args[2:])
	case "token":
		tokenCmd(os.Args[2:])
	default:
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `cloudless (working title) — group-private inference mesh

usage:
  cloudless up     [-join <secret>@<host:port>] [-backend <url>]   # zero-config start
  cloudless serve  -config config.json
  cloudless status -addr http://127.0.0.1:8080`)
}

// up is the zero-friction path: detect a local runtime, generate a config
// with a fresh cluster secret (or join an existing mesh), persist it, print
// the join command for the next machine, and serve.
func up(args []string) {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	joinArg := fs.String("join", "", "join an existing mesh: <secret>@<host:port>")
	backend := fs.String("backend", "", "local inference endpoint (default: auto-detect)")
	listen := fs.String("listen", ":8080", "gateway listen address")
	bind := fs.String("bind", "0.0.0.0:7946", "gossip bind address")
	relayAddr := fs.String("relay", ":9443", "mutual-TLS relay listen address")
	seedAPI := fs.String("seed-api", "", "seed node gateway URL for enrollment (default http://<join-host>:8080)")
	location := fs.String("location", "", "node locality: continent/country/state/city/village")
	joinToken := fs.String("join-token", "", "single-use join token minted on the seed (console or 'cloudless token')")
	fs.Parse(args)

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	dir := filepath.Join(home, ".cloudless")
	cfgPath := filepath.Join(dir, "config.json")

	cfg, err := config.Load(cfgPath)
	if err == nil {
		log.Printf("using existing config %s", cfgPath)
	} else {
		cfg = buildConfig(*joinArg, *backend, *listen, *bind)
		cfg.Gossip.Location = *location
		cfg.PKIDir = filepath.Join(dir, "pki")
		cfg.Relay = *relayAddr
		cfg.Gossip.RelayURL = "https://" + advertiseAddr(*relayAddr) + "/v1"
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Fatal(err)
		}
		// A1/A2: first node mints the cluster CA and self-issues; joiners
		// enroll their public key with the seed, authenticated by the secret.
		if *joinArg == "" {
			if err := pki.EnsureCA(cfg.PKIDir); err != nil {
				log.Fatal(err)
			}
			if err := pki.SelfIssue(cfg.PKIDir, cfg.Gossip.NodeName); err != nil {
				log.Fatal(err)
			}
			log.Print("pki: cluster CA created; node certificate issued")
		} else {
			api := *seedAPI
			if api == "" {
				if _, seed, ok := strings.Cut(*joinArg, "@"); ok {
					seedHost, _, _ := strings.Cut(seed, ":")
					api = "http://" + seedHost + ":8080"
				}
			}
			if err := relay.Enroll(api, cfg.PKIDir, cfg.Gossip.NodeName, []byte(cfg.Gossip.Secret), *joinToken); err != nil {
				log.Fatal(err)
			}
			log.Printf("pki: enrolled with %s; certificate received", api)
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", cfgPath)
	}

	fmt.Printf("\n  Console:  http://127.0.0.1%s/\n", portOf(cfg.Listen))
	fmt.Printf("  API:      http://<this-ip>%s/v1/chat/completions (Bearer %s)\n", portOf(cfg.Listen), cfg.APIKey)
	if cfg.Gossip != nil {
		fmt.Printf("  Add a node: cloudless up -join %s@%s -seed-api http://%s\n\n",
			cfg.Gossip.Secret, advertiseAddr(cfg.Gossip.Bind), advertiseAddr(cfg.Listen))
	}
	runServe(cfg)
}

func buildConfig(joinArg, backend, listen, bind string) *config.Config {
	if backend == "" {
		backend = detectRuntime()
	}
	host, _ := os.Hostname()
	// Suffix guarantees mesh-wide uniqueness even when hostnames collide.
	g := &config.Gossip{NodeName: host + "-" + randomHex(2), Bind: bind, BackendURL: backend}
	apiKey := randomHex(16)
	if joinArg != "" {
		secret, seed, ok := strings.Cut(joinArg, "@")
		if !ok {
			log.Fatal("-join must be <secret>@<host:port>")
		}
		g.Secret = secret
		g.Join = []string{seed}
	} else {
		g.Secret = randomHex(16) // 32 hex chars = 32-byte gossip key
	}
	return &config.Config{Listen: listen, APIKey: apiKey, HealthIntervalSeconds: 5, Gossip: g,
		Quotas: &config.Quotas{RequestsPerMinute: 120, TokensPerDay: 0}}
}

// detectRuntime probes well-known local inference runtime ports.
func detectRuntime() string {
	candidates := []string{"http://127.0.0.1:11434/v1", "http://127.0.0.1:8000/v1", "http://127.0.0.1:8081/v1"}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	for _, base := range candidates {
		resp, err := client.Get(base + "/models")
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < 500 {
			log.Printf("detected local runtime at %s", base)
			return base
		}
	}
	log.Print("no local runtime detected; this node will route to peers only")
	return ""
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func portOf(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return listen[i:]
	}
	return ":8080"
}

// advertiseAddr derives the address peers should dial: an explicitly bound
// IP is kept as-is; wildcard binds advertise the primary outbound IP.
func advertiseAddr(bind string) string {
	port := portOf(bind)
	host := strings.TrimSuffix(bind, port)
	if host != "" && host != "0.0.0.0" && host != "::" && host != "[::]" {
		return host + port
	}
	conn, err := net.Dial("udp", "192.0.2.1:9") // no traffic sent; kernel picks the route
	if err != nil {
		return "<this-ip>" + port
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String() + port
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "config.json", "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	runServe(cfg)
}

func runServe(cfg *config.Config) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Model store, share limits, and the revocation set are opened early so
	// the relay can serve blobs, enforce the share budget, and refuse
	// revoked peers.
	var modelStore *store.Store
	var dataVault *vault.Vault
	var shareStore *share.Store
	var revoked *revoke.Set
	if cfg.PKIDir != "" {
		base := filepath.Dir(cfg.PKIDir)
		if st, err := store.Open(filepath.Join(base, "models")); err == nil {
			modelStore = st
		} else {
			log.Printf("store: %v", err)
		}
		if v, err := vault.Open(filepath.Join(base, "vault")); err == nil {
			dataVault = v
		} else {
			log.Printf("vault: %v", err)
		}
		shareStore = share.Open(filepath.Join(base, "share.json"))
		revoked = revoke.Open(filepath.Join(base, "revocations.json"))
	} else {
		shareStore = share.Open("share.json")
		revoked = revoke.Open("revocations.json")
	}
	log.Printf("share: %d%% CPU (%d shared core(s)); ceiling %d%%",
		shareStore.Get().CPUPercent, shareStore.MaxProcs(), share.Ceiling)

	// A3: with PKI present, start the mutual-TLS relay and dial peers with
	// the node certificate; peer traffic is never plaintext.
	var peerTLS *tls.Config
	secure := cfg.PKIDir != "" && pki.HasCreds(cfg.PKIDir)
	if secure {
		var err error
		peerTLS, err = pki.ClientTLS(cfg.PKIDir, revoked.Has)
		if err != nil {
			log.Fatal(err)
		}
		backendURL := ""
		if cfg.Gossip != nil {
			backendURL = cfg.Gossip.BackendURL
		}
		var models, vaultSet *relay.BlobSet
		if modelStore != nil {
			models = &relay.BlobSet{
				List: func() []relay.Entry {
					out := []relay.Entry{}
					for _, e := range modelStore.List() {
						out = append(out, relay.Entry{Name: e.Name, SHA256: e.SHA256, Size: e.Size, Format: e.Format})
					}
					return out
				},
				Path: modelStore.Path,
				Add: func(name string, r io.Reader) error {
					_, err := modelStore.Add(name, r)
					return err
				},
			}
		}
		if dataVault != nil {
			vaultSet = &relay.BlobSet{
				List: func() []relay.Entry {
					out := []relay.Entry{}
					for _, e := range dataVault.List() {
						out = append(out, relay.Entry{Name: e.Name, SHA256: e.SHA256, Size: e.Size, Format: "sealed"})
					}
					return out
				},
				Path: dataVault.Path,
				Add: func(name string, r io.Reader) error {
					_, err := dataVault.AddSealed(name, r)
					return err
				},
			}
		}
		go func() {
			if err := relay.ListenAndServe(cfg.Relay, cfg.PKIDir, backendURL, models, vaultSet, shareStore.MaxProcs, revoked.Has); err != nil {
				log.Fatalf("relay: %v", err)
			}
		}()
	}

	reg := registry.New(cfg.Backends, time.Duration(cfg.HealthIntervalSeconds)*time.Second, peerTLS)
	go reg.Run(ctx)

	// applyRevoke evicts a node locally: record it and drop it from routing.
	applyRevoke := func(name string) {
		revoked.Add(name)
		reg.Remove(name)
	}

	var mesh *gossip.Mesh
	if cfg.Gossip != nil {
		if cfg.Gossip.BackendURL != "" {
			reg.Upsert(config.Backend{Name: cfg.Gossip.NodeName, BaseURL: cfg.Gossip.BackendURL, Location: cfg.Gossip.Location})
		}
		advertise := cfg.Gossip.BackendURL
		if secure && cfg.Gossip.RelayURL != "" {
			advertise = cfg.Gossip.RelayURL
		}
		var err error
		mesh, err = gossip.Start(gossip.Options{
			NodeName:   cfg.Gossip.NodeName,
			BindAddr:   cfg.Gossip.Bind,
			Join:       cfg.Gossip.Join,
			BackendURL: advertise,
			Location:   cfg.Gossip.Location,
			Secret:     []byte(cfg.Gossip.Secret),
			OnRevoke:   applyRevoke, // apply revocations received from peers
		}, reg)
		if err != nil {
			log.Fatal(err)
		}
		defer mesh.Leave()
		log.Printf("gossip: node %s on %s", cfg.Gossip.NodeName, cfg.Gossip.Bind)
	}

	gw := gateway.New(reg, cfg.APIKey, peerTLS)
	// Revoking here applies locally and broadcasts to the whole mesh.
	gw.Revoke = func(name string) bool {
		if !revoked.Add(name) {
			return false
		}
		reg.Remove(name)
		if mesh != nil {
			mesh.BroadcastRevoke(name)
		}
		return true
	}
	gw.RevokedList = revoked.List
	usagePath := "usage.json"
	if cfg.PKIDir != "" {
		usagePath = filepath.Join(filepath.Dir(cfg.PKIDir), "usage.json")
	}
	gw.Usage = usage.Open(usagePath)
	gw.Keys = keys.Open(strings.TrimSuffix(usagePath, "usage.json") + "keys.json")
	if modelStore == nil {
		if st, err := store.Open(strings.TrimSuffix(usagePath, "usage.json") + "models"); err == nil {
			modelStore = st
		} else {
			log.Printf("store: %v", err)
		}
	}
	gw.Models = modelStore
	gw.Share = shareStore

	gw.Vault = dataVault

	// M1/M3: keep every stored object on N nodes across failure domains —
	// model artifacts as-is, vault objects as owner-sealed ciphertext.
	// Needs the mTLS relay (secure) to survey peers and push replicas.
	if secure {
		self, loc := "this-node", ""
		if cfg.Gossip != nil {
			self, loc = cfg.Gossip.NodeName, cfg.Gossip.Location
		}
		peers := func() []replicate.Peer {
			out := []replicate.Peer{}
			for _, b := range reg.Ranked() {
				if !b.Healthy || b.Backend.Name == self || !strings.HasPrefix(b.Backend.BaseURL, "https://") {
					continue
				}
				out = append(out, replicate.Peer{Name: b.Backend.Name, BaseURL: b.Backend.BaseURL, Location: b.Backend.Location})
			}
			return out
		}
		peerClient := &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{TLSClientConfig: peerTLS}}
		newMgr := func(endpoint string, list func() []replicate.Blob, path func(string) (string, bool)) *replicate.Manager {
			return &replicate.Manager{
				Target: cfg.ReplicationFactor, Self: self, Location: loc,
				List: list, Path: path, Endpoint: endpoint,
				Client: peerClient, Peers: peers,
			}
		}
		var modelMgr, vaultMgr *replicate.Manager
		if modelStore != nil {
			modelMgr = newMgr("/store", func() []replicate.Blob {
				out := []replicate.Blob{}
				for _, e := range modelStore.List() {
					out = append(out, replicate.Blob{Name: e.Name, SHA256: e.SHA256})
				}
				return out
			}, modelStore.Path)
			go modelMgr.Run(ctx, time.Minute)
			gw.ReplicateWrite = modelMgr.AckWrite
		}
		if dataVault != nil {
			vaultMgr = newMgr("/vault", func() []replicate.Blob {
				out := []replicate.Blob{}
				for _, e := range dataVault.List() {
					out = append(out, replicate.Blob{Name: e.Name, SHA256: e.SHA256})
				}
				return out
			}, dataVault.Path)
			go vaultMgr.Run(ctx, time.Minute)
			gw.VaultReplicateWrite = vaultMgr.AckWrite
		}
		gw.Replication = func() map[string]any {
			out := map[string]any{"target": cfg.ReplicationFactor}
			if modelMgr != nil {
				out = modelMgr.Status()
			}
			if vaultMgr != nil {
				out["vault"] = vaultMgr.Status()
			}
			return out
		}
	}
	auditPath := "audit.log"
	if cfg.PKIDir != "" {
		auditPath = filepath.Join(filepath.Dir(cfg.PKIDir), "audit.log")
	}
	gw.Audit = audit.Open(auditPath)
	// Backpressure defaults: 8 concurrent, up to 64 waiting, 5s wait.
	cc := cfg.Concurrency
	if cc == nil {
		cc = &config.Concurrency{MaxInFlight: 8, MaxQueue: 64, WaitSeconds: 5}
	}
	gw.Limiter = inflight.New(cc.MaxInFlight, cc.MaxQueue, time.Duration(cc.WaitSeconds)*time.Second)
	log.Printf("backpressure: %d concurrent, %d queued, %ds wait", cc.MaxInFlight, cc.MaxQueue, cc.WaitSeconds)
	if cfg.Quotas != nil {
		gw.Quota = quota.New(quota.Limits{
			RequestsPerMinute: cfg.Quotas.RequestsPerMinute,
			TokensPerDay:      cfg.Quotas.TokensPerDay,
		})
		log.Printf("quotas: %d req/min, %d tokens/day per key (0 = unlimited)",
			cfg.Quotas.RequestsPerMinute, cfg.Quotas.TokensPerDay)
	}
	if secure && cfg.Gossip != nil {
		if _, err := os.Stat(filepath.Join(cfg.PKIDir, "ca.key")); err == nil {
			// A2: single-use expiring join tokens — minted here (the CA
			// node), burned on first enrollment, persisted across restarts.
			secret := []byte(cfg.Gossip.Secret)
			used := jointoken.OpenUsed(filepath.Join(cfg.PKIDir, "join-tokens-used.json"))
			gw.EnrollHandler = relay.EnrollHandler(cfg.PKIDir, secret, func(tok string) error {
				id, exp, err := jointoken.Parse(secret, tok)
				if err != nil {
					return err
				}
				return used.Burn(id, exp)
			})
			gw.MintJoinToken = func(ttl time.Duration) (string, time.Time, error) {
				return jointoken.New(secret, ttl)
			}
			gw.JoinInfo = func() (string, string, string) {
				return cfg.Gossip.Secret, advertiseAddr(cfg.Gossip.Bind), "http://" + advertiseAddr(cfg.Listen)
			}
		}
	}
	srv := &http.Server{Addr: cfg.Listen, Handler: gw.Handler()}
	go func() {
		<-ctx.Done()
		shutCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		srv.Shutdown(shutCtx)
	}()
	log.Printf("cloudless gateway listening on %s with %d backend(s)", cfg.Listen, len(cfg.Backends))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func usageCmd(args []string) {
	fs := flag.NewFlagSet("usage", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	fs.Parse(args)
	resp, err := http.Get(*addr + "/usage")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Usage  []usage.Record `json:"usage"`
		Limits quota.Limits   `json:"limits"`
		Quotas []quota.Status `json:"quotas"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%-12s %-30s %8s %10s %10s  %s\n", "KEY", "BACKEND", "REQS", "PROMPT", "COMPLETE", "LAST USED")
	for _, u := range out.Usage {
		fmt.Printf("%-12s %-30s %8d %10d %10d  %s\n",
			u.APIKey, u.Backend, u.Requests, u.PromptTokens, u.CompletionTokens, u.LastUsed.Format("15:04:05"))
	}
	fmt.Printf("\nQUOTAS (per key): %d req/min, %d tokens/day (0 = unlimited)\n",
		out.Limits.RequestsPerMinute, out.Limits.TokensPerDay)
	for _, q := range out.Quotas {
		fmt.Printf("  %-12s %d req last min · %d tokens today\n", q.Key, q.RequestsLastMin, q.TokensToday)
	}
}

// keysCmd manages per-user API keys: list | create <name> | revoke <prefix>.
func keysCmd(args []string) {
	fs := flag.NewFlagSet("keys", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	adminKey := fs.String("admin-key", "", "cluster admin key (default: from ~/.cloudless/config.json)")
	fs.Parse(args)
	if *adminKey == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if cfg, err := config.Load(filepath.Join(home, ".cloudless", "config.json")); err == nil {
				*adminKey = cfg.APIKey
			}
		}
	}
	do := func(method, path string, body string) *http.Response {
		req, _ := http.NewRequest(method, *addr+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+*adminKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		return resp
	}
	sub := "list"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	switch sub {
	case "list":
		resp := do("GET", "/keys", "")
		defer resp.Body.Close()
		var out struct {
			Keys []keys.Public `json:"keys"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		fmt.Printf("%-20s %-12s %-10s %s\n", "NAME", "KEY", "STATE", "CREATED")
		for _, k := range out.Keys {
			state := "active"
			if k.Revoked {
				state = "revoked"
			}
			fmt.Printf("%-20s %-12s %-10s %s\n", k.Name, k.Key, state, k.Created.Format("2006-01-02 15:04"))
		}
	case "create":
		if fs.NArg() < 2 {
			log.Fatal("usage: cloudless keys create <name>")
		}
		body, _ := json.Marshal(map[string]string{"name": fs.Arg(1)})
		resp := do("POST", "/keys", string(body))
		defer resp.Body.Close()
		var out map[string]string
		json.NewDecoder(resp.Body).Decode(&out)
		fmt.Printf("created key for %s (save it — shown only once):\n  %s\n", out["name"], out["key"])
	case "revoke":
		if fs.NArg() < 2 {
			log.Fatal("usage: cloudless keys revoke <key-prefix>")
		}
		resp := do("DELETE", "/keys/"+strings.TrimSuffix(fs.Arg(1), "…"), "")
		resp.Body.Close()
		if resp.StatusCode == http.StatusNoContent {
			fmt.Println("revoked")
		} else {
			log.Fatalf("revoke failed (%d)", resp.StatusCode)
		}
	default:
		log.Fatal("usage: cloudless keys [list|create <name>|revoke <prefix>]")
	}
}

// modelsCmd manages the content-addressed model store:
// list | add <file> [name] | verify <name> | rm <name>
func modelsCmd(args []string) {
	fs := flag.NewFlagSet("models", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	adminKey := fs.String("admin-key", "", "cluster admin key (default: from ~/.cloudless/config.json)")
	fs.Parse(args)
	if *adminKey == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if cfg, err := config.Load(filepath.Join(home, ".cloudless", "config.json")); err == nil {
				*adminKey = cfg.APIKey
			}
		}
	}
	sub := "list"
	if fs.NArg() > 0 {
		sub = fs.Arg(0)
	}
	switch sub {
	case "list":
		resp, err := http.Get(*addr + "/store")
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Artifacts []store.Entry `json:"artifacts"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		fmt.Printf("%-28s %-12s %10s  %s\n", "NAME", "FORMAT", "SIZE", "SHA256")
		for _, e := range out.Artifacts {
			fmt.Printf("%-28s %-12s %10d  %s\n", e.Name, e.Format, e.Size, e.SHA256[:16]+"…")
		}
	case "add":
		if fs.NArg() < 2 {
			log.Fatal("usage: cloudless models add <file> [name]")
		}
		path := fs.Arg(1)
		name := filepath.Base(path)
		if fs.NArg() > 2 {
			name = fs.Arg(2)
		}
		f, err := os.Open(path)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		req, _ := http.NewRequest("PUT", *addr+"/store?name="+name, f)
		req.Header.Set("Authorization", "Bearer "+*adminKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("add failed: %s", body)
		}
		fmt.Printf("added: %s\n", body)
	case "pull":
		if fs.NArg() < 2 {
			log.Fatal("usage: cloudless models pull <name>")
		}
		req, _ := http.NewRequest("POST", *addr+"/store/pull?name="+fs.Arg(1), nil)
		req.Header.Set("Authorization", "Bearer "+*adminKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			fmt.Printf("not in the mesh: %s\n(fall back to a public repository for this model)\n", body)
			return
		}
		fmt.Printf("pull: %s\n", body)
	case "verify":
		if fs.NArg() < 2 {
			log.Fatal("usage: cloudless models verify <name>")
		}
		resp, err := http.Get(*addr + "/store/verify?name=" + fs.Arg(1))
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		fmt.Println(string(body))
	case "rm":
		if fs.NArg() < 2 {
			log.Fatal("usage: cloudless models rm <name>")
		}
		req, _ := http.NewRequest("DELETE", *addr+"/store/"+fs.Arg(1), nil)
		req.Header.Set("Authorization", "Bearer "+*adminKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
		fmt.Println("status:", resp.StatusCode)
	default:
		log.Fatal("usage: cloudless models [list|add <file> [name]|verify <name>|rm <name>]")
	}
}

// shareCmd shows or sets this node's resource-share limits (5% default,
// tunable up to a 70% ceiling): share [show] | share set -cpu N [-when charging]
func shareCmd(args []string) {
	// Pull the subcommand out first so flags can appear in any position
	// (Go's flag parser otherwise stops at the "set"/"show" positional).
	sub := "show"
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "set" || a == "show" {
			sub = a
			continue
		}
		rest = append(rest, a)
	}
	fs := flag.NewFlagSet("share", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	adminKey := fs.String("admin-key", "", "cluster admin key (default: from ~/.cloudless/config.json)")
	cpu := fs.Int("cpu", -1, "CPU share percent (0..70)")
	when := fs.String("when", "", "share when: always | charging | idle")
	fs.Parse(rest)
	if *adminKey == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if cfg, err := config.Load(filepath.Join(home, ".cloudless", "config.json")); err == nil {
				*adminKey = cfg.APIKey
			}
		}
	}
	if sub == "set" {
		body := map[string]any{}
		if *cpu >= 0 {
			body["cpu_percent"] = *cpu
		}
		if *when != "" {
			body["share_when"] = *when
		}
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("PUT", *addr+"/share", strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer "+*adminKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		fmt.Printf("applied: %s\n", out)
		return
	}
	resp, err := http.Get(*addr + "/share")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var d struct {
		Limits struct {
			CPUPercent int    `json:"cpu_percent"`
			ShareWhen  string `json:"share_when"`
		} `json:"limits"`
		Ceiling     int `json:"ceiling"`
		SharedCores int `json:"shared_cores"`
	}
	json.NewDecoder(resp.Body).Decode(&d)
	fmt.Printf("CPU share: %d%% (ceiling %d%%) · %d shared core(s) · when: %s\n",
		d.Limits.CPUPercent, d.Ceiling, d.SharedCores, d.Limits.ShareWhen)
}

// nodesCmd manages membership: nodes revoke <name> | nodes revocations
func nodesCmd(args []string) {
	sub := ""
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "revoke" || a == "revocations" {
			sub = a
			continue
		}
		rest = append(rest, a)
	}
	fs := flag.NewFlagSet("nodes", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	adminKey := fs.String("admin-key", "", "cluster admin key (default: from ~/.cloudless/config.json)")
	fs.Parse(rest)
	if *adminKey == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if cfg, err := config.Load(filepath.Join(home, ".cloudless", "config.json")); err == nil {
				*adminKey = cfg.APIKey
			}
		}
	}
	switch sub {
	case "revoke":
		if fs.NArg() < 1 {
			log.Fatal("usage: cloudless nodes revoke <node-name>")
		}
		req, _ := http.NewRequest("POST", *addr+"/revoke/"+fs.Arg(0), nil)
		req.Header.Set("Authorization", "Bearer "+*adminKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusNoContent {
			fmt.Printf("revoked %s — evicted mesh-wide\n", fs.Arg(0))
		} else {
			log.Fatalf("revoke failed (%d)", resp.StatusCode)
		}
	default:
		resp, err := http.Get(*addr + "/revocations")
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Revoked []revoke.Record `json:"revoked"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		fmt.Println("REVOKED NODES")
		for _, r := range out.Revoked {
			fmt.Printf("  %-30s %s\n", r.Name, r.Revoked.Format("2006-01-02 15:04"))
		}
	}
}

func auditCmd(args []string) {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	fs.Parse(args)
	resp, err := http.Get(*addr + "/audit")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Entries  []audit.Entry `json:"entries"`
		Intact   bool          `json:"intact"`
		BrokenAt int64         `json:"broken_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
	if out.Intact {
		fmt.Println("audit chain: INTACT ✓")
	} else {
		fmt.Printf("audit chain: TAMPERED — broken at seq %d ✗\n", out.BrokenAt)
	}
	fmt.Printf("%-5s %-19s %-10s %-16s %s\n", "SEQ", "TIME", "ACTOR", "ACTION", "TARGET")
	for i := len(out.Entries) - 1; i >= 0; i-- {
		e := out.Entries[i]
		fmt.Printf("%-5d %-19s %-10s %-16s %s %s\n",
			e.Seq, e.Time.Format("2006-01-02 15:04:05"), e.Actor, e.Action, e.Target, e.Detail)
	}
}

func capacityCmd(args []string) {
	fs := flag.NewFlagSet("capacity", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	fs.Parse(args)
	resp, err := http.Get(*addr + "/capacity")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		IdleNodes int `json:"idle_nodes"`
		Nodes     []struct {
			Node        string `json:"node"`
			Healthy     bool   `json:"healthy"`
			Requests    int64  `json:"requests"`
			Idle        bool   `json:"idle"`
			IdleSeconds int64  `json:"idle_seconds"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%-30s %-9s %8s  %s\n", "NODE", "STATE", "REQS", "VERDICT")
	for _, n := range out.Nodes {
		state, verdict := "down", "unavailable"
		if n.Healthy {
			state = "healthy"
			if n.Idle {
				verdict = "IDLE — give it work"
			} else {
				verdict = "busy/warm"
			}
		}
		fmt.Printf("%-30s %-9s %8d  %s\n", n.Node, state, n.Requests, verdict)
	}
	fmt.Printf("idle nodes: %d\n", out.IdleNodes)
}

func savingsCmd(args []string) {
	fs := flag.NewFlagSet("savings", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	rp := fs.String("prompt-rate", "0.50", "reference USD per 1M prompt tokens")
	rc := fs.String("completion-rate", "1.50", "reference USD per 1M completion tokens")
	fs.Parse(args)
	resp, err := http.Get(*addr + "/savings?prompt_per_1m=" + *rp + "&completion_per_1m=" + *rc)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Requests         int64   `json:"requests"`
		PromptTokens     int64   `json:"prompt_tokens"`
		CompletionTokens int64   `json:"completion_tokens"`
		Hosted           float64 `json:"hosted_equivalent_usd"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("mesh served: %d requests · %d prompt + %d completion tokens\n",
		out.Requests, out.PromptTokens, out.CompletionTokens)
	fmt.Printf("hosted-API equivalent: $%.4f    mesh marginal cost: $0.00\n", out.Hosted)
}

func ledgerCmd(args []string) {
	fs := flag.NewFlagSet("ledger", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	fs.Parse(args)
	resp, err := http.Get(*addr + "/ledger")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		TotalTokens int64                `json:"total_tokens"`
		Contributed []gateway.LedgerLine `json:"contributed"`
		Consumed    []gateway.LedgerLine `json:"consumed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
	fmt.Println("CONTRIBUTED (by node)")
	for _, l := range out.Contributed {
		fmt.Printf("  %-30s %6d reqs %8d tokens %5.1f%%\n", l.Party, l.Requests, l.Tokens, l.Share)
	}
	fmt.Println("CONSUMED (by key)")
	for _, l := range out.Consumed {
		fmt.Printf("  %-30s %6d reqs %8d tokens %5.1f%%\n", l.Party, l.Requests, l.Tokens, l.Share)
	}
	fmt.Printf("TOTAL tokens exchanged: %d\n", out.TotalTokens)
}

// tokenCmd mints a single-use expiring join token on the CA node (A2).
func tokenCmd(args []string) {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	adminKey := fs.String("admin-key", "", "cluster admin key (default: from ~/.cloudless/config.json)")
	ttl := fs.Int("ttl", 15, "token validity in minutes")
	fs.Parse(args)
	if *adminKey == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if cfg, err := config.Load(filepath.Join(home, ".cloudless", "config.json")); err == nil {
				*adminKey = cfg.APIKey
			}
		}
	}
	body := fmt.Sprintf(`{"ttl_minutes":%d}`, *ttl)
	req, _ := http.NewRequest(http.MethodPost, *addr+"/join-tokens", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+*adminKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("mint failed: %s", strings.TrimSpace(string(b)))
	}
	var out struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("JOIN TOKEN (single-use, expires %s)\n%s\n\nOn the new machine:\n  cloudless up -join <secret>@<host:port> -join-token %s\n",
		out.Expires.Local().Format(time.RFC1123), out.Token, out.Token)
}

func status(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	addr := fs.String("addr", "http://127.0.0.1:8080", "gateway address")
	fs.Parse(args)

	resp, err := http.Get(*addr + "/status")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Backends []registry.BackendState `json:"backends"`
		Routes   []gateway.RouteEntry    `json:"routes"`
		Load     struct {
			InFlight      int64 `json:"inflight"`
			Waiting       int64 `json:"waiting"`
			MaxConcurrent int   `json:"max_concurrent"`
		} `json:"load"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("LOAD  %d in-flight, %d waiting (max concurrent %d)\n",
		out.Load.InFlight, out.Load.Waiting, out.Load.MaxConcurrent)
	fmt.Println("BACKENDS")
	for _, b := range out.Backends {
		state := "unhealthy"
		if b.Healthy {
			state = fmt.Sprintf("healthy %dms", b.LatencyMS)
		}
		fmt.Printf("  %-12s %-30s %s\n", b.Backend.Name, b.Backend.BaseURL, state)
	}
	fmt.Println("RECENT ROUTES")
	for _, r := range out.Routes {
		fmt.Printf("  %s %-24s -> %-12s %d (retries %d)\n",
			r.Time.Format("15:04:05"), r.Path, r.Backend, r.Status, r.Retries)
	}
}
