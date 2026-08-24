//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/platform/postgres/migrations"
)

func TestMigration005FreshSchemaConstraintsAndSafeDownReapply(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	const ownerB = "usr-migration-recurrence-owner-b"
	insertSyntheticUser(t, ctx, pool, ownerB)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 5)
	})
	for _, table := range []string{"recurrences", "recurrence_audit_events", "recurrence_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, true)
	}
	assertColumnType(t, ctx, pool, "recurrences", "starts_on", "date")
	assertColumnType(t, ctx, pool, "recurrences", "created_at", "timestamp with time zone")
	assertColumnType(t, ctx, pool, "recurrences", "cancelled_at", "timestamp with time zone")
	assertColumnExists(t, ctx, pool, "transactions", "recurrence_id", false)

	createdAt := time.Date(2026, time.January, 31, 15, 0, 0, 123_456_000, time.UTC)
	for index, startsOn := range []string{"2026-02-28", "2024-02-29", "2026-04-30", "2026-01-31", "2027-01-01"} {
		id := fmt.Sprintf("rec-migration-valid-%d", index)
		if err := insertRawRecurrence(ctx, pool, id, syntheticUserID, startsOn, "ACTIVE", createdAt, nil); err != nil {
			t.Fatalf("valid CivilDate %s was rejected: %v", startsOn, err)
		}
	}

	invalidStatements := []struct {
		name      string
		statement string
		args      []any
	}{
		{name: "missing owner", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-missing-owner", "usr-missing", "EXPENSE", 11900, "BRL", "MONTHLY", "2026-08-10", "ACTIVE", createdAt, nil)},
		{name: "Income type", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-income", syntheticUserID, "INCOME", 11900, "BRL", "MONTHLY", "2026-08-10", "ACTIVE", createdAt, nil)},
		{name: "currency", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-usd", syntheticUserID, "EXPENSE", 11900, "USD", "MONTHLY", "2026-08-10", "ACTIVE", createdAt, nil)},
		{name: "frequency", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-weekly", syntheticUserID, "EXPENSE", 11900, "BRL", "WEEKLY", "2026-08-10", "ACTIVE", createdAt, nil)},
		{name: "zero amount", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-zero", syntheticUserID, "EXPENSE", 0, "BRL", "MONTHLY", "2026-08-10", "ACTIVE", createdAt, nil)},
		{name: "negative amount", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-negative", syntheticUserID, "EXPENSE", -1, "BRL", "MONTHLY", "2026-08-10", "ACTIVE", createdAt, nil)},
		{name: "date above domain", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-date-high", syntheticUserID, "EXPENSE", 11900, "BRL", "MONTHLY", "10000-01-01", "ACTIVE", createdAt, nil)},
		{name: "unknown status", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-status", syntheticUserID, "EXPENSE", 11900, "BRL", "MONTHLY", "2026-08-10", "PAUSED", createdAt, nil)},
		{name: "active with cancellation", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-active-cancelled", syntheticUserID, "EXPENSE", 11900, "BRL", "MONTHLY", "2026-08-10", "ACTIVE", createdAt, createdAt.Add(time.Hour))},
		{name: "cancelled without timestamp", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-cancelled-null", syntheticUserID, "EXPENSE", 11900, "BRL", "MONTHLY", "2026-08-10", "CANCELLED", createdAt, nil)},
		{name: "cancelled before creation", statement: rawRecurrenceInsertSQL, args: rawRecurrenceArgs("rec-cancelled-before", syntheticUserID, "EXPENSE", 11900, "BRL", "MONTHLY", "2026-08-10", "CANCELLED", createdAt, createdAt.Add(-time.Microsecond))},
	}
	for _, test := range invalidStatements {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.statement, test.args...); err == nil {
				t.Fatal("invalid recurrence row was accepted")
			}
		})
	}
	if _, err := pool.Exec(ctx, "UPDATE recurrences SET description = ' ' WHERE id = 'rec-migration-valid-0'"); err == nil {
		t.Fatal("blank recurrence description was accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE recurrences SET id = E'rec-invalid\\n' WHERE id = 'rec-migration-valid-0'"); err == nil {
		t.Fatal("controlled recurrence identifier was accepted")
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO recurrence_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, 'CREATE_EXPENSE', 'invalid-operation', $2, 'PENDING', $3)
	`, syntheticUserID, make([]byte, 32), createdAt); err == nil {
		t.Fatal("transaction idempotency operation was accepted by recurrence storage")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO recurrence_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, 'CREATE_RECURRENCE', 'invalid-fingerprint', $2, 'PENDING', $3)
	`, syntheticUserID, make([]byte, 31), createdAt)
	assertPostgresCheckConstraint(
		t,
		err,
		"recurrence_idempotency_records_fingerprint_sha256",
	)

	completedAt := createdAt.Add(2 * time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO recurrence_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			recurrence_id, result_transaction_type, result_description,
			result_expected_amount_minor, result_currency, result_frequency,
			result_starts_on, result_status, result_created_at, result_cancelled_at,
			created_at, completed_at
		) VALUES (
			$1, 'CREATE_RECURRENCE', 'invalid-create-snapshot', $2, 'COMPLETED',
			'rec-migration-valid-0', 'EXPENSE', 'Serviço sintético recorrente',
			11900, 'BRL', 'MONTHLY', DATE '2026-02-28', 'CANCELLED', $3, $4,
			$3, $5
		)
	`, syntheticUserID, make([]byte, 32), createdAt, createdAt.Add(time.Hour), completedAt)
	assertPostgresCheckConstraint(
		t,
		err,
		"recurrence_idempotency_records_result_valid",
	)

	_, err = pool.Exec(ctx, `
		INSERT INTO recurrence_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			recurrence_id, result_transaction_type, result_description,
			result_expected_amount_minor, result_currency, result_frequency,
			result_starts_on, result_status, result_created_at, result_cancelled_at,
			created_at, completed_at
		) VALUES (
			$1, 'CANCEL_RECURRENCE', 'invalid-cancel-snapshot', $2, 'COMPLETED',
			'rec-migration-valid-0', 'EXPENSE', 'Serviço sintético recorrente',
			11900, 'BRL', 'MONTHLY', DATE '2026-02-28', 'ACTIVE', $3, NULL,
			$3, $4
		)
	`, syntheticUserID, make([]byte, 32), createdAt, completedAt)
	assertPostgresCheckConstraint(
		t,
		err,
		"recurrence_idempotency_records_result_valid",
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO recurrence_audit_events (user_id, recurrence_id, event_type, created_at)
		VALUES ($1, 'rec-migration-valid-0', 'RECURRENCE_CONFIRMED', $2)
	`, syntheticUserID, createdAt); err == nil {
		t.Fatal("unsupported recurrence audit event was accepted")
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO recurrence_audit_events (user_id, recurrence_id, event_type, created_at)
		VALUES ($1, 'rec-migration-valid-0', 'RECURRENCE_CREATED', $2)
	`, ownerB, createdAt); err == nil {
		t.Fatal("cross-owner recurrence audit reference was accepted")
	}

	// Existing recurrence rows make DOWN refuse. Remove only synthetic rows to
	// prove the empty migration can safely downgrade and reapply.
	if _, err := pool.Exec(ctx, "DELETE FROM recurrences"); err != nil {
		t.Fatal("clearing synthetic recurrence constraints fixture failed")
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("empty migration 005 DOWN failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 4)
	})
	for _, table := range []string{"recurrences", "recurrence_audit_events", "recurrence_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, false)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 005 reapply failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 5)
	})
}

func TestMigration005UpgradeFrom004PreservesFinancialAndCategoryData(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigration004(t, ctx, pool)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	now := time.Date(2026, time.August, 16, 15, 0, 0, 0, time.UTC)
	if err := insertRawTransactionWithCategory(ctx, pool, "exp-before-005", syntheticUserID, "EXPENSE", "PIX", "expense.food", now); err != nil {
		t.Fatalf("version 004 transaction fixture failed: %v", err)
	}
	if err := insertRawTypedAuditEvent(ctx, pool, syntheticUserID, "exp-before-005", "EXPENSE", "EXPENSE_RECORDED", now); err != nil {
		t.Fatalf("version 004 audit fixture failed: %v", err)
	}
	if err := insertPendingIdempotency(ctx, pool, syntheticUserID, "CREATE_EXPENSE", "before-005-key", now); err != nil {
		t.Fatalf("version 004 idempotency fixture failed: %v", err)
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 004 to 005 failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 5)
	})
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
	assertStoredCategory(t, ctx, pool, "exp-before-005", "expense.food")
	var categoryCount, recurrenceCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM categories").Scan(&categoryCount); err != nil {
		t.Fatal("category count after migration 005 failed")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM recurrences").Scan(&recurrenceCount); err != nil {
		t.Fatal("recurrence count after migration 005 failed")
	}
	if categoryCount != 17 || recurrenceCount != 0 {
		t.Fatalf("upgrade category/recurrence counts = %d/%d, want 17/0", categoryCount, recurrenceCount)
	}
}

func TestMigration005DownRefusesEverySubsystemStateAtomically(t *testing.T) {
	t.Run("recurrence with audit and idempotency", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		createdAt := time.Date(2026, time.August, 16, 15, 0, 0, 0, time.UTC)
		if err := insertRawRecurrence(ctx, pool, "rec-down-guard", syntheticUserID, "2026-08-10", "ACTIVE", createdAt, nil); err != nil {
			t.Fatal("recurrence DOWN fixture failed")
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO recurrence_audit_events (user_id, recurrence_id, event_type, created_at)
			VALUES ($1, 'rec-down-guard', 'RECURRENCE_CREATED', $2)
		`, syntheticUserID, createdAt); err != nil {
			t.Fatal("audit DOWN fixture failed")
		}
		assertMigration005DownRefusedAndIntact(t, ctx, pool)
	})

	t.Run("pending idempotency without recurrence", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		if _, err := pool.Exec(ctx, `
			INSERT INTO recurrence_idempotency_records (
				user_id, operation, idempotency_key, request_fingerprint, state, created_at
			) VALUES ($1, 'CREATE_RECURRENCE', 'pending-down-guard', $2, 'PENDING', $3)
		`, syntheticUserID, make([]byte, 32), time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal("pending idempotency DOWN fixture failed")
		}
		assertMigration005DownRefusedAndIntact(t, ctx, pool)
	})
}

func TestMigration005DownWaitsForWriterBeforeLockAndRefuses(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	writerConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring recurrence writer failed")
	}
	defer writerConnection.Release()
	writerPID := connectionBackendPID(t, ctx, writerConnection)
	writerTransaction, err := writerConnection.Begin(ctx)
	if err != nil {
		t.Fatal("beginning recurrence writer failed")
	}
	defer func() { _ = writerTransaction.Rollback(context.Background()) }()
	createdAt := time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	if err := insertRawRecurrence(ctx, writerTransaction, "rec-down-before-lock", syntheticUserID, "2026-08-10", "ACTIVE", createdAt, nil); err != nil {
		t.Fatal("uncommitted recurrence writer failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForBackendBlockedBy(t, ctx, pool, downPID, writerPID)

	if err := writerTransaction.Commit(ctx); err != nil {
		t.Fatal("committing recurrence writer failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); !errors.Is(err, migrations.ErrRollback) {
		t.Fatalf("concurrent DOWN error = %v, want rollback", err)
	}
	assertMigration005DownRefusedAndIntact(t, ctx, pool)
	var storedStatus string
	if err := pool.QueryRow(ctx, "SELECT status FROM recurrences WHERE id = 'rec-down-before-lock'").Scan(&storedStatus); err != nil || storedStatus != "ACTIVE" {
		t.Fatalf("committed recurrence was not preserved: status=%q err=%v", storedStatus, err)
	}
}

func TestMigration005DownBlocksWriterAfterLockWithoutSilentLoss(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring recurrence audit blocker failed")
	}
	defer blocker.Release()
	blockerPID := connectionBackendPID(t, ctx, blocker)
	blockerTransaction, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal("beginning recurrence audit blocker failed")
	}
	defer func() { _ = blockerTransaction.Rollback(context.Background()) }()
	if _, err := blockerTransaction.Exec(ctx, "LOCK TABLE recurrence_audit_events IN ACCESS SHARE MODE"); err != nil {
		t.Fatal("locking recurrence audit table failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForRecurrenceDownLocks(t, ctx, pool, downPID, blockerPID)

	writerConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring post-lock recurrence writer failed")
	}
	defer writerConnection.Release()
	writerPID := connectionBackendPID(t, ctx, writerConnection)
	writerResult := make(chan error, 1)
	createdAt := time.Date(2026, 8, 16, 15, 0, 0, 0, time.UTC)
	go func() {
		writerResult <- insertRawRecurrence(ctx, writerConnection, "rec-down-after-lock", syntheticUserID, "2026-08-10", "ACTIVE", createdAt, nil)
	}()
	waitForBackendBlockedBy(t, ctx, pool, writerPID, downPID)

	if err := blockerTransaction.Commit(ctx); err != nil {
		t.Fatal("releasing recurrence audit blocker failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); err != nil {
		t.Fatalf("empty migration 005 DOWN failed: %v", err)
	}
	if err := waitForMigrationResult(t, ctx, writerResult); err == nil {
		t.Fatal("recurrence writer crossed a completed migration 005 DOWN")
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 4)
	})
	for _, table := range []string{"recurrences", "recurrence_audit_events", "recurrence_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, false)
	}
}

func moveToMigration004(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("migration 005 DOWN failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 4)
	})
}

