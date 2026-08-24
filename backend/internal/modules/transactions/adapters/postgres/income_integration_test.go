//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
	"jarvis/backend/internal/platform/postgres/migrations"
)

func TestMigration003PreservesVersion2ExpenseData(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration UP failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 005 DOWN failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 004 DOWN failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 003 DOWN failed")
		}
		assertMigrationVersion(t, ctx, connection, 2)
	})

	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	legacy := validRawTransaction("exp_legacy_before_003", syntheticUserID)
	if err := insertRawTransaction(ctx, pool, legacy); err != nil {
		t.Fatal("inserting migration 002 Expense failed")
	}
	if err := insertRawAuditEvent(ctx, pool, legacy.userID, legacy.id, legacy.version); err != nil {
		t.Fatal("inserting migration 002 audit event failed")
	}
	fingerprint := make([]byte, 32)
	if _, err := pool.Exec(ctx, `
		INSERT INTO idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint,
			state, transaction_id, created_at, completed_at
		) VALUES ($1, 'CREATE_EXPENSE', 'legacy-key', $2, 'COMPLETED', $3, $4, $4)
	`, legacy.userID, fingerprint, legacy.id, legacy.createdAt); err != nil {
		t.Fatal("inserting migration 002 idempotency record failed")
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration 002 to current failed")
		}
		assertMigrationVersion(t, ctx, connection, 5)
	})

	var transactionType, paymentMethod, operationType string
	if err := pool.QueryRow(ctx, `
		SELECT type, payment_method FROM transactions WHERE id = $1
	`, legacy.id).Scan(&transactionType, &paymentMethod); err != nil {
		t.Fatal("legacy Expense lookup after migration 003 failed")
	}
	if err := pool.QueryRow(ctx, `
		SELECT transaction_type
		FROM idempotency_records
		WHERE user_id = $1 AND operation = 'CREATE_EXPENSE' AND idempotency_key = 'legacy-key'
	`, legacy.userID).Scan(&operationType); err != nil {
		t.Fatal("legacy idempotency operation type lookup failed")
	}
	if transactionType != "EXPENSE" || paymentMethod != "PIX" || operationType != "EXPENSE" {
		t.Fatal("migration 003 changed legacy Expense semantics")
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 005 DOWN with no recurrence data failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 004 DOWN with uncategorized data failed")
		}
		assertMigrationVersion(t, ctx, connection, 3)
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 003 DOWN with Expense-only data failed")
		}
		assertMigrationVersion(t, ctx, connection, 2)
	})
	assertMigration002SchemaRestored(t, ctx, pool)
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestMigration003DownFailsAtomicallyWhenIncomeExists(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	useCase := newRecordIncomeUseCase(t, repository, fixedIncomeIntegrationIDGenerator{id: "inc_down_guard"})
	if _, err := useCase.Execute(ctx, integrationIncomeInput(syntheticUserID, "income-down-guard")); err != nil {
		t.Fatalf("RecordIncome.Execute() error = %v", err)
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 005 DOWN with no recurrence data failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 004 DOWN with uncategorized Income failed")
		}
		assertMigrationVersion(t, ctx, connection, 3)
		err := migrations.Down(ctx, connection)
		if !errors.Is(err, migrations.ErrRollback) {
			t.Fatalf("migration 003 DOWN error = %v, want rollback category", err)
		}
		assertMigrationVersion(t, ctx, connection, 3)
	})
	assertSchemaConstraints(t, ctx, pool)
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestMigration003ConstraintMatrix(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const ownerB = "usr_income_constraint_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)

	now := time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)
	if err := insertRawFinancialTransaction(ctx, pool, "exp_constraint_valid", syntheticUserID, "EXPENSE", "PIX", 4250, now); err != nil {
		t.Fatal("valid Expense was rejected")
	}
	if err := insertRawFinancialTransaction(ctx, pool, "inc_constraint_valid", syntheticUserID, "INCOME", nil, 725000, now); err != nil {
		t.Fatal("valid Income was rejected")
	}
	if err := insertRawTypedAuditEvent(ctx, pool, syntheticUserID, "exp_constraint_valid", "EXPENSE", "EXPENSE_RECORDED", now); err != nil {
		t.Fatal("valid Expense audit event was rejected")
	}
	if err := insertRawTypedAuditEvent(ctx, pool, syntheticUserID, "inc_constraint_valid", "INCOME", "INCOME_RECORDED", now); err != nil {
		t.Fatal("valid Income audit event was rejected")
	}
	if err := insertPendingIdempotency(ctx, pool, syntheticUserID, "CREATE_EXPENSE", "shared-operation-key", now); err != nil {
		t.Fatal("CREATE_EXPENSE idempotency operation was rejected")
	}
	if err := insertPendingIdempotency(ctx, pool, syntheticUserID, "CREATE_INCOME", "shared-operation-key", now); err != nil {
		t.Fatal("CREATE_INCOME idempotency operation with the same key was rejected")
	}

	invalidTransactions := []struct {
		name, id, transactionType string
		paymentMethod             any
		amountMinor               int64
	}{
		{name: "Expense without payment method", id: "exp_constraint_no_payment", transactionType: "EXPENSE", amountMinor: 1},
		{name: "Income with payment method", id: "inc_constraint_payment", transactionType: "INCOME", paymentMethod: "PIX", amountMinor: 1},
		{name: "unknown type", id: "txn_constraint_unknown", transactionType: "TRANSFER", amountMinor: 1},
		{name: "zero amount", id: "inc_constraint_zero", transactionType: "INCOME", amountMinor: 0},
		{name: "negative amount", id: "inc_constraint_negative", transactionType: "INCOME", amountMinor: -1},
	}
	for _, test := range invalidTransactions {
		t.Run(test.name, func(t *testing.T) {
			if err := insertRawFinancialTransaction(
				ctx, pool, test.id, syntheticUserID, test.transactionType,
				test.paymentMethod, test.amountMinor, now,
			); err == nil {
				t.Fatal("invalid transaction was accepted")
			}
		})
	}

	for _, test := range []struct {
		name, transactionID, aggregateType, eventType string
	}{
		{name: "Income with Expense event", transactionID: "inc_constraint_valid", aggregateType: "INCOME", eventType: "EXPENSE_RECORDED"},
		{name: "Expense with Income event", transactionID: "exp_constraint_valid", aggregateType: "EXPENSE", eventType: "INCOME_RECORDED"},
		{name: "Income transaction with Expense aggregate", transactionID: "inc_constraint_valid", aggregateType: "EXPENSE", eventType: "EXPENSE_RECORDED"},
		{name: "Expense transaction with Income aggregate", transactionID: "exp_constraint_valid", aggregateType: "INCOME", eventType: "INCOME_RECORDED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := insertRawTypedAuditEvent(
				ctx, pool, syntheticUserID, test.transactionID,
				test.aggregateType, test.eventType, now,
			); err == nil {
				t.Fatal("invalid audit combination was accepted")
			}
		})
	}

	if err := insertPendingIdempotency(ctx, pool, syntheticUserID, "UNKNOWN", "unknown-operation", now); err == nil {
		t.Fatal("unknown idempotency operation was accepted")
	}
	if err := insertCompletedIdempotency(
		ctx, pool, syntheticUserID, "CREATE_EXPENSE", "cross-income", "inc_constraint_valid", now,
	); err == nil {
		t.Fatal("CREATE_EXPENSE was completed with an Income transaction")
	}
	if err := insertCompletedIdempotency(
		ctx, pool, syntheticUserID, "CREATE_INCOME", "cross-expense", "exp_constraint_valid", now,
	); err == nil {
		t.Fatal("CREATE_INCOME was completed with an Expense transaction")
	}
	if err := insertCompletedIdempotency(
		ctx, pool, ownerB, "CREATE_INCOME", "cross-owner", "inc_constraint_valid", now,
	); err == nil {
		t.Fatal("idempotency record was completed with another owner's transaction")
	}
	if err := insertRawTypedAuditEvent(
		ctx, pool, ownerB, "inc_constraint_valid", "INCOME", "INCOME_RECORDED", now,
	); err == nil {
		t.Fatal("Income audit event was linked by another owner")
	}
}

