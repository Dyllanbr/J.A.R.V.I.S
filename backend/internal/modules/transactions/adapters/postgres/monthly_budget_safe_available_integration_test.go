//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestSafeAvailablePostgresAppliesOwnerMonthlyBudgetInSameSnapshot(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_safe_available_budget_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	cardID := "card_00000000000000000000000000000c01"
	insertSafeAvailableCard(t, ctx, pool, cardID, owner)
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "inc_safe_available_budget_opening", ownerID: owner, transactionType: "INCOME", description: "Saldo", amountMinor: 10_000,
		occurredAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "inc_safe_available_budget_period", ownerID: owner, transactionType: "INCOME", description: "Receita", amountMinor: 2_000,
		occurredAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_budget_period", ownerID: owner, transactionType: "EXPENSE", description: "Despesa", amountMinor: 3_000,
		paymentMethod: "PIX", occurredAt: time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_budget_plan", ownerID: owner, transactionType: "EXPENSE", description: "Plano", amountMinor: 4_000,
		paymentMethod: "CREDIT", cardID: &cardID, statementDueOn: "2026-09-10", occurredAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailablePlan(t, ctx, pool, owner, cardID, "exp_safe_available_budget_plan", "ipl_00000000000000000000000000000c01", 4_000, 2, "2026-09-10", 10, "ACTIVE", nil)
	insertSafeAvailableRecurrence(t, ctx, pool, owner, "rec_safe_available_budget", 1_000, "2026-09-15", "ACTIVE", nil)

	monthlyRepository := newMonthlyBudgetRepository(t, pool)
	month := mustMonthlyBudgetMonth(t, "2026-09")
	store, err := application.NewSetMonthlyBudget(monthlyRepository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Execute(ctx, application.SetMonthlyBudgetInput{OwnerID: owner, Month: month, Amount: mustMonthlyBudgetMoney(t, 10_000)}); err != nil {
		t.Fatalf("set budget: %v", err)
	}

	repository, err := adapter.NewSafeAvailableRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	calculate, err := application.NewCalculateSafeAvailable(repository)
	if err != nil {
		t.Fatal(err)
	}
	period := safeAvailableIntegrationPeriod(t, 2026, time.September, 1, 2026, time.September, 30)
	before := readSafeAvailableCounts(t, ctx, pool)
	result, err := calculate.Execute(ctx, application.CalculateSafeAvailableInput{OwnerID: owner, Period: period})
	if err != nil {
		t.Fatalf("calculate safe available: %v", err)
	}
	available := result.Available
	if available.AvailableBalance().MinorUnits() != 6_000 || available.TotalConfirmedIncome().MinorUnits() != 2_000 || available.TotalConfirmedExpense().MinorUnits() != 3_000 || available.TotalConfirmedCommitments().MinorUnits() != 3_000 || available.FinalAmount().MinorUnits() != 2_000 {
		t.Fatalf("budgeted result = balance %d income %d expense %d commitments %d final %d", available.AvailableBalance().MinorUnits(), available.TotalConfirmedIncome().MinorUnits(), available.TotalConfirmedExpense().MinorUnits(), available.TotalConfirmedCommitments().MinorUnits(), available.FinalAmount().MinorUnits())
	}
	budget, ok := available.Budget()
	if !ok || budget.Amount().MinorUnits() != 10_000 {
		t.Fatalf("budget = %+v present=%t", budget, ok)
	}
	remaining, ok := available.BudgetRemaining()
	if !ok || remaining.MinorUnits() != 4_000 {
		t.Fatalf("budget remaining = %d present=%t", remaining.MinorUnits(), ok)
	}
	if len(available.MissingData()) != 0 {
		t.Fatalf("missing data = %#v, want none", available.MissingData())
	}
	after := readSafeAvailableCounts(t, ctx, pool)
	if before != after {
		t.Fatalf("safe available read changed financial counts: before=%+v after=%+v", before, after)
	}

	// A multi-month request has no defined aggregation rule in this slice and
	// therefore remains explicitly unbudgeted.
	crossPeriod := safeAvailableIntegrationPeriod(t, 2026, time.September, 1, 2026, time.October, 1)
	crossResult, err := calculate.Execute(ctx, application.CalculateSafeAvailableInput{OwnerID: owner, Period: crossPeriod})
	if err != nil {
		t.Fatalf("cross-month calculate: %v", err)
	}
	if _, ok := crossResult.Available.Budget(); ok || len(crossResult.Available.MissingData()) != 1 || crossResult.Available.MissingData()[0] != domain.SafeAvailableMissingBudget {
		t.Fatalf("cross-month budget state = present=%t missing=%#v", ok, crossResult.Available.MissingData())
	}
}
