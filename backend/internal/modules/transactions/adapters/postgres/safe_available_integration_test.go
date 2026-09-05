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

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestSafeAvailablePostgresReadIsOwnerScopedAndReadOnly(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_safe_available_owner_a"
	const ownerB = "usr_safe_available_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	cardID := "card_00000000000000000000000000000a01"
	insertSafeAvailableCard(t, ctx, pool, cardID, ownerA)
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "inc_safe_available_opening", ownerID: ownerA, transactionType: "INCOME", description: "Saldo inicial", amountMinor: 70_000,
		occurredAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_opening", ownerID: ownerA, transactionType: "EXPENSE", description: "Despesa anterior", amountMinor: 10_000,
		paymentMethod: "PIX", occurredAt: time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_plan", ownerID: ownerA, transactionType: "EXPENSE", description: "Compra parcelada", amountMinor: 12_000,
		paymentMethod: "CREDIT", cardID: &cardID, statementDueOn: "2026-09-10", occurredAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "inc_safe_available_period", ownerID: ownerA, transactionType: "INCOME", description: "Receita do período", amountMinor: 5_000,
		occurredAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_period", ownerID: ownerA, transactionType: "EXPENSE", description: "Despesa do período", amountMinor: 2_000,
		paymentMethod: "DEBIT", occurredAt: time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailablePlan(t, ctx, pool, ownerA, cardID, "exp_safe_available_plan", "ipl_00000000000000000000000000000a01", 12_000, 3, "2026-09-10", 10, "ACTIVE", nil)
	insertSafeAvailableRecurrence(t, ctx, pool, ownerA, "rec_safe_available_001", 3_000, "2026-09-15", "ACTIVE", nil)

	repository, err := adapter.NewSafeAvailableRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewSafeAvailableRepository() error = %v", err)
	}
	useCase, err := application.NewCalculateSafeAvailable(repository)
	if err != nil {
		t.Fatalf("NewCalculateSafeAvailable() error = %v", err)
	}
	period := safeAvailableIntegrationPeriod(t, 2026, time.September, 1, 2026, time.September, 30)

	before := readSafeAvailableCounts(t, ctx, pool)
	result, err := useCase.Execute(ctx, application.CalculateSafeAvailableInput{OwnerID: ownerA, Period: period})
	if err != nil {
		t.Fatalf("owner A Execute() error = %v", err)
	}
	if result.Available.AvailableBalance().MinorUnits() != 48_000 || result.Available.TotalConfirmedIncome().MinorUnits() != 5_000 || result.Available.TotalConfirmedExpense().MinorUnits() != 2_000 || result.Available.TotalConfirmedCommitments().MinorUnits() != 7_000 || result.Available.FinalAmount().MinorUnits() != 44_000 {
		t.Fatalf("owner A result = balance %d income %d expense %d commitments %d final %d", result.Available.AvailableBalance().MinorUnits(), result.Available.TotalConfirmedIncome().MinorUnits(), result.Available.TotalConfirmedExpense().MinorUnits(), result.Available.TotalConfirmedCommitments().MinorUnits(), result.Available.FinalAmount().MinorUnits())
	}
	incomeLines, expenseLines, commitmentLines := countSafeAvailableBreakdown(result.Available.Breakdown())
	if incomeLines != 1 || expenseLines != 1 || commitmentLines != 2 {
		t.Fatalf("owner A entries = incomes %d expenses %d commitments %d", incomeLines, expenseLines, commitmentLines)
	}
	if len(result.Available.MissingData()) != 1 || result.Available.MissingData()[0] != domain.SafeAvailableMissingBudget {
		t.Fatalf("missing data = %#v, want explicit budget", result.Available.MissingData())
	}

	resultB, err := useCase.Execute(ctx, application.CalculateSafeAvailableInput{OwnerID: ownerB, Period: period})
	if err != nil {
		t.Fatalf("owner B Execute() error = %v", err)
	}
	if resultB.Available.FinalAmount().MinorUnits() != 0 || len(resultB.Available.Breakdown()) != 1 {
		t.Fatalf("owner B leaked data: %+v", resultB.Available)
	}
	after := readSafeAvailableCounts(t, ctx, pool)
	if before != after {
		t.Fatalf("read changed financial counts: before=%+v after=%+v", before, after)
	}
}