func assertMigration005DownRefusedAndIntact(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		err := migrations.Down(ctx, connection)
		if !errors.Is(err, migrations.ErrRollback) {
			t.Fatalf("migration 005 DOWN error = %v, want rollback", err)
		}
		assertMigrationVersion(t, ctx, connection, 5)
	})
	for _, table := range []string{"recurrences", "recurrence_audit_events", "recurrence_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, true)
	}
}

func waitForRecurrenceDownLocks(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	downPID, blockingPID int32,
) {
	t.Helper()
	for {
		var ready bool
		if err := pool.QueryRow(ctx, `
			SELECT
				EXISTS (
					SELECT 1 FROM pg_locks
					WHERE pid = $1
					  AND relation = 'public.recurrence_idempotency_records'::regclass
					  AND mode = 'AccessExclusiveLock'
					  AND granted
				)
				AND EXISTS (
					SELECT 1 FROM pg_locks
					WHERE pid = $1
					  AND relation = 'public.recurrences'::regclass
					  AND mode = 'AccessExclusiveLock'
					  AND granted
				)
				AND $2::integer = ANY(pg_blocking_pids($1::integer))
		`, downPID, blockingPID).Scan(&ready); err != nil {
			t.Fatal("migration 005 DOWN lock state lookup failed")
		}
		if ready {
			return
		}
	}
}

