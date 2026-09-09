package domain_test

import (
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const simulationCardID = "card_0123456789abcdef0123456789abcdef"

func TestSimulatePurchaseOneTimeUsesExistingSafeAvailableAndDoesNotPersist(t *testing.T) {
	period := simulationPeriod(t, 2026, time.January, 1, 2026, time.February, 28)
	card := simulationCard(t, domain.CreditCardStatusActive)
	snapshot := simulationSnapshot(t, period, 10_000)
	request := simulationRequest(t, 1_250, domain.PurchaseSimulationModeOneTime, 0, 2026, time.January, 20)

	result, err := domain.SimulatePurchase(snapshot, card, request)
	if err != nil {
		t.Fatalf("SimulatePurchase() error = %v", err)
	}
	if result.Baseline().FinalAmount().MinorUnits() != 10_000 || result.Projected().FinalAmount().MinorUnits() != 8_750 || result.Impact().MinorUnits() != -1_250 {
		t.Fatalf("amounts = baseline %d projected %d impact %d", result.Baseline().FinalAmount().MinorUnits(), result.Projected().FinalAmount().MinorUnits(), result.Impact().MinorUnits())
	}
	lines := result.HypotheticalCommitments()
	if len(lines) != 1 || lines[0].Amount().MinorUnits() != 1_250 || lines[0].Sequence() != 1 {
		t.Fatalf("hypothetical lines = %#v", lines)
	}
	if len(result.Assumptions()) != 2 || result.Assumptions()[0] != domain.PurchaseSimulationAssumptionNotPersisted || result.Assumptions()[1] != domain.PurchaseSimulationAssumptionNoExpenseCreated {
		t.Fatalf("assumptions = %#v", result.Assumptions())
	}
	mutated := result.HypotheticalCommitments()
	mutated[0] = domain.SafeAvailableEntry{}
	if result.HypotheticalCommitments()[0].Amount().MinorUnits() != 1_250 {
		t.Fatal("result exposed mutable hypothetical lines")
	}
	if len(snapshot.Commitments()) != 0 {
		t.Fatal("simulation created a confirmed commitment")
	}
}

func TestSimulatePurchaseInstallmentsUsesScheduleAndExplicitPeriod(t *testing.T) {
	period := simulationPeriod(t, 2026, time.January, 1, 2026, time.April, 30)
	card := simulationCard(t, domain.CreditCardStatusActive)
	request := simulationRequest(t, 3_000, domain.PurchaseSimulationModeInstallment, 3, 2026, time.January, 20)
	result, err := domain.SimulatePurchase(simulationSnapshot(t, period, 10_000), card, request)
	if err != nil {
		t.Fatalf("SimulatePurchase() error = %v", err)
	}
	if result.Projected().FinalAmount().MinorUnits() != 7_000 || result.Impact().MinorUnits() != -3_000 {
		t.Fatalf("projected/impact = %d/%d, want 7000/-3000", result.Projected().FinalAmount().MinorUnits(), result.Impact().MinorUnits())
	}
	lines := result.HypotheticalCommitments()
	if len(lines) != 3 || lines[0].Sequence() != 1 || lines[1].Sequence() != 2 || lines[2].Sequence() != 3 || lines[0].Amount().MinorUnits()+lines[1].Amount().MinorUnits()+lines[2].Amount().MinorUnits() != 3_000 {
		t.Fatalf("schedule lines = %#v", lines)
	}

	narrowPeriod := simulationPeriod(t, 2026, time.March, 1, 2026, time.March, 31)
	narrowResult, err := domain.SimulatePurchase(simulationSnapshot(t, narrowPeriod, 10_000), card, request)
	if err != nil {
		t.Fatalf("narrow simulation error = %v", err)
	}
	if len(narrowResult.HypotheticalCommitments()) != 1 || narrowResult.HypotheticalCommitments()[0].Sequence() != 2 || narrowResult.Projected().FinalAmount().MinorUnits() != 9_000 {
		t.Fatalf("narrow result = %#v / %d", narrowResult.HypotheticalCommitments(), narrowResult.Projected().FinalAmount().MinorUnits())
	}
}

func TestSimulatePurchaseOutsidePeriodIsStableAndDoesNotChangeBaseline(t *testing.T) {
	period := simulationPeriod(t, 2026, time.January, 1, 2026, time.January, 31)
	card := simulationCard(t, domain.CreditCardStatusActive)
	request := simulationRequest(t, 900, domain.PurchaseSimulationModeOneTime, 0, 2026, time.January, 20)
	snapshot := simulationSnapshot(t, period, 10_000)
	first, err := domain.SimulatePurchase(snapshot, card, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.SimulatePurchase(snapshot, card, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.HypotheticalCommitments()) != 0 || first.Projected().FinalAmount().MinorUnits() != 10_000 || first.Impact().MinorUnits() != 0 {
		t.Fatalf("outside-period result = lines %d projected %d impact %d", len(first.HypotheticalCommitments()), first.Projected().FinalAmount().MinorUnits(), first.Impact().MinorUnits())
	}
	if first.Projected().FinalAmount().MinorUnits() != second.Projected().FinalAmount().MinorUnits() || len(second.HypotheticalCommitments()) != 0 {
		t.Fatal("repeated simulation was not deterministic")
	}
}

func TestSimulatePurchaseRespectsBudgetCapWithoutChangingBaseline(t *testing.T) {
	period := simulationPeriod(t, 2026, time.February, 1, 2026, time.February, 28)
	budgetAmount := simulationMoney(t, 2_000)
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{OwnerID: "owner-1", Month: simulationMonth(t, 2026, time.February), Amount: budgetAmount})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: simulationMoney(t, 10_000), Budget: &budget})
	if err != nil {
		t.Fatal(err)
	}
	request := simulationRequest(t, 1_500, domain.PurchaseSimulationModeOneTime, 0, 2026, time.January, 20)
	result, err := domain.SimulatePurchase(snapshot, simulationCard(t, domain.CreditCardStatusActive), request)
	if err != nil {
		t.Fatalf("SimulatePurchase() error = %v", err)
	}
	if result.Baseline().FinalAmount().MinorUnits() != 2_000 || result.Projected().FinalAmount().MinorUnits() != 500 {
		t.Fatalf("budget-capped amounts = %d/%d, want 2000/500", result.Baseline().FinalAmount().MinorUnits(), result.Projected().FinalAmount().MinorUnits())
	}
}

