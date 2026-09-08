package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestCivilMonthIsStrictAndDeterministic(t *testing.T) {
	month, err := domain.ParseCivilMonth("2026-02")
	if err != nil {
		t.Fatalf("ParseCivilMonth() error = %v", err)
	}
	if month.String() != "2026-02" || month.StartOn().String() != "2026-02-01" || month.EndOn().String() != "2026-02-28" {
		t.Fatalf("month = %q, start %q, end %q", month.String(), month.StartOn(), month.EndOn())
	}
	for _, value := range []string{"", "2026-2", "2026/02", "2026-00", "2026-13", "0000-01", "2026-02x", "２０２６-０２"} {
		if _, err := domain.ParseCivilMonth(value); !errors.Is(err, domain.ErrInvalidCivilMonth) {
			t.Errorf("ParseCivilMonth(%q) error = %v, want ErrInvalidCivilMonth", value, err)
		}
	}
	if _, err := domain.NewCivilMonth(2024, time.February); err != nil {
		t.Fatalf("NewCivilMonth(leap) error = %v", err)
	}
	leap, _ := domain.ParseCivilMonth("2024-02")
	if leap.EndOn().String() != "2024-02-29" {
		t.Fatalf("leap month end = %s, want 2024-02-29", leap.EndOn())
	}
}

