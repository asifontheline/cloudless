package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Backend struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}

type Gossip struct {
	NodeName   string   `json:"node_name"`
	Bind       string   `json:"bind"`        // e.g. "0.0.0.0:7946"
	Join       []string `json:"join"`        // seed peers, host:port
	BackendURL string   `json:"backend_url"` // this node's local inference endpoint
	RelayURL   string   `json:"relay_url"`   // mutual-TLS relay URL advertised to peers (preferred over backend_url)
	Secret     string   `json:"secret"`      // shared cluster key (16/24/32 bytes) encrypting gossip
}

type Config struct {
	Listen                string    `json:"listen"`
	APIKey                string    `json:"api_key"`
	HealthIntervalSeconds int       `json:"health_interval_seconds"`
	Backends              []Backend `json:"backends"`
	Gossip                *Gossip   `json:"gossip,omitempty"`
	PKIDir                string    `json:"pki_dir,omitempty"` // cluster PKI directory; enables the mTLS relay
	Relay                 string    `json:"relay,omitempty"`   // relay listen address (default :9443 when PKI present)
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
