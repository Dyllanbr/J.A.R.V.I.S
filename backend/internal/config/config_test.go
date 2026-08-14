package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFromEnvUsesSecureDefaults(t *testing.T) {
	disableFinancialAPI(t)
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

func TestFromEnvRedactsInvalidRawValues(t *testing.T) {
	disableFinancialAPI(t)
	tests := []struct {
		name          string
		address       string
		timeout       string
		sensitiveRaw  string
		expectedField string
	}{
		{
			name:          "address",
			address:       "synthetic-sensitive-address-marker",
			timeout:       "10s",
			sensitiveRaw:  "synthetic-sensitive-address-marker",
			expectedField: "JARVIS_HTTP_ADDRESS",
		},
		{
			name:          "shutdown timeout",
			address:       "127.0.0.1:8080",
			timeout:       "synthetic-sensitive-duration-marker",
			sensitiveRaw:  "synthetic-sensitive-duration-marker",
			expectedField: "JARVIS_SHUTDOWN_TIMEOUT",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("JARVIS_HTTP_ADDRESS", test.address)
			t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", test.timeout)

			_, err := FromEnv()
			if err == nil {
				t.Fatal("FromEnv() returned nil error for invalid configuration")
			}
			if strings.Contains(err.Error(), test.sensitiveRaw) {
				t.Fatal("configuration error exposed the raw invalid value")
			}
			if !strings.Contains(err.Error(), test.expectedField) {
				t.Fatalf("configuration error does not identify %s", test.expectedField)
			}
		})
	}
}

func TestFromEnvRejectsInvalidValues(t *testing.T) {
	disableFinancialAPI(t)
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
	disableFinancialAPI(t)
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

func TestFromEnvEnablesFinancialAPIWithExplicitOwner(t *testing.T) {
	t.Setenv("JARVIS_HTTP_ADDRESS", "127.0.0.1:8080")
	t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", "10s")
	t.Setenv("JARVIS_FINANCIAL_API_ENABLED", "true")
	t.Setenv("JARVIS_OWNER_ID", "usr_synthetic_owner_001")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if !cfg.FinancialAPIEnabled || cfg.OwnerID != "usr_synthetic_owner_001" {
		t.Fatal("FromEnv() did not preserve the explicit financial application context")
	}
}

func TestFromEnvRejectsInvalidFinancialAPIConfigurationSafely(t *testing.T) {
	t.Setenv("JARVIS_HTTP_ADDRESS", "127.0.0.1:8080")
	t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", "10s")

	t.Setenv("JARVIS_FINANCIAL_API_ENABLED", "SUPER_SECRET_MARKER_98431")
	if _, err := FromEnv(); !errors.Is(err, ErrInvalidFinancialAPIEnabled) {
		t.Fatalf("FromEnv() error = %v, want ErrInvalidFinancialAPIEnabled", err)
	} else if strings.Contains(err.Error(), "SUPER_SECRET_MARKER_98431") {
		t.Fatal("financial API configuration error exposed its raw value")
	}

	t.Setenv("JARVIS_FINANCIAL_API_ENABLED", "true")
	t.Setenv("JARVIS_OWNER_ID", "")
	if _, err := FromEnv(); !errors.Is(err, ErrMissingOwnerID) {
		t.Fatalf("FromEnv() error = %v, want ErrMissingOwnerID", err)
	}
}

func disableFinancialAPI(t *testing.T) {
	t.Helper()
	t.Setenv("JARVIS_FINANCIAL_API_ENABLED", "")
	t.Setenv("JARVIS_OWNER_ID", "")
}
