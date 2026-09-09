package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

const simulationAppCardID = "card_0123456789abcdef0123456789abcdef"

type purchaseSimulationReaderFake struct {
	snapshot application.PurchaseSimulationSnapshot
	err      error
	calls    int
	query    application.PurchaseSimulationSnapshotQuery
}

func (reader *purchaseSimulationReaderFake) Read(_ context.Context, query application.PurchaseSimulationSnapshotQuery) (application.PurchaseSimulationSnapshot, error) {
	reader.calls++
	reader.query = query
	return reader.snapshot, reader.err
}

func TestSimulatePurchaseUsesOneCombinedSnapshotRead(t *testing.T) {
	period := simulationAppPeriod(t, 2026, time.January, 1, 2026, time.February, 28)
	reader := &purchaseSimulationReaderFake{snapshot: application.PurchaseSimulationSnapshot{
		SafeAvailable: simulationAppSnapshot(t, period, 10_000), CreditCard: simulationAppCard(t), Found: true,
	}}
	useCase, err := application.NewSimulatePurchase(reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Execute(context.Background(), application.SimulatePurchaseInput{
		OwnerID: "owner-1", Period: period, CreditCardID: simulationAppCardID, AmountMinor: 1_250, Currency: domain.CurrencyBRL,
		PurchaseOn: simulationAppDate(t, 2026, time.January, 20), Mode: domain.PurchaseSimulationModeOneTime,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("combined reader calls = %d, want 1", reader.calls)
	}
	if reader.query.OwnerID != "owner-1" || reader.query.CreditCardID != simulationAppCardID || !reader.query.Period.StartOn().Equal(period.StartOn()) || !reader.query.Period.EndOn().Equal(period.EndOn()) {
		t.Fatalf("query = %#v, want owner/card/period", reader.query)
	}
	if result.Simulation.Projected().FinalAmount().MinorUnits() != 8_750 || result.Simulation.Impact().MinorUnits() != -1_250 {
		t.Fatalf("projected/impact = %d/%d, want 8750/-1250", result.Simulation.Projected().FinalAmount().MinorUnits(), result.Simulation.Impact().MinorUnits())
	}
}

func TestSimulatePurchaseValidatesBeforeReadingAndPreservesCancellation(t *testing.T) {
	period := simulationAppPeriod(t, 2026, time.March, 1, 2026, time.March, 31)
	reader := &purchaseSimulationReaderFake{snapshot: application.PurchaseSimulationSnapshot{SafeAvailable: simulationAppSnapshot(t, period, 1_000), CreditCard: simulationAppCard(t), Found: true}}
	useCase := simulationAppUseCase(t, reader)
	base := application.SimulatePurchaseInput{OwnerID: "owner-1", Period: period, CreditCardID: simulationAppCardID, AmountMinor: 100, Currency: domain.CurrencyBRL, PurchaseOn: simulationAppDate(t, 2026, time.February, 20), Mode: domain.PurchaseSimulationModeOneTime}
	invalid := []application.SimulatePurchaseInput{
		{Period: period, CreditCardID: simulationAppCardID, AmountMinor: 100, Currency: domain.CurrencyBRL, PurchaseOn: base.PurchaseOn, Mode: domain.PurchaseSimulationModeOneTime},
		{OwnerID: "owner-1", CreditCardID: simulationAppCardID, AmountMinor: 100, Currency: domain.CurrencyBRL, PurchaseOn: base.PurchaseOn, Mode: domain.PurchaseSimulationModeOneTime},
		{OwnerID: "owner-1", Period: period, CreditCardID: "card-invalid", AmountMinor: 100, Currency: domain.CurrencyBRL, PurchaseOn: base.PurchaseOn, Mode: domain.PurchaseSimulationModeOneTime},
		{OwnerID: "owner-1", Period: period, CreditCardID: simulationAppCardID, AmountMinor: 0, Currency: domain.CurrencyBRL, PurchaseOn: base.PurchaseOn, Mode: domain.PurchaseSimulationModeOneTime},
	}
	for index, input := range invalid {
		if _, err := useCase.Execute(context.Background(), input); err == nil {
			t.Fatalf("invalid input %d unexpectedly succeeded", index)
		}
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls for invalid inputs = %d, want 0", reader.calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(ctx, base); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel error = %v, want context.Canceled", err)
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls after pre-cancel = %d, want 0", reader.calls)
	}
}

func TestSimulatePurchaseSanitizesDependencyErrorsAndPreservesContext(t *testing.T) {
	period := simulationAppPeriod(t, 2026, time.April, 1, 2026, time.April, 30)
	base := application.SimulatePurchaseInput{OwnerID: "owner-1", Period: period, CreditCardID: simulationAppCardID, AmountMinor: 100, Currency: domain.CurrencyBRL, PurchaseOn: simulationAppDate(t, 2026, time.March, 20), Mode: domain.PurchaseSimulationModeOneTime}
	reader := &purchaseSimulationReaderFake{err: errors.New("dsn=secret SELECT owner=owner-2")}
	useCase := simulationAppUseCase(t, reader)
	_, err := useCase.Execute(context.Background(), base)
	if !errors.Is(err, application.ErrPurchaseSimulationSnapshotQuery) || err.Error() != application.ErrPurchaseSimulationSnapshotQuery.Error() {
		t.Fatalf("sanitized error = %v", err)
	}
	for _, expected := range []error{context.Canceled, context.DeadlineExceeded} {
		reader.err = expected
		_, err = useCase.Execute(context.Background(), base)
		if !errors.Is(err, expected) {
			t.Fatalf("reader error = %v, want %v", err, expected)
		}
	}
}

func TestSimulatePurchaseRejectsMissingAndCrossOwnerSnapshots(t *testing.T) {
	period := simulationAppPeriod(t, 2026, time.May, 1, 2026, time.May, 31)
	input := application.SimulatePurchaseInput{OwnerID: "owner-1", Period: period, CreditCardID: simulationAppCardID, AmountMinor: 100, Currency: domain.CurrencyBRL, PurchaseOn: simulationAppDate(t, 2026, time.April, 20), Mode: domain.PurchaseSimulationModeOneTime}
	reader := &purchaseSimulationReaderFake{snapshot: application.PurchaseSimulationSnapshot{Found: false}}
	useCase := simulationAppUseCase(t, reader)
	if _, err := useCase.Execute(context.Background(), input); !errors.Is(err, application.ErrPurchaseSimulationCardNotFound) {
		t.Fatalf("not found error = %v", err)
	}
	foreign := simulationAppCardWithOwner(t, "owner-2")
	reader.snapshot = application.PurchaseSimulationSnapshot{SafeAvailable: simulationAppSnapshot(t, period, 1_000), CreditCard: foreign, Found: true}
	if _, err := useCase.Execute(context.Background(), input); !errors.Is(err, application.ErrPurchaseSimulationDependency) || err.Error() != application.ErrPurchaseSimulationDependency.Error() {
		t.Fatalf("cross-owner error = %v", err)
	}
}

func simulationAppUseCase(t *testing.T, reader application.PurchaseSimulationSnapshotReader) *application.SimulatePurchase {
	t.Helper()
	useCase, err := application.NewSimulatePurchase(reader)
	if err != nil {
		t.Fatal(err)
	}
	return useCase
}

func simulationAppCard(t *testing.T) domain.CreditCard {
	return simulationAppCardWithOwner(t, "owner-1")
}

func simulationAppCardWithOwner(t *testing.T, owner string) domain.CreditCard {
	t.Helper()
	name, _ := domain.NewCreditCardName("Simulation Card")
	closing, _ := domain.NewDayOfMonthAnchor(25)
	due, _ := domain.NewDayOfMonthAnchor(10)
	card, err := domain.NewCreditCard(domain.CreditCardParams{ID: simulationAppCardID, Details: domain.CreditCardDetails{UserID: owner, Name: name, ClosingDayAnchor: closing, DueDayAnchor: due}, CreatedAt: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func simulationAppSnapshot(t *testing.T, period domain.SafeAvailablePeriod, balance int64) domain.SafeAvailableSnapshot {
	t.Helper()
	money, err := domain.NewMoney(balance, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: money})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func simulationAppPeriod(t *testing.T, startYear int, startMonth time.Month, startDay int, endYear int, endMonth time.Month, endDay int) domain.SafeAvailablePeriod {
	t.Helper()
	start := simulationAppDate(t, startYear, startMonth, startDay)
	end := simulationAppDate(t, endYear, endMonth, endDay)
	period, err := domain.NewSafeAvailablePeriod(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return period
}

func simulationAppDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
