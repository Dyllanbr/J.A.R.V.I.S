//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/domain"
	platformpostgres "jarvis/backend/internal/platform/postgres"
	"jarvis/backend/internal/platform/postgres/migrations"
)

const syntheticUserID = "usr_test_owner_001"

func TestMigrationsUpDownAndReapply(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration UP failed")
		}
		assertMigrationVersion(t, ctx, connection, 4)
	})
	assertTablesExist(t, ctx, pool, true)
	assertTableExists(t, ctx, pool, "idempotency_records", true)
	assertTableExists(t, ctx, pool, "categories", true)
	assertSchemaTypes(t, ctx, pool)
	assertAuditSchemaIsMinimal(t, ctx, pool)
	assertIdempotencySchemaIsMinimal(t, ctx, pool)
	assertSchemaConstraints(t, ctx, pool)
	assertMonthlyIndex(t, ctx, pool, true)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("reapplying migration UP failed")
		}
		assertMigrationVersion(t, ctx, connection, 4)
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 004 DOWN failed")
		}
		assertMigrationVersion(t, ctx, connection, 3)
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 003 DOWN failed")
		}
		assertMigrationVersion(t, ctx, connection, 2)
	})
	assertTablesExist(t, ctx, pool, true)
	assertTableExists(t, ctx, pool, "idempotency_records", true)
	assertMonthlyIndex(t, ctx, pool, true)
	assertMigration002SchemaRestored(t, ctx, pool)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 002 DOWN failed")
		}
		assertMigrationVersion(t, ctx, connection, 1)
	})
	assertTableExists(t, ctx, pool, "idempotency_records", false)
	assertMonthlyIndex(t, ctx, pool, false)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration UP after DOWN failed")
		}
		assertMigrationVersion(t, ctx, connection, 4)
	})
	assertTablesExist(t, ctx, pool, true)
	assertTableExists(t, ctx, pool, "idempotency_records", true)
	assertTableExists(t, ctx, pool, "categories", true)
	assertSchemaTypes(t, ctx, pool)
	assertAuditSchemaIsMinimal(t, ctx, pool)
	assertIdempotencySchemaIsMinimal(t, ctx, pool)
	assertSchemaConstraints(t, ctx, pool)
	assertMonthlyIndex(t, ctx, pool, true)
}

