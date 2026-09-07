package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAppliesDefaultsAndNormalizes(t *testing.T) {
	path := writeConfig(t, `{
		"node": {"id": "node-a", "name": "laptop"},
		"services": [{"name": "Hello.Core", "address": "127.0.0.1", "port": 8081}],
		"nodes": ["192.168.1.20:7000"],
		"future_field": "ignored"
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Listen.DNS != DefaultDNSAddr || cfg.Listen.HTTP != DefaultHTTPAddr {
		t.Errorf("listen defaults not applied: %+v", cfg.Listen)
	}
	if cfg.Listen.Control != DefaultControlPath {
		t.Errorf("control default not applied: %q", cfg.Listen.Control)
	}
	if cfg.Path != path {
		t.Errorf("Path = %q, want %q", cfg.Path, path)
	}
	if got := cfg.Services[0].Name; got != "hello.core" {
		t.Errorf("name not normalized: %q", got)
	}
	if got := cfg.Services[0].Source; got != "config" {
		t.Errorf("source = %q, want config", got)
	}
}

func TestLoadRejectsBadConfigurations(t *testing.T) {
	cases := map[string]string{
		"non-core name":   `{"services": [{"name": "hello.test", "address": "127.0.0.1", "port": 80}]}`,
		"missing address": `{"services": [{"name": "hello.core", "port": 80}]}`,
		"bad port":        `{"services": [{"name": "hello.core", "address": "127.0.0.1", "port": 0}]}`,
		"duplicate name":  `{"services": [{"name": "a.core", "address": "1.1.1.1", "port": 80}, {"name": "a.core", "address": "1.1.1.2", "port": 80}]}`,
		"bad listen":      `{"listen": {"dns": "127.0.0.1"}}`,
		"bad node":        `{"nodes": ["192.168.1.20"]}`,
		"empty node id":   `{"node": {"id": ""}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadMissingFileReportsPath(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

func TestDefaultIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
	if strings.ContainsAny(cfg.Node.ID, " _.") {
		t.Errorf("node id %q should be sanitized", cfg.Node.ID)
	}
}
