//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"jarvis/backend/internal/platform/postgres/migrations"
)

func TestMigration006FreshSchemaConstraintsSafeDownAndReapply(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 6)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	const ownerB = "usr_suggestion_migration_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerB)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 6)
	})
	assertTableExists(t, ctx, pool, "recurrence_suggestion_suppressions", true)
	assertColumnType(t, ctx, pool, "recurrence_suggestion_suppressions", "evidence_fingerprint", "bytea")
	assertColumnType(t, ctx, pool, "recurrence_suggestion_suppressions", "dismissed_at", "timestamp with time zone")

	validID := recurrenceSuggestionTestID('a')
	dismissedAt := time.Date(2026, 8, 24, 14, 30, 0, 123_456_000, time.UTC)
	if err := insertRawSuggestionSuppression(
		ctx,
		pool,
		syntheticUserID,
		validID,
		"DISMISS_RECURRENCE_SUGGESTION",
		bytesOf(0x11, 32),
		dismissedAt,
	); err != nil {
		t.Fatalf("valid suppression rejected: %v", err)
	}
	if err := insertRawSuggestionSuppression(
		ctx,
		pool,
		ownerB,
		validID,
		"DISMISS_RECURRENCE_SUGGESTION",
		bytesOf(0x12, 32),
		dismissedAt,
	); err != nil {
		t.Fatalf("same SuggestionID for another owner was rejected: %v", err)
	}

	invalid := []struct {
		name, userID, suggestionID, operation string
		fingerprint                           []byte
		constraint                            string
	}{
		{
			name: "unknown owner", userID: "usr_missing_owner", suggestionID: recurrenceSuggestionTestID('b'),
			operation: "DISMISS_RECURRENCE_SUGGESTION", fingerprint: bytesOf(0x22, 32),
			constraint: "recurrence_suggestion_suppressions_user_id_fkey",
		},
		{
			name: "suggestion id", userID: syntheticUserID, suggestionID: "rsg_NOT_HEX",
			operation: "DISMISS_RECURRENCE_SUGGESTION", fingerprint: bytesOf(0x33, 32),
			constraint: "recurrence_suggestion_suppressions_suggestion_id_valid",
		},
		{
			name: "operation", userID: syntheticUserID, suggestionID: recurrenceSuggestionTestID('c'),
			operation: "CREATE_RECURRENCE", fingerprint: bytesOf(0x44, 32),
			constraint: "recurrence_suggestion_suppressions_operation_valid",
		},
		{
			name: "fingerprint", userID: syntheticUserID, suggestionID: recurrenceSuggestionTestID('d'),
			operation: "DISMISS_RECURRENCE_SUGGESTION", fingerprint: bytesOf(0x55, 31),
			constraint: "recurrence_suggestion_suppressions_fingerprint_sha256",
		},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			err := insertRawSuggestionSuppression(
				ctx,
				pool,
				test.userID,
				test.suggestionID,
				test.operation,
				test.fingerprint,
				dismissedAt,
			)
			assertPostgresConstraint(t, err, test.constraint)
		})
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		err := migrations.Down(ctx, connection)
		if !errors.Is(err, migrations.ErrRollback) {
			t.Fatalf("migration 006 DOWN with data error = %v, want rollback", err)
		}
		assertMigrationVersion(t, ctx, connection, 6)
	})
	assertTableExists(t, ctx, pool, "recurrence_suggestion_suppressions", true)

	if _, err := pool.Exec(ctx, "DELETE FROM recurrence_suggestion_suppressions"); err != nil {
		t.Fatal("clearing suppression fixture failed")
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("empty migration 006 DOWN failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 5)
	})
	assertTableExists(t, ctx, pool, "recurrence_suggestion_suppressions", false)
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 006 reapply failed: %v", err)
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("migration 008 DOWN after migration 006 reapply failed: %v", err)
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("migration 007 DOWN after migration 006 reapply failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 6)
	})
	assertTableExists(t, ctx, pool, "recurrence_suggestion_suppressions", true)
}