func TestIncomeCommandStoreFirstWriteReplayConflictAndRestart(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	clock := fixedFinancialClock{now: time.Date(2026, time.August, 14, 18, 0, 0, 987_654_321, time.UTC)}
	useCase := newRecordIncomeUseCaseWithClock(
		t, repository, &sequenceIncomeIDGenerator{prefix: "inc_record"}, clock,
	)
	input := integrationIncomeInput(syntheticUserID, "income-first-write")
	input.Income.OccurredAt = time.Date(2026, time.August, 14, 12, 0, 0, 123_456_789, time.FixedZone("minus-three", -3*60*60))

	created, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("first RecordIncome.Execute() error = %v", err)
	}
	if created.Replayed || created.Income.OccurredAt().Nanosecond() != 123_456_000 ||
		created.Income.CreatedAt().Nanosecond() != 987_654_000 {
		t.Fatal("created Income did not preserve canonical microsecond timestamps")
	}
	assertStoredIncomeMetadata(t, ctx, pool, created.Income, input.IdempotencyKey)
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)

	replayed, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("same-store replay error = %v", err)
	}
	assertSameIncome(t, replayed.Income, created.Income)
	if !replayed.Replayed {
		t.Fatal("same-store replay was not identified")
	}

	conflicting := input
	conflicting.Income.Description = "Outra receita sintética"
	if _, err := useCase.Execute(ctx, conflicting); !errors.Is(err, application.ErrIncomeIdempotencyConflict) {
		t.Fatalf("conflict error = %v, want ErrIncomeIdempotencyConflict", err)
	}
	assertStoredIncomeMetadata(t, ctx, pool, created.Income, input.IdempotencyKey)
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)

	poolB, err := pgxpool.New(ctx, pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal("opening independent restart pool failed")
	}
	defer poolB.Close()
	if err := poolB.Ping(ctx); err != nil {
		t.Fatal("independent restart pool readiness failed")
	}
	repositoryB := newRepository(t, poolB)
	useCaseB := newRecordIncomeUseCaseWithClock(
		t, repositoryB, fixedIncomeIntegrationIDGenerator{id: "inc_unused_after_restart"},
		fixedFinancialClock{now: time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)},
	)
	restarted, err := useCaseB.Execute(ctx, input)
	if err != nil {
		t.Fatalf("cross-restart replay error = %v", err)
	}
	if !restarted.Replayed {
		t.Fatal("cross-restart request was not identified as replay")
	}
	assertSameIncome(t, restarted.Income, created.Income)
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIncomeCommandStoreIsConcurrencySafeForSamePayload(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	useCase := newRecordIncomeUseCase(t, repository, &sequenceIncomeIDGenerator{prefix: "inc_concurrent_same"})
	input := integrationIncomeInput(syntheticUserID, "income-concurrent-same")

	const requests = 8
	start := make(chan struct{})
	results := make(chan application.RecordIncomeResult, requests)
	errorsChannel := make(chan error, requests)
	var wait sync.WaitGroup
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := useCase.Execute(ctx, input)
			results <- result
			errorsChannel <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsChannel)

	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent RecordIncome.Execute() error = %v", err)
		}
	}
	ids := map[string]struct{}{}
	newWrites := 0
	for result := range results {
		ids[result.Income.ID()] = struct{}{}
		if !result.Replayed {
			newWrites++
		}
	}
	if len(ids) != 1 || newWrites != 1 {
		t.Fatalf("unique Income IDs/new writes = %d/%d, want 1/1", len(ids), newWrites)
	}
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIncomeCommandStoreIsConcurrencySafeForDifferentPayloads(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	useCase := newRecordIncomeUseCase(t, repository, &sequenceIncomeIDGenerator{prefix: "inc_concurrent_different"})
	first := integrationIncomeInput(syntheticUserID, "income-concurrent-different")
	second := first
	second.Income.Description = "Receita sintética alternativa"

	start := make(chan struct{})
	errorsChannel := make(chan error, 2)
	var wait sync.WaitGroup
	for _, input := range []application.RecordIncomeInput{first, second} {
		input := input
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := useCase.Execute(ctx, input)
			errorsChannel <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsChannel)

	successes, conflicts := 0, 0
	for err := range errorsChannel {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, application.ErrIncomeIdempotencyConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success/conflict = %d/%d, want 1/1", successes, conflicts)
	}
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIncomeCommandStoreRollsBackAndAllowsRetry(t *testing.T) {
	tests := []struct {
		name       string
		incomeID   string
		key        string
		installSQL string
		dropSQL    string
		want       error
	}{
		{
			name:       "transaction insert failure",
			incomeID:   "inc_rollback_transaction",
			key:        "income-rollback-transaction",
			installSQL: "ALTER TABLE transactions ADD CONSTRAINT transactions_income_synthetic_failure CHECK (id <> 'inc_rollback_transaction')",
			dropSQL:    "ALTER TABLE transactions DROP CONSTRAINT transactions_income_synthetic_failure",
			want:       adapter.ErrInsertIncome,
		},
		{
			name:       "audit insert failure",
			incomeID:   "inc_rollback_audit",
			key:        "income-rollback-audit",
			installSQL: "ALTER TABLE audit_events ADD CONSTRAINT audit_income_synthetic_failure CHECK (aggregate_id <> 'inc_rollback_audit')",
			dropSQL:    "ALTER TABLE audit_events DROP CONSTRAINT audit_income_synthetic_failure",
			want:       adapter.ErrInsertIncomeAuditEvent,
		},
		{
			name:       "idempotency completion failure",
			incomeID:   "inc_rollback_completion",
			key:        "income-rollback-completion",
			installSQL: "ALTER TABLE idempotency_records ADD CONSTRAINT idempotency_income_synthetic_failure CHECK (idempotency_key <> 'income-rollback-completion' OR state <> 'COMPLETED')",
			dropSQL:    "ALTER TABLE idempotency_records DROP CONSTRAINT idempotency_income_synthetic_failure",
			want:       adapter.ErrCompleteIncomeIdempotency,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := newMigratedTestDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			insertSyntheticUser(t, ctx, pool, syntheticUserID)
			if _, err := pool.Exec(ctx, test.installSQL); err != nil {
				t.Fatal("installing synthetic failure failed")
			}

			repository := newRepository(t, pool)
			useCase := newRecordIncomeUseCase(t, repository, fixedIncomeIntegrationIDGenerator{id: test.incomeID})
			input := integrationIncomeInput(syntheticUserID, test.key)
			if _, err := useCase.Execute(ctx, input); !errors.Is(err, test.want) {
				t.Fatalf("RecordIncome.Execute() error = %v, want %v", err, test.want)
			} else if strings.Contains(err.Error(), test.incomeID) || strings.Contains(err.Error(), test.key) {
				t.Fatal("repository error exposed synthetic financial metadata")
			}
			assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)

			if _, err := pool.Exec(ctx, test.dropSQL); err != nil {
				t.Fatal("removing synthetic failure failed")
			}
			result, err := useCase.Execute(ctx, input)
			if err != nil {
				t.Fatalf("retry after rollback error = %v", err)
			}
			if result.Replayed {
				t.Fatal("retry after rollback was incorrectly classified as replay")
			}
			assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)
		})
	}
}

