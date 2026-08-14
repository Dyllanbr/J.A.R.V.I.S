package postgres

import (
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMaxConnections   = int32(4)
	defaultMinConnections   = int32(0)
	defaultConnectTimeout   = 5 * time.Second
	defaultOperationTimeout = 5 * time.Second
	maximumConnections      = int32(100)
	maximumTimeout          = 30 * time.Second
)

var (
	ErrMissingDatabaseURL      = errors.New("postgres: JARVIS_DATABASE_URL is required")
	ErrInvalidDatabaseURL      = errors.New("postgres: JARVIS_DATABASE_URL is invalid")
	ErrInvalidMaxConnections   = errors.New("postgres: JARVIS_DB_MAX_CONNS is invalid")
	ErrInvalidMinConnections   = errors.New("postgres: JARVIS_DB_MIN_CONNS is invalid")
	ErrInvalidConnectTimeout   = errors.New("postgres: JARVIS_DB_CONNECT_TIMEOUT is invalid")
	ErrInvalidOperationTimeout = errors.New("postgres: JARVIS_DB_OPERATION_TIMEOUT is invalid")
	ErrInvalidConnectionLimits = errors.New("postgres: connection limits are invalid")
	ErrPoolInitialization      = errors.New("postgres: pool initialization failed")
	ErrPoolReadiness           = errors.New("postgres: readiness check failed")
)

// Config contains the explicit PostgreSQL settings used by database commands
// and adapters. The health-only API runtime does not load this configuration.
type Config struct {
	URL              string
	MaxConnections   int32
	MinConnections   int32
	ConnectTimeout   time.Duration
	OperationTimeout time.Duration
}

// LoadConfig reads PostgreSQL configuration without exposing invalid raw values
// in returned error messages.
func LoadConfig(getenv func(string) string) (Config, error) {
	config := Config{
		URL:              getenv("JARVIS_DATABASE_URL"),
		MaxConnections:   defaultMaxConnections,
		MinConnections:   defaultMinConnections,
		ConnectTimeout:   defaultConnectTimeout,
		OperationTimeout: defaultOperationTimeout,
	}

	if config.URL == "" {
		return Config{}, ErrMissingDatabaseURL
	}
	if _, err := pgxpool.ParseConfig(config.URL); err != nil {
		return Config{}, newSafeError(ErrInvalidDatabaseURL, err)
	}

	var err error
	if config.MaxConnections, err = parseConnectionLimit(getenv("JARVIS_DB_MAX_CONNS"), defaultMaxConnections, false); err != nil {
		return Config{}, newSafeError(ErrInvalidMaxConnections, err)
	}
	if config.MinConnections, err = parseConnectionLimit(getenv("JARVIS_DB_MIN_CONNS"), defaultMinConnections, true); err != nil {
		return Config{}, newSafeError(ErrInvalidMinConnections, err)
	}
	if config.MinConnections > config.MaxConnections {
		return Config{}, ErrInvalidConnectionLimits
	}
	if config.ConnectTimeout, err = parseTimeout(getenv("JARVIS_DB_CONNECT_TIMEOUT"), defaultConnectTimeout); err != nil {
		return Config{}, newSafeError(ErrInvalidConnectTimeout, err)
	}
	if config.OperationTimeout, err = parseTimeout(getenv("JARVIS_DB_OPERATION_TIMEOUT"), defaultOperationTimeout); err != nil {
		return Config{}, newSafeError(ErrInvalidOperationTimeout, err)
	}

	return config, nil
}

func validateConfig(config Config) error {
	if config.URL == "" {
		return ErrMissingDatabaseURL
	}
	if _, err := pgxpool.ParseConfig(config.URL); err != nil {
		return newSafeError(ErrInvalidDatabaseURL, err)
	}
	if config.MaxConnections < 1 || config.MaxConnections > maximumConnections {
		return ErrInvalidMaxConnections
	}
	if config.MinConnections < 0 || config.MinConnections > config.MaxConnections {
		return ErrInvalidConnectionLimits
	}
	if config.ConnectTimeout <= 0 || config.ConnectTimeout > maximumTimeout {
		return ErrInvalidConnectTimeout
	}
	if config.OperationTimeout <= 0 || config.OperationTimeout > maximumTimeout {
		return ErrInvalidOperationTimeout
	}
	return nil
}

func parseConnectionLimit(raw string, defaultValue int32, allowZero bool) (int32, error) {
	if raw == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	minimum := int64(1)
	if allowZero {
		minimum = 0
	}
	if parsed < minimum || parsed > int64(maximumConnections) {
		return 0, errors.New("connection limit outside allowed range")
	}

	return int32(parsed), nil
}

func parseTimeout(raw string, defaultValue time.Duration) (time.Duration, error) {
	if raw == "" {
		return defaultValue, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 || parsed > maximumTimeout {
		return 0, errors.New("timeout outside allowed range")
	}

	return parsed, nil
}

type safeError struct {
	category error
	cause    error
}

func newSafeError(category, cause error) error {
	return safeError{category: category, cause: cause}
}

func (err safeError) Error() string {
	return err.category.Error()
}

func (err safeError) Unwrap() []error {
	return []error{err.category, err.cause}
}
