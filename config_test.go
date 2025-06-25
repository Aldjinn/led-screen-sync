package main

import (
	"os"
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

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