func TestMigration006UpgradeFrom005PreservesExistingFinancialAndRecurrenceData(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigration005(t, ctx, pool)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	now := time.Date(2026, 8, 24, 14, 0, 0, 0, time.UTC)
	if err := insertRawFinancialTransaction(ctx, pool, "exp-before-006", syntheticUserID, "EXPENSE", "PIX", 11900, now); err != nil {
		t.Fatalf("version 005 Expense fixture failed: %v", err)
	}
	if err := insertRawRecurrence(ctx, pool, "rec-before-006", syntheticUserID, "2026-09-10", "ACTIVE", now, nil); err != nil {
		t.Fatalf("version 005 Recurrence fixture failed: %v", err)
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 005 to 006 failed: %v", err)
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("migration 008 DOWN after migration 006 upgrade failed: %v", err)
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("migration 007 DOWN after migration 006 upgrade failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 6)
	})
	var expenseCount, recurrenceCount, suppressionCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE id = 'exp-before-006'").Scan(&expenseCount); err != nil {
		t.Fatal("Expense count after migration 006 failed")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM recurrences WHERE id = 'rec-before-006'").Scan(&recurrenceCount); err != nil {
		t.Fatal("Recurrence count after migration 006 failed")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM recurrence_suggestion_suppressions").Scan(&suppressionCount); err != nil {
		t.Fatal("suppression count after migration 006 failed")
	}
	if expenseCount != 1 || recurrenceCount != 1 || suppressionCount != 0 {
		t.Fatalf("migration 006 counts = expense %d recurrence %d suppression %d, want 1/1/0", expenseCount, recurrenceCount, suppressionCount)
	}
}

func TestMigration006DownWaitsForWriterBeforeLockAndRefuses(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 6)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring suppression writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	tx, err := writer.Begin(ctx)
	if err != nil {
		t.Fatal("beginning suppression writer failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := insertRawSuggestionSuppression(
		ctx,
		tx,
		syntheticUserID,
		recurrenceSuggestionTestID('e'),
		"DISMISS_RECURRENCE_SUGGESTION",
		bytesOf(0x66, 32),
		time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatal("uncommitted suppression writer failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForBackendBlockedBy(t, ctx, pool, downPID, writerPID)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("committing suppression writer failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); !errors.Is(err, migrations.ErrRollback) {
		t.Fatalf("concurrent migration 006 DOWN error = %v, want rollback", err)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 6)
	})
	assertTableExists(t, ctx, pool, "recurrence_suggestion_suppressions", true)
}

func TestMigration006DownBlocksWriterQueuedAfterLockWithoutSilentLoss(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 6)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring suppression DOWN blocker failed")
	}
	defer blocker.Release()
	blockerPID := connectionBackendPID(t, ctx, blocker)
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal("beginning suppression DOWN blocker failed")
	}
	defer func() { _ = blockerTx.Rollback(context.Background()) }()
	if _, err := blockerTx.Exec(ctx, "LOCK TABLE recurrence_suggestion_suppressions IN ACCESS SHARE MODE"); err != nil {
		t.Fatal("locking suppression table fixture failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForBackendBlockedBy(t, ctx, pool, downPID, blockerPID)

	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring post-lock suppression writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	writerResult := make(chan error, 1)
	go func() {
		writerResult <- insertRawSuggestionSuppression(
			ctx,
			writer,
			syntheticUserID,
			recurrenceSuggestionTestID('f'),
			"DISMISS_RECURRENCE_SUGGESTION",
			bytesOf(0x77, 32),
			time.Date(2026, 8, 24, 16, 0, 0, 0, time.UTC),
		)
	}()
	waitForBackendBlockedBy(t, ctx, pool, writerPID, downPID)
	if err := blockerTx.Commit(ctx); err != nil {
		t.Fatal("releasing suppression DOWN blocker failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); err != nil {
		t.Fatalf("empty migration 006 DOWN failed: %v", err)
	}
	if err := waitForMigrationResult(t, ctx, writerResult); err == nil {
		t.Fatal("suppression writer crossed completed migration 006 DOWN")
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 5)
	})
	assertTableExists(t, ctx, pool, "recurrence_suggestion_suppressions", false)
}

type suggestionSuppressionExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertRawSuggestionSuppression(
	ctx context.Context,
	executor suggestionSuppressionExecer,
	userID, suggestionID, operation string,
	fingerprint []byte,
	dismissedAt time.Time,
) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO recurrence_suggestion_suppressions (
			user_id, suggestion_id, operation, evidence_fingerprint, dismissed_at
		) VALUES ($1, $2, $3, $4, $5)
	`, userID, suggestionID, operation, fingerprint, dismissedAt)
	return err
}

func recurrenceSuggestionTestID(character byte) string {
	return "rsg_" + strings.Repeat(string(character), 64)
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}

func assertPostgresConstraint(t testing.TB, err error, expectedConstraint string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("constraint %s error = %v, want *pgconn.PgError", expectedConstraint, err)
	}
	expectedCode := "23514"
	if expectedConstraint == "recurrence_suggestion_suppressions_user_id_fkey" {
		expectedCode = "23503"
	}
	if postgresError.Code != expectedCode || postgresError.ConstraintName != expectedConstraint {
		t.Fatalf(
			"constraint failure = %q/%q, want %q/%q",
			postgresError.Code,
			postgresError.ConstraintName,
			expectedCode,
			expectedConstraint,
		)
	}
}
