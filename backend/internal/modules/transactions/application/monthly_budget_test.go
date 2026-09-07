package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type monthlyBudgetStoreFake struct {
	budgets []domain.MonthlyBudget
	err     error
	calls   int
}

func (store *monthlyBudgetStoreFake) ReplaceMonthlyBudget(_ context.Context, budget domain.MonthlyBudget) error {
	store.calls++
	if store.err != nil {
		return store.err
	}
	var err error
	store.budgets, err = domain.ReplaceMonthlyBudget(store.budgets, budget)
	return err
}

func TestSetMonthlyBudgetValidatesAndReplacesOwnerMonth(t *testing.T) {
	month := appMonthlyBudgetMonth(t, "2026-08")
	store := &monthlyBudgetStoreFake{}
	useCase, err := application.NewSetMonthlyBudget(store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := useCase.Execute(context.Background(), application.SetMonthlyBudgetInput{OwnerID: "owner-1", Month: month, Amount: appMonthlyBudgetMoney(t, 100)})
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	second, err := useCase.Execute(context.Background(), application.SetMonthlyBudgetInput{OwnerID: "owner-1", Month: month, Amount: appMonthlyBudgetMoney(t, 250)})
	if err != nil {
		t.Fatalf("replacement Execute() error = %v", err)
	}
	if store.calls != 2 || len(store.budgets) != 1 || !first.Budget.Month().Equal(second.Budget.Month()) || store.budgets[0].Amount().MinorUnits() != 250 {
		t.Fatalf("calls/budgets = %d/%#v", store.calls, store.budgets)
	}
}

func TestSetMonthlyBudgetRejectsInvalidInputBeforeStore(t *testing.T) {
	store := &monthlyBudgetStoreFake{}
	useCase, err := application.NewSetMonthlyBudget(store)
	if err != nil {
		t.Fatal(err)
	}
	validMonth := appMonthlyBudgetMonth(t, "2026-09")
	negative := appMonthlyBudgetMoney(t, -1)
	cases := []struct {
		name  string
		input application.SetMonthlyBudgetInput
		want  error
	}{
		{name: "owner", input: application.SetMonthlyBudgetInput{Month: validMonth, Amount: appMonthlyBudgetMoney(t, 1)}, want: application.ErrInvalidMonthlyBudgetOwnerID},
		{name: "month", input: application.SetMonthlyBudgetInput{OwnerID: "owner-1", Amount: appMonthlyBudgetMoney(t, 1)}, want: application.ErrInvalidMonthlyBudgetMonth},
		{name: "negative", input: application.SetMonthlyBudgetInput{OwnerID: "owner-1", Month: validMonth, Amount: negative}, want: application.ErrInvalidMonthlyBudgetAmount},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := useCase.Execute(context.Background(), testCase.input)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Execute() error = %v, want %v", err, testCase.want)
			}
		})
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
}

func TestSetMonthlyBudgetPreservesContextAndSanitizesPersistence(t *testing.T) {
	month := appMonthlyBudgetMonth(t, "2026-10")
	store := &monthlyBudgetStoreFake{}
	useCase, _ := application.NewSetMonthlyBudget(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(ctx, application.SetMonthlyBudgetInput{OwnerID: "owner-1", Month: month, Amount: appMonthlyBudgetMoney(t, 1)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Execute() error = %v, want context.Canceled", err)
	}
	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer deadlineCancel()
	if _, err := useCase.Execute(deadlineCtx, application.SetMonthlyBudgetInput{OwnerID: "owner-1", Month: month, Amount: appMonthlyBudgetMoney(t, 1)}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline Execute() error = %v, want context.DeadlineExceeded", err)
	}
	store = &monthlyBudgetStoreFake{err: errors.New("INSERT monthly_budgets owner=owner-1 DSN=secret")}
	useCase, _ = application.NewSetMonthlyBudget(store)
	_, err := useCase.Execute(context.Background(), application.SetMonthlyBudgetInput{OwnerID: "owner-1", Month: month, Amount: appMonthlyBudgetMoney(t, 1)})
	if !errors.Is(err, application.ErrMonthlyBudgetPersistence) || err.Error() != application.ErrMonthlyBudgetPersistence.Error() {
		t.Fatalf("persistence error = %v, want sanitized error", err)
	}

	deadlineStore := &monthlyBudgetStoreFake{err: context.DeadlineExceeded}
	deadlineUseCase, _ := application.NewSetMonthlyBudget(deadlineStore)
	deadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	_, err = deadlineUseCase.Execute(deadline, application.SetMonthlyBudgetInput{OwnerID: "owner-1", Month: month, Amount: appMonthlyBudgetMoney(t, 1)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline store error = %v, want context.DeadlineExceeded", err)
	}
}

func TestCalculateSafeAvailableUsesSuppliedBudgetWithoutChangingReaderBoundary(t *testing.T) {
	month := appMonthlyBudgetMonth(t, "2026-11")
	period := appMonthlyBudgetPeriod(t, month)
	budget := appMonthlyBudget(t, "owner-1", month, 100)
	snapshot := appSafeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: appMonthlyBudgetMoney(t, 500), Budget: &budget,
		Expenses: []domain.SafeAvailableEntry{appSafeAvailableEntry(t, domain.SafeAvailableEntryKindExpense, "expense-budget", 0, period.StartOn(), 60)},
	})
	reader := &safeAvailableReaderFake{snapshot: snapshot}
	useCase, err := application.NewCalculateSafeAvailable(reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Execute(context.Background(), application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if reader.calls != 1 || result.Available.FinalAmount().MinorUnits() != 40 {
		t.Fatalf("reader calls/final = %d/%d, want 1/40", reader.calls, result.Available.FinalAmount().MinorUnits())
	}
}

func appMonthlyBudget(t *testing.T, owner string, month domain.CivilMonth, amount int64) domain.MonthlyBudget {
	t.Helper()
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{OwnerID: owner, Month: month, Amount: appMonthlyBudgetMoney(t, amount)})
	if err != nil {
		t.Fatal(err)
	}
	return budget
}

func appMonthlyBudgetMonth(t *testing.T, value string) domain.CivilMonth {
	t.Helper()
	month, err := domain.ParseCivilMonth(value)
	if err != nil {
		t.Fatal(err)
	}
	return month
}

func appMonthlyBudgetMoney(t *testing.T, minor int64) domain.Money {
	t.Helper()
	money, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func appMonthlyBudgetPeriod(t *testing.T, month domain.CivilMonth) domain.SafeAvailablePeriod {
	t.Helper()
	period, err := domain.NewSafeAvailablePeriod(month.StartOn(), month.EndOn())
	if err != nil {
		t.Fatal(err)
	}
	return period
}