func TestIncomeCommandStoreDoesNotReportSuccessWhenCommitFails(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE synthetic_income_commit_guard (description TEXT PRIMARY KEY);
		ALTER TABLE transactions
		ADD CONSTRAINT transactions_income_synthetic_deferred_failure
		FOREIGN KEY (description) REFERENCES synthetic_income_commit_guard(description)
		DEFERRABLE INITIALLY DEFERRED;
	`); err != nil {
		t.Fatal("installing deferred Income commit failure failed")
	}

	repository := newRepository(t, pool)
	useCase := newRecordIncomeUseCase(t, repository, fixedIncomeIntegrationIDGenerator{id: "inc_commit_failure"})
	input := integrationIncomeInput(syntheticUserID, "income-commit-failure")
	if _, err := useCase.Execute(ctx, input); !errors.Is(err, adapter.ErrCommitIncomeTransaction) {
		t.Fatalf("RecordIncome.Execute() error = %v, want commit failure", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)

	if _, err := pool.Exec(ctx, `
		ALTER TABLE transactions DROP CONSTRAINT transactions_income_synthetic_deferred_failure;
		DROP TABLE synthetic_income_commit_guard;
	`); err != nil {
		t.Fatal("removing deferred Income commit failure failed")
	}
	if _, err := useCase.Execute(ctx, input); err != nil {
		t.Fatalf("retry after commit failure error = %v", err)
	}
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIncomeCommandStoreHonorsCanceledContext(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	setupContext, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	insertSyntheticUser(t, setupContext, pool, syntheticUserID)
	cancelSetup()

	repository := newRepository(t, pool)
	income := incomeAt(t, "inc_canceled", syntheticUserID, time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.RecordIncome(ctx, application.IdempotentIncomeCommand{
		Operation:      application.IdempotencyOperationCreateIncome,
		IdempotencyKey: "income-canceled",
		Income:         income,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("IncomeCommandStore.RecordIncome() error = %v, want context.Canceled", err)
	}

	assertContext, cancelAssert := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAssert()
	assertFinancialRowCounts(t, assertContext, pool, 0, 0, 0)
}

func TestIncomeOwnerIsolationOperationScopeAndMixedMonthlyReader(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const ownerB = "usr_income_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)

	repository := newRepository(t, pool)
	incomeUseCase := newRecordIncomeUseCase(t, repository, &sequenceIncomeIDGenerator{prefix: "inc_owner"})
	for _, owner := range []string{syntheticUserID, ownerB} {
		if _, err := incomeUseCase.Execute(ctx, integrationIncomeInput(owner, "shared-owner-operation-key")); err != nil {
			t.Fatalf("RecordIncome.Execute(%s) error = %v", owner, err)
		}
	}
	expenseUseCase := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: "exp_same_literal_key"})
	expenseInput := integrationRecordInput(syntheticUserID, "shared-owner-operation-key")
	if _, err := expenseUseCase.Execute(ctx, expenseInput); err != nil {
		t.Fatalf("RecordExpense with same literal key error = %v", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 3, 3, 3)

	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		t.Fatal("financial timezone unavailable")
	}
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, location).UTC()
	end := time.Date(2026, time.September, 1, 0, 0, 0, 0, location).UTC()
	transactions, err := repository.ListMonthlyTransactions(ctx, application.ExpenseMonthQuery{
		UserID: syntheticUserID, Start: start, End: end, FinancialTimezone: application.FinancialTimezone,
	})
	if err != nil {
		t.Fatalf("ListMonthlyTransactions() error = %v", err)
	}
	if len(transactions) != 2 {
		t.Fatalf("owner A mixed monthly count = %d, want 2", len(transactions))
	}
	for _, transaction := range transactions {
		if transaction.UserID != syntheticUserID {
			t.Fatal("mixed monthly reader leaked another owner")
		}
	}
}

func TestMixedMonthlyReaderUsesBoundariesTypeSpecificFieldsAndTotalOrdering(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	const ownerB = "usr_income_month_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)

	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		t.Fatal("financial timezone unavailable")
	}
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, location).UTC()
	end := time.Date(2026, time.September, 1, 0, 0, 0, 0, location).UTC()
	repository := newRepository(t, pool)

	incomeFixtures := []struct {
		id, owner, key string
		instant        time.Time
	}{
		{id: "inc_month_before", owner: syntheticUserID, key: "income-month-before", instant: start.Add(-time.Microsecond)},
		{id: "inc_month_start", owner: syntheticUserID, key: "income-month-start", instant: start},
		{id: "inc_month_end", owner: syntheticUserID, key: "income-month-end", instant: end.Add(-time.Microsecond)},
		{id: "inc_month_next", owner: syntheticUserID, key: "income-month-next", instant: end},
		{id: "inc_month_other_owner", owner: ownerB, key: "income-month-other", instant: start.Add(time.Hour)},
	}
	for _, fixture := range incomeFixtures {
		useCase := newRecordIncomeUseCase(t, repository, fixedIncomeIntegrationIDGenerator{id: fixture.id})
		input := integrationIncomeInput(fixture.owner, fixture.key)
		input.Income.OccurredAt = fixture.instant
		if _, err := useCase.Execute(ctx, input); err != nil {
			t.Fatalf("RecordIncome.Execute(%s) error = %v", fixture.id, err)
		}
	}
	if err := repository.Save(ctx, expenseAt(t, "exp_month_same", syntheticUserID, end.Add(-time.Microsecond))); err != nil {
		t.Fatalf("Save(exp_month_same) error = %v", err)
	}

	query := application.ExpenseMonthQuery{
		UserID: syntheticUserID, Start: start, End: end, FinancialTimezone: application.FinancialTimezone,
	}
	transactions, err := repository.ListMonthlyTransactions(ctx, query)
	if err != nil {
		t.Fatalf("ListMonthlyTransactions() error = %v", err)
	}
	ids := make([]string, 0, len(transactions))
	for _, transaction := range transactions {
		ids = append(ids, transaction.ID)
		switch transaction.Type {
		case domain.TransactionTypeExpense:
			if transaction.PaymentMethod == nil || *transaction.PaymentMethod != domain.PaymentMethodCash {
				t.Fatal("Expense projection lost payment method")
			}
		case domain.TransactionTypeIncome:
			if transaction.PaymentMethod != nil {
				t.Fatal("Income projection acquired a payment method")
			}
		default:
			t.Fatal("mixed monthly projection returned unknown type")
		}
	}
	expected := []string{"inc_month_end", "exp_month_same", "inc_month_start"}
	if fmt.Sprint(ids) != fmt.Sprint(expected) {
		t.Fatalf("mixed monthly IDs = %v, want %v", ids, expected)
	}

	expenses, err := repository.ListByFinancialMonth(ctx, query)
	if err != nil {
		t.Fatalf("Expense-only ListByFinancialMonth() error = %v", err)
	}
	if len(expenses) != 1 || expenses[0].ID() != "exp_month_same" {
		t.Fatal("Expense-only reader scanned an Income row")
	}
}

type fixedIncomeIntegrationIDGenerator struct{ id string }

func (generator fixedIncomeIntegrationIDGenerator) NewIncomeID() (string, error) {
	return generator.id, nil
}

type sequenceIncomeIDGenerator struct {
	prefix string
	next   atomic.Uint64
}

func (generator *sequenceIncomeIDGenerator) NewIncomeID() (string, error) {
	return fmt.Sprintf("%s_%03d", generator.prefix, generator.next.Add(1)), nil
}

func newRecordIncomeUseCase(
	t *testing.T,
	repository *adapter.ExpenseRepository,
	idGenerator application.IncomeIDGenerator,
) *application.RecordIncome {
	t.Helper()
	return newRecordIncomeUseCaseWithClock(t, repository, idGenerator, fixedIntegrationClock{})
}

func newRecordIncomeUseCaseWithClock(
	t *testing.T,
	repository *adapter.ExpenseRepository,
	idGenerator application.IncomeIDGenerator,
	clock application.Clock,
) *application.RecordIncome {
	t.Helper()
	useCase, err := application.NewRecordIncome(repository, idGenerator, clock)
	if err != nil {
		t.Fatalf("NewRecordIncome() error = %v", err)
	}
	return useCase
}

func integrationIncomeInput(userID, key string) application.RecordIncomeInput {
	return application.RecordIncomeInput{
		IdempotencyKey: key,
		Income: application.CreateIncomeInput{
			UserID:            userID,
			Description:       "Receita sintética",
			AmountMinor:       725000,
			Currency:          domain.CurrencyBRL,
			OccurredAt:        time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
	}
}

func incomeAt(t *testing.T, id, userID string, occurredAt time.Time) domain.Income {
	t.Helper()
	amount, err := domain.NewMoney(725000, domain.CurrencyBRL)
	if err != nil {
		t.Fatal("NewMoney() failed")
	}
	income, err := domain.NewIncome(domain.IncomeParams{
		ID: id,
		Details: domain.IncomeDetails{
			UserID:            userID,
			Description:       "Receita sintética",
			Amount:            amount,
			OccurredAt:        occurredAt,
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewIncome() error = %v", err)
	}
	return income
}

func insertRawFinancialTransaction(
	ctx context.Context,
	pool *pgxpool.Pool,
	id string,
	userID string,
	transactionType string,
	paymentMethod any,
	amountMinor int64,
	createdAt time.Time,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, type, description, amount_minor, currency, payment_method,
			occurred_at, financial_timezone, origin, status, version, created_at, updated_at
		) VALUES ($1, $2, $3, 'Receita sintética', $4, 'BRL', $5, $6,
			'America/Sao_Paulo', 'IOS', 'RECORDED', 1, $6, $6)
	`, id, userID, transactionType, amountMinor, paymentMethod, createdAt)
	return err
}