func TestMonthlyBudgetValidatesBRLNonNegativeAmountAndOwner(t *testing.T) {
	month, _ := domain.ParseCivilMonth("2026-03")
	zero, _ := domain.NewMoney(0, domain.CurrencyBRL)
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{OwnerID: "owner-1", Month: month, Amount: zero})
	if err != nil {
		t.Fatalf("zero budget error = %v", err)
	}
	if budget.OwnerID() != "owner-1" || budget.Month().String() != "2026-03" || budget.Amount().MinorUnits() != 0 {
		t.Fatalf("budget accessors = %#v", budget)
	}
	negative, _ := domain.NewMoney(-1, domain.CurrencyBRL)
	cases := []struct {
		name   string
		params domain.MonthlyBudgetParams
		want   error
	}{
		{name: "owner", params: domain.MonthlyBudgetParams{Month: month, Amount: zero}, want: domain.ErrInvalidMonthlyBudgetOwnerID},
		{name: "month", params: domain.MonthlyBudgetParams{OwnerID: "owner-1", Amount: zero}, want: domain.ErrInvalidMonthlyBudgetMonth},
		{name: "negative", params: domain.MonthlyBudgetParams{OwnerID: "owner-1", Month: month, Amount: negative}, want: domain.ErrInvalidMonthlyBudgetAmount},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := domain.NewMonthlyBudget(testCase.params)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("NewMonthlyBudget() error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestReplaceMonthlyBudgetIsIdempotentByOwnerAndMonth(t *testing.T) {
	month, _ := domain.ParseCivilMonth("2026-04")
	first := monthlyBudget(t, "owner-b", month, 100)
	second := monthlyBudget(t, "owner-a", month, 200)
	replacement := monthlyBudget(t, "owner-b", month, 300)
	collection, err := domain.ReplaceMonthlyBudget([]domain.MonthlyBudget{first, second}, replacement)
	if err != nil {
		t.Fatalf("ReplaceMonthlyBudget() error = %v", err)
	}
	if len(collection) != 2 || collection[0].OwnerID() != "owner-a" || collection[1].Amount().MinorUnits() != 300 {
		t.Fatalf("replaced collection = %#v", collection)
	}
	again, err := domain.ReplaceMonthlyBudget(collection, replacement)
	if err != nil || len(again) != 2 || !again[1].Equal(replacement) {
		t.Fatalf("repeated replacement = %#v, error %v", again, err)
	}
	duplicate := []domain.MonthlyBudget{first, first}
	if _, err := domain.ReplaceMonthlyBudget(duplicate, replacement); !errors.Is(err, domain.ErrMonthlyBudgetDuplicate) {
		t.Fatalf("duplicate collection error = %v, want ErrMonthlyBudgetDuplicate", err)
	}
	otherMonth, _ := domain.ParseCivilMonth("2026-05")
	other := monthlyBudget(t, "owner-a", otherMonth, 50)
	if _, err := domain.ReplaceMonthlyBudget([]domain.MonthlyBudget{other, other}, replacement); !errors.Is(err, domain.ErrMonthlyBudgetDuplicate) {
		t.Fatalf("unrelated duplicate collection error = %v, want ErrMonthlyBudgetDuplicate", err)
	}
}

func TestCalculateSafeAvailableAppliesMonthlyBudgetCapWithoutChangingNoBudgetContract(t *testing.T) {
	month, _ := domain.ParseCivilMonth("2026-05")
	period := monthlyBudgetPeriod(t, month)
	budget := monthlyBudget(t, "owner-1", month, 80)
	snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: monthlyBudgetMoney(t, 100), Budget: &budget,
		Incomes:     []domain.SafeAvailableEntry{monthlyBudgetEntry(t, domain.SafeAvailableEntryKindIncome, "income-1", period.StartOn(), 10)},
		Expenses:    []domain.SafeAvailableEntry{monthlyBudgetEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", period.StartOn(), 30)},
		Commitments: []domain.SafeAvailableEntry{monthlyBudgetEntry(t, domain.SafeAvailableEntryKindCommitment, "ipl_00000000000000000000000000000001", period.StartOn(), 1, 20)},
	})
	if err != nil {
		t.Fatalf("NewSafeAvailableSnapshot() error = %v", err)
	}
	result, err := domain.CalculateSafeAvailable(snapshot)
	if err != nil {
		t.Fatalf("CalculateSafeAvailable() error = %v", err)
	}
	// Financial amount is 100 + 10 - 30 - 20 = 60. The budget cap is
	// 80 - 30 - 20 = 30, so final amount is the smaller value.
	if result.FinalAmount().MinorUnits() != 30 {
		t.Fatalf("final amount = %d, want 30", result.FinalAmount().MinorUnits())
	}
	remaining, ok := result.BudgetRemaining()
	if !ok || remaining.MinorUnits() != 30 {
		t.Fatalf("budget remaining = %d/%v, want 30/true", remaining.MinorUnits(), ok)
	}
	if _, ok := result.Budget(); !ok || len(result.MissingData()) != 0 {
		t.Fatalf("budget result = present %v, missing %#v", ok, result.MissingData())
	}
	if result.TotalConfirmedIncome().MinorUnits() != 10 || result.TotalConfirmedExpense().MinorUnits() != 30 || result.TotalConfirmedCommitments().MinorUnits() != 20 {
		t.Fatalf("budget component totals = %d/%d/%d, want 10/30/20", result.TotalConfirmedIncome().MinorUnits(), result.TotalConfirmedExpense().MinorUnits(), result.TotalConfirmedCommitments().MinorUnits())
	}
	breakdown := result.Breakdown()
	if len(breakdown) != 4 || breakdown[0].Kind() != domain.SafeAvailableEntryKindAvailableBalance || breakdown[1].Kind() != domain.SafeAvailableEntryKindIncome || breakdown[2].Kind() != domain.SafeAvailableEntryKindExpense || breakdown[3].Kind() != domain.SafeAvailableEntryKindCommitment {
		t.Fatalf("budget breakdown = %#v, want deterministic balance/income/expense/commitment", breakdown)
	}
	noBudget, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: monthlyBudgetMoney(t, 100)})
	if err != nil {
		t.Fatal(err)
	}
	noBudgetResult, err := domain.CalculateSafeAvailable(noBudget)
	noBudgetRemaining, hasNoBudgetRemaining := noBudgetResult.BudgetRemaining()
	if err != nil || noBudgetResult.FinalAmount().MinorUnits() != 100 || hasNoBudgetRemaining || noBudgetRemaining.MinorUnits() != 0 {
		t.Fatalf("no-budget result = %d/%d/%v, error %v", noBudgetResult.FinalAmount().MinorUnits(), noBudgetRemaining.MinorUnits(), hasNoBudgetRemaining, err)
	}
}

