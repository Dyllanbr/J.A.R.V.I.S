package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigUsesSafeDefaults(t *testing.T) {
	config, err := LoadConfig(environment(map[string]string{
		"JARVIS_DATABASE_URL": "postgres://jarvis:synthetic@127.0.0.1:5432/jarvis?sslmode=disable",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.MaxConnections != 4 || config.MinConnections != 0 {
		t.Fatalf("connection limits = %d/%d, want 4/0", config.MaxConnections, config.MinConnections)
	}
	if config.ConnectTimeout != 5*time.Second || config.OperationTimeout != 5*time.Second {
		t.Fatalf("timeouts = %s/%s, want 5s/5s", config.ConnectTimeout, config.OperationTimeout)
	}
}

func TestLoadConfigAcceptsExplicitValues(t *testing.T) {
	config, err := LoadConfig(environment(map[string]string{
		"JARVIS_DATABASE_URL":         "postgres://jarvis:synthetic@127.0.0.1:5432/jarvis?sslmode=disable",
		"JARVIS_DB_MAX_CONNS":         "8",
		"JARVIS_DB_MIN_CONNS":         "2",
		"JARVIS_DB_CONNECT_TIMEOUT":   "3s",
		"JARVIS_DB_OPERATION_TIMEOUT": "7s",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if config.MaxConnections != 8 || config.MinConnections != 2 {
		t.Fatalf("connection limits = %d/%d, want 8/2", config.MaxConnections, config.MinConnections)
	}
	if config.ConnectTimeout != 3*time.Second || config.OperationTimeout != 7*time.Second {
		t.Fatalf("timeouts = %s/%s, want 3s/7s", config.ConnectTimeout, config.OperationTimeout)
	}
}

func TestLoadConfigRejectsUnsafeValuesWithoutEchoingThem(t *testing.T) {
	const marker = "SUPER_SECRET_MARKER_98431"
	tests := []struct {
		name     string
		values   map[string]string
		expected error
	}{
		{
			name: "database URL",
			values: map[string]string{
				"JARVIS_DATABASE_URL": marker,
			},
			expected: ErrInvalidDatabaseURL,
		},
		{
			name: "maximum connections",
			values: map[string]string{
				"JARVIS_DATABASE_URL": "postgres://jarvis:synthetic@127.0.0.1:5432/jarvis",
				"JARVIS_DB_MAX_CONNS": marker,
			},
			expected: ErrInvalidMaxConnections,
		},
		{
			name: "connect timeout",
			values: map[string]string{
				"JARVIS_DATABASE_URL":       "postgres://jarvis:synthetic@127.0.0.1:5432/jarvis",
				"JARVIS_DB_CONNECT_TIMEOUT": marker,
			},
			expected: ErrInvalidConnectTimeout,
		},
		{
			name: "minimum connections",
			values: map[string]string{
				"JARVIS_DATABASE_URL": "postgres://jarvis:synthetic@127.0.0.1:5432/jarvis",
				"JARVIS_DB_MIN_CONNS": marker,
			},
			expected: ErrInvalidMinConnections,
		},
		{
			name: "operation timeout",
			values: map[string]string{
				"JARVIS_DATABASE_URL":         "postgres://jarvis:synthetic@127.0.0.1:5432/jarvis",
				"JARVIS_DB_OPERATION_TIMEOUT": marker,
			},
			expected: ErrInvalidOperationTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := LoadConfig(environment(test.values))
			if !errors.Is(err, test.expected) {
				t.Fatalf("LoadConfig() error = %v, want %v", err, test.expected)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatal("configuration error exposed the invalid raw value")
			}
		})
	}
}

func TestLoadConfigRejectsInvalidRanges(t *testing.T) {
	tests := []map[string]string{
		{"JARVIS_DATABASE_URL": "postgres://jarvis:synthetic@localhost/jarvis", "JARVIS_DB_MAX_CONNS": "0"},
		{"JARVIS_DATABASE_URL": "postgres://jarvis:synthetic@localhost/jarvis", "JARVIS_DB_MIN_CONNS": "5", "JARVIS_DB_MAX_CONNS": "4"},
		{"JARVIS_DATABASE_URL": "postgres://jarvis:synthetic@localhost/jarvis", "JARVIS_DB_OPERATION_TIMEOUT": "31s"},
	}

	for _, values := range tests {
		if _, err := LoadConfig(environment(values)); err == nil {
			t.Fatal("LoadConfig() error = nil, want invalid configuration")
		}
	}
}

func TestValidateConfigRejectsInvalidDirectConfiguration(t *testing.T) {
	valid := Config{
		URL:              "postgres://jarvis:synthetic@localhost/jarvis",
		MaxConnections:   4,
		MinConnections:   0,
		ConnectTimeout:   time.Second,
		OperationTimeout: time.Second,
	}

	tests := []struct {
		name     string
		mutate   func(*Config)
		expected error
	}{
		{name: "max connections", mutate: func(config *Config) { config.MaxConnections = 0 }, expected: ErrInvalidMaxConnections},
		{name: "min connections", mutate: func(config *Config) { config.MinConnections = 5 }, expected: ErrInvalidConnectionLimits},
		{name: "connect timeout", mutate: func(config *Config) { config.ConnectTimeout = 0 }, expected: ErrInvalidConnectTimeout},
		{name: "operation timeout", mutate: func(config *Config) { config.OperationTimeout = 0 }, expected: ErrInvalidOperationTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			if err := validateConfig(config); !errors.Is(err, test.expected) {
				t.Fatalf("validateConfig() error = %v, want %v", err, test.expected)
			}
		})
	}
}

func environment(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
