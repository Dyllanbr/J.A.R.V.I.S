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
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
	"jarvis/backend/internal/platform/postgres/migrations"
)

func TestMigration007FreshSchemaConstraintsSafeDownAndReapply(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_credit_card_migration_a"
	const ownerB = "usr_credit_card_migration_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 7)
	})
	for _, table := range []string{"credit_cards", "credit_card_audit_events", "credit_card_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, true)
	}
	assertColumnType(t, ctx, pool, "credit_cards", "credit_limit_minor", "bigint")
	assertColumnType(t, ctx, pool, "credit_cards", "created_at", "timestamp with time zone")
	assertColumnType(t, ctx, pool, "credit_cards", "archived_at", "timestamp with time zone")
	assertColumnType(t, ctx, pool, "credit_card_idempotency_records", "request_fingerprint", "bytea")
	assertCreditCardTimestampPrecision(t, ctx, pool, "credit_cards", "created_at", 6)
	assertCreditCardTimestampPrecision(t, ctx, pool, "credit_cards", "archived_at", 6)
	assertCreditCardTimestampPrecision(t, ctx, pool, "credit_card_audit_events", "created_at", 6)
	assertCreditCardTimestampPrecision(t, ctx, pool, "credit_card_idempotency_records", "created_at", 6)
	assertCreditCardTimestampHasNoDefault(t, ctx, pool, "credit_cards", "created_at")
	assertCreditCardTimestampHasNoDefault(t, ctx, pool, "credit_cards", "archived_at")
	assertCreditCardOwnerIndex(t, ctx, pool)

	createdAt := time.Date(2026, time.August, 25, 13, 0, 0, 123_456_000, time.UTC)
	validID := creditCardTestID('a')
	if err := insertRawCreditCard(
		ctx, pool, rawCreditCard{
			id: validID, userID: ownerA, name: "Cartão válido", lastFour: stringPointer("4242"),
			brand: stringPointer("VISA"), closingDay: 31, dueDay: 10,
			limitMinor: int64Pointer(250000), limitCurrency: stringPointer("BRL"),
			status: "ACTIVE", createdAt: createdAt,
		},
	); err != nil {
		t.Fatalf("valid credit card rejected: %v", err)
	}
	if err := insertRawCreditCard(
		ctx, pool, rawCreditCard{
			id: creditCardTestID('b'), userID: ownerA, name: "Cartão mínimo",
			closingDay: 1, dueDay: 31, status: "ACTIVE", createdAt: createdAt,
		},
	); err != nil {
		t.Fatalf("valid nullable card rejected: %v", err)
	}

	invalidCards := []struct {
		name, constraint string
		card             rawCreditCard
		code             string
	}{
		{name: "id", constraint: "credit_cards_id_valid", code: "23514", card: rawCreditCard{id: "card_NOT_HEX", userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: createdAt}},
		{name: "owner FK", constraint: "credit_cards_user_id_fkey", code: "23503", card: rawCreditCard{id: creditCardTestID('c'), userID: "usr_missing_credit_card", name: "Inválido", closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: createdAt}},
		{name: "blank name", constraint: "credit_cards_name_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('d'), userID: ownerA, name: " ", closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: createdAt}},
		{name: "long name", constraint: "credit_cards_name_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('d'), userID: ownerA, name: strings.Repeat("á", 201), closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: createdAt}},
		{name: "last four", constraint: "credit_cards_last_four_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('e'), userID: ownerA, name: "Inválido", lastFour: stringPointer("12AB"), closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: createdAt}},
		{name: "brand", constraint: "credit_cards_brand_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('f'), userID: ownerA, name: "Inválido", brand: stringPointer("DINERS"), closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: createdAt}},
		{name: "closing day", constraint: "credit_cards_closing_day_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('1'), userID: ownerA, name: "Inválido", closingDay: 0, dueDay: 1, status: "ACTIVE", createdAt: createdAt}},
		{name: "due day", constraint: "credit_cards_due_day_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('2'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 32, status: "ACTIVE", createdAt: createdAt}},
		{name: "limit without currency", constraint: "credit_cards_limit_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('3'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, limitMinor: int64Pointer(10), status: "ACTIVE", createdAt: createdAt}},
		{name: "currency without limit", constraint: "credit_cards_limit_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('4'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, limitCurrency: stringPointer("BRL"), status: "ACTIVE", createdAt: createdAt}},
		{name: "zero limit", constraint: "credit_cards_limit_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('5'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, limitMinor: int64Pointer(0), limitCurrency: stringPointer("BRL"), status: "ACTIVE", createdAt: createdAt}},
		{name: "foreign currency", constraint: "credit_cards_limit_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('6'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, limitMinor: int64Pointer(10), limitCurrency: stringPointer("USD"), status: "ACTIVE", createdAt: createdAt}},
		{name: "status", constraint: "credit_cards_status_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('7'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, status: "BLOCKED", createdAt: createdAt}},
		{name: "active archived", constraint: "credit_cards_lifecycle_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('8'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: createdAt, archivedAt: timePointer(createdAt.Add(time.Hour))}},
		{name: "archived missing time", constraint: "credit_cards_lifecycle_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('9'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, status: "ARCHIVED", createdAt: createdAt}},
		{name: "archive before create", constraint: "credit_cards_lifecycle_valid", code: "23514", card: rawCreditCard{id: creditCardTestID('0'), userID: ownerA, name: "Inválido", closingDay: 1, dueDay: 1, status: "ARCHIVED", createdAt: createdAt, archivedAt: timePointer(createdAt.Add(-time.Microsecond))}},
	}
	for _, test := range invalidCards {
		t.Run(test.name, func(t *testing.T) {
			assertCreditCardPostgresConstraint(t, insertRawCreditCard(ctx, pool, test.card), test.code, test.constraint)
		})
	}
	_, err := pool.Exec(ctx, `
		UPDATE credit_cards SET status = 'ARCHIVED' WHERE id = $1
	`, validID)
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_cards_lifecycle_valid")

	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_audit_events (user_id, credit_card_id, event_type, created_at)
		VALUES ($1, $2, 'CREDIT_CARD_CONFIRMED', $3)
	`, ownerA, validID, createdAt)
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_audit_events_type_valid")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_audit_events (user_id, credit_card_id, event_type, created_at)
		VALUES ($1, $2, 'CREDIT_CARD_CREATED', $3)
	`, ownerB, validID, createdAt)
	assertCreditCardPostgresConstraint(t, err, "23503", "credit_card_audit_events_owner_fkey")
	if _, err := pool.Exec(ctx, `
		INSERT INTO credit_card_audit_events (user_id, credit_card_id, event_type, created_at)
		VALUES ($1, $2, 'CREDIT_CARD_CREATED', $3)
	`, ownerA, validID, createdAt); err != nil {
		t.Fatalf("valid credit card audit rejected: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_audit_events (user_id, credit_card_id, event_type, created_at)
		VALUES ($1, $2, 'CREDIT_CARD_CREATED', $3)
	`, ownerA, validID, createdAt)
	assertCreditCardPostgresConstraint(t, err, "23505", "credit_card_audit_events_unique_event")

	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, 'CREATE_RECURRENCE', 'invalid-operation', $2, 'PENDING', $3)
	`, ownerA, make([]byte, 32), createdAt)
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_operation_valid")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, 'CREATE_CREDIT_CARD', 'invalid-fingerprint', $2, 'PENDING', $3)
	`, ownerA, make([]byte, 31), createdAt)
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_fingerprint_sha256")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, 'CREATE_CREDIT_CARD', 'invalid-state', $2, 'DONE', $3)
	`, ownerA, make([]byte, 32), createdAt)
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_state_valid")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, 'CREATE_CREDIT_CARD', E'invalid\nkey', $2, 'PENDING', $3)
	`, ownerA, make([]byte, 32), createdAt)
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_key_visible_ascii")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			credit_card_id, result_name, result_closing_day, result_due_day,
			result_status, result_created_at, result_archived_at, created_at, completed_at
		) VALUES (
			$1, 'CREATE_CREDIT_CARD', 'invalid-create-result', $2, 'COMPLETED',
			$3, 'Cartão válido', 31, 10, 'ARCHIVED', $4, $5, $4, $5
		)
	`, ownerA, make([]byte, 32), validID, createdAt, createdAt.Add(time.Hour))
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_result_valid")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			credit_card_id, result_name, result_closing_day, result_due_day,
			result_credit_limit_minor, result_status, result_created_at,
			created_at, completed_at
		) VALUES (
			$1, 'CREATE_CREDIT_CARD', 'invalid-limit-result', $2, 'COMPLETED',
			$3, 'Cartão válido', 31, 10, 250000, 'ACTIVE', $4, $4, $5
		)
	`, ownerA, make([]byte, 32), validID, createdAt, createdAt.Add(time.Hour))
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_result_valid")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			credit_card_id, result_name, result_closing_day, result_due_day,
			result_status, result_created_at, created_at, completed_at
		) VALUES (
			$1, 'ARCHIVE_CREDIT_CARD', 'invalid-archive-result', $2, 'COMPLETED',
			$3, 'Cartão válido', 31, 10, 'ACTIVE', $4, $4, $5
		)
	`, ownerA, make([]byte, 32), validID, createdAt, createdAt.Add(time.Hour))
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_result_valid")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			credit_card_id, result_name, result_closing_day, result_due_day,
			result_status, result_created_at, created_at, completed_at
		) VALUES (
			$1, 'CREATE_CREDIT_CARD', 'cross-owner-result', $2, 'COMPLETED',
			$3, 'Cartão válido', 31, 10, 'ACTIVE', $4, $4, $5
		)
	`, ownerB, make([]byte, 32), validID, createdAt, createdAt.Add(time.Hour))
	assertCreditCardPostgresConstraint(t, err, "23503", "credit_card_idem_owner_fkey")
	_, err = pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			credit_card_id, result_name, result_closing_day, result_due_day,
			result_status, result_created_at, created_at, completed_at
		) VALUES (
			$1, 'CREATE_CREDIT_CARD', 'invalid-time-order', $2, 'COMPLETED',
			$3, 'Cartão válido', 31, 10, 'ACTIVE', $4, $5, $4
		)
	`, ownerA, make([]byte, 32), validID, createdAt, createdAt.Add(time.Hour))
	assertCreditCardPostgresConstraint(t, err, "23514", "credit_card_idem_timestamps_ordered")
	if _, err := pool.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state,
			credit_card_id, result_name, result_last_four, result_brand,
			result_closing_day, result_due_day, result_credit_limit_minor,
			result_credit_limit_currency, result_status, result_created_at,
			created_at, completed_at
		) VALUES (
			$1, 'CREATE_CREDIT_CARD', 'valid-completed', $2, 'COMPLETED',
			$3, 'Cartão válido', '4242', 'VISA', 31, 10, 250000,
			'BRL', 'ACTIVE', $4, $4, $5
		)
	`, ownerA, make([]byte, 32), validID, createdAt, createdAt.Add(time.Hour)); err != nil {
		t.Fatalf("valid completed snapshot rejected: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM credit_card_idempotency_records"); err != nil {
		t.Fatal("clearing credit card idempotency migration fixtures failed")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM credit_card_audit_events"); err != nil {
		t.Fatal("clearing credit card audit migration fixtures failed")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM credit_cards"); err != nil {
		t.Fatal("clearing credit card migration fixtures failed")
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatalf("empty migration 007 DOWN failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 6)
	})
	for _, table := range []string{"credit_cards", "credit_card_audit_events", "credit_card_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, false)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 007 reapply failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 7)
	})

	repository := newCreditCardRepository(t, pool)
	createInput := fullCreditCardInput(ownerA, "reapplied-create-key")
	created, err := newRecordCreditCardUseCase(
		t,
		repository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('f')},
		fixedCreditCardClock{now: createdAt},
	).Execute(ctx, createInput)
	if err != nil || created.Replayed {
		t.Fatalf("create after migration 007 reapply=%+v error=%v", created, err)
	}
	replayedCreate, err := newRecordCreditCardUseCase(
		t,
		repository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('e')},
		fixedCreditCardClock{now: createdAt.Add(time.Hour)},
	).Execute(ctx, createInput)
	if err != nil || !replayedCreate.Replayed ||
		!creditCardsMatch(replayedCreate.CreditCard, created.CreditCard) {
		t.Fatalf("create replay after migration 007 reapply=%+v error=%v", replayedCreate, err)
	}
	archivedAt := createdAt.Add(2 * time.Hour)
	archiveInput := application.ArchiveCreditCardInput{
		UserID: ownerA, CreditCardID: created.CreditCard.ID(), IdempotencyKey: "reapplied-archive-key",
	}
	archived, err := newArchiveCreditCardUseCase(
		t,
		repository,
		fixedCreditCardClock{now: archivedAt},
	).Execute(ctx, archiveInput)
	if err != nil || archived.Replayed || archived.CreditCard.Status() != domain.CreditCardStatusArchived {
		t.Fatalf("archive after migration 007 reapply=%+v error=%v", archived, err)
	}
	replayedArchive, err := newArchiveCreditCardUseCase(
		t,
		repository,
		fixedCreditCardClock{now: archivedAt.Add(time.Hour)},
	).Execute(ctx, archiveInput)
	if err != nil || !replayedArchive.Replayed ||
		!creditCardsMatch(replayedArchive.CreditCard, archived.CreditCard) {
		t.Fatalf("archive replay after migration 007 reapply=%+v error=%v", replayedArchive, err)
	}
	if replayedCreate.CreditCard.Status() != domain.CreditCardStatusActive {
		t.Fatal("reapplied migration create replay lost its historical ACTIVE snapshot")
	}
	if _, hasArchivedAt := replayedCreate.CreditCard.ArchivedAt(); hasArchivedAt {
		t.Fatal("reapplied migration create replay acquired ArchivedAt")
	}
	replayedArchivedAt, ok := replayedArchive.CreditCard.ArchivedAt()
	if !ok || !replayedArchivedAt.Equal(archivedAt) {
		t.Fatal("reapplied migration archive replay changed ArchivedAt")
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertCreditCardOperationCount(t, ctx, pool, "CREDIT_CARD_CREATED", 1)
	assertCreditCardOperationCount(t, ctx, pool, "CREDIT_CARD_ARCHIVED", 1)
	assertCompletedCreditCardOperationCount(t, ctx, pool, "CREATE_CREDIT_CARD", 1)
	assertCompletedCreditCardOperationCount(t, ctx, pool, "ARCHIVE_CREDIT_CARD", 1)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)
}

func TestMigration007UpgradeFrom006PreservesLegacyDataWithoutBackfill(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigrationVersion(t, ctx, pool, 6)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	now := time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC)
	if err := insertRawFinancialTransaction(ctx, pool, "exp-before-007", syntheticUserID, "EXPENSE", "PIX", 9900, now); err != nil {
		t.Fatalf("version 006 Expense fixture failed: %v", err)
	}
	if err := insertRawRecurrence(ctx, pool, "rec-before-007", syntheticUserID, "2026-09-10", "ACTIVE", now, nil); err != nil {
		t.Fatalf("version 006 Recurrence fixture failed: %v", err)
	}
	if err := insertRawSuggestionSuppression(
		ctx,
		pool,
		syntheticUserID,
		recurrenceSuggestionTestID('a'),
		"DISMISS_RECURRENCE_SUGGESTION",
		bytesOf(0x42, 32),
		now,
	); err != nil {
		t.Fatalf("version 006 suppression fixture failed: %v", err)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatalf("migration 006 to 007 failed: %v", err)
		}
		assertMigrationVersion(t, ctx, connection, 7)
	})
	for table, want := range map[string]int{
		"transactions":                       1,
		"recurrences":                        1,
		"recurrence_suggestion_suppressions": 1,
		"credit_cards":                       0,
		"credit_card_audit_events":           0,
		"credit_card_idempotency_records":    0,
	} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != want {
			t.Fatalf("table %s count = %d error=%v, want %d", table, count, err, want)
		}
	}
	assertColumnExists(t, ctx, pool, "transactions", "credit_card_id", false)
}

func TestMigration007DownRefusesAnySubsystemData(t *testing.T) {
	tests := []struct {
		name  string
		setup func(testing.TB, context.Context, *pgxpool.Pool, string, time.Time)
	}{
		{
			name: "card",
			setup: func(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner string, now time.Time) {
				t.Helper()
				if err := insertRawCreditCard(ctx, pool, rawCreditCard{id: creditCardTestID('a'), userID: owner, name: "Guard", closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: now}); err != nil {
					t.Fatalf("card guard fixture failed: %v", err)
				}
			},
		},
		{
			name: "audit",
			setup: func(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner string, now time.Time) {
				t.Helper()
				id := creditCardTestID('b')
				if err := insertRawCreditCard(ctx, pool, rawCreditCard{id: id, userID: owner, name: "Guard", closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: now}); err != nil {
					t.Fatalf("audit card fixture failed: %v", err)
				}
				if _, err := pool.Exec(ctx, `INSERT INTO credit_card_audit_events (user_id, credit_card_id, event_type, created_at) VALUES ($1, $2, 'CREDIT_CARD_CREATED', $3)`, owner, id, now); err != nil {
					t.Fatalf("audit guard fixture failed: %v", err)
				}
			},
		},
		{
			name: "idempotency",
			setup: func(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner string, now time.Time) {
				t.Helper()
				if _, err := pool.Exec(ctx, `INSERT INTO credit_card_idempotency_records (user_id, operation, idempotency_key, request_fingerprint, state, created_at) VALUES ($1, 'CREATE_CREDIT_CARD', 'guard-key', $2, 'PENDING', $3)`, owner, make([]byte, 32), now); err != nil {
					t.Fatalf("idempotency guard fixture failed: %v", err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := newMigratedTestDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			owner := "usr_credit_card_down_guard"
			insertSyntheticUser(t, ctx, pool, owner)
			test.setup(t, ctx, pool, owner, creditCardCreatedAt())
			withConnection(t, ctx, pool, func(connection *pgx.Conn) {
				err := migrations.Down(ctx, connection)
				if !errors.Is(err, migrations.ErrRollback) {
					t.Fatalf("migration 007 DOWN with %s data error=%v, want rollback", test.name, err)
				}
				assertMigrationVersion(t, ctx, connection, 7)
			})
			assertTableExists(t, ctx, pool, "credit_cards", true)
		})
	}
}

func TestMigration007DownWaitsForWriterBeforeLockAndRefuses(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := "usr_credit_card_down_writer_before"
	insertSyntheticUser(t, ctx, pool, owner)
	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring credit card writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	tx, err := writer.Begin(ctx)
	if err != nil {
		t.Fatal("beginning credit card writer failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := insertRawCreditCard(ctx, tx, rawCreditCard{id: creditCardTestID('c'), userID: owner, name: "Writer", closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: creditCardCreatedAt()}); err != nil {
		t.Fatalf("uncommitted credit card writer failed: %v", err)
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
		t.Fatal("committing credit card writer failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); !errors.Is(err, migrations.ErrRollback) {
		t.Fatalf("concurrent migration 007 DOWN error=%v, want rollback", err)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 7)
	})
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM credit_cards").Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed card count=%d error=%v, want 1", count, err)
	}
}

func TestMigration007DownWaitsForWriterRollbackThenSucceeds(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := "usr_credit_card_down_writer_rollback"
	insertSyntheticUser(t, ctx, pool, owner)
	writer, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring rollback credit card writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	tx, err := writer.Begin(ctx)
	if err != nil {
		t.Fatal("beginning rollback credit card writer failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := insertRawCreditCard(ctx, tx, rawCreditCard{
		id: creditCardTestID('d'), userID: owner, name: "Writer rollback",
		closingDay: 1, dueDay: 1, status: "ACTIVE", createdAt: creditCardCreatedAt(),
	}); err != nil {
		t.Fatalf("uncommitted rollback credit card writer failed: %v", err)
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring rollback migration DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() { downResult <- migrations.Down(ctx, downConnection.Conn()) }()
	waitForBackendBlockedBy(t, ctx, pool, downPID, writerPID)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("rolling back credit card writer failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); err != nil {
		t.Fatalf("migration 007 DOWN after writer rollback failed: %v", err)
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 6)
	})
	for _, table := range []string{"credit_cards", "credit_card_audit_events", "credit_card_idempotency_records"} {
		assertTableExists(t, ctx, pool, table, false)
	}
	assertLegacyTablesUnaffectedAfterCreditCardDown(t, ctx, pool)
}

func TestMigration007DownBlocksWriterQueuedAfterLockWithoutSilentLoss(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := "usr_credit_card_down_writer_after"
	insertSyntheticUser(t, ctx, pool, owner)
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring credit card DOWN blocker failed")
	}
	defer blocker.Release()
	blockerPID := connectionBackendPID(t, ctx, blocker)
	blockerTx, err := blocker.Begin(ctx)
	if err != nil {
		t.Fatal("beginning credit card DOWN blocker failed")
	}
	defer func() { _ = blockerTx.Rollback(context.Background()) }()
	if _, err := blockerTx.Exec(ctx, "LOCK TABLE credit_card_idempotency_records IN ACCESS SHARE MODE"); err != nil {
		t.Fatal("locking credit card idempotency table failed")
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
		t.Fatal("acquiring post-lock credit card writer failed")
	}
	defer writer.Release()
	writerPID := connectionBackendPID(t, ctx, writer)
	writerResult := make(chan error, 1)
	go func() {
		_, err := writer.Exec(ctx, `
			INSERT INTO credit_card_idempotency_records (
				user_id, operation, idempotency_key, request_fingerprint, state, created_at
			) VALUES ($1, 'CREATE_CREDIT_CARD', 'queued-writer', $2, 'PENDING', $3)
		`, owner, make([]byte, 32), creditCardCreatedAt())
		writerResult <- err
	}()
	waitForBackendBlockedBy(t, ctx, pool, writerPID, downPID)
	if err := blockerTx.Commit(ctx); err != nil {
		t.Fatal("releasing credit card DOWN blocker failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); err != nil {
		t.Fatalf("empty migration 007 DOWN failed: %v", err)
	}
	if err := waitForMigrationResult(t, ctx, writerResult); err == nil {
		t.Fatal("credit card writer crossed completed migration 007 DOWN")
	}
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 6)
	})
	assertTableExists(t, ctx, pool, "credit_card_idempotency_records", false)
}

type creditCardRawExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type rawCreditCard struct {
	id, userID, name string
	lastFour         *string
	brand            *string
	closingDay       int
	dueDay           int
	limitMinor       *int64
	limitCurrency    *string
	status           string
	createdAt        time.Time
	archivedAt       *time.Time
}

func insertRawCreditCard(ctx context.Context, executor creditCardRawExecer, card rawCreditCard) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO credit_cards (
			id, user_id, name, last_four, brand, closing_day, due_day,
			credit_limit_minor, credit_limit_currency, status, created_at, archived_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		card.id,
		card.userID,
		card.name,
		card.lastFour,
		card.brand,
		card.closingDay,
		card.dueDay,
		card.limitMinor,
		card.limitCurrency,
		card.status,
		card.createdAt,
		card.archivedAt,
	)
	return err
}

func assertCreditCardPostgresConstraint(t testing.TB, err error, code, constraint string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("constraint %s error=%v, want *pgconn.PgError", constraint, err)
	}
	if postgresError.Code != code || postgresError.ConstraintName != constraint {
		t.Fatalf(
			"constraint failure=%q/%q, want %q/%q",
			postgresError.Code,
			postgresError.ConstraintName,
			code,
			constraint,
		)
	}
}

func assertCreditCardTimestampPrecision(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	table, column string,
	want int,
) {
	t.Helper()
	var precision int
	if err := pool.QueryRow(ctx, `
		SELECT datetime_precision
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&precision); err != nil || precision != want {
		t.Fatalf("%s.%s precision=%d error=%v, want %d", table, column, precision, err, want)
	}
}

func assertCreditCardTimestampHasNoDefault(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	table, column string,
) {
	t.Helper()
	var hasDefault bool
	if err := pool.QueryRow(ctx, `
		SELECT column_default IS NOT NULL
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
	`, table, column).Scan(&hasDefault); err != nil || hasDefault {
		t.Fatalf("%s.%s has default=%t error=%v, want false", table, column, hasDefault, err)
	}
}

func assertCreditCardOwnerIndex(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var definition string
	if err := pool.QueryRow(ctx, `
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND tablename = 'credit_cards'
		  AND indexname = 'credit_cards_user_id_idx'
	`).Scan(&definition); err != nil || !strings.Contains(definition, "(user_id)") {
		t.Fatalf("credit card owner index definition=%q error=%v", definition, err)
	}
}

func assertCreditCardOperationCount(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	event string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM credit_card_audit_events WHERE event_type = $1
	`, event).Scan(&count); err != nil || count != want {
		t.Fatalf("credit card audit %s count=%d error=%v, want %d", event, count, err, want)
	}
}

func assertCompletedCreditCardOperationCount(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	operation string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM credit_card_idempotency_records
		WHERE operation = $1 AND state = 'COMPLETED'
	`, operation).Scan(&count); err != nil || count != want {
		t.Fatalf("completed credit card operation %s count=%d error=%v, want %d", operation, count, err, want)
	}
}

func assertLegacyTablesUnaffectedAfterCreditCardDown(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	for _, table := range []string{
		"transactions",
		"audit_events",
		"idempotency_records",
		"recurrences",
		"recurrence_audit_events",
		"recurrence_idempotency_records",
		"recurrence_suggestion_suppressions",
	} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("legacy table %s count=%d error=%v after credit card DOWN, want zero", table, count, err)
		}
	}
	var categoryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM categories").Scan(&categoryCount); err != nil || categoryCount != 17 {
		t.Fatalf("category count=%d error=%v after credit card DOWN, want 17", categoryCount, err)
	}
}

func stringPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64    { return &value }
func timePointer(value time.Time) *time.Time {
	return &value
}
