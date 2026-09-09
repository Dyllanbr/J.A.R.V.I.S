//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestMonthlyBudgetPostgresMigrationAndOwnerScopedReplacement(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 11)
	})
	assertTableExists(t, ctx, pool, "monthly_budgets", true)

	const ownerA = "usr_monthly_budget_owner_a"
	const ownerB = "usr_monthly_budget_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	repository := newMonthlyBudgetRepository(t, pool)
	store, err := application.NewSetMonthlyBudget(repository)
	if err != nil {
		t.Fatalf("NewSetMonthlyBudget() error = %v", err)
	}
	month := mustMonthlyBudgetMonth(t, "2026-09")
	zero := mustMonthlyBudgetMoney(t, 0)
	first := mustMonthlyBudgetMoney(t, 12500)
	replacement := mustMonthlyBudgetMoney(t, 24000)

	if _, found, err := repository.ReadMonthlyBudget(ctx, ownerA, month); err != nil || found {
		t.Fatalf("missing budget read = found %t error %v, want empty result", found, err)
	}
	if _, err := store.Execute(ctx, application.SetMonthlyBudgetInput{OwnerID: ownerA, Month: month, Amount: first}); err != nil {
		t.Fatalf("creating monthly budget failed: %v", err)
	}
	read, found, err := repository.ReadMonthlyBudget(ctx, ownerA, month)
	if err != nil || !found || read.Amount().MinorUnits() != first.MinorUnits() || read.OwnerID() != ownerA || read.Month().String() != month.String() {
		t.Fatalf("created monthly budget = %+v found=%t error=%v", read, found, err)
	}
	if _, err := store.Execute(ctx, application.SetMonthlyBudgetInput{OwnerID: ownerA, Month: month, Amount: replacement}); err != nil {
		t.Fatalf("replacing monthly budget failed: %v", err)
	}
	read, found, err = repository.ReadMonthlyBudget(ctx, ownerA, month)
	if err != nil || !found || read.Amount().MinorUnits() != replacement.MinorUnits() {
		t.Fatalf("replaced monthly budget = %+v found=%t error=%v", read, found, err)
	}
	if _, err := store.Execute(ctx, application.SetMonthlyBudgetInput{OwnerID: ownerA, Month: month, Amount: zero}); err != nil {
		t.Fatalf("zero replacement failed: %v", err)
	}
	read, found, err = repository.ReadMonthlyBudget(ctx, ownerA, month)
	if err != nil || !found || read.Amount().MinorUnits() != 0 {
		t.Fatalf("zero monthly budget = %+v found=%t error=%v", read, found, err)
	}
	if _, found, err := repository.ReadMonthlyBudget(ctx, ownerB, month); err != nil || found {
		t.Fatalf("owner B read = found %t error %v, want empty result", found, err)
	}
	if _, err := store.Execute(ctx, application.SetMonthlyBudgetInput{OwnerID: ownerB, Month: month, Amount: mustMonthlyBudgetMoney(t, 777)}); err != nil {
		t.Fatalf("owner B budget failed: %v", err)
	}
	readA, foundA, err := repository.ReadMonthlyBudget(ctx, ownerA, month)
	if err != nil || !foundA || readA.Amount().MinorUnits() != 0 {
		t.Fatalf("owner A changed by owner B replacement = %+v found=%t error=%v", readA, foundA, err)
	}
	readB, foundB, err := repository.ReadMonthlyBudget(ctx, ownerB, month)
	if err != nil || !foundB || readB.Amount().MinorUnits() != 777 {
		t.Fatalf("owner B budget = %+v found=%t error=%v", readB, foundB, err)
	}
}