func TestSafeAvailablePostgresUsesEffectiveScheduleAndPreservesContext(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_safe_available_cancelled"
	insertSyntheticUser(t, ctx, pool, owner)
	cardID := "card_00000000000000000000000000000a02"
	insertSafeAvailableCard(t, ctx, pool, cardID, owner)
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_cancelled", ownerID: owner, transactionType: "EXPENSE", description: "Compra cancelada", amountMinor: 9_000,
		paymentMethod: "CREDIT", cardID: &cardID, statementDueOn: "2026-09-10", occurredAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailablePlan(t, ctx, pool, owner, cardID, "exp_safe_available_cancelled", "ipl_00000000000000000000000000000a02", 9_000, 3, "2026-09-10", 10, "CANCELLED", safeAvailableStringPointer("2026-09-15"))
	insertSafeAvailableRecurrence(t, ctx, pool, owner, "rec_safe_available_cancelled", 1_000, "2026-09-01", "CANCELLED", timeStringPointer(time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)))

	repository, err := adapter.NewSafeAvailableRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewSafeAvailableRepository() error = %v", err)
	}
	useCase, err := application.NewCalculateSafeAvailable(repository)
	if err != nil {
		t.Fatalf("NewCalculateSafeAvailable() error = %v", err)
	}
	period := safeAvailableIntegrationPeriod(t, 2026, time.September, 1, 2026, time.September, 30)
	result, err := useCase.Execute(ctx, application.CalculateSafeAvailableInput{OwnerID: owner, Period: period})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var commitments []domain.SafeAvailableBreakdownLine
	for _, line := range result.Available.Breakdown() {
		if line.Kind() == domain.SafeAvailableEntryKindCommitment {
			commitments = append(commitments, line)
		}
	}
	if len(commitments) != 1 || commitments[0].Sequence() != 1 || commitments[0].DueOn().String() != "2026-09-10" || commitments[0].Amount().MinorUnits() != 3_000 {
		t.Fatalf("effective commitments = %#v, want only first installment", commitments)
	}

	cancelledContext, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	if _, err := repository.Read(cancelledContext, application.SafeAvailableSnapshotQuery{OwnerID: owner, Period: period}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Read() error = %v, want context.Canceled", err)
	}
	deadlineContext, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if _, err := repository.Read(deadlineContext, application.SafeAvailableSnapshotQuery{OwnerID: owner, Period: period}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline Read() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestSafeAvailablePostgresSanitizesDatabaseFailure(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repository, err := adapter.NewSafeAvailableRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewSafeAvailableRepository() error = %v", err)
	}
	pool.Close()
	period := safeAvailableIntegrationPeriod(t, 2026, time.October, 1, 2026, time.October, 31)
	_, err = repository.Read(ctx, application.SafeAvailableSnapshotQuery{OwnerID: "usr_safe_available_failure", Period: period})
	if err == nil || strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "postgres") && strings.Contains(err.Error(), "DSN") {
		t.Fatalf("database failure was not safely represented: %v", err)
	}
}

type safeAvailableTransaction struct {
	id, ownerID, transactionType, description, paymentMethod string
	amountMinor                                              int64
	cardID                                                   *string
	statementDueOn                                           string
	occurredAt                                               time.Time
}

