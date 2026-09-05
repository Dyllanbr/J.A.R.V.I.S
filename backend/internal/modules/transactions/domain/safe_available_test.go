package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestCalculateSafeAvailableUsesExactFormulaAndExplainsBreakdown(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.January, 1, 2026, time.January, 31)
	balance := safeAvailableMoney(t, 100_000)
	income := safeAvailableEntry(t, domain.SafeAvailableEntryKindIncome, "income-1", 0, period.StartOn(), 10_000)
	expense := safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", 0, period.StartOn(), 20_000)
	commitment := safeAvailableEntry(t, domain.SafeAvailableEntryKindCommitment, "ipl_00000000000000000000000000000001", 1, period.EndOn(), 30_000)
	snapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: balance,
		Incomes: []domain.SafeAvailableEntry{income}, Expenses: []domain.SafeAvailableEntry{expense}, Commitments: []domain.SafeAvailableEntry{commitment},
	})

	result, err := domain.CalculateSafeAvailable(snapshot)
	if err != nil {
		t.Fatalf("CalculateSafeAvailable() error = %v", err)
	}
	if result.FinalAmount().MinorUnits() != 60_000 {
		t.Fatalf("FinalAmount() = %d, want 60000", result.FinalAmount().MinorUnits())
	}
	if result.TotalConfirmedIncome().MinorUnits() != 10_000 || result.TotalConfirmedExpense().MinorUnits() != 20_000 || result.TotalConfirmedCommitments().MinorUnits() != 30_000 {
		t.Fatalf("component totals = %d/%d/%d, want 10000/20000/30000", result.TotalConfirmedIncome().MinorUnits(), result.TotalConfirmedExpense().MinorUnits(), result.TotalConfirmedCommitments().MinorUnits())
	}
	breakdown := result.Breakdown()
	if len(breakdown) != 4 {
		t.Fatalf("Breakdown() length = %d, want 4", len(breakdown))
	}
	if breakdown[0].Kind() != domain.SafeAvailableEntryKindAvailableBalance || breakdown[1].Kind() != domain.SafeAvailableEntryKindIncome || breakdown[2].Kind() != domain.SafeAvailableEntryKindExpense || breakdown[3].Kind() != domain.SafeAvailableEntryKindCommitment {
		t.Fatalf("breakdown order = %q, %q, %q, %q", breakdown[0].Kind(), breakdown[1].Kind(), breakdown[2].Kind(), breakdown[3].Kind())
	}
	var explained int64
	for _, line := range breakdown {
		switch line.Kind() {
		case domain.SafeAvailableEntryKindAvailableBalance, domain.SafeAvailableEntryKindIncome:
			explained += line.Amount().MinorUnits()
		case domain.SafeAvailableEntryKindExpense, domain.SafeAvailableEntryKindCommitment:
			explained -= line.Amount().MinorUnits()
		}
	}
	if explained != result.FinalAmount().MinorUnits() {
		t.Fatalf("breakdown total = %d, want final amount %d", explained, result.FinalAmount().MinorUnits())
	}
	if len(result.MissingData()) != 1 || result.MissingData()[0] != domain.SafeAvailableMissingBudget {
		t.Fatalf("MissingData() = %#v, want explicit BUDGET marker", result.MissingData())
	}
}

func TestCalculateSafeAvailableAddsConfirmedIncome(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.January, 1, 2026, time.January, 31)
	snapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: safeAvailableMoney(t, 100),
		Incomes: []domain.SafeAvailableEntry{safeAvailableEntry(t, domain.SafeAvailableEntryKindIncome, "income-1", 0, period.StartOn(), 25)},
	})
	result := safeAvailableResult(t, snapshot)
	if result.FinalAmount().MinorUnits() != 125 {
		t.Fatalf("FinalAmount() = %d, want 125", result.FinalAmount().MinorUnits())
	}
}

func TestCalculateSafeAvailableSubtractsConfirmedExpense(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.January, 1, 2026, time.January, 31)
	snapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: safeAvailableMoney(t, 100),
		Expenses: []domain.SafeAvailableEntry{safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", 0, period.StartOn(), 25)},
	})
	result := safeAvailableResult(t, snapshot)
	if result.FinalAmount().MinorUnits() != 75 {
		t.Fatalf("FinalAmount() = %d, want 75", result.FinalAmount().MinorUnits())
	}
}

func TestCalculateSafeAvailableSubtractsConfirmedCommitment(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.January, 1, 2026, time.January, 31)
	snapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: safeAvailableMoney(t, 100),
		Commitments: []domain.SafeAvailableEntry{safeAvailableEntry(t, domain.SafeAvailableEntryKindCommitment, "ipl_00000000000000000000000000000001", 1, period.StartOn(), 25)},
	})
	result := safeAvailableResult(t, snapshot)
	if result.FinalAmount().MinorUnits() != 75 {
		t.Fatalf("FinalAmount() = %d, want 75", result.FinalAmount().MinorUnits())
	}
}

