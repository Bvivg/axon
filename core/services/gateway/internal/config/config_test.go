package config_test

import (
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/gateway/internal/config"
)

func TestTrustedProxiesMustBeAPlausibleHopCount(t *testing.T) {
	for _, value := range []string{"-1", "9", "100"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", value)
			if _, err := config.Load(); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") {
				t.Fatalf("TRUSTED_PROXIES=%s: got %v, want a TRUSTED_PROXIES error", value, err)
			}
		})
	}

	for _, value := range []string{"0", "1", "2"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", value)
			if _, err := config.Load(); err != nil {
				t.Fatalf("TRUSTED_PROXIES=%s was refused: %v", value, err)
			}
		})
	}
}

func TestRequestLimitsHaveSafeDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case cfg.HTTP.MaxBodyBytes != 1<<20:
		t.Errorf("MaxBodyBytes = %d", cfg.HTTP.MaxBodyBytes)
	case cfg.HTTP.ReadTimeout <= 0 || cfg.HTTP.IdleTimeout <= 0:
		t.Errorf("timeouts = %v / %v", cfg.HTTP.ReadTimeout, cfg.HTTP.IdleTimeout)
	case cfg.HTTP.MaxListItems != 100:
		t.Errorf("MaxListItems = %d", cfg.HTTP.MaxListItems)
	case cfg.RPCReadMaxBytes != 256<<10:
		t.Errorf("RPCReadMaxBytes = %d", cfg.RPCReadMaxBytes)
	case cfg.RateLimit.SocketsPerMinute <= 0 || cfg.RateLimit.SocketsPerUser <= 0:
		t.Errorf("socket limits = %d / %d", cfg.RateLimit.SocketsPerMinute, cfg.RateLimit.SocketsPerUser)
	}
}

func TestABodyLimitOfZeroIsRefused(t *testing.T) {
	t.Setenv("HTTP_MAX_BODY_BYTES", "0")
	if _, err := config.Load(); err == nil {
		t.Fatal("a body limit of zero, which refuses every request body, was accepted")
	}
}
