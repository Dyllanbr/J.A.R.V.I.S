package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open initializes and verifies a bounded PostgreSQL connection pool.
func Open(ctx context.Context, config Config) (*pgxpool.Pool, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	poolConfig, err := pgxpool.ParseConfig(config.URL)
	if err != nil {
		return nil, newSafeError(ErrInvalidDatabaseURL, err)
	}
	poolConfig.MaxConns = config.MaxConnections
	poolConfig.MinConns = config.MinConnections
	poolConfig.ConnConfig.ConnectTimeout = config.ConnectTimeout

	connectContext, cancelConnect := context.WithTimeout(ctx, config.ConnectTimeout)
	defer cancelConnect()

	pool, err := pgxpool.NewWithConfig(connectContext, poolConfig)
	if err != nil {
		return nil, newSafeError(ErrPoolInitialization, err)
	}

	pingContext, cancelPing := context.WithTimeout(ctx, config.ConnectTimeout)
	defer cancelPing()
	if err := pool.Ping(pingContext); err != nil {
		pool.Close()
		return nil, newSafeError(ErrPoolReadiness, err)
	}

	return pool, nil
}