func TestCalculateSafeAvailableSortsBreakdownDeterministically(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.January, 1, 2026, time.January, 31)
	first := safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-b", 0, period.StartOn(), 20)
	second := safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-a", 0, period.StartOn(), 10)
	snapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: safeAvailableMoney(t, 100),
		Expenses: []domain.SafeAvailableEntry{first, second},
	})
	result := safeAvailableResult(t, snapshot)
	breakdown := result.Breakdown()
	if len(breakdown) != 3 || breakdown[1].SourceID() != "expense-a" || breakdown[2].SourceID() != "expense-b" {
		t.Fatalf("deterministic breakdown order = %#v", breakdown)
	}
}

func TestCalculateSafeAvailableSupportsZeroAndNegativeResults(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.February, 1, 2026, time.February, 28)
	zeroSnapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: safeAvailableMoney(t, 0),
	})
	zero, err := domain.CalculateSafeAvailable(zeroSnapshot)
	if err != nil || zero.FinalAmount().MinorUnits() != 0 {
		t.Fatalf("zero calculation = %d, error %v", zero.FinalAmount().MinorUnits(), err)
	}

	negativeSnapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: safeAvailableMoney(t, 100),
		Expenses: []domain.SafeAvailableEntry{safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", 0, period.StartOn(), 101)},
	})
	negative, err := domain.CalculateSafeAvailable(negativeSnapshot)
	if err != nil || negative.FinalAmount().MinorUnits() != -1 {
		t.Fatalf("negative calculation = %d, error %v", negative.FinalAmount().MinorUnits(), err)
	}
}

func TestNewSafeAvailableSnapshotRejectsInvalidEntriesAndDuplicates(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.March, 1, 2026, time.March, 31)
	amount := safeAvailableMoney(t, 1)
	valid := safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", 0, period.StartOn(), 1)

	cases := []struct {
		name   string
		params domain.SafeAvailableSnapshotParams
		want   error
	}{
		{name: "invalid owner", params: domain.SafeAvailableSnapshotParams{OwnerID: "", Period: period, AvailableBalance: amount}, want: domain.ErrInvalidSafeAvailableOwnerID},
		{name: "invalid period", params: domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", AvailableBalance: amount}, want: domain.ErrInvalidSafeAvailablePeriod},
		{name: "outside period", params: domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: amount, Expenses: []domain.SafeAvailableEntry{safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-2", 0, safeAvailableDate(t, 2026, time.April, 1), 1)}}, want: domain.ErrSafeAvailableEntryOutsidePeriod},
		{name: "duplicate", params: domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: amount, Expenses: []domain.SafeAvailableEntry{valid, valid}}, want: domain.ErrSafeAvailableDuplicateEntry},
		{name: "wrong classified group", params: domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: amount, Incomes: []domain.SafeAvailableEntry{valid}}, want: domain.ErrInvalidSafeAvailableEntryKind},
		{name: "duplicate missing marker", params: domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: amount, MissingData: []domain.SafeAvailableMissingData{domain.SafeAvailableMissingBudget, domain.SafeAvailableMissingBudget}}, want: domain.ErrSafeAvailableDuplicateEntry},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := domain.NewSafeAvailableSnapshot(testCase.params)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("NewSafeAvailableSnapshot() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestNewSafeAvailableEntryRejectsInvalidInput(t *testing.T) {
	date := safeAvailableDate(t, 2026, time.March, 10)
	amount := safeAvailableMoney(t, 1)
	cases := []struct {
		name   string
		params domain.SafeAvailableEntryParams
		want   error
	}{
		{name: "invalid kind", params: domain.SafeAvailableEntryParams{Kind: "UNKNOWN", OwnerID: "owner-1", SourceID: "source", DueOn: date, Amount: amount}, want: domain.ErrInvalidSafeAvailableEntryKind},
		{name: "invalid owner", params: domain.SafeAvailableEntryParams{Kind: domain.SafeAvailableEntryKindExpense, OwnerID: "", SourceID: "source", DueOn: date, Amount: amount}, want: domain.ErrInvalidSafeAvailableEntryOwnerID},
		{name: "invalid source", params: domain.SafeAvailableEntryParams{Kind: domain.SafeAvailableEntryKindExpense, OwnerID: "owner-1", SourceID: "", DueOn: date, Amount: amount}, want: domain.ErrInvalidSafeAvailableEntryID},
		{name: "commitment sequence zero", params: domain.SafeAvailableEntryParams{Kind: domain.SafeAvailableEntryKindCommitment, OwnerID: "owner-1", SourceID: "plan", DueOn: date, Amount: amount}, want: domain.ErrInvalidSafeAvailableEntrySequence},
		{name: "invalid date", params: domain.SafeAvailableEntryParams{Kind: domain.SafeAvailableEntryKindExpense, OwnerID: "owner-1", SourceID: "source", DueOn: domain.CivilDate{}, Amount: amount}, want: domain.ErrInvalidSafeAvailableEntryDate},
		{name: "invalid amount", params: domain.SafeAvailableEntryParams{Kind: domain.SafeAvailableEntryKindExpense, OwnerID: "owner-1", SourceID: "source", DueOn: date}, want: domain.ErrInvalidSafeAvailableEntryAmount},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := domain.NewSafeAvailableEntry(testCase.params)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("NewSafeAvailableEntry() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestCalculateSafeAvailableRejectsOverflowAndPreservesDefensiveCopies(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.April, 1, 2026, time.April, 30)
	maxAmount := safeAvailableMoney(t, math.MaxInt64)
	incomeOverflow := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: maxAmount,
		Incomes: []domain.SafeAvailableEntry{safeAvailableEntry(t, domain.SafeAvailableEntryKindIncome, "income-overflow", 0, period.StartOn(), 1)},
	})
	if _, err := domain.CalculateSafeAvailable(incomeOverflow); !errors.Is(err, domain.ErrSafeAvailableOverflow) {
		t.Fatalf("balance plus income overflow error = %v, want ErrSafeAvailableOverflow", err)
	}

	first := safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", 0, period.StartOn(), math.MaxInt64)
	second := safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-2", 0, period.StartOn(), 1)
	snapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: maxAmount, Expenses: []domain.SafeAvailableEntry{first, second}})
	if _, err := domain.CalculateSafeAvailable(snapshot); !errors.Is(err, domain.ErrSafeAvailableOverflow) {
		t.Fatalf("sum overflow error = %v, want ErrSafeAvailableOverflow", err)
	}

	defensiveSnapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: maxAmount, Expenses: []domain.SafeAvailableEntry{first}})
	entries := defensiveSnapshot.Expenses()
	entries[0] = domain.SafeAvailableEntry{}
	if defensiveSnapshot.Expenses()[0].SourceID() != "expense-1" {
		t.Fatal("snapshot Expenses() leaked mutable backing storage")
	}
	result := safeAvailableResult(t, defensiveSnapshot)
	lines := result.Breakdown()
	lines[0] = domain.SafeAvailableBreakdownLine{}
	if result.Breakdown()[0].SourceID() != "available-balance" {
		t.Fatal("result Breakdown() leaked mutable backing storage")
	}
}

