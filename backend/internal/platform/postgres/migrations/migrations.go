package migrations

import (
	"context"
	"embed"
	"errors"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/tern/v2/migrate"
)

const versionTable = "public.schema_version"

var (
	ErrInitialize = errors.New("postgres migrations: initialization failed")
	ErrLoad       = errors.New("postgres migrations: loading failed")
	ErrApply      = errors.New("postgres migrations: apply failed")
	ErrRollback   = errors.New("postgres migrations: rollback failed")
	ErrVersion    = errors.New("postgres migrations: version lookup failed")
)

//go:embed sql/*.sql
var migrationFiles embed.FS

// Up applies every pending migration.
func Up(ctx context.Context, connection *pgx.Conn) error {
	migrator, err := newMigrator(ctx, connection)
	if err != nil {
		return err
	}
	if err := migrator.Migrate(ctx); err != nil {
		return newMigrationError(ErrApply, err)
	}
	return nil
}

// Down rolls back exactly one applied migration. Repeated calls are required
// to reach version zero, making a published base schema harder to remove by
// accident.
func Down(ctx context.Context, connection *pgx.Conn) error {
	migrator, err := newMigrator(ctx, connection)
	if err != nil {
		return err
	}
	version, err := migrator.GetCurrentVersion(ctx)
	if err != nil {
		return newMigrationError(ErrVersion, err)
	}
	if version == 0 {
		return nil
	}
	if err := migrator.MigrateTo(ctx, version-1); err != nil {
		return newMigrationError(ErrRollback, err)
	}
	return nil
}

// Version reports the applied migration version.
func Version(ctx context.Context, connection *pgx.Conn) (int32, error) {
	migrator, err := newMigrator(ctx, connection)
	if err != nil {
		return 0, err
	}
	version, err := migrator.GetCurrentVersion(ctx)
	if err != nil {
		return 0, newMigrationError(ErrVersion, err)
	}
	return version, nil
}

func newMigrator(ctx context.Context, connection *pgx.Conn) (*migrate.Migrator, error) {
	migrator, err := migrate.NewMigrator(ctx, connection, versionTable)
	if err != nil {
		return nil, newMigrationError(ErrInitialize, err)
	}

	files, err := fs.Sub(migrationFiles, "sql")
	if err != nil {
		return nil, newMigrationError(ErrLoad, err)
	}
	if err := migrator.LoadMigrations(files); err != nil {
		return nil, newMigrationError(ErrLoad, err)
	}

	return migrator, nil
}

type migrationError struct {
	category error
	cause    error
}

func newMigrationError(category, cause error) error {
	return migrationError{category: category, cause: cause}
}

func (err migrationError) Error() string {
	return err.category.Error()
}

func (err migrationError) Unwrap() []error {
	return []error{err.category, err.cause}
}
