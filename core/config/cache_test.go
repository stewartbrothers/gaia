package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stewartbrothers/gaia/core/config"
)

func TestLoadCacheConfigKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `default_profile: x
profiles:
  x:
    provider: forgejo
    api_url: https://example.org/api/v1
cache:
  enabled: false
  ttl_seconds:
    single: 600
    list: 60
  max_size_mb: 200
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cache == nil {
		t.Fatal("expected non-nil Cache config")
	}
	if cfg.Cache.Enabled == nil || *cfg.Cache.Enabled != false {
		t.Errorf("Cache.Enabled: got %v, want pointer-to-false", cfg.Cache.Enabled)
	}
	if cfg.Cache.TTLSeconds.Single != 600 {
		t.Errorf("ttl_seconds.single: got %d, want 600", cfg.Cache.TTLSeconds.Single)
	}
	if cfg.Cache.TTLSeconds.List != 60 {
		t.Errorf("ttl_seconds.list: got %d, want 60", cfg.Cache.TTLSeconds.List)
	}
	if cfg.Cache.MaxSizeMB != 200 {
		t.Errorf("max_size_mb: got %d, want 200", cfg.Cache.MaxSizeMB)
	}
}

func TestLoadCacheConfigDefaultsWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `default_profile: x
profiles:
  x:
    provider: forgejo
    api_url: https://example.org/api/v1
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Cache is omitted in YAML; the parsed value is nil. Callers
	// branch on nil to apply documented defaults.
	if cfg.Cache != nil {
		t.Errorf("expected nil Cache when YAML omits it; got %+v", cfg.Cache)
	}
}

func TestCacheEnabledHelperRespectsExplicitFalse(t *testing.T) {
	cfg := &config.Cache{Enabled: ptrBool(false)}
	if config.CacheEnabled(cfg) != false {
		t.Errorf("explicit false should stay false")
	}
}

func TestCacheEnabledHelperDefaultsToTrue(t *testing.T) {
	if config.CacheEnabled(nil) != true {
		t.Errorf("nil cache config should default to enabled=true")
	}
	if config.CacheEnabled(&config.Cache{}) != true {
		t.Errorf("empty cache config should default to enabled=true")
	}
}

func TestCacheTTLDefaultsApplied(t *testing.T) {
	if got := config.CacheTTLSingleSeconds(nil); got != 300 {
		t.Errorf("nil → default 300; got %d", got)
	}
	if got := config.CacheTTLListSeconds(nil); got != 30 {
		t.Errorf("nil → default 30; got %d", got)
	}
	cfg := &config.Cache{TTLSeconds: config.CacheTTL{Single: 120, List: 5}}
	if got := config.CacheTTLSingleSeconds(cfg); got != 120 {
		t.Errorf("explicit 120; got %d", got)
	}
	if got := config.CacheTTLListSeconds(cfg); got != 5 {
		t.Errorf("explicit 5; got %d", got)
	}
}

func ptrBool(b bool) *bool { return &b }