func TestCalculateSafeAvailableSupportsZeroAndNegativeBudgetRemaining(t *testing.T) {
	month, _ := domain.ParseCivilMonth("2026-06")
	period := monthlyBudgetPeriod(t, month)
	for _, testCase := range []struct {
		name    string
		want    int64
		budget  int64
		expense int64
	}{
		{name: "zero", budget: 50, expense: 50, want: 0},
		{name: "negative", budget: 40, expense: 50, want: -10},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			budget := monthlyBudget(t, "owner-1", month, testCase.budget)
			snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{
				OwnerID: "owner-1", Period: period, AvailableBalance: monthlyBudgetMoney(t, 100), Budget: &budget,
				Expenses: []domain.SafeAvailableEntry{monthlyBudgetEntry(t, domain.SafeAvailableEntryKindExpense, "expense-1", period.StartOn(), testCase.expense)},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := domain.CalculateSafeAvailable(snapshot)
			if err != nil || result.FinalAmount().MinorUnits() != testCase.want {
				t.Fatalf("result = %d, error %v, want %d", result.FinalAmount().MinorUnits(), err, testCase.want)
			}
		})
	}
}

func TestMonthlyBudgetRejectsInconsistentSnapshotAndOverflow(t *testing.T) {
	month, _ := domain.ParseCivilMonth("2026-07")
	period := monthlyBudgetPeriod(t, month)
	budget := monthlyBudget(t, "owner-1", month, 0)
	foreign := monthlyBudget(t, "owner-2", month, 100)
	if _, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: monthlyBudgetMoney(t, 1), Budget: &foreign}); !errors.Is(err, domain.ErrInvalidSafeAvailableBudget) {
		t.Fatalf("foreign budget error = %v", err)
	}
	otherMonth, _ := domain.ParseCivilMonth("2026-08")
	otherBudget := monthlyBudget(t, "owner-1", otherMonth, 100)
	if _, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: monthlyBudgetMoney(t, 1), Budget: &otherBudget}); !errors.Is(err, domain.ErrSafeAvailableBudgetPeriodMismatch) {
		t.Fatalf("cross-month budget error = %v", err)
	}
	if _, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: monthlyBudgetMoney(t, 1), Budget: &budget, MissingData: []domain.SafeAvailableMissingData{domain.SafeAvailableMissingBudget}}); !errors.Is(err, domain.ErrInvalidSafeAvailableSnapshot) {
		t.Fatalf("budget marker error = %v", err)
	}
	first := monthlyBudgetEntry(t, domain.SafeAvailableEntryKindExpense, "expense-max", period.StartOn(), math.MaxInt64)
	second := monthlyBudgetEntry(t, domain.SafeAvailableEntryKindCommitment, "ipl_00000000000000000000000000000002", period.StartOn(), 1, 2)
	snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: "owner-1", Period: period, AvailableBalance: monthlyBudgetMoney(t, 0), Budget: &budget, Expenses: []domain.SafeAvailableEntry{first}, Commitments: []domain.SafeAvailableEntry{second}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.CalculateSafeAvailable(snapshot); !errors.Is(err, domain.ErrSafeAvailableOverflow) {
		t.Fatalf("budget subtraction overflow = %v, want ErrSafeAvailableOverflow", err)
	}
}

func monthlyBudget(t *testing.T, owner string, month domain.CivilMonth, amount int64) domain.MonthlyBudget {
	t.Helper()
	money := monthlyBudgetMoney(t, amount)
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{OwnerID: owner, Month: month, Amount: money})
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func monthlyBudgetMoney(t *testing.T, minor int64) domain.Money {
	t.Helper()
	money, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func monthlyBudgetPeriod(t *testing.T, month domain.CivilMonth) domain.SafeAvailablePeriod {
	t.Helper()
	period, err := domain.NewSafeAvailablePeriod(month.StartOn(), month.EndOn())
	if err != nil {
		t.Fatal(err)
	}
	return period
}

func monthlyBudgetEntry(t *testing.T, kind domain.SafeAvailableEntryKind, sourceID string, date domain.CivilDate, values ...int64) domain.SafeAvailableEntry {
	t.Helper()
	sequence := int64(0)
	amount := values[0]
	if kind == domain.SafeAvailableEntryKindCommitment {
		sequence = values[0]
		amount = values[1]
	}
	entry, err := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{
		Kind: kind, OwnerID: "owner-1", SourceID: sourceID, Sequence: int(sequence), DueOn: date, Amount: monthlyBudgetMoney(t, amount),
	})
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
