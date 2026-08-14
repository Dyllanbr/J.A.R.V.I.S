package config

import (
	"testing"
	"time"
)

func TestFromEnvUsesSecureDefaults(t *testing.T) {
	t.Setenv("JARVIS_HTTP_ADDRESS", "")
	t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() returned an error: %v", err)
	}

	if cfg.HTTPAddress != "127.0.0.1:8080" {
		t.Fatalf("HTTPAddress = %q, want %q", cfg.HTTPAddress, "127.0.0.1:8080")
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 10*time.Second)
	}
}

func TestFromEnvRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		address string
		timeout string
	}{
		{name: "invalid address", address: "localhost", timeout: "10s"},
		{name: "zero port", address: "localhost:0", timeout: "10s"},
		{name: "invalid port", address: "localhost:70000", timeout: "10s"},
		{name: "invalid timeout", address: "localhost:8080", timeout: "later"},
		{name: "non-positive timeout", address: "localhost:8080", timeout: "0s"},
		{name: "excessive timeout", address: "localhost:8080", timeout: "31s"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("JARVIS_HTTP_ADDRESS", test.address)
			t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", test.timeout)

			if _, err := FromEnv(); err == nil {
				t.Fatal("FromEnv() returned nil error for invalid configuration")
			}
		})
	}
}

func TestFromEnvAcceptsOperationalBounds(t *testing.T) {
	t.Setenv("JARVIS_HTTP_ADDRESS", "0.0.0.0:65535")
	t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", "30s")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() returned an error: %v", err)
	}

	if cfg.HTTPAddress != "0.0.0.0:65535" {
		t.Fatalf("HTTPAddress = %q, want %q", cfg.HTTPAddress, "0.0.0.0:65535")
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 30*time.Second)
	}
}
