package main

import (
	"crypto/tls"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"cloudless/internal/config"
	"cloudless/internal/gateway"
	"cloudless/internal/gossip"
	"cloudless/internal/pki"
	"cloudless/internal/quota"
	"cloudless/internal/registry"
	"cloudless/internal/relay"
	"cloudless/internal/usage"
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
			if err := relay.Enroll(api, cfg.PKIDir, cfg.Gossip.NodeName, []byte(cfg.Gossip.Secret)); err != nil {
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

	// A3: with PKI present, start the mutual-TLS relay and dial peers with
	// the node certificate; peer traffic is never plaintext.
	var peerTLS *tls.Config
	secure := cfg.PKIDir != "" && pki.HasCreds(cfg.PKIDir)
	if secure {
		var err error
		peerTLS, err = pki.ClientTLS(cfg.PKIDir)
		if err != nil {
			log.Fatal(err)
		}
		backendURL := ""
		if cfg.Gossip != nil {
			backendURL = cfg.Gossip.BackendURL
		}
		go func() {
			if err := relay.ListenAndServe(cfg.Relay, cfg.PKIDir, backendURL); err != nil {
				log.Fatalf("relay: %v", err)
			}
		}()
	}

	reg := registry.New(cfg.Backends, time.Duration(cfg.HealthIntervalSeconds)*time.Second, peerTLS)
	go reg.Run(ctx)

	if cfg.Gossip != nil {
		if cfg.Gossip.BackendURL != "" {
			reg.Upsert(config.Backend{Name: cfg.Gossip.NodeName, BaseURL: cfg.Gossip.BackendURL})
		}
		advertise := cfg.Gossip.BackendURL
		if secure && cfg.Gossip.RelayURL != "" {
			advertise = cfg.Gossip.RelayURL
		}
		mesh, err := gossip.Start(gossip.Options{
			NodeName:   cfg.Gossip.NodeName,
			BindAddr:   cfg.Gossip.Bind,
			Join:       cfg.Gossip.Join,
			BackendURL: advertise,
			Secret:     []byte(cfg.Gossip.Secret),
		}, reg)
		if err != nil {
			log.Fatal(err)
		}
		defer mesh.Leave()
		log.Printf("gossip: node %s on %s", cfg.Gossip.NodeName, cfg.Gossip.Bind)
	}

	gw := gateway.New(reg, cfg.APIKey, peerTLS)
	usagePath := "usage.json"
	if cfg.PKIDir != "" {
		usagePath = filepath.Join(filepath.Dir(cfg.PKIDir), "usage.json")
	}
	gw.Usage = usage.Open(usagePath)
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
			gw.EnrollHandler = relay.EnrollHandler(cfg.PKIDir, []byte(cfg.Gossip.Secret))
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Fatal(err)
	}
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