func TestExpenseRepositoryPersistsExpenseAndAuditAtomically(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	expense := validExpense(t, "exp_test_001", syntheticUserID)
	if err := repository.Save(ctx, expense); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var (
		id, userID, transactionType, description string
		currency, paymentMethod, timezone        string
		origin, status                           string
		amountMinor, version                     int64
		occurredAt, createdAt, updatedAt         time.Time
	)
	err := pool.QueryRow(ctx, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, occurred_at, financial_timezone, origin,
			status, version, created_at, updated_at
		FROM transactions
		WHERE id = $1
	`, expense.ID()).Scan(
		&id, &userID, &transactionType, &description, &amountMinor, &currency,
		&paymentMethod, &occurredAt, &timezone, &origin, &status, &version,
		&createdAt, &updatedAt,
	)
	if err != nil {
		t.Fatal("persisted expense lookup failed")
	}

	if id != expense.ID() || userID != expense.UserID() || transactionType != string(expense.Type()) {
		t.Fatalf("stored identity = %q/%q/%q, want expense identity", id, userID, transactionType)
	}
	if description != expense.Description() || amountMinor != expense.Amount().MinorUnits() || currency != string(domain.CurrencyBRL) {
		t.Fatalf("stored financial fields do not match the synthetic expense")
	}
	if paymentMethod != string(expense.PaymentMethod()) || timezone != expense.FinancialTimezone() {
		t.Fatalf("stored payment/timezone fields do not match the expense")
	}
	if origin != string(expense.Origin()) || status != string(expense.Status()) || version != int64(expense.Version()) {
		t.Fatalf("stored lifecycle fields do not match the expense")
	}
	assertSameInstant(t, occurredAt, expense.OccurredAt(), "occurred_at")
	assertSameInstant(t, createdAt, expense.CreatedAt(), "created_at")
	assertSameInstant(t, updatedAt, expense.UpdatedAt(), "updated_at")

	var aggregateType, aggregateID, eventType, auditUserID string
	var aggregateVersion int64
	var auditCreatedAt time.Time
	err = pool.QueryRow(ctx, `
		SELECT user_id, aggregate_type, aggregate_id, aggregate_version, event_type, created_at
		FROM audit_events
		WHERE aggregate_id = $1
	`, expense.ID()).Scan(
		&auditUserID, &aggregateType, &aggregateID, &aggregateVersion, &eventType, &auditCreatedAt,
	)
	if err != nil {
		t.Fatal("audit event lookup failed")
	}
	if auditUserID != expense.UserID() || aggregateType != "EXPENSE" || aggregateID != expense.ID() {
		t.Fatal("audit event aggregate identity does not match the expense")
	}
	if aggregateVersion != int64(expense.Version()) || eventType != "EXPENSE_RECORDED" {
		t.Fatal("audit event version/type does not match the expense")
	}
	assertSameInstant(t, auditCreatedAt, expense.CreatedAt(), "audit created_at")
}

func TestExpenseRepositoryRollsBackWhenExpenseInsertFails(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repository := newRepository(t, pool)
	expense := validExpense(t, "exp_test_missing_owner", "usr_test_missing")

	err := repository.Save(ctx, expense)
	if !errors.Is(err, adapter.ErrInsertExpense) {
		t.Fatalf("Save() error = %v, want expense insert category", err)
	}
	assertRowCounts(t, ctx, pool, 0, 0)
}

func TestExpenseRepositoryRollsBackWhenAuditInsertFails(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	_, err := pool.Exec(ctx, `
		ALTER TABLE audit_events
		ADD CONSTRAINT audit_events_synthetic_failure
		CHECK (aggregate_id <> 'exp_test_audit_failure')
	`)
	if err != nil {
		t.Fatal("failed to install the synthetic audit failure constraint")
	}

	repository := newRepository(t, pool)
	err = repository.Save(ctx, validExpense(t, "exp_test_audit_failure", syntheticUserID))
	if !errors.Is(err, adapter.ErrInsertAuditEvent) {
		t.Fatalf("Save() error = %v, want audit insert category", err)
	}
	if strings.Contains(err.Error(), "exp_test_audit_failure") {
		t.Fatal("repository error exposed the rejected identifier")
	}
	assertRowCounts(t, ctx, pool, 0, 0)
}

func TestAuditEventOwnerMustMatchTransactionOwner(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const (
		ownerA = "usr_test_owner_a"
		ownerB = "usr_test_owner_b"
	)
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	matching := validRawTransaction("exp_test_owner_match", ownerA)
	if err := insertRawTransaction(ctx, pool, matching); err != nil {
		t.Fatal("inserting transaction for matching owner failed")
	}
	if err := insertRawAuditEvent(ctx, pool, ownerA, matching.id, matching.version); err != nil {
		t.Fatal("audit event with the transaction owner was rejected")
	}

	mismatched := validRawTransaction("exp_test_owner_mismatch", ownerA)
	if err := insertRawTransaction(ctx, pool, mismatched); err != nil {
		t.Fatal("inserting transaction for mismatched owner test failed")
	}
	if err := insertRawAuditEvent(ctx, pool, ownerB, mismatched.id, mismatched.version); err == nil {
		t.Fatal("audit event with a different owner was accepted")
	}

	assertRowCounts(t, ctx, pool, 2, 1)
}

func TestAuditOwnerMismatchRollsBackExpenseTransaction(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const (
		ownerA = "usr_test_rollback_owner_a"
		ownerB = "usr_test_rollback_owner_b"
	)
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("beginning owner mismatch transaction failed")
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()

	value := validRawTransaction("exp_test_owner_rollback", ownerA)
	if err := insertRawTransaction(ctx, transaction, value); err != nil {
		t.Fatal("inserting expense before owner mismatch failed")
	}
	if err := insertRawAuditEvent(ctx, transaction, ownerB, value.id, value.version); err == nil {
		t.Fatal("audit owner mismatch was accepted inside the transaction")
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatal("rolling back owner mismatch transaction failed")
	}

	assertRowCounts(t, ctx, pool, 0, 0)
}

func TestAuditEventUniqueConstraintRejectsDuplicateEvent(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	value := validRawTransaction("exp_test_duplicate_audit", syntheticUserID)
	if err := insertRawTransaction(ctx, pool, value); err != nil {
		t.Fatal("inserting transaction for audit uniqueness test failed")
	}
	if err := insertRawAuditEvent(ctx, pool, value.userID, value.id, value.version); err != nil {
		t.Fatal("inserting first audit event failed")
	}
	if err := insertRawAuditEvent(ctx, pool, value.userID, value.id, value.version); err == nil {
		t.Fatal("duplicate audit event was accepted")
	}

	assertRowCounts(t, ctx, pool, 1, 1)
}

func TestDatabaseConstraintsRejectInvalidTransactions(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	tests := []struct {
		name   string
		modify func(*rawTransaction)
	}{
		{name: "non-positive amount", modify: func(value *rawTransaction) { value.amountMinor = 0 }},
		{name: "unsupported currency", modify: func(value *rawTransaction) { value.currency = "USD" }},
		{name: "invalid payment method", modify: func(value *rawTransaction) { value.paymentMethod = "WIRE" }},
		{name: "missing user", modify: func(value *rawTransaction) { value.userID = "usr_test_missing" }},
		{name: "oversized id", modify: func(value *rawTransaction) { value.id = strings.Repeat("x", 129) }},
		{name: "blank description", modify: func(value *rawTransaction) { value.description = "   " }},
		{name: "oversized description", modify: func(value *rawTransaction) { value.description = strings.Repeat("á", 201) }},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validRawTransaction(fmt.Sprintf("exp_constraint_%d", index), syntheticUserID)
			test.modify(&value)
			if err := insertRawTransaction(ctx, pool, value); err == nil {
				t.Fatal("invalid transaction was accepted by PostgreSQL")
			}
		})
	}
}

func TestDatabaseIdentifierConstraintsMatchDomainBoundaries(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	validIDs := []struct {
		name  string
		value string
	}{
		{name: "exactly 128 ASCII bytes", value: strings.Repeat("x", 128)},
		{name: "multibyte within 128 bytes", value: "id_" + strings.Repeat("é", 60)},
	}
	invalidIDs := []struct {
		name  string
		value string
	}{
		{name: "129 bytes", value: strings.Repeat("x", 129)},
		{name: "leading ASCII space", value: " exp_test_space"},
		{name: "leading tab", value: "\texp_test_tab"},
		{name: "trailing newline", value: "exp_test_newline\n"},
		{name: "trailing carriage return", value: "exp_test_carriage\r"},
		{name: "leading NBSP", value: "\u00a0exp_test_nbsp"},
	}

	for _, subject := range []struct {
		name   string
		insert func(string) error
	}{
		{
			name: "user id",
			insert: func(value string) error {
				now := time.Date(2026, time.July, 10, 15, 0, 0, 0, time.UTC)
				_, err := pool.Exec(ctx, `
					INSERT INTO users (id, created_at, updated_at)
					VALUES ($1, $2, $3)
				`, value, now, now)
				return err
			},
		},
		{
			name: "expense id",
			insert: func(value string) error {
				transaction := validRawTransaction(value, syntheticUserID)
				return insertRawTransaction(ctx, pool, transaction)
			},
		},
	} {
		t.Run(subject.name, func(t *testing.T) {
			for _, test := range validIDs {
				t.Run("accepts "+test.name, func(t *testing.T) {
					if err := subject.insert(test.value); err != nil {
						t.Fatal("valid identifier was rejected")
					}
				})
			}
			for _, test := range invalidIDs {
				t.Run("rejects "+test.name, func(t *testing.T) {
					if err := subject.insert(test.value); err == nil {
						t.Fatal("invalid identifier was accepted")
					}
				})
			}
		})
	}
}

func TestDatabaseDescriptionWhitespaceAndCharacterLimits(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	tests := []struct {
		name        string
		description string
		wantError   bool
	}{
		{name: "empty", description: "", wantError: true},
		{name: "ASCII whitespace only", description: "   ", wantError: true},
		{name: "NBSP only", description: "\u00a0", wantError: true},
		{name: "200 Unicode characters", description: strings.Repeat("á", 200)},
		{name: "201 Unicode characters", description: strings.Repeat("á", 201), wantError: true},
		{name: "internal whitespace preserved", description: "Mercado\u00a0sintético"},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validRawTransaction(fmt.Sprintf("exp_description_%d", index), syntheticUserID)
			value.description = test.description
			err := insertRawTransaction(ctx, pool, value)
			if test.wantError {
				if err == nil {
					t.Fatal("invalid description was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal("valid description was rejected")
			}

			var stored string
			if err := pool.QueryRow(ctx, `
				SELECT description FROM transactions WHERE id = $1
			`, value.id).Scan(&stored); err != nil {
				t.Fatal("stored description lookup failed")
			}
			if stored != test.description {
				t.Fatal("PostgreSQL changed valid internal description content")
			}
		})
	}
}

func TestExpenseRepositoryRejectsDuplicateWithoutSecondAudit(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	expense := validExpense(t, "exp_test_duplicate", syntheticUserID)
	if err := repository.Save(ctx, expense); err != nil {
		t.Fatal("first Save() failed")
	}
	if err := repository.Save(ctx, expense); !errors.Is(err, adapter.ErrInsertExpense) {
		t.Fatalf("second Save() error = %v, want expense insert category", err)
	}
	assertRowCounts(t, ctx, pool, 1, 1)
}

func TestExpenseRepositoryHonorsCanceledContext(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	setupContext, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	insertSyntheticUser(t, setupContext, pool, syntheticUserID)
	cancelSetup()

	repository := newRepository(t, pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := repository.Save(ctx, validExpense(t, "exp_test_canceled", syntheticUserID))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Save() error = %v, want context cancellation", err)
	}

	assertContext, cancelAssert := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAssert()
	assertRowCounts(t, assertContext, pool, 0, 0)
}

type rawTransaction struct {
	id, userID, transactionType, description string
	amountMinor                              int64
	currency, paymentMethod                  string
	occurredAt                               time.Time
	timezone, origin, status                 string
	version                                  int64
	createdAt, updatedAt                     time.Time
}

type databaseExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func validRawTransaction(id, userID string) rawTransaction {
	createdAt := time.Date(2026, time.July, 10, 15, 30, 0, 0, time.UTC)
	return rawTransaction{
		id:              id,
		userID:          userID,
		transactionType: "EXPENSE",
		description:     "Mercado sintético",
		amountMinor:     12345,
		currency:        "BRL",
		paymentMethod:   "PIX",
		occurredAt:      createdAt.Add(-time.Hour),
		timezone:        "America/Sao_Paulo",
		origin:          "IOS",
		status:          "RECORDED",
		version:         1,
		createdAt:       createdAt,
		updatedAt:       createdAt,
	}
}

func insertRawTransaction(ctx context.Context, executor databaseExecer, value rawTransaction) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, type, description, amount_minor, currency, payment_method,
			occurred_at, financial_timezone, origin, status, version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, value.id, value.userID, value.transactionType, value.description, value.amountMinor,
		value.currency, value.paymentMethod, value.occurredAt, value.timezone, value.origin,
		value.status, value.version, value.createdAt, value.updatedAt)
	return err
}

func insertRawAuditEvent(
	ctx context.Context,
	executor databaseExecer,
	userID string,
	aggregateID string,
	aggregateVersion int64,
) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO audit_events (
			user_id, aggregate_type, aggregate_id, aggregate_version, event_type, created_at
		) VALUES ($1, 'EXPENSE', $2, $3, 'EXPENSE_RECORDED', $4)
	`, userID, aggregateID, aggregateVersion, time.Date(2026, time.July, 10, 15, 30, 0, 0, time.UTC))
	return err
}