func TestSimulatePurchaseRejectsInvalidCardAndDuplicateCommitment(t *testing.T) {
	period := simulationPeriod(t, 2026, time.January, 1, 2026, time.February, 28)
	card := simulationCard(t, domain.CreditCardStatusActive)
	request := simulationRequest(t, 1_000, domain.PurchaseSimulationModeOneTime, 0, 2026, time.January, 20)
	foreignCard := simulationCardWithOwner(t, domain.CreditCardStatusActive, "owner-2")
	if _, err := domain.SimulatePurchase(simulationSnapshot(t, period, 10_000), foreignCard, request); !errors.Is(err, domain.ErrPurchaseSimulationCardOwnerMismatch) {
		t.Fatalf("owner mismatch error = %v", err)
	}
	if _, err := domain.SimulatePurchase(simulationSnapshotWithCommitment(t, period, 10_000, "simulation_purchase_"+simulationCardID, 1, 2026, time.February, 10), card, request); !errors.Is(err, domain.ErrPurchaseSimulationDuplicateCommitment) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := domain.SimulatePurchase(simulationSnapshot(t, period, 10_000), simulationCard(t, domain.CreditCardStatusArchived), request); !errors.Is(err, domain.ErrPurchaseSimulationCardArchived) {
		t.Fatalf("archived error = %v", err)
	}
}