const rawRecurrenceInsertSQL = `
	INSERT INTO recurrences (
		id, user_id, transaction_type, description, expected_amount_minor,
		currency, frequency, starts_on, status, created_at, cancelled_at
	) VALUES ($1, $2, $3, 'Serviço sintético recorrente', $4, $5, $6, $7, $8, $9, $10)
`

func rawRecurrenceArgs(
	id, userID, transactionType string,
	amount int64,
	currency, frequency, startsOn, status string,
	createdAt time.Time,
	cancelledAt any,
) []any {
	return []any{id, userID, transactionType, amount, currency, frequency, startsOn, status, createdAt, cancelledAt}
}

type recurrenceSQLExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertRawRecurrence(
	ctx context.Context,
	executor recurrenceSQLExecutor,
	id, userID, startsOn, status string,
	createdAt time.Time,
	cancelledAt any,
) error {
	_, err := executor.Exec(ctx, rawRecurrenceInsertSQL, rawRecurrenceArgs(
		id, userID, "EXPENSE", 11900, "BRL", "MONTHLY", startsOn, status, createdAt, cancelledAt,
	)...)
	return err
}

func assertColumnType(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, column, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `
		SELECT data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&got); err != nil {
		t.Fatal("column type lookup failed")
	}
	if got != want {
		t.Fatalf("%s.%s type = %q, want %q", table, column, got, want)
	}
}

func assertPostgresCheckConstraint(t testing.TB, err error, expectedConstraint string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("constraint %s error = %v, want *pgconn.PgError", expectedConstraint, err)
	}
	if postgresError.Code != "23514" || postgresError.ConstraintName != expectedConstraint {
		t.Fatalf(
			"constraint failure code/name = %q/%q, want 23514/%q",
			postgresError.Code,
			postgresError.ConstraintName,
			expectedConstraint,
		)
	}
}
