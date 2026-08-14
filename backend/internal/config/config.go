package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddress     = "127.0.0.1:8080"
	defaultShutdownTimeout = 10 * time.Second
	maxShutdownTimeout     = 30 * time.Second
)

// Config contains the process configuration required by the foundation.
type Config struct {
	HTTPAddress     string
	ShutdownTimeout time.Duration
}

// FromEnv loads and validates configuration from environment variables.
func FromEnv() (Config, error) {
	cfg := Config{
		HTTPAddress:     valueOrDefault("JARVIS_HTTP_ADDRESS", defaultHTTPAddress),
		ShutdownTimeout: defaultShutdownTimeout,
	}

	if err := validateAddress(cfg.HTTPAddress); err != nil {
		return Config{}, fmt.Errorf("JARVIS_HTTP_ADDRESS: %w", err)
	}

	if raw := strings.TrimSpace(os.Getenv("JARVIS_SHUTDOWN_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("JARVIS_SHUTDOWN_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("JARVIS_SHUTDOWN_TIMEOUT: must be greater than zero")
		}
		if timeout > maxShutdownTimeout {
			return Config{}, fmt.Errorf("JARVIS_SHUTDOWN_TIMEOUT: must not exceed %s", maxShutdownTimeout)
		}
		cfg.ShutdownTimeout = timeout
	}

	return cfg, nil
}

func valueOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func validateAddress(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("must use host:port format: %w", err)
	}

	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("port must be a number between 1 and 65535")
	}

	return nil
}