func validExpense(t *testing.T, id, userID string) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(12345, domain.CurrencyBRL)
	if err != nil {
		t.Fatal("failed to create synthetic Money")
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID:            userID,
			Description:       "Mercado sintético",
			Amount:            amount,
			PaymentMethod:     domain.PaymentMethodPIX,
			OccurredAt:        time.Date(2026, time.July, 10, 12, 30, 0, 0, time.FixedZone("synthetic", -3*60*60)),
			FinancialTimezone: "America/Sao_Paulo",
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.July, 10, 15, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal("failed to create synthetic Expense")
	}
	return expense
}

func newRepository(t *testing.T, pool *pgxpool.Pool) *adapter.ExpenseRepository {
	t.Helper()
	repository, err := adapter.NewExpenseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewExpenseRepository() error = %v", err)
	}
	return repository
}

func newMigratedTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration UP failed")
		}
	})
	return pool
}

func newTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	sourceURL := os.Getenv("JARVIS_TEST_DATABASE_URL")
	if sourceURL == "" {
		t.Fatal("JARVIS_TEST_DATABASE_URL is required for integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	adminConfig, err := pgx.ParseConfig(sourceURL)
	if err != nil {
		t.Fatal("invalid synthetic integration database configuration")
	}
	adminConnection, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("synthetic integration database connection failed")
	}

	digest := sha256.Sum256([]byte(t.Name()))
	databaseName := fmt.Sprintf("jarvis_it_%x", digest[:8])
	identifier := pgx.Identifier{databaseName}.Sanitize()
	_, _ = adminConnection.Exec(ctx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
	if _, err := adminConnection.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		_ = adminConnection.Close(ctx)
		t.Fatal("creating disposable integration database failed")
	}

	databaseURL, err := url.Parse(sourceURL)
	if err != nil {
		_ = adminConnection.Close(ctx)
		t.Fatal("parsing disposable database URL failed")
	}
	databaseURL.Path = "/" + databaseName
	databaseURL.RawPath = ""
	pool, err := platformpostgres.Open(ctx, platformpostgres.Config{
		URL:              databaseURL.String(),
		MaxConnections:   4,
		MinConnections:   0,
		ConnectTimeout:   5 * time.Second,
		OperationTimeout: 5 * time.Second,
	})
	if err != nil {
		_, _ = adminConnection.Exec(ctx, "DROP DATABASE "+identifier+" WITH (FORCE)")
		_ = adminConnection.Close(ctx)
		t.Fatal("opening disposable integration database failed")
	}

	t.Cleanup(func() {
		pool.Close()
		cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelCleanup()
		_, _ = adminConnection.Exec(cleanupContext, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)")
		_ = adminConnection.Close(cleanupContext)
	})

	return pool
}

func insertSyntheticUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) {
	t.Helper()
	now := time.Date(2026, time.July, 10, 15, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, created_at, updated_at)
		VALUES ($1, $2, $3)
	`, userID, now, now); err != nil {
		t.Fatal("inserting synthetic user fixture failed")
	}
}

func withConnection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, operation func(*pgx.Conn)) {
	t.Helper()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring integration database connection failed")
	}
	defer connection.Release()
	operation(connection.Conn())
}

func assertMigrationVersion(t *testing.T, ctx context.Context, connection *pgx.Conn, expected int32) {
	t.Helper()
	version, err := migrations.Version(ctx, connection)
	if err != nil {
		t.Fatal("migration version lookup failed")
	}
	if version != expected {
		t.Fatalf("migration version = %d, want %d", version, expected)
	}
}

func assertTablesExist(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expected bool) {
	t.Helper()
	for _, table := range []string{"users", "transactions", "audit_events"} {
		assertTableExists(t, ctx, pool, table, expected)
	}
}

func assertTableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, expected bool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass('public.' || $1) IS NOT NULL
	`, table).Scan(&exists); err != nil {
		t.Fatal("schema lookup failed")
	}
	if exists != expected {
		t.Fatalf("table %s exists = %t, want %t", table, exists, expected)
	}
}

