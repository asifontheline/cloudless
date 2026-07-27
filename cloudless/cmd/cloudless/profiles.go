package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"cloudless/internal/config"
	"cloudless/internal/usage"
)

// Multi-profile config (Q4): a purpose-built client-side profile store,
// deliberately separate from ~/.cloudless/config.json (the node's own
// server startup config, read elsewhere in this file as a fallback admin
// key when the CLI happens to run on the same machine as the node it's
// talking to). Profiles let one operator switch between many meshes
// without re-typing -addr/-key every time.

type profile struct {
	Addr   string `json:"addr"`
	APIKey string `json:"api_key"`
}

type profileStore struct {
	Current  string             `json:"current,omitempty"`
	Profiles map[string]profile `json:"profiles"`
}

func profilesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cloudless", "profiles.json"), nil
}

func loadProfiles() *profileStore {
	s := &profileStore{Profiles: map[string]profile{}}
	path, err := profilesPath()
	if err != nil {
		return s
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	json.Unmarshal(raw, s) // corrupt or missing file: start empty, never fatal
	if s.Profiles == nil {
		s.Profiles = map[string]profile{}
	}
	return s
}

func (s *profileStore) save() error {
	path, err := profilesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// resolveAddrKey fills in addr/key when the caller didn't pass -addr/-key
// explicitly (both still empty after fs.Parse). Priority: explicit flag
// (already set, nothing to do) > CLOUDLESS_API_KEY env var (Q5 — so CI and
// scripts never need the key on the command line or in shell history) >
// active profile > the standard default address and, for the key, the
// node's own config.json — unchanged behavior for anyone using neither.
func resolveAddrKey(addr, key *string) {
	if *key == "" {
		*key = os.Getenv("CLOUDLESS_API_KEY")
	}
	if *addr == "" || *key == "" {
		s := loadProfiles()
		if p, ok := s.Profiles[s.Current]; ok {
			if *addr == "" {
				*addr = p.Addr
			}
			if *key == "" {
				*key = p.APIKey
			}
		}
	}
	if *addr == "" {
		*addr = "http://127.0.0.1:8080"
	}
	if *key == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if cfg, err := config.Load(filepath.Join(home, ".cloudless", "config.json")); err == nil {
				*key = cfg.APIKey
			}
		}
	}
}

// configCmd follows this file's established convention (see keysCmd,
// nodesCmd): flags first, subcommand and its positional args after —
// `cloudless config -addr <addr> -key <key> set <name>` — since Go's flag
// package stops parsing at the first non-flag argument, a flag can't
// follow a positional name on the same command line.
func configCmd(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	addr := fs.String("addr", "", "gateway address (for 'set')")
	key := fs.String("key", "", "API key (for 'set')")
	fs.Parse(args)
	if fs.NArg() == 0 {
		log.Fatal("usage: cloudless config [-addr <addr>] [-key <key>] <set|use|list|get|rm> [name]")
	}
	sub := fs.Arg(0)
	name := ""
	if fs.NArg() > 1 {
		name = fs.Arg(1)
	}

	switch sub {
	case "set":
		if name == "" {
			log.Fatal("usage: cloudless config -addr <addr> -key <key> set <name>")
		}
		s := loadProfiles()
		s.Profiles[name] = profile{Addr: *addr, APIKey: *key}
		if s.Current == "" {
			s.Current = name // first profile becomes active automatically
		}
		if err := s.save(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("saved profile %q\n", name)

	case "use":
		if name == "" {
			log.Fatal("usage: cloudless config use <name>")
		}
		s := loadProfiles()
		if _, ok := s.Profiles[name]; !ok {
			log.Fatalf("no such profile %q — cloudless config list", name)
		}
		s.Current = name
		if err := s.save(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("active profile: %s\n", name)

	case "list":
		s := loadProfiles()
		names := make([]string, 0, len(s.Profiles))
		for n := range s.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Println("no profiles — cloudless config -addr <addr> -key <key> set <name>")
			return
		}
		for _, n := range names {
			mark := "  "
			if n == s.Current {
				mark = "* "
			}
			fmt.Printf("%s%-16s %s\n", mark, n, s.Profiles[n].Addr)
		}

	case "get":
		s := loadProfiles()
		if name == "" {
			name = s.Current
		}
		p, ok := s.Profiles[name]
		if !ok {
			log.Fatalf("no such profile %q", name)
		}
		fmt.Printf("name: %s\naddr: %s\nkey:  %s\n", name, p.Addr, usage.Redact(p.APIKey))

	case "rm":
		if name == "" {
			log.Fatal("usage: cloudless config rm <name>")
		}
		s := loadProfiles()
		if _, ok := s.Profiles[name]; !ok {
			log.Fatalf("no such profile %q", name)
		}
		delete(s.Profiles, name)
		if s.Current == name {
			s.Current = ""
		}
		if err := s.save(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("removed profile %q\n", name)

	default:
		log.Fatalf("unknown config subcommand %q — set, use, list, get, rm", sub)
	}
}
