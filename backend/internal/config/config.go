package config

import (
	"errors"
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

var (
	ErrInvalidFinancialAPIEnabled = errors.New("JARVIS_FINANCIAL_API_ENABLED: must be true or false")
	ErrMissingOwnerID             = errors.New("JARVIS_OWNER_ID: is required when the financial API is enabled")
)

// Config contains the process configuration required by the foundation.
type Config struct {
	HTTPAddress         string
	ShutdownTimeout     time.Duration
	FinancialAPIEnabled bool
	OwnerID             string
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
			return Config{}, errors.New("JARVIS_SHUTDOWN_TIMEOUT: must be a valid duration")
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("JARVIS_SHUTDOWN_TIMEOUT: must be greater than zero")
		}
		if timeout > maxShutdownTimeout {
			return Config{}, fmt.Errorf("JARVIS_SHUTDOWN_TIMEOUT: must not exceed %s", maxShutdownTimeout)
		}
		cfg.ShutdownTimeout = timeout
	}

	switch enabled := os.Getenv("JARVIS_FINANCIAL_API_ENABLED"); enabled {
	case "", "false":
		cfg.FinancialAPIEnabled = false
	case "true":
		cfg.FinancialAPIEnabled = true
	default:
		return Config{}, ErrInvalidFinancialAPIEnabled
	}
	if cfg.FinancialAPIEnabled {
		cfg.OwnerID = os.Getenv("JARVIS_OWNER_ID")
		if cfg.OwnerID == "" {
			return Config{}, ErrMissingOwnerID
		}
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
		return errors.New("must use host:port format")
	}

	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("port must be a number between 1 and 65535")
	}

	return nil
}
