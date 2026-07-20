package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Backend struct {
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
	Location string `json:"location,omitempty"` // hierarchical: continent/country/state/city/village
}

type Gossip struct {
	NodeName   string   `json:"node_name"`
	Bind       string   `json:"bind"`               // e.g. "0.0.0.0:7946"
	Join       []string `json:"join"`               // seed peers, host:port
	BackendURL string   `json:"backend_url"`        // this node's local inference endpoint
	RelayURL   string   `json:"relay_url"`          // mutual-TLS relay URL advertised to peers (preferred over backend_url)
	Location   string   `json:"location,omitempty"` // continent/country/state/city/village
	Secret     string   `json:"secret"`             // shared cluster key (16/24/32 bytes) encrypting gossip
}

type Config struct {
	Listen                string       `json:"listen"`
	APIKey                string       `json:"api_key"`
	HealthIntervalSeconds int          `json:"health_interval_seconds"`
	Backends              []Backend    `json:"backends"`
	Gossip                *Gossip      `json:"gossip,omitempty"`
	PKIDir                string       `json:"pki_dir,omitempty"`            // cluster PKI directory; enables the mTLS relay
	Relay                 string       `json:"relay,omitempty"`              // relay listen address (default :9443 when PKI present)
	Quotas                *Quotas      `json:"quotas,omitempty"`             // per-key fair-use limits (0 = unlimited)
	Concurrency           *Concurrency `json:"concurrency,omitempty"`        // gateway backpressure (nil = defaults)
	ReplicationFactor     int          `json:"replication_factor,omitempty"` // copies per stored artifact (default 3)
	Backup                *Backup      `json:"backup,omitempty"`             // scheduled off-mesh export (M5)
}

// Backup schedules an automatic off-mesh export: a passphrase-encrypted
// archive of this node's vault written to a local path.
type Backup struct {
	Path       string `json:"path"`        // where the archive is written (overwritten each run)
	EveryHours int    `json:"every_hours"` // export interval (default 24)
	Passphrase string `json:"passphrase"`  // encrypts the archive; keep this config file private
}

type Quotas struct {
	RequestsPerMinute int   `json:"requests_per_minute"`
	TokensPerDay      int64 `json:"tokens_per_day"`
}

type Concurrency struct {
	MaxInFlight int `json:"max_in_flight"` // concurrent requests served (0 = unlimited)
	MaxQueue    int `json:"max_queue"`     // requests allowed to wait for a slot
	WaitSeconds int `json:"wait_seconds"`  // how long a request waits before 503
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.HealthIntervalSeconds <= 0 {
		c.HealthIntervalSeconds = 5
	}
	if len(c.Backends) == 0 && c.Gossip == nil {
		return nil, fmt.Errorf("%s: at least one backend or a gossip section is required", path)
	}
	if c.Gossip != nil {
		if c.Gossip.Bind == "" {
			c.Gossip.Bind = "0.0.0.0:7946"
		}
		if c.Gossip.NodeName == "" {
			host, err := os.Hostname()
			if err != nil {
				return nil, fmt.Errorf("gossip.node_name not set and hostname unavailable: %w", err)
			}
			c.Gossip.NodeName = host
		}
		if n := len(c.Gossip.Secret); n != 0 && n != 16 && n != 24 && n != 32 {
			return nil, fmt.Errorf("gossip.secret must be 16, 24, or 32 bytes (got %d)", n)
		}
	}
	return &c, nil
}