func assertSchemaTypes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tests := []struct {
		table, column, expected string
	}{
		{table: "transactions", column: "amount_minor", expected: "bigint"},
		{table: "transactions", column: "occurred_at", expected: "timestamp with time zone"},
		{table: "transactions", column: "created_at", expected: "timestamp with time zone"},
		{table: "transactions", column: "updated_at", expected: "timestamp with time zone"},
		{table: "audit_events", column: "id", expected: "bigint"},
		{table: "audit_events", column: "created_at", expected: "timestamp with time zone"},
		{table: "idempotency_records", column: "request_fingerprint", expected: "bytea"},
		{table: "idempotency_records", column: "created_at", expected: "timestamp with time zone"},
		{table: "idempotency_records", column: "completed_at", expected: "timestamp with time zone"},
	}

	for _, test := range tests {
		var actual string
		err := pool.QueryRow(ctx, `
			SELECT data_type
			FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		`, test.table, test.column).Scan(&actual)
		if err != nil {
			t.Fatal("schema column lookup failed")
		}
		if actual != test.expected {
			t.Fatalf("%s.%s type = %q, want %q", test.table, test.column, actual, test.expected)
		}
	}
}

func assertAuditSchemaIsMinimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'audit_events'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatal("audit schema lookup failed")
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal("audit schema scan failed")
		}
		columns = append(columns, column)
	}
	if rows.Err() != nil {
		t.Fatal("audit schema iteration failed")
	}

	expected := []string{"id", "user_id", "aggregate_type", "aggregate_id", "aggregate_version", "event_type", "created_at"}
	if fmt.Sprint(columns) != fmt.Sprint(expected) {
		t.Fatalf("audit columns = %v, want minimal schema %v", columns, expected)
	}
}

func assertIdempotencySchemaIsMinimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'idempotency_records'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatal("idempotency schema lookup failed")
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal("idempotency schema scan failed")
		}
		columns = append(columns, column)
	}
	expected := []string{
		"user_id",
		"operation",
		"idempotency_key",
		"request_fingerprint",
		"state",
		"transaction_id",
		"created_at",
		"completed_at",
		"transaction_type",
	}
	if fmt.Sprint(columns) != fmt.Sprint(expected) {
		t.Fatalf("idempotency columns = %v, want minimal schema %v", columns, expected)
	}

	var generated, expression string
	if err := pool.QueryRow(ctx, `
		SELECT is_generated, generation_expression
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'idempotency_records'
		  AND column_name = 'transaction_type'
	`).Scan(&generated, &expression); err != nil {
		t.Fatal("idempotency transaction_type generation lookup failed")
	}
	if generated != "ALWAYS" || !strings.Contains(expression, "CREATE_EXPENSE") ||
		!strings.Contains(expression, "CREATE_INCOME") {
		t.Fatal("idempotency transaction_type is not derived from operation")
	}
}

func assertMonthlyIndex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expected bool) {
	t.Helper()
	var definition *string
	if err := pool.QueryRow(ctx, `
		SELECT (
			SELECT indexdef
			FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename = 'transactions'
			  AND indexname = 'transactions_owner_month_idx'
		)
	`).Scan(&definition); err != nil {
		t.Fatal("monthly index lookup failed")
	}
	if (definition != nil) != expected {
		t.Fatalf("monthly index exists = %t, want %t", definition != nil, expected)
	}
	if definition != nil && !strings.Contains(*definition, "(user_id, occurred_at DESC, id DESC)") {
		t.Fatal("monthly index does not match the owner/time/order query")
	}
}

func assertMigration002SchemaRestored(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	var paymentMethodNullable string
	if err := pool.QueryRow(ctx, `
		SELECT is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'transactions'
		  AND column_name = 'payment_method'
	`).Scan(&paymentMethodNullable); err != nil {
		t.Fatal("payment_method nullability lookup failed")
	}
	if paymentMethodNullable != "NO" {
		t.Fatalf("migration 002 payment_method is_nullable = %q, want NO", paymentMethodNullable)
	}

	var transactionTypeColumnExists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'idempotency_records'
			  AND column_name = 'transaction_type'
		)
	`).Scan(&transactionTypeColumnExists); err != nil {
		t.Fatal("migration 002 idempotency schema lookup failed")
	}
	if transactionTypeColumnExists {
		t.Fatal("migration 003 generated column remained after DOWN")
	}

	for _, constraint := range []string{
		"transactions_type_expense",
		"transactions_payment_method_valid",
		"audit_events_aggregate_type_expense",
		"audit_events_event_type_recorded",
		"audit_events_transaction_owner_fkey",
		"idempotency_records_operation_create_expense",
		"idempotency_records_transaction_owner_fkey",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = $1
			)
		`, constraint).Scan(&exists); err != nil {
			t.Fatal("migration 002 constraint lookup failed")
		}
		if !exists {
			t.Fatalf("migration 002 constraint %s was not restored", constraint)
		}
	}
}

func assertSchemaConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	expectations := []struct {
		table, name, kind, definitionFragment string
	}{
		{table: "users", name: "users_pkey", kind: "p", definitionFragment: "PRIMARY KEY (id)"},
		{table: "transactions", name: "transactions_pkey", kind: "p", definitionFragment: "PRIMARY KEY (id)"},
		{table: "audit_events", name: "audit_events_pkey", kind: "p", definitionFragment: "PRIMARY KEY (id)"},
		{table: "transactions", name: "transactions_user_id_fkey", kind: "f", definitionFragment: "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT"},
		{table: "audit_events", name: "audit_events_user_id_fkey", kind: "f", definitionFragment: "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT"},
		{table: "audit_events", name: "audit_events_transaction_owner_type_fkey", kind: "f", definitionFragment: "FOREIGN KEY (aggregate_id, user_id, aggregate_type) REFERENCES transactions(id, user_id, type) ON DELETE RESTRICT"},
		{table: "transactions", name: "transactions_id_user_id_unique", kind: "u", definitionFragment: "UNIQUE (id, user_id)"},
		{table: "transactions", name: "transactions_id_user_id_type_unique", kind: "u", definitionFragment: "UNIQUE (id, user_id, type)"},
		{table: "audit_events", name: "audit_events_unique_version", kind: "u", definitionFragment: "UNIQUE (aggregate_type, aggregate_id, aggregate_version, event_type)"},
		{table: "idempotency_records", name: "idempotency_records_pkey", kind: "p", definitionFragment: "PRIMARY KEY (user_id, operation, idempotency_key)"},
		{table: "idempotency_records", name: "idempotency_records_user_id_fkey", kind: "f", definitionFragment: "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT"},
		{table: "idempotency_records", name: "idempotency_records_transaction_owner_type_fkey", kind: "f", definitionFragment: "FOREIGN KEY (transaction_id, user_id, transaction_type) REFERENCES transactions(id, user_id, type) ON DELETE RESTRICT"},
		{table: "idempotency_records", name: "idempotency_records_transaction_unique", kind: "u", definitionFragment: "UNIQUE (transaction_id)"},
	}

	criticalChecks := map[string][]string{
		"users": {
			"users_id_length",
			"users_id_trimmed",
			"users_id_no_controls",
			"users_timestamps_ordered",
		},
		"transactions": {
			"transactions_id_length",
			"transactions_id_trimmed",
			"transactions_id_no_controls",
			"transactions_user_id_length",
			"transactions_user_id_trimmed",
			"transactions_user_id_no_controls",
			"transactions_type_valid",
			"transactions_description_valid",
			"transactions_amount_positive",
			"transactions_currency_brl",
			"transactions_payment_method_by_type",
			"transactions_timezone_present",
			"transactions_origin_valid",
			"transactions_status_recorded",
			"transactions_version_positive",
			"transactions_timestamps_ordered",
		},
		"audit_events": {
			"audit_events_user_id_length",
			"audit_events_user_id_trimmed",
			"audit_events_user_id_no_controls",
			"audit_events_aggregate_type_valid",
			"audit_events_aggregate_id_length",
			"audit_events_aggregate_id_trimmed",
			"audit_events_aggregate_id_no_controls",
			"audit_events_aggregate_version_positive",
			"audit_events_event_matches_aggregate",
		},
		"idempotency_records": {
			"idempotency_records_operation_valid",
			"idempotency_records_key_length",
			"idempotency_records_key_visible_ascii",
			"idempotency_records_fingerprint_sha256",
			"idempotency_records_state_valid",
			"idempotency_records_completion_valid",
			"idempotency_records_timestamps_ordered",
		},
	}
	for table, names := range criticalChecks {
		for _, name := range names {
			expectations = append(expectations, struct {
				table, name, kind, definitionFragment string
			}{table: table, name: name, kind: "c"})
		}
	}

	for _, expectation := range expectations {
		var kind, definition string
		err := pool.QueryRow(ctx, `
			SELECT schema_constraint.contype::text, pg_get_constraintdef(schema_constraint.oid)
			FROM pg_constraint AS schema_constraint
			JOIN pg_class AS relation ON relation.oid = schema_constraint.conrelid
			JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
			WHERE namespace.nspname = 'public'
			  AND relation.relname = $1
			  AND schema_constraint.conname = $2
		`, expectation.table, expectation.name).Scan(&kind, &definition)
		if err != nil {
			t.Fatalf("constraint %s.%s lookup failed", expectation.table, expectation.name)
		}
		if kind != expectation.kind {
			t.Fatalf("constraint %s.%s kind = %q, want %q", expectation.table, expectation.name, kind, expectation.kind)
		}
		if expectation.definitionFragment != "" && !strings.Contains(definition, expectation.definitionFragment) {
			t.Fatalf("constraint %s.%s definition does not contain expected structure", expectation.table, expectation.name)
		}
	}
}

func assertRowCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, transactions, auditEvents int) {
	t.Helper()
	var transactionCount, auditCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions").Scan(&transactionCount); err != nil {
		t.Fatal("transaction count failed")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_events").Scan(&auditCount); err != nil {
		t.Fatal("audit event count failed")
	}
	if transactionCount != transactions || auditCount != auditEvents {
		t.Fatalf("row counts = %d/%d, want %d/%d", transactionCount, auditCount, transactions, auditEvents)
	}
}

func assertSameInstant(t *testing.T, actual, expected time.Time, field string) {
	t.Helper()
	if !actual.Equal(expected) {
		t.Fatalf("%s instant = %s, want %s", field, actual, expected)
	}
}