func insertRawTypedAuditEvent(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID string,
	transactionID string,
	aggregateType string,
	eventType string,
	createdAt time.Time,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO audit_events (
			user_id, aggregate_type, aggregate_id, aggregate_version, event_type, created_at
		) VALUES ($1, $2, $3, 1, $4, $5)
	`, userID, aggregateType, transactionID, eventType, createdAt)
	return err
}

func insertPendingIdempotency(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID string,
	operation string,
	key string,
	createdAt time.Time,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint,
			state, transaction_id, created_at, completed_at
		) VALUES ($1, $2, $3, $4, 'PENDING', NULL, $5, NULL)
	`, userID, operation, key, make([]byte, 32), createdAt)
	return err
}

func insertCompletedIdempotency(
	ctx context.Context,
	pool *pgxpool.Pool,
	userID string,
	operation string,
	key string,
	transactionID string,
	createdAt time.Time,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint,
			state, transaction_id, created_at, completed_at
		) VALUES ($1, $2, $3, $4, 'COMPLETED', $5, $6, $6)
	`, userID, operation, key, make([]byte, 32), transactionID, createdAt)
	return err
}

func assertStoredIncomeMetadata(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	income domain.Income,
	key string,
) {
	t.Helper()
	var (
		transactionType, description, currency string
		timezone, origin, status               string
		paymentMethod                          *string
		amountMinor, version                   int64
		occurredAt, createdAt, updatedAt       time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT
			type, description, amount_minor, currency, payment_method,
			occurred_at, financial_timezone, origin, status, version, created_at, updated_at
		FROM transactions
		WHERE id = $1 AND user_id = $2
	`, income.ID(), income.UserID()).Scan(
		&transactionType, &description, &amountMinor, &currency, &paymentMethod,
		&occurredAt, &timezone, &origin, &status, &version, &createdAt, &updatedAt,
	); err != nil {
		t.Fatal("stored Income lookup failed")
	}
	if transactionType != "INCOME" || paymentMethod != nil || description != income.Description() ||
		amountMinor != income.Amount().MinorUnits() || currency != "BRL" ||
		timezone != income.FinancialTimezone() || origin != string(income.Origin()) ||
		status != "RECORDED" || version != 1 {
		t.Fatal("stored Income fields do not match the aggregate")
	}
	assertSameInstant(t, occurredAt, income.OccurredAt(), "income occurred_at")
	assertSameInstant(t, createdAt, income.CreatedAt(), "income created_at")
	assertSameInstant(t, updatedAt, income.UpdatedAt(), "income updated_at")

	var auditType, eventType, operation, state, transactionID, operationType string
	if err := pool.QueryRow(ctx, `
		SELECT aggregate_type, event_type
		FROM audit_events
		WHERE aggregate_id = $1 AND user_id = $2
	`, income.ID(), income.UserID()).Scan(&auditType, &eventType); err != nil {
		t.Fatal("stored Income audit lookup failed")
	}
	if err := pool.QueryRow(ctx, `
		SELECT operation, state, transaction_id, transaction_type
		FROM idempotency_records
		WHERE user_id = $1 AND operation = 'CREATE_INCOME' AND idempotency_key = $2
	`, income.UserID(), key).Scan(&operation, &state, &transactionID, &operationType); err != nil {
		t.Fatal("stored Income idempotency lookup failed")
	}
	if auditType != "INCOME" || eventType != "INCOME_RECORDED" ||
		operation != "CREATE_INCOME" || state != "COMPLETED" ||
		transactionID != income.ID() || operationType != "INCOME" {
		t.Fatal("stored Income audit/idempotency metadata is inconsistent")
	}
}

