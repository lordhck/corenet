// Package config loads the corenetd configuration file.
//
// The format is plain JSON: it is a standard, it is in the standard library,
// and it saves CoreNet from owning a configuration parser.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"corenet/pkg/protocol"
)

// SystemPath and UserPath are searched, in that order, when no explicit
// configuration file is given.
const (
	SystemPath = "/etc/corenet/config.json"
	UserPath   = ".corenet/config.json"
)

// Default listen addresses. Ports 53 and 80 need root or
// CAP_NET_BIND_SERVICE; development setups override them in the config file.
const (
	DefaultDNSAddr     = "127.0.0.1:53"
	DefaultHTTPAddr    = "127.0.0.1:80"
	DefaultControlPath = "/run/corenet/corenetd.sock"
)

// DefaultPullSeconds is how often a node refreshes its peers' directories.
const DefaultPullSeconds = 30

// Config is the whole of corenetd's configuration.
type Config struct {
	Node     Node               `json:"node"`
	Listen   Listen             `json:"listen"`
	Services []protocol.Service `json:"services"`
	Nodes    []string           `json:"nodes"`

	// PullIntervalSeconds is how often peer directories are refreshed.
	// Zero means DefaultPullSeconds.
	PullIntervalSeconds int `json:"pull_interval_seconds,omitempty"`

	// Path is where this configuration was read from. Empty when defaults
	// were used.
	Path string `json:"-"`
}

// Node identifies this CoreNet node.
type Node struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Listen holds the daemon's listen addresses. An empty DNS, HTTP or Peer
// address disables that listener.
type Listen struct {
	DNS     string `json:"dns"`
	HTTP    string `json:"http"`
	Peer    string `json:"peer"`
	Control string `json:"control"`
}

// Default returns the built-in configuration: a single local node serving
// .core on the privileged ports, with no services and no peers.
func Default() Config {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "corenet"
	}
	return Config{
		Node: Node{ID: defaultNodeID(host), Name: host},
		Listen: Listen{
			DNS:     DefaultDNSAddr,
			HTTP:    DefaultHTTPAddr,
			Control: DefaultControlPath,
		},
	}
}

// Load reads the configuration from path. If path is empty the system and
// user locations are tried in turn, falling back to Default.
func Load(path string) (Config, error) {
	if path != "" {
		return loadFile(path)
	}
	for _, candidate := range searchPaths() {
		cfg, err := loadFile(candidate)
		if os.IsNotExist(err) {
			continue
		}
		return cfg, err
	}
	cfg := Default()
	return cfg, cfg.Validate()
}

func searchPaths() []string {
	paths := []string{SystemPath}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, UserPath))
	}
	return paths
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	// Services and nodes are replaced wholesale, never merged with defaults.
	cfg.Services = nil
	cfg.Nodes = nil
	// Unknown fields are ignored on purpose: specifications section 8.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Validate normalizes the configuration and reports the first problem found.
func (c *Config) Validate() error {
	if c.Node.ID == "" {
		return fmt.Errorf("node.id is empty")
	}
	if c.Listen.Control == "" {
		return fmt.Errorf("listen.control is empty")
	}
	for _, addr := range []struct{ field, value string }{
		{"listen.dns", c.Listen.DNS},
		{"listen.http", c.Listen.HTTP},
		{"listen.peer", c.Listen.Peer},
	} {
		if addr.value == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(addr.value); err != nil {
			return fmt.Errorf("%s: %q is not a host:port address", addr.field, addr.value)
		}
	}

	seen := make(map[string]bool, len(c.Services))
	for i := range c.Services {
		svc := &c.Services[i]
		svc.Name = protocol.NormalizeName(svc.Name)
		if err := protocol.ValidateName(svc.Name); err != nil {
			return fmt.Errorf("services: %w", err)
		}
		if svc.Address == "" {
			return fmt.Errorf("services: %s has no address", svc.Name)
		}
		if svc.Port < 1 || svc.Port > 65535 {
			return fmt.Errorf("services: %s has an invalid port %d", svc.Name, svc.Port)
		}
		if seen[svc.Name] {
			return fmt.Errorf("services: %s is declared twice", svc.Name)
		}
		seen[svc.Name] = true
		svc.Source = protocol.SourceConfig
	}

	if c.PullIntervalSeconds < 0 {
		return fmt.Errorf("pull_interval_seconds must not be negative")
	}

	for _, endpoint := range c.Nodes {
		host, port, err := net.SplitHostPort(endpoint)
		if err != nil || host == "" {
			return fmt.Errorf("nodes: %q is not a host:port address", endpoint)
		}
		if _, err := strconv.Atoi(port); err != nil {
			return fmt.Errorf("nodes: %q has an invalid port", endpoint)
		}
	}
	return nil
}

// PullInterval is how often peer directories are refreshed.
func (c *Config) PullInterval() time.Duration {
	if c.PullIntervalSeconds <= 0 {
		return DefaultPullSeconds * time.Second
	}
	return time.Duration(c.PullIntervalSeconds) * time.Second
}

// defaultNodeID derives a stable node ID from the hostname.
func defaultNodeID(host string) string {
	id := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, host)
	id = strings.Trim(id, "-")
	if id == "" {
		return "corenet"
	}
	return id
}
