package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const (
	installmentPlanTestOwner = "user-installments"
	installmentPlanTestCard  = "card_0123456789abcdef0123456789abcdef"
	installmentPlanTestID    = "ipl_0123456789abcdef0123456789abcdef"
	installmentPlanExpense   = "expense-installments"
)

func mustPlanDate(t testing.TB, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatalf("NewCivilDate(%d-%02d-%02d): %v", year, month, day, err)
	}
	return date
}

func mustAnchor(t *testing.T, day int) domain.DayOfMonthAnchor {
	t.Helper()
	anchor, err := domain.NewDayOfMonthAnchor(day)
	if err != nil {
		t.Fatalf("NewDayOfMonthAnchor(%d): %v", day, err)
	}
	return anchor
}

func TestCalculateCardCycleUsesOriginalAnchorsAcrossBoundaries(t *testing.T) {
	closing := mustAnchor(t, 31)
	due := mustAnchor(t, 10)
	tests := []struct {
		name     string
		purchase domain.CivilDate
		closing  string
		due      string
	}{
		{"before closing", mustPlanDate(t, 2026, time.February, 27), "2026-02-28", "2026-03-10"},
		{"on clamped closing", mustPlanDate(t, 2026, time.February, 28), "2026-03-31", "2026-04-10"},
		{"leap year month end", mustPlanDate(t, 2024, time.February, 28), "2024-02-29", "2024-03-10"},
		{"year boundary", mustPlanDate(t, 2026, time.December, 31), "2027-01-31", "2027-02-10"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cycle, err := domain.CalculateCardCycle(test.purchase, closing, due)
			if err != nil {
				t.Fatalf("CalculateCardCycle(): %v", err)
			}
			if cycle.StatementClosingOn().String() != test.closing || cycle.StatementDueOn().String() != test.due {
				t.Fatalf("cycle = %s/%s, want %s/%s", cycle.StatementClosingOn(), cycle.StatementDueOn(), test.closing, test.due)
			}
		})
	}
}

func TestCalculateCardCycleDueAnchorAdvancesWhenNotStrictlyAfterClosing(t *testing.T) {
	purchase := mustPlanDate(t, 2026, time.August, 12)
	closing := mustAnchor(t, 15)
	for _, dueDay := range []int{10, 15} {
		cycle, err := domain.CalculateCardCycle(purchase, closing, mustAnchor(t, dueDay))
		if err != nil {
			t.Fatalf("due %d: %v", dueDay, err)
		}
		if !cycle.StatementClosingOn().Before(cycle.StatementDueOn()) || cycle.StatementDueOn().Month() != time.September {
			t.Fatalf("due %d produced non-advanced date: %s/%s", dueDay, cycle.StatementClosingOn(), cycle.StatementDueOn())
		}
	}
	sameMonth, err := domain.CalculateCardCycle(purchase, closing, mustAnchor(t, 20))
	if err != nil || sameMonth.StatementClosingOn().String() != "2026-08-15" || sameMonth.StatementDueOn().String() != "2026-08-20" {
		t.Fatalf("due after closing = %s/%s/%v", sameMonth.StatementClosingOn(), sameMonth.StatementDueOn(), err)
	}
}

func TestCalculateCardCycleDistinguishesBeforeOnAndAfterClosing(t *testing.T) {
	closing := mustAnchor(t, 25)
	due := mustAnchor(t, 10)
	tests := []struct {
		purchase  string
		wantClose string
	}{
		{"2026-08-24", "2026-08-25"},
		{"2026-08-25", "2026-09-25"},
		{"2026-08-26", "2026-09-25"},
	}
	for _, test := range tests {
		purchase, _ := time.Parse("2006-01-02", test.purchase)
		civil := mustPlanDate(t, purchase.Year(), purchase.Month(), purchase.Day())
		cycle, err := domain.CalculateCardCycle(civil, closing, due)
		if err != nil || cycle.StatementClosingOn().String() != test.wantClose {
			t.Fatalf("purchase %s cycle = %s/%v, want %s", test.purchase, cycle.StatementClosingOn(), err, test.wantClose)
		}
	}
}

