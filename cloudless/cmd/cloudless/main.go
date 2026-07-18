package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloudless/internal/config"
	"cloudless/internal/gateway"
	"cloudless/internal/gossip"
	"cloudless/internal/registry"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "status":
		status(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `cloudless — group-private OpenAI-compatible inference mesh (M0)

usage:
  cloudless serve  -config config.json
  cloudless status -addr http://127.0.0.1:8080`)
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "config.json", "path to config file")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	reg := registry.New(cfg.Backends, time.Duration(cfg.HealthIntervalSeconds)*time.Second)
	go reg.Run(ctx)

	if cfg.Gossip != nil {
		if cfg.Gossip.BackendURL != "" {
			reg.Upsert(config.Backend{Name: cfg.Gossip.NodeName, BaseURL: cfg.Gossip.BackendURL})
		}
		mesh, err := gossip.Start(gossip.Options{
			NodeName:   cfg.Gossip.NodeName,
			BindAddr:   cfg.Gossip.Bind,
			Join:       cfg.Gossip.Join,
			BackendURL: cfg.Gossip.BackendURL,
			Secret:     []byte(cfg.Gossip.Secret),
		}, reg)
		if err != nil {
			log.Fatal(err)
		}
		defer mesh.Leave()
		log.Printf("gossip: node %s on %s", cfg.Gossip.NodeName, cfg.Gossip.Bind)
	}

	gw := gateway.New(reg, cfg.APIKey)
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
