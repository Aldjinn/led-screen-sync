package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Success(t *testing.T) {
	tmp := `env:
  HA_URL: "http://localhost:8123"
  HA_TOKEN: "testtoken"
  LED_ENTITY: "light.test"
  EXPORT_JSON: true
  EXPORT_SCREENSHOT: false
  COLOR_CHANGE_THRESHOLD: 42.5
  UPDATE_INTERVAL_MS: 123
`
	f, err := os.CreateTemp("", "ledsync-test-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(tmp)
	f.Close()
	if err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	cfg, err := LoadConfig(f.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Env.HA_URL != "http://localhost:8123" {
		t.Errorf("HA_URL mismatch: got %q", cfg.Env.HA_URL)
	}
	if cfg.Env.HA_TOKEN != "testtoken" {
		t.Errorf("HA_TOKEN mismatch: got %q", cfg.Env.HA_TOKEN)
	}
	if cfg.Env.LED_ENTITY != "light.test" {
		t.Errorf("LED_ENTITY mismatch: got %q", cfg.Env.LED_ENTITY)
	}
	if !cfg.Env.EXPORT_JSON {
		t.Errorf("EXPORT_JSON should be true")
	}
	if cfg.Env.EXPORT_SCREENSHOT {
		t.Errorf("EXPORT_SCREENSHOT should be false")
	}
	if cfg.Env.COLOR_CHANGE_THRESHOLD != 42.5 {
		t.Errorf("COLOR_CHANGE_THRESHOLD mismatch: got %v", cfg.Env.COLOR_CHANGE_THRESHOLD)
	}
	if cfg.Env.UPDATE_INTERVAL_MS != 123 {
		t.Errorf("UPDATE_INTERVAL_MS mismatch: got %v", cfg.Env.UPDATE_INTERVAL_MS)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig(writeTemp(t, "env:\n  HA_URL: \"http://ha:8123/\"\n  HA_TOKEN: x\n  LED_ENTITY: light.a\n"))
	if err != nil {
		t.Fatal(err)
	}
	e := cfg.Env
	if e.HA_URL != "http://ha:8123" {
		t.Errorf("trailing slash not trimmed: %q", e.HA_URL)
	}
	if e.UPDATE_INTERVAL_MS != 100 || e.COLOR_CHANGE_THRESHOLD != 32 || e.LOG_LEVEL != "info" ||
		e.COLOR_MODE != "dominant" || e.BRIGHTNESS != 255 || e.MIN_CALL_INTERVAL_MS != 150 {
		t.Errorf("defaults not applied: %+v", e)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("defaults should validate: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	base := func() *Config {
		c := &Config{}
		c.Env.HA_URL = "http://ha:8123"
		c.Env.HA_TOKEN = "t"
		c.Env.LED_ENTITY = "light.a"
		c.applyDefaults()
		return c
	}
	cases := map[string]func(*Config){
		"HA_URL":     func(c *Config) { c.Env.HA_URL = "" },
		"scheme":     func(c *Config) { c.Env.HA_URL = "ha:8123" },
		"LED_ENTITY": func(c *Config) { c.Env.LED_ENTITY = "" },
		"HA_TOKEN":   func(c *Config) { c.Env.HA_TOKEN = "" },
		"LOG_LEVEL":  func(c *Config) { c.Env.LOG_LEVEL = "loud" },
		"COLOR_MODE": func(c *Config) { c.Env.COLOR_MODE = "rainbow" },
		"THRESHOLD":  func(c *Config) { c.Env.COLOR_CHANGE_THRESHOLD = 900 },
		"BRIGHTNESS": func(c *Config) { c.Env.BRIGHTNESS = 300 },
		"DISPLAY":    func(c *Config) { c.Env.DISPLAY_INDEX = -1 },
	}
	for name, mutate := range cases {
		c := base()
		mutate(c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if err := base().Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("HA_TOKEN", "from-env")
	t.Setenv("HA_URL", "http://other:8123/")
	c := &Config{}
	c.Env.HA_TOKEN = "from-yaml"
	c.applyEnvOverrides()
	if c.Env.HA_TOKEN != "from-env" || c.Env.HA_URL != "http://other:8123" {
		t.Errorf("overrides not applied: %+v", c.Env)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
