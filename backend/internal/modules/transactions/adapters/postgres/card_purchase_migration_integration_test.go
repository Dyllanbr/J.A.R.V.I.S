//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"jarvis/backend/internal/platform/postgres/migrations"
)

func TestMigration008FreshSchemaConstraintsSafeDownAndReapply(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 8)
	const owner = "usr_card_purchase_migration_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	createdAt := time.Date(2026, time.August, 25, 12, 0, 0, 123_456_000, time.UTC)
	cardID := creditCardTestID('a')
	if err := insertRawCreditCard(ctx, pool, rawCreditCard{
		id: cardID, userID: owner, name: "Cartão de compras", closingDay: 31,
		dueDay: 10, status: "ACTIVE", createdAt: createdAt,
	}); err != nil {
		t.Fatalf("card fixture failed: %v", err)
	}

	for _, table := range []string{
		"installment_plans", "installment_plan_audit_events",
		"card_purchase_idempotency_records", "installment_plan_idempotency_records",
	} {
		assertTableExists(t, ctx, pool, table, true)
	}
	assertColumnType(t, ctx, pool, "transactions", "credit_card_id", "text")
	assertColumnType(t, ctx, pool, "transactions", "statement_due_on", "date")
	assertColumnType(t, ctx, pool, "installment_plans", "created_at", "timestamp with time zone")
	assertColumnType(t, ctx, pool, "card_purchase_idempotency_records", "request_fingerprint", "bytea")
	assertColumnType(t, ctx, pool, "card_purchase_idempotency_records", "expense_user_id", "text")
	assertColumnType(t, ctx, pool, "card_purchase_idempotency_records", "plan_user_id", "text")
	assertColumnType(t, ctx, pool, "installment_plan_idempotency_records", "plan_user_id", "text")
	assertCreditCardTimestampPrecision(t, ctx, pool, "installment_plans", "created_at", 6)

	if _, err := pool.Exec(ctx, `
		INSERT INTO card_purchase_idempotency_records
		(user_id, operation, idempotency_key, request_fingerprint, state, created_at)
		VALUES ($1, 'CREATE_CARD_PURCHASE', 'migration-008-guard', $2, 'PENDING', $3)
	`, owner, make([]byte, 32), createdAt); err != nil {
		t.Fatalf("pending idempotency fixture failed: %v", err)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		err := migrations.Down(ctx, connection)
		if !errors.Is(err, migrations.ErrRollback) {
			t.Fatalf("migration 008 DOWN with data error=%v, want rollback", err)
		}
		assertMigrationVersion(t, ctx, connection, 8)
	})
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM card_purchase_idempotency_records").Scan(&count); err != nil || count != 1 {
		t.Fatalf("guard row count=%d error=%v, want 1", count, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM card_purchase_idempotency_records"); err != nil {
		t.Fatal("clearing migration 008 guard row failed")
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("empty migration 008 DOWN failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 7)
	})
	for _, table := range []string{
		"installment_plans", "installment_plan_audit_events",
		"card_purchase_idempotency_records", "installment_plan_idempotency_records",
	} {
		assertTableExists(t, ctx, pool, table, false)
	}
	assertColumnExists(t, ctx, pool, "transactions", "credit_card_id", false)
	assertColumnExists(t, ctx, pool, "transactions", "statement_due_on", false)
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 008 reapply failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 8)
	})
	for _, table := range []string{"installment_plans", "card_purchase_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, true)
	}
}

func TestMigration008UpgradeFrom007PreservesLegacyRows(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 7)
	const owner = "usr_card_purchase_upgrade_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	now := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	if err := insertRawFinancialTransaction(ctx, pool, "exp-before-008", owner, "EXPENSE", "PIX", 1500, now); err != nil {
		t.Fatalf("legacy transaction fixture failed: %v", err)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 007 to 008 failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 8)
	})
	var cardID, dueOn *string
	if err := pool.QueryRow(ctx, "SELECT credit_card_id, statement_due_on::text FROM transactions WHERE id='exp-before-008'").Scan(&cardID, &dueOn); err != nil {
		t.Fatalf("legacy transaction lookup failed: %v", err)
	}
	if cardID != nil || dueOn != nil {
		t.Fatalf("legacy transaction unexpectedly received card fields: card=%v due=%v", cardID, dueOn)
	}
}