func insertSafeAvailableCard(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, id, ownerID string) {
	t.Helper()
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO credit_cards (id, user_id, name, last_four, brand, closing_day, due_day, credit_limit_minor, credit_limit_currency, status, created_at, archived_at)
		VALUES ($1, $2, 'Teste', '1234', 'VISA', 10, 15, 100000, 'BRL', 'ACTIVE', $3, NULL)
	`, id, ownerID, now); err != nil {
		t.Fatalf("insert card: %v", err)
	}
}

func insertSafeAvailableTransaction(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, transaction safeAvailableTransaction) {
	t.Helper()
	now := transaction.occurredAt.Add(time.Hour)
	var paymentMethod any = transaction.paymentMethod
	if transaction.transactionType == "INCOME" {
		paymentMethod = nil
	}
	var cardID any
	if transaction.cardID != nil {
		cardID = *transaction.cardID
	}
	var statementDueOn any
	if transaction.statementDueOn != "" {
		statementDueOn = transaction.statementDueOn
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO transactions (id, user_id, type, description, amount_minor, currency, payment_method, category_id, credit_card_id, statement_due_on, occurred_at, financial_timezone, origin, status, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'BRL', $6, NULL, $7, $8, $9, $10, 'IOS', 'RECORDED', 1, $11, $11)
	`, transaction.id, transaction.ownerID, transaction.transactionType, transaction.description, transaction.amountMinor, paymentMethod, cardID, statementDueOn, transaction.occurredAt, application.FinancialTimezone, now); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
}

func insertSafeAvailablePlan(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, ownerID, cardID, expenseID, planID string, totalMinor int64, count int, firstDue string, dueDay int, status string, cancelledOn *string) {
	t.Helper()
	createdAt := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO installment_plans (id, user_id, credit_card_id, expense_id, total_minor, total_currency, installment_count, first_due_on, due_day, status, created_at, cancelled_on)
		VALUES ($1, $2, $3, $4, $5, 'BRL', $6, $7, $8, $9, $10, $11)
	`, planID, ownerID, cardID, expenseID, totalMinor, count, firstDue, dueDay, status, createdAt, cancelledOn); err != nil {
		t.Fatalf("insert installment plan: %v", err)
	}
}

func insertSafeAvailableRecurrence(t *testing.T, ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, ownerID, id string, amountMinor int64, startsOn, status string, cancelledAt *time.Time) {
	t.Helper()
	createdAt := time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO recurrences (id, user_id, transaction_type, description, expected_amount_minor, currency, frequency, starts_on, status, created_at, cancelled_at)
		VALUES ($1, $2, 'EXPENSE', 'Recorrência teste', $3, 'BRL', 'MONTHLY', $4, $5, $6, $7)
	`, id, ownerID, amountMinor, startsOn, status, createdAt, cancelledAt); err != nil {
		t.Fatalf("insert recurrence: %v", err)
	}
}

type safeAvailableCounts struct {
	transactions, plans, recurrences, auditEvents, idempotency int
}

func readSafeAvailableCounts(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) safeAvailableCounts {
	t.Helper()
	var counts safeAvailableCounts
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions").Scan(&counts.transactions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM installment_plans").Scan(&counts.plans); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM recurrences").Scan(&counts.recurrences); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM audit_events").Scan(&counts.auditEvents); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM idempotency_records").Scan(&counts.idempotency); err != nil {
		t.Fatal(err)
	}
	return counts
}

func safeAvailableIntegrationPeriod(t *testing.T, startYear int, startMonth time.Month, startDay, endYear int, endMonth time.Month, endDay int) domain.SafeAvailablePeriod {
	t.Helper()
	start, err := domain.NewCivilDate(startYear, startMonth, startDay)
	if err != nil {
		t.Fatal(err)
	}
	end, err := domain.NewCivilDate(endYear, endMonth, endDay)
	if err != nil {
		t.Fatal(err)
	}
	period, err := domain.NewSafeAvailablePeriod(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return period
}

func countSafeAvailableBreakdown(lines []domain.SafeAvailableBreakdownLine) (incomes, expenses, commitments int) {
	for _, line := range lines {
		switch line.Kind() {
		case domain.SafeAvailableEntryKindIncome:
			incomes++
		case domain.SafeAvailableEntryKindExpense:
			expenses++
		case domain.SafeAvailableEntryKindCommitment:
			commitments++
		}
	}
	return incomes, expenses, commitments
}

func safeAvailableStringPointer(value string) *string { return &value }
func timeStringPointer(value time.Time) *time.Time    { return &value }