func TestCalculateSafeAvailableRejectsSubtractionUnderflow(t *testing.T) {
	period := safeAvailablePeriod(t, 2026, time.May, 1, 2026, time.May, 31)
	snapshot := safeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: safeAvailableMoney(t, math.MinInt64),
		Expenses: []domain.SafeAvailableEntry{safeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", 0, period.StartOn(), 1)},
	})
	if _, err := domain.CalculateSafeAvailable(snapshot); !errors.Is(err, domain.ErrSafeAvailableOverflow) {
		t.Fatalf("subtraction underflow error = %v, want ErrSafeAvailableOverflow", err)
	}
}

func safeAvailableSnapshot(t *testing.T, params domain.SafeAvailableSnapshotParams) domain.SafeAvailableSnapshot {
	t.Helper()
	snapshot, err := domain.NewSafeAvailableSnapshot(params)
	if err != nil {
		t.Fatalf("NewSafeAvailableSnapshot() error = %v", err)
	}
	return snapshot
}

func safeAvailableResult(t *testing.T, snapshot domain.SafeAvailableSnapshot) domain.SafeAvailableResult {
	t.Helper()
	result, err := domain.CalculateSafeAvailable(snapshot)
	if err != nil {
		t.Fatalf("CalculateSafeAvailable() error = %v", err)
	}
	return result
}

func safeAvailableEntry(t *testing.T, kind domain.SafeAvailableEntryKind, sourceID string, sequence int, date domain.CivilDate, amountMinor int64) domain.SafeAvailableEntry {
	t.Helper()
	amount := safeAvailableMoney(t, amountMinor)
	entry, err := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{Kind: kind, OwnerID: "owner-1", SourceID: sourceID, Sequence: sequence, DueOn: date, Amount: amount})
	if err != nil {
		t.Fatalf("NewSafeAvailableEntry() error = %v", err)
	}
	return entry
}

func safeAvailableMoney(t *testing.T, minor int64) domain.Money {
	t.Helper()
	money, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	return money
}

func safeAvailablePeriod(t *testing.T, startYear int, startMonth time.Month, startDay int, endYear int, endMonth time.Month, endDay int) domain.SafeAvailablePeriod {
	t.Helper()
	period, err := domain.NewSafeAvailablePeriod(safeAvailableDate(t, startYear, startMonth, startDay), safeAvailableDate(t, endYear, endMonth, endDay))
	if err != nil {
		t.Fatalf("NewSafeAvailablePeriod() error = %v", err)
	}
	return period
}

func safeAvailableDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatalf("NewCivilDate() error = %v", err)
	}
	return date
}
