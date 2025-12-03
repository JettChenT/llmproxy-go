package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// ProxyConfig represents configuration for a single proxy instance
type ProxyConfig struct {
	Name   string `toml:"name"`   // Human-readable name for this proxy
	Listen string `toml:"listen"` // Address to listen on (e.g., ":8080")
	Target string `toml:"target"` // Target URL to proxy to
}

// CacheConfigTOML represents cache configuration in TOML format
type CacheConfigTOML struct {
	Mode            string `toml:"mode"`             // "none", "memory", or "global"
	TTL             string `toml:"ttl"`              // Duration string (e.g., "24h", "7d")
	SimulateLatency bool   `toml:"simulate_latency"` // Simulate original response latency
	Dir             string `toml:"dir"`              // Directory for persistent cache
}

// Config represents the full TOML configuration file
type Config struct {
	Proxies  []ProxyConfig   `toml:"proxy"`
	Cache    CacheConfigTOML `toml:"cache"`
	SaveTape string          `toml:"save_tape"` // Auto-save session to tape file
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Proxies: []ProxyConfig{
			{
				Name:   "default",
				Listen: ":8080",
				Target: "http://localhost:3000",
			},
		},
		Cache: CacheConfigTOML{
			Mode:            "none",
			TTL:             "24h",
			SimulateLatency: false,
			Dir:             "",
		},
	}
}

// LoadConfig loads configuration from a TOML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := DefaultConfig()
	if _, err := toml.Decode(string(data), config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate configuration
	if len(config.Proxies) == 0 {
		return nil, fmt.Errorf("at least one proxy must be configured")
	}

	// Check for duplicate listen addresses
	listenAddrs := make(map[string]bool)
	for _, p := range config.Proxies {
		if listenAddrs[p.Listen] {
			return nil, fmt.Errorf("duplicate listen address: %s", p.Listen)
		}
		listenAddrs[p.Listen] = true

		if p.Listen == "" {
			return nil, fmt.Errorf("proxy listen address cannot be empty")
		}
		if p.Target == "" {
			return nil, fmt.Errorf("proxy target URL cannot be empty")
		}
	}

	return config, nil
}

// ParseTTL parses a TTL string into a duration
// Supports standard Go durations plus "d" for days
func ParseTTL(ttl string) (time.Duration, error) {
	if ttl == "" {
		return 24 * time.Hour, nil // Default
	}

	// Handle days suffix
	if len(ttl) > 1 && ttl[len(ttl)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(ttl, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}

	// Standard Go duration
	return time.ParseDuration(ttl)
}

// ToCacheConfig converts TOML cache config to internal CacheConfig
func (c *CacheConfigTOML) ToCacheConfig() (CacheConfig, error) {
	ttl, err := ParseTTL(c.TTL)
	if err != nil {
		return CacheConfig{}, fmt.Errorf("invalid TTL: %w", err)
	}

	badgerPath := c.Dir
	if badgerPath == "" && c.Mode == "global" {
		home, _ := os.UserHomeDir()
		badgerPath = filepath.Join(home, ".llmproxy-cache")
	}

	return CacheConfig{
		Mode:            CacheMode(c.Mode),
		TTL:             ttl,
		SimulateLatency: c.SimulateLatency,
		BadgerPath:      badgerPath,
	}, nil
}

// GenerateExampleConfig returns a string containing an example TOML configuration
func GenerateExampleConfig() string {
	return `# llmproxy-go configuration file
#
# Multiple proxies can be configured to run simultaneously.
# Each proxy listens on a different port and forwards to a different target.

# Proxy configurations
# You can define multiple [[proxy]] sections for multiple proxies

[[proxy]]
name = "openai"
listen = ":8080"
target = "https://api.openai.com"

[[proxy]]
name = "anthropic"
listen = ":8081"
target = "https://api.anthropic.com"

[[proxy]]
name = "local-llm"
listen = ":8082"
target = "http://localhost:11434"

# Cache configuration
[cache]
# mode: "none" (disabled), "memory" (in-memory), or "global" (persistent BadgerDB)
mode = "memory"

# TTL for cached responses. Supports Go duration format (e.g., "1h", "24h")
# and days with "d" suffix (e.g., "7d")
ttl = "24h"

# Whether to simulate the original response latency for cached responses
simulate_latency = false

# Directory for persistent cache (only used when mode = "global")
# Defaults to ~/.llmproxy-cache if not specified
# dir = "/path/to/cache"

# Auto-save session to a tape file (optional)
# save_tape = "session.tape"
`
}