func TestMonthlyBudgetPostgresReplacementIsAtomicAndDoesNotTouchFinancialTables(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_monthly_budget_atomic_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	repository := newMonthlyBudgetRepository(t, pool)
	store, err := application.NewSetMonthlyBudget(repository)
	if err != nil {
		t.Fatalf("NewSetMonthlyBudget() error = %v", err)
	}
	month := mustMonthlyBudgetMonth(t, "2026-10")
	values := []int64{100, 200, 300, 400, 500, 600, 700, 800}
	moneyValues := make([]domain.Money, len(values))
	for index, value := range values {
		moneyValues[index] = mustMonthlyBudgetMoney(t, value)
	}
	before := monthlyBudgetFinancialCounts(t, ctx, pool)

	var wait sync.WaitGroup
	errorsCh := make(chan error, len(values))
	for _, money := range moneyValues {
		money := money
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, executeErr := store.Execute(ctx, application.SetMonthlyBudgetInput{OwnerID: owner, Month: month, Amount: money})
			errorsCh <- executeErr
		}()
	}
	wait.Wait()
	close(errorsCh)
	for executeErr := range errorsCh {
		if executeErr != nil {
			t.Fatalf("concurrent replacement error = %v", executeErr)
		}
	}

	read, found, err := repository.ReadMonthlyBudget(ctx, owner, month)
	if err != nil || !found {
		t.Fatalf("atomic replacement read = found %t error %v", found, err)
	}
	validValue := false
	for _, value := range values {
		if read.Amount().MinorUnits() == value {
			validValue = true
			break
		}
	}
	if !validValue {
		t.Fatalf("final amount = %d, want one concurrent replacement value", read.Amount().MinorUnits())
	}
	var rowCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM monthly_budgets WHERE user_id = $1 AND month = $2`, owner, monthlyBudgetStart(t, month)).Scan(&rowCount); err != nil {
		t.Fatalf("monthly budget row count failed: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("monthly budget row count = %d, want 1", rowCount)
	}
	after := monthlyBudgetFinancialCounts(t, ctx, pool)
	if before != after {
		t.Fatalf("financial tables changed: before=%+v after=%+v", before, after)
	}
}

func TestMonthlyBudgetPostgresPreservesContextAndSanitizesDatabaseErrors(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const owner = "usr_monthly_budget_context_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	repository := newMonthlyBudgetRepository(t, pool)
	month := mustMonthlyBudgetMonth(t, "2026-11")
	budget := mustMonthlyBudget(t, owner, month, 900)
	if err := repository.ReplaceMonthlyBudget(ctx, domain.MonthlyBudget{}); !errors.Is(err, adapter.ErrInvalidMonthlyBudgetInput) {
		t.Fatalf("invalid budget replacement error = %v, want ErrInvalidMonthlyBudgetInput", err)
	}
	if _, _, err := repository.ReadMonthlyBudget(ctx, "", month); !errors.Is(err, adapter.ErrInvalidMonthlyBudgetInput) {
		t.Fatalf("invalid owner read error = %v, want ErrInvalidMonthlyBudgetInput", err)
	}
	if err := repository.ReplaceMonthlyBudget(ctx, mustMonthlyBudget(t, "usr_missing_monthly_budget_owner", month, 900)); err == nil {
		t.Fatal("replacement for nonexistent owner unexpectedly succeeded")
	}

	cancelled, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	if err := repository.ReplaceMonthlyBudget(cancelled, budget); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replacement error = %v, want context.Canceled", err)
	}
	if _, _, err := repository.ReadMonthlyBudget(cancelled, owner, month); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v, want context.Canceled", err)
	}
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if err := repository.ReplaceMonthlyBudget(deadline, budget); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline replacement error = %v, want context.DeadlineExceeded", err)
	}
	if _, _, err := repository.ReadMonthlyBudget(deadline, owner, month); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline read error = %v, want context.DeadlineExceeded", err)
	}

	payload := "postgres://jarvis:jarvis@localhost:55432/jarvis_test"
	pool.Close()
	err := repository.ReplaceMonthlyBudget(context.Background(), budget)
	if err == nil || strings.Contains(err.Error(), payload) || strings.Contains(strings.ToLower(err.Error()), "insert into") {
		t.Fatalf("database replacement error was not sanitized: %v", err)
	}
	_, _, err = repository.ReadMonthlyBudget(context.Background(), owner, month)
	if err == nil || strings.Contains(err.Error(), payload) || strings.Contains(strings.ToLower(err.Error()), "select") {
		t.Fatalf("database read error was not sanitized: %v", err)
	}
}

func newMonthlyBudgetRepository(t *testing.T, pool *pgxpool.Pool) *adapter.MonthlyBudgetRepository {
	t.Helper()
	repository, err := adapter.NewMonthlyBudgetRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewMonthlyBudgetRepository() error = %v", err)
	}
	return repository
}

func mustMonthlyBudget(t *testing.T, owner string, month domain.CivilMonth, amount int64) domain.MonthlyBudget {
	t.Helper()
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{OwnerID: owner, Month: month, Amount: mustMonthlyBudgetMoney(t, amount)})
	if err != nil {
		t.Fatalf("NewMonthlyBudget() error = %v", err)
	}
	return budget
}

func mustMonthlyBudgetMonth(t *testing.T, value string) domain.CivilMonth {
	t.Helper()
	month, err := domain.ParseCivilMonth(value)
	if err != nil {
		t.Fatalf("ParseCivilMonth(%q) error = %v", value, err)
	}
	return month
}

func mustMonthlyBudgetMoney(t *testing.T, minor int64) domain.Money {
	t.Helper()
	money, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney(%d) error = %v", minor, err)
	}
	return money
}

func monthlyBudgetStart(t *testing.T, month domain.CivilMonth) time.Time {
	t.Helper()
	return time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
}

type monthlyBudgetFinancialCountSet struct {
	transactions, auditEvents, idempotencyRecords                     int64
	recurrences, recurrenceAuditEvents, recurrenceIdempotencyRecords  int64
	creditCards, creditCardAuditEvents, creditCardIdempotencyRecords  int64
	installmentPlans, installmentPlanAuditEvents                      int64
	cardPurchaseIdempotencyRecords, installmentPlanIdempotencyRecords int64
}

func monthlyBudgetFinancialCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) monthlyBudgetFinancialCountSet {
	t.Helper()
	var counts monthlyBudgetFinancialCountSet
	queries := []struct {
		target *int64
		query  string
	}{
		{&counts.transactions, "SELECT count(*) FROM transactions"},
		{&counts.auditEvents, "SELECT count(*) FROM audit_events"},
		{&counts.idempotencyRecords, "SELECT count(*) FROM idempotency_records"},
		{&counts.recurrences, "SELECT count(*) FROM recurrences"},
		{&counts.recurrenceAuditEvents, "SELECT count(*) FROM recurrence_audit_events"},
		{&counts.recurrenceIdempotencyRecords, "SELECT count(*) FROM recurrence_idempotency_records"},
		{&counts.creditCards, "SELECT count(*) FROM credit_cards"},
		{&counts.creditCardAuditEvents, "SELECT count(*) FROM credit_card_audit_events"},
		{&counts.creditCardIdempotencyRecords, "SELECT count(*) FROM credit_card_idempotency_records"},
		{&counts.installmentPlans, "SELECT count(*) FROM installment_plans"},
		{&counts.installmentPlanAuditEvents, "SELECT count(*) FROM installment_plan_audit_events"},
		{&counts.cardPurchaseIdempotencyRecords, "SELECT count(*) FROM card_purchase_idempotency_records"},
		{&counts.installmentPlanIdempotencyRecords, "SELECT count(*) FROM installment_plan_idempotency_records"},
	}
	for _, query := range queries {
		if err := pool.QueryRow(ctx, query.query).Scan(query.target); err != nil {
			t.Fatalf("count query failed: %v", err)
		}
	}
	return counts
}