func assertSameIncome(t *testing.T, actual, expected domain.Income) {
	t.Helper()
	actualCategoryID, actualHasCategory := actual.CategoryID()
	expectedCategoryID, expectedHasCategory := expected.CategoryID()
	if actual.ID() != expected.ID() || actual.UserID() != expected.UserID() ||
		actual.Type() != expected.Type() || actual.Description() != expected.Description() ||
		!actual.Amount().Equal(expected.Amount()) || actual.OccurredAt() != expected.OccurredAt() ||
		actual.FinancialTimezone() != expected.FinancialTimezone() || actual.Origin() != expected.Origin() ||
		actual.Status() != expected.Status() || actual.Version() != expected.Version() ||
		actual.CreatedAt() != expected.CreatedAt() || actual.UpdatedAt() != expected.UpdatedAt() ||
		actualHasCategory != expectedHasCategory || actualCategoryID != expectedCategoryID {
		t.Fatal("replayed Income differs from the original persisted aggregate")
	}
}

func assertIncomeFinancialRowCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	transactions int,
	auditEvents int,
	idempotencyRecords int,
) {
	t.Helper()
	queries := []string{
		"SELECT count(*) FROM transactions WHERE type = 'INCOME'",
		"SELECT count(*) FROM audit_events WHERE aggregate_type = 'INCOME' AND event_type = 'INCOME_RECORDED'",
		"SELECT count(*) FROM idempotency_records WHERE operation = 'CREATE_INCOME' AND state = 'COMPLETED'",
	}
	wanted := []int{transactions, auditEvents, idempotencyRecords}
	actual := make([]int, len(queries))
	for index, query := range queries {
		if err := pool.QueryRow(ctx, query).Scan(&actual[index]); err != nil {
			t.Fatal("Income financial row count failed")
		}
	}
	if fmt.Sprint(actual) != fmt.Sprint(wanted) {
		t.Fatalf("Income financial row counts = %v, want %v", actual, wanted)
	}
}
