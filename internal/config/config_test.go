package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := Save(&Config{APIURL: "http://local", Token: "tok123"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.APIURL != "http://local" || got.Token != "tok123" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Token file must be 0600 (it holds a secret).
	p, _ := Path()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("want 0600 perms, got %v", info.Mode().Perm())
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Token != "" {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestBaseURLPrecedence(t *testing.T) {
	t.Setenv("CHARIOT_API_URL", "")
	cfg := &Config{}
	if cfg.BaseURL() != DefaultAPIURL {
		t.Fatalf("want default, got %s", cfg.BaseURL())
	}
	cfg.APIURL = "http://from-config"
	if cfg.BaseURL() != "http://from-config" {
		t.Fatalf("want config value, got %s", cfg.BaseURL())
	}
	t.Setenv("CHARIOT_API_URL", "http://from-env")
	if cfg.BaseURL() != "http://from-env" {
		t.Fatalf("env should win, got %s", cfg.BaseURL())
	}
}

func TestPathUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(home, ".chariot", "config.json") {
		t.Fatalf("unexpected path: %s", p)
	}
}