func TestMigration008DownWaitsForWriterBeforeLockAndRefuses(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 8)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration 008 writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	writerTx, err := writer.Begin(ctx)
	if err != nil {
		t.Fatal("beginning migration 008 writer failed")
	}
	defer func() { _ = writerTx.Rollback(context.Background()) }()
	if _, err := writerTx.Exec(ctx, `
		INSERT INTO card_purchase_idempotency_records
		(user_id, operation, idempotency_key, request_fingerprint, state, created_at)
		VALUES ($1, 'CREATE_CARD_PURCHASE', 'down-before-lock', $2, 'PENDING', $3)
	`, syntheticUserID, make([]byte, 32), time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal("creating uncommitted migration 008 writer failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration 008 DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForBackendBlockedBy(t, ctx, pool, downPID, writerPID)
	if err := writerTx.Commit(ctx); err != nil {
		t.Fatal("committing migration 008 writer failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); !errors.Is(err, migrations.ErrRollback) {
		t.Fatalf("migration 008 DOWN error=%v, want rollback", err)
	}
	assertMigrationVersion(t, ctx, downConnection.Conn(), 8)
	assertTableExists(t, ctx, pool, "card_purchase_idempotency_records", true)
}

func TestMigration008DownWaitsForWriterRollbackThenSucceeds(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 8)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration 008 rollback writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	writerTx, err := writer.Begin(ctx)
	if err != nil {
		t.Fatal("beginning migration 008 rollback writer failed")
	}
	if _, err := writerTx.Exec(ctx, `
		INSERT INTO card_purchase_idempotency_records
		(user_id, operation, idempotency_key, request_fingerprint, state, created_at)
		VALUES ($1, 'CREATE_CARD_PURCHASE', 'down-rollback', $2, 'PENDING', $3)
	`, syntheticUserID, make([]byte, 32), time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal("creating rollback migration 008 writer failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration 008 rollback DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForBackendBlockedBy(t, ctx, pool, downPID, writerPID)
	if err := writerTx.Rollback(ctx); err != nil {
		t.Fatal("rolling back migration 008 writer failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); err != nil {
		t.Fatalf("migration 008 DOWN after writer rollback failed: %v", err)
	}
	assertMigrationVersion(t, ctx, downConnection.Conn(), 7)
	assertTableExists(t, ctx, pool, "card_purchase_idempotency_records", false)
}

func TestMigration008DownBlocksWriterQueuedAfterLockWithoutLoss(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 8)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration 008 blocker failed")
	}
	defer blocker.Release()
	blockerPID := connectionBackendPID(t, ctx, blocker)
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal("beginning migration 008 blocker failed")
	}
	defer func() { _ = blockerTx.Rollback(context.Background()) }()
	if _, err := blockerTx.Exec(ctx, "LOCK TABLE card_purchase_idempotency_records IN ACCESS SHARE MODE"); err != nil {
		t.Fatal("locking migration 008 blocker table failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring migration 008 queued DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForBackendBlockedBy(t, ctx, pool, downPID, blockerPID)

	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring post-lock migration 008 writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	writerResult := make(chan error, 1)
	go func() {
		_, writerErr := writer.Exec(ctx, `
			INSERT INTO card_purchase_idempotency_records
			(user_id, operation, idempotency_key, request_fingerprint, state, created_at)
			VALUES ($1, 'CREATE_CARD_PURCHASE', 'down-after-lock', $2, 'PENDING', $3)
		`, syntheticUserID, make([]byte, 32), time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC))
		writerResult <- writerErr
	}()
	waitForBackendBlockedBy(t, ctx, pool, writerPID, downPID)
	if err := blockerTx.Commit(ctx); err != nil {
		t.Fatal("releasing migration 008 blocker failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); err != nil {
		t.Fatalf("migration 008 DOWN after queued writer failed: %v", err)
	}
	if err := waitForMigrationResult(t, ctx, writerResult); err == nil {
		t.Fatal("queued migration 008 writer unexpectedly committed after DOWN")
	}
	assertMigrationVersion(t, ctx, downConnection.Conn(), 7)
	assertTableExists(t, ctx, pool, "card_purchase_idempotency_records", false)
}
