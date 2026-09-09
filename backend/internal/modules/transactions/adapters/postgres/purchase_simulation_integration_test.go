//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestPurchaseSimulationPostgresUsesOneOwnerScopedReadSnapshot(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_purchase_simulation_a"
	const ownerB = "usr_purchase_simulation_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	cardID := "card_00000000000000000000000000000b01"
	insertSafeAvailableCard(t, ctx, pool, cardID, ownerA)
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "income_simulation_opening", ownerID: ownerA, transactionType: "INCOME", description: "Abertura", amountMinor: 80_000,
		occurredAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "income_simulation_period", ownerID: ownerA, transactionType: "INCOME", description: "Receita", amountMinor: 10_000,
		occurredAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "expense_simulation_period", ownerID: ownerA, transactionType: "EXPENSE", description: "Despesa", amountMinor: 5_000,
		paymentMethod: "PIX", occurredAt: time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "expense_simulation_plan", ownerID: ownerA, transactionType: "EXPENSE", description: "Compra", amountMinor: 30_000,
		paymentMethod: "CREDIT", cardID: &cardID, statementDueOn: "2026-09-15", occurredAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailablePlan(t, ctx, pool, ownerA, cardID, "expense_simulation_plan", "ipl_00000000000000000000000000000b01", 30_000, 3, "2026-09-15", 15, "ACTIVE", nil)
	insertSafeAvailableRecurrence(t, ctx, pool, ownerA, "rec_simulation_001", 2_000, "2026-09-01", "ACTIVE", nil)

	repository, err := adapter.NewPurchaseSimulationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	useCase, err := application.NewSimulatePurchase(repository)
	if err != nil {
		t.Fatal(err)
	}
	period := safeAvailableIntegrationPeriod(t, 2026, time.September, 1, 2026, time.October, 31)
	input := application.SimulatePurchaseInput{
		OwnerID: ownerA, Period: period, CreditCardID: cardID, AmountMinor: 12_000, Currency: domain.CurrencyBRL,
		PurchaseOn: safeAvailableIntegrationDate(t, 2026, time.September, 5), Mode: domain.PurchaseSimulationModeOneTime,
	}
	before := readSafeAvailableCounts(t, ctx, pool)
	result, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("owner A simulation error = %v", err)
	}
	if result.Simulation.Baseline().FinalAmount().MinorUnits() != 31_000 || result.Simulation.Projected().FinalAmount().MinorUnits() != 19_000 || result.Simulation.Impact().MinorUnits() != -12_000 {
		t.Fatalf("A amounts = baseline %d projected %d impact %d", result.Simulation.Baseline().FinalAmount().MinorUnits(), result.Simulation.Projected().FinalAmount().MinorUnits(), result.Simulation.Impact().MinorUnits())
	}
	if len(result.Simulation.HypotheticalCommitments()) != 1 || result.Simulation.HypotheticalCommitments()[0].Amount().MinorUnits() != 12_000 {
		t.Fatalf("A hypothetical commitments = %#v", result.Simulation.HypotheticalCommitments())
	}

	ownerBInput := input
	ownerBInput.OwnerID = ownerB
	ownerBResult, err := useCase.Execute(ctx, ownerBInput)
	if !errors.Is(err, application.ErrPurchaseSimulationCardNotFound) || ownerBResult.Simulation.Projected().FinalAmount().MinorUnits() != 0 {
		t.Fatalf("B isolation result = %+v error=%v", ownerBResult, err)
	}
	if after := readSafeAvailableCounts(t, ctx, pool); after != before {
		t.Fatalf("simulation reads changed persistence: before=%+v after=%+v", before, after)
	}

	replay, err := useCase.Execute(ctx, input)
	if err != nil || replay.Simulation.Projected().FinalAmount().MinorUnits() != 19_000 {
		t.Fatalf("A replay = %+v error=%v", replay, err)
	}
}

func TestPurchaseSimulationPostgresSanitizesFailureAndPreservesContext(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner := "usr_purchase_simulation_failure"
	insertSyntheticUser(t, ctx, pool, owner)
	cardID := "card_00000000000000000000000000000b02"
	insertSafeAvailableCard(t, ctx, pool, cardID, owner)
	repository, err := adapter.NewPurchaseSimulationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	period := safeAvailableIntegrationPeriod(t, 2026, time.September, 1, 2026, time.September, 30)
	_, err = repository.Read(ctx, application.PurchaseSimulationSnapshotQuery{OwnerID: owner, Period: period, CreditCardID: cardID})
	if err == nil || strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "DSN") {
		t.Fatalf("unsanitized database failure = %v", err)
	}
	cancelled, cancelRead := context.WithCancel(context.Background())
	cancelRead()
	if _, err := repository.Read(cancelled, application.PurchaseSimulationSnapshotQuery{OwnerID: owner, Period: period, CreditCardID: cardID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v", err)
	}
}

func safeAvailableIntegrationDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