func TestNewPurchaseSimulationRequestValidatesShape(t *testing.T) {
	date := simulationDate(t, 2026, time.January, 20)
	amount := simulationMoney(t, 1)
	valid := domain.PurchaseSimulationRequestParams{Amount: amount, PurchaseOn: date, Mode: domain.PurchaseSimulationModeOneTime, CreditCardID: simulationCardID}
	cases := []struct {
		name   string
		params domain.PurchaseSimulationRequestParams
		want   error
	}{
		{"zero amount", domain.PurchaseSimulationRequestParams{Amount: simulationMoney(t, 0), PurchaseOn: date, Mode: domain.PurchaseSimulationModeOneTime, CreditCardID: simulationCardID}, domain.ErrInvalidPurchaseSimulationAmount},
		{"unsupported currency", domain.PurchaseSimulationRequestParams{Amount: domain.Money{}, PurchaseOn: date, Mode: domain.PurchaseSimulationModeOneTime, CreditCardID: simulationCardID}, domain.ErrInvalidPurchaseSimulationAmount},
		{"invalid card", func() domain.PurchaseSimulationRequestParams {
			value := valid
			value.CreditCardID = "card-invalid"
			return value
		}(), domain.ErrInvalidPurchaseSimulationCardID},
		{"installment count on one-time", func() domain.PurchaseSimulationRequestParams {
			value := valid
			value.InstallmentCount = 2
			return value
		}(), domain.ErrInvalidPurchaseSimulationInstallments},
		{"missing installments", func() domain.PurchaseSimulationRequestParams {
			value := valid
			value.Mode = domain.PurchaseSimulationModeInstallment
			return value
		}(), domain.ErrInvalidPurchaseSimulationInstallments},
		{"unknown mode", func() domain.PurchaseSimulationRequestParams { value := valid; value.Mode = "UNKNOWN"; return value }(), domain.ErrInvalidPurchaseSimulationMode},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := domain.NewPurchaseSimulationRequest(testCase.params); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func simulationCard(t *testing.T, status domain.CreditCardStatus) domain.CreditCard {
	return simulationCardWithOwner(t, status, "owner-1")
}

func simulationCardWithOwner(t *testing.T, status domain.CreditCardStatus, owner string) domain.CreditCard {
	t.Helper()
	name, _ := domain.NewCreditCardName("Simulation Card")
	closing, _ := domain.NewDayOfMonthAnchor(25)
	due, _ := domain.NewDayOfMonthAnchor(10)
	params := domain.CreditCardRehydrationParams{ID: simulationCardID, Details: domain.CreditCardDetails{UserID: owner, Name: name, ClosingDayAnchor: closing, DueDayAnchor: due}, Status: status, CreatedAt: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)}
	if status == domain.CreditCardStatusArchived {
		archivedAt := time.Date(2026, time.February, 1, 12, 0, 0, 0, time.UTC)
		params.ArchivedAt = &archivedAt
	}
	card, err := domain.RehydrateCreditCard(params)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func simulationRequest(t *testing.T, amountMinor int64, mode domain.PurchaseSimulationMode, count, year int, month time.Month, day int) domain.PurchaseSimulationRequest {
	t.Helper()
	request, err := domain.NewPurchaseSimulationRequest(domain.PurchaseSimulationRequestParams{Amount: simulationMoney(t, amountMinor), PurchaseOn: simulationDate(t, year, month, day), Mode: mode, InstallmentCount: count, CreditCardID: simulationCardID})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func simulationSnapshot(t *testing.T, period domain.SafeAvailablePeriod, balance int64) domain.SafeAvailableSnapshot {
	t.Helper()
	snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: simulationMoney(t, balance)})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func simulationSnapshotWithCommitment(t *testing.T, period domain.SafeAvailablePeriod, balance int64, sourceID string, sequence, year int, month time.Month, day int) domain.SafeAvailableSnapshot {
	t.Helper()
	entry, err := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{Kind: domain.SafeAvailableEntryKindCommitment, OwnerID: "owner-1", SourceID: sourceID, Sequence: sequence, DueOn: simulationDate(t, year, month, day), Amount: simulationMoney(t, 1)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: simulationMoney(t, balance), Commitments: []domain.SafeAvailableEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func simulationPeriod(t *testing.T, startYear int, startMonth time.Month, startDay int, endYear int, endMonth time.Month, endDay int) domain.SafeAvailablePeriod {
	t.Helper()
	period, err := domain.NewSafeAvailablePeriod(simulationDate(t, startYear, startMonth, startDay), simulationDate(t, endYear, endMonth, endDay))
	if err != nil {
		t.Fatal(err)
	}
	return period
}

func simulationMonth(t *testing.T, year int, month time.Month) domain.CivilMonth {
	t.Helper()
	value, err := domain.NewCivilMonth(year, month)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func simulationDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	value, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func simulationMoney(t *testing.T, minor int64) domain.Money {
	t.Helper()
	value, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