func TestAnchoredCivilDatePreservesAnchorWhenMonthsAreShort(t *testing.T) {
	base := mustPlanDate(t, 2024, time.January, 31)
	anchor := mustAnchor(t, 31)
	for offset, want := range map[int]string{1: "2024-02-29", 2: "2024-03-31", 13: "2025-02-28"} {
		got, err := domain.AnchoredCivilDateAtMonthOffset(base, offset, anchor)
		if err != nil || got.String() != want {
			t.Fatalf("offset %d = %s/%v, want %s", offset, got, err, want)
		}
	}
	if _, err := domain.AnchoredCivilDateAtMonthOffset(mustPlanDate(t, 9999, time.December, 31), 1, anchor); !errors.Is(err, domain.ErrInvalidInstallmentMonthOffset) {
		t.Fatalf("overflow error = %v", err)
	}
}

func validInstallmentPlanParams(t *testing.T) domain.InstallmentPlanParams {
	t.Helper()
	amount, err := domain.NewMoney(100, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return domain.InstallmentPlanParams{
		ID: installmentPlanTestID, OwnerID: installmentPlanTestOwner, CreditCardID: installmentPlanTestCard,
		ExpenseID: installmentPlanExpense, TotalAmount: amount, InstallmentCount: 3,
		FirstDueDate: mustPlanDate(t, 2026, time.January, 31), DueDayAnchor: mustAnchor(t, 31),
		CreatedAt: time.Date(2026, time.January, 1, 12, 0, 0, 123_000, time.UTC),
	}
}

func TestInstallmentPlanDistributionAndEffectiveSchedule(t *testing.T) {
	params := validInstallmentPlanParams(t)
	plan, err := domain.NewInstallmentPlan(params)
	if err != nil {
		t.Fatalf("NewInstallmentPlan(): %v", err)
	}
	schedule, err := plan.Schedule()
	if err != nil || len(schedule) != 3 {
		t.Fatalf("Schedule() = %d/%v", len(schedule), err)
	}
	wantAmounts := []int64{33, 33, 34}
	wantDates := []string{"2026-01-31", "2026-02-28", "2026-03-31"}
	for index, item := range schedule {
		if item.Number() != index+1 || item.TotalCount() != 3 || item.Amount().MinorUnits() != wantAmounts[index] || item.DueDate().String() != wantDates[index] {
			t.Fatalf("installment %d = %d/%s/%d/%d", index, item.Amount().MinorUnits(), item.DueDate(), item.Number(), item.TotalCount())
		}
	}
	cancelled, err := plan.Cancel(mustPlanDate(t, 2026, time.February, 28))
	if err != nil {
		t.Fatalf("Cancel(): %v", err)
	}
	effective, err := cancelled.EffectiveSchedule()
	if err != nil || len(effective) != 2 {
		t.Fatalf("EffectiveSchedule() = %d/%v", len(effective), err)
	}
	if len(schedule) != 3 || plan.Status() != domain.InstallmentPlanStatusActive {
		t.Fatal("cancel mutated the original plan or schedule")
	}
	if _, err := cancelled.Cancel(mustPlanDate(t, 2026, time.March, 1)); !errors.Is(err, domain.ErrInstallmentPlanAlreadyCancelled) {
		t.Fatalf("repeated cancellation error = %v", err)
	}
}

func TestInstallmentPlanRejectsInvalidBoundariesAndStates(t *testing.T) {
	for _, count := range []int{1, 0, -1, 121} {
		params := validInstallmentPlanParams(t)
		params.InstallmentCount = count
		if _, err := domain.NewInstallmentPlan(params); !errors.Is(err, domain.ErrInvalidInstallmentCount) {
			t.Fatalf("count %d error = %v", count, err)
		}
	}
	for _, amount := range []int64{0, 2, -1} {
		params := validInstallmentPlanParams(t)
		params.TotalAmount, _ = domain.NewMoney(amount, domain.CurrencyBRL)
		if _, err := domain.NewInstallmentPlan(params); !errors.Is(err, domain.ErrInvalidInstallmentTotalAmount) {
			t.Fatalf("amount %d error = %v", amount, err)
		}
	}
	params := validInstallmentPlanParams(t)
	params.FirstDueDate = mustPlanDate(t, 2026, time.February, 27)
	params.DueDayAnchor = mustAnchor(t, 31)
	if _, err := domain.NewInstallmentPlan(params); !errors.Is(err, domain.ErrInvalidInstallmentFirstDueDate) {
		t.Fatalf("uncanonical first date error = %v", err)
	}
	params = validInstallmentPlanParams(t)
	if _, err := domain.RehydrateInstallmentPlan(domain.InstallmentPlanRehydrationParams{ID: params.ID, OwnerID: params.OwnerID, CreditCardID: params.CreditCardID, ExpenseID: params.ExpenseID, TotalAmount: params.TotalAmount, InstallmentCount: params.InstallmentCount, FirstDueDate: params.FirstDueDate, DueDayAnchor: params.DueDayAnchor, Status: domain.InstallmentPlanStatusCancelled, CreatedAt: params.CreatedAt}); !errors.Is(err, domain.ErrInvalidInstallmentPlanState) {
		t.Fatalf("cancelled without cutoff error = %v", err)
	}
}

func TestExpenseCreditCardLinkIsAtomicAndLegacyCreditRemainsValid(t *testing.T) {
	amount, _ := domain.NewMoney(100, domain.CurrencyBRL)
	base := domain.ExpenseDetails{UserID: "user-expense", Description: "Compra", Amount: amount, PaymentMethod: domain.PaymentMethodCredit, OccurredAt: time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC), FinancialTimezone: "America/Sao_Paulo", Origin: domain.OriginIOS}
	if err := domain.ValidateExpenseDetails(base); err != nil {
		t.Fatalf("legacy CREDIT should remain valid: %v", err)
	}
	due := mustPlanDate(t, 2026, time.February, 10)
	card := installmentPlanTestCard
	base.CreditCardID = &card
	base.StatementDueOn = &due
	if err := domain.ValidateExpenseDetails(base); err != nil {
		t.Fatalf("linked CREDIT: %v", err)
	}
	base.StatementDueOn = nil
	if err := domain.ValidateExpenseDetails(base); !errors.Is(err, domain.ErrInvalidExpenseCreditCardLink) {
		t.Fatalf("partial link error = %v", err)
	}
	base.StatementDueOn = &due
	base.PaymentMethod = domain.PaymentMethodPIX
	if err := domain.ValidateExpenseDetails(base); !errors.Is(err, domain.ErrInvalidExpenseCreditCardLink) {
		t.Fatalf("non-CREDIT link error = %v", err)
	}
}

func TestInstallmentScheduleHandlesLargeExactValues(t *testing.T) {
	amount, _ := domain.NewMoney(math.MaxInt64, domain.CurrencyBRL)
	schedule, err := domain.BuildInstallmentSchedule(amount, 120, mustPlanDate(t, 2026, time.January, 31), mustAnchor(t, 31))
	if err != nil || len(schedule) != 120 {
		t.Fatalf("max schedule = %d/%v", len(schedule), err)
	}
	var sum int64
	for _, item := range schedule {
		if item.Amount().MinorUnits() > 0 && math.MaxInt64-sum >= item.Amount().MinorUnits() {
			sum += item.Amount().MinorUnits()
		} else {
			t.Fatal("schedule overflow")
		}
	}
	if sum != math.MaxInt64 {
		t.Fatalf("schedule sum = %d, want %d", sum, int64(math.MaxInt64))
	}
}
