package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type installmentPlanReaderFake struct {
	plan      domain.InstallmentPlan
	found     bool
	force     bool
	findCalls int
	items     []domain.InstallmentPlan
	err       error
}

func (reader *installmentPlanReaderFake) FindInstallmentPlan(_ context.Context, owner, id string) (application.InstallmentPlanLookup, error) {
	reader.findCalls++
	if reader.err != nil {
		return application.InstallmentPlanLookup{}, reader.err
	}
	if !reader.found || (!reader.force && (owner != reader.plan.OwnerID() || id != reader.plan.ID())) {
		return application.InstallmentPlanLookup{}, nil
	}
	return application.InstallmentPlanLookup{InstallmentPlan: reader.plan, Found: true}, nil
}

func (reader *installmentPlanReaderFake) ListInstallmentPlans(context.Context, string) ([]domain.InstallmentPlan, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	return reader.items, nil
}

type financialDateFake struct {
	date  domain.CivilDate
	calls int
	err   error
}

func (provider *financialDateFake) CurrentFinancialDate(context.Context) (domain.CivilDate, error) {
	provider.calls++
	return provider.date, provider.err
}

type installmentCancelReplayFake struct {
	lookup application.InstallmentPlanCancellationReplayLookup
	calls  int
}

func (replay *installmentCancelReplayFake) FindInstallmentPlanCancellationReplay(context.Context, application.InstallmentPlanCancellationReplayQuery) (application.InstallmentPlanCancellationReplayLookup, error) {
	replay.calls++
	return replay.lookup, nil
}

type installmentCancelStoreFake struct {
	result  application.IdempotentInstallmentPlanCancellationResult
	calls   int
	command application.IdempotentInstallmentPlanCancellationCommand
}

func (store *installmentCancelStoreFake) CancelInstallmentPlan(_ context.Context, command application.IdempotentInstallmentPlanCancellationCommand) (application.IdempotentInstallmentPlanCancellationResult, error) {
	store.calls++
	store.command = command
	if store.result.InstallmentPlan.ID() == "" {
		return application.IdempotentInstallmentPlanCancellationResult{InstallmentPlan: command.InstallmentPlan}, nil
	}
	return store.result, nil
}

func validPlanForApplication(t *testing.T) domain.InstallmentPlan {
	t.Helper()
	amount, _ := domain.NewMoney(300, domain.CurrencyBRL)
	firstDue, _ := domain.NewCivilDate(2026, time.September, 10)
	anchor, _ := domain.NewDayOfMonthAnchor(10)
	plan, err := domain.NewInstallmentPlan(domain.InstallmentPlanParams{ID: "ipl_0123456789abcdef0123456789abcdef", OwnerID: cardPurchaseTestUser, CreditCardID: "card_0123456789abcdef0123456789abcdef", ExpenseID: "expense-plan", TotalAmount: amount, InstallmentCount: 3, FirstDueDate: firstDue, DueDayAnchor: anchor, CreatedAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestInstallmentPlanReadsAreOwnerScopedAndDeterministicallySorted(t *testing.T) {
	plan := validPlanForApplication(t)
	other := plan
	otherPlanID := "ipl_fedcba9876543210fedcba9876543210"
	otherParams := domain.InstallmentPlanRehydrationParams{ID: otherPlanID, OwnerID: plan.OwnerID(), CreditCardID: plan.CreditCardID(), ExpenseID: "expense-plan-2", TotalAmount: plan.TotalAmount(), InstallmentCount: plan.InstallmentCount(), FirstDueDate: mustAppDate(t, 2026, time.August, 10), DueDayAnchor: mustCardAnchor(t, 10), Status: domain.InstallmentPlanStatusActive, CreatedAt: plan.CreatedAt().Add(-time.Hour)}
	other, err := domain.RehydrateInstallmentPlan(otherParams)
	if err != nil {
		t.Fatal(err)
	}
	reader := &installmentPlanReaderFake{items: []domain.InstallmentPlan{plan, other}}
	list, err := application.NewListInstallmentPlans(reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := list.Execute(context.Background(), plan.OwnerID())
	if err != nil || len(result.Items) != 2 {
		t.Fatalf("list = %d/%v", len(result.Items), err)
	}
	if result.Items[0].ID() != other.ID() {
		t.Fatalf("ordering = %s, want %s", result.Items[0].ID(), other.ID())
	}
	get, _ := application.NewGetInstallmentPlan(reader)
	if _, err := get.Execute(context.Background(), application.GetInstallmentPlanInput{OwnerID: "other-owner", PlanID: plan.ID()}); !errors.Is(err, application.ErrInstallmentPlanNotFound) {
		t.Fatalf("cross-owner error = %v", err)
	}
}

func TestCancelInstallmentPlanPreviewAndReplay(t *testing.T) {
	plan := validPlanForApplication(t)
	reader := &installmentPlanReaderFake{plan: plan, found: true}
	date := &financialDateFake{date: mustAppDate(t, 2026, time.August, 31)}
	preview, err := application.NewPreviewInstallmentPlanCancellation(reader, date)
	if err != nil {
		t.Fatal(err)
	}
	previewResult, err := preview.Execute(context.Background(), application.PreviewInstallmentPlanCancellationInput{OwnerID: plan.OwnerID(), PlanID: plan.ID()})
	if err != nil || !previewResult.CancelledOn.Equal(date.date) {
		t.Fatalf("preview = %s/%v", previewResult.CancelledOn, err)
	}
	store := &installmentCancelStoreFake{}
	replay := &installmentCancelReplayFake{}
	cancel, err := application.NewCancelInstallmentPlan(store, replay, reader, date)
	if err != nil {
		t.Fatal(err)
	}
	input := application.CancelInstallmentPlanInput{OwnerID: plan.OwnerID(), PlanID: plan.ID(), ExpectedCancelledOn: date.date, IdempotencyKey: "cancel-plan-key"}
	result, err := cancel.Execute(context.Background(), input)
	if err != nil || result.Replayed || result.InstallmentPlan.Status() != domain.InstallmentPlanStatusCancelled {
		t.Fatalf("cancel = %v/%v", result.InstallmentPlan.Status(), err)
	}
	if store.calls != 1 || date.calls != 2 {
		t.Fatalf("store/date calls = %d/%d", store.calls, date.calls)
	}
	cancelled := result.InstallmentPlan
	reader.plan = cancelled
	replay.lookup = application.InstallmentPlanCancellationReplayLookup{Found: true, InstallmentPlan: cancelled}
	date.date = mustAppDate(t, 2026, time.September, 1)
	replayed, err := cancel.Execute(context.Background(), input)
	if err != nil || !replayed.Replayed || replayed.InstallmentPlan.Status() != domain.InstallmentPlanStatusCancelled {
		t.Fatalf("replay = %v/%v", replayed.Replayed, err)
	}
	if store.calls != 1 || date.calls != 2 {
		t.Fatal("replay consulted mutable state")
	}
	replay.lookup = application.InstallmentPlanCancellationReplayLookup{}
	if _, err := cancel.Execute(context.Background(), application.CancelInstallmentPlanInput{OwnerID: plan.OwnerID(), PlanID: plan.ID(), ExpectedCancelledOn: date.date, IdempotencyKey: "new-key"}); !errors.Is(err, domain.ErrInstallmentPlanAlreadyCancelled) {
		t.Fatalf("new key after cancellation = %v", err)
	}
}

func TestCancelInstallmentPlanRejectsStaleConfirmationBeforeWriting(t *testing.T) {
	plan := validPlanForApplication(t)
	reader := &installmentPlanReaderFake{plan: plan, found: true}
	date := &financialDateFake{date: mustAppDate(t, 2026, time.September, 1)}
	store := &installmentCancelStoreFake{}
	replay := &installmentCancelReplayFake{}
	cancel, err := application.NewCancelInstallmentPlan(store, replay, reader, date)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cancel.Execute(context.Background(), application.CancelInstallmentPlanInput{
		OwnerID: plan.OwnerID(), PlanID: plan.ID(), ExpectedCancelledOn: mustAppDate(t, 2026, time.August, 31), IdempotencyKey: "stale-cancel",
	})
	if !errors.Is(err, application.ErrInstallmentCancellationDateStale) {
		t.Fatalf("stale cancellation error = %v", err)
	}
	if store.calls != 0 || date.calls != 1 {
		t.Fatalf("stale cancellation calls store/date = %d/%d", store.calls, date.calls)
	}
}

func TestInstallmentPlanCancellationRejectsWrongOwnerSnapshot(t *testing.T) {
	plan := validPlanForApplication(t)
	reader := &installmentPlanReaderFake{plan: plan, found: true, force: true}
	date := &financialDateFake{date: mustAppDate(t, 2026, time.August, 31)}
	preview, err := application.NewPreviewInstallmentPlanCancellation(reader, date)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := preview.Execute(context.Background(), application.PreviewInstallmentPlanCancellationInput{OwnerID: "other-owner", PlanID: plan.ID()}); !errors.Is(err, application.ErrInstallmentPlanDependency) {
		t.Fatalf("wrong owner snapshot error = %v", err)
	}
}

func TestLegacyCreditGuardPreservesReplayAndBlocksOnlyNewCredit(t *testing.T) {
	input := application.RecordExpenseInput{Expense: application.CreateExpenseInput{UserID: "user-legacy", Description: "Compra", AmountMinor: 100, Currency: domain.CurrencyBRL, PaymentMethod: domain.PaymentMethodCredit, OccurredAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC), FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS}, IdempotencyKey: "legacy-key"}
	amount, _ := domain.NewMoney(100, domain.CurrencyBRL)
	expense, err := domain.NewExpense(domain.ExpenseParams{ID: "expense-legacy", Details: domain.ExpenseDetails{UserID: input.Expense.UserID, Description: input.Expense.Description, Amount: amount, PaymentMethod: domain.PaymentMethodCredit, OccurredAt: input.Expense.OccurredAt, FinancialTimezone: application.FinancialTimezone, Origin: input.Expense.Origin}, CreatedAt: time.Date(2026, time.September, 1, 12, 0, 0, 123_000_000, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	replay := &legacyExpenseReplayFake{lookup: application.ExpenseReplayLookup{Expense: expense, Found: true}}
	guard, err := application.NewLegacyExpenseCreditGuard(replay)
	if err != nil {
		t.Fatal(err)
	}
	result, err := guard.Check(context.Background(), input)
	if err != nil || !result.Found {
		t.Fatalf("historical replay = %v/%v", result.Found, err)
	}
	replay.lookup = application.ExpenseReplayLookup{}
	if _, err := guard.Check(context.Background(), input); !errors.Is(err, application.ErrCreditCardRequired) {
		t.Fatalf("new CREDIT guard = %v", err)
	}
	input.Expense.PaymentMethod = domain.PaymentMethodPIX
	if _, err := guard.Check(context.Background(), input); err != nil {
		t.Fatalf("PIX legacy path = %v", err)
	}
}

func TestLegacyCreditGuardRejectsCorruptReplaySnapshots(t *testing.T) {
	input := application.RecordExpenseInput{Expense: application.CreateExpenseInput{
		UserID: "user-legacy", Description: "Compra", AmountMinor: 100, Currency: domain.CurrencyBRL,
		PaymentMethod: domain.PaymentMethodCredit, OccurredAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
		FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS,
	}, IdempotencyKey: "legacy-corrupt-key"}
	category := domain.CategoryID("cat_food")
	input.Expense.CategoryID = &category

	type variant struct {
		name   string
		mutate func(*domain.ExpenseDetails)
	}
	variants := []variant{
		{name: "owner", mutate: func(details *domain.ExpenseDetails) { details.UserID = "other-owner" }},
		{name: "payment method", mutate: func(details *domain.ExpenseDetails) { details.PaymentMethod = domain.PaymentMethodPIX }},
		{name: "description", mutate: func(details *domain.ExpenseDetails) { details.Description = "Outra compra" }},
		{name: "amount", mutate: func(details *domain.ExpenseDetails) { details.Amount, _ = domain.NewMoney(101, domain.CurrencyBRL) }},
		{name: "occurred at", mutate: func(details *domain.ExpenseDetails) { details.OccurredAt = details.OccurredAt.Add(time.Hour) }},
		{name: "timezone", mutate: func(details *domain.ExpenseDetails) { details.FinancialTimezone = "UTC" }},
		{name: "origin", mutate: func(details *domain.ExpenseDetails) { details.Origin = domain.OriginWhatsApp }},
		{name: "category", mutate: func(details *domain.ExpenseDetails) {
			other := domain.CategoryID("cat_other")
			details.CategoryID = &other
		}},
		{name: "linked card", mutate: func(details *domain.ExpenseDetails) {
			card := "card_0123456789abcdef0123456789abcdef"
			due := mustAppDate(t, 2026, time.September, 10)
			details.CreditCardID = &card
			details.StatementDueOn = &due
		}},
	}
	for _, tc := range variants {
		t.Run(tc.name, func(t *testing.T) {
			amount, err := domain.NewMoney(input.Expense.AmountMinor, input.Expense.Currency)
			if err != nil {
				t.Fatal(err)
			}
			details := domain.ExpenseDetails{UserID: input.Expense.UserID, Description: input.Expense.Description, Amount: amount, PaymentMethod: input.Expense.PaymentMethod, CategoryID: input.Expense.CategoryID, OccurredAt: input.Expense.OccurredAt, FinancialTimezone: input.Expense.FinancialTimezone, Origin: input.Expense.Origin}
			tc.mutate(&details)
			created, err := domain.NewExpense(domain.ExpenseParams{ID: "expense-corrupt", Details: details, CreatedAt: time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)})
			if err != nil {
				t.Fatalf("variant fixture: %v", err)
			}
			guard, err := application.NewLegacyExpenseCreditGuard(&legacyExpenseReplayFake{lookup: application.ExpenseReplayLookup{Expense: created, Found: true}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := guard.Check(context.Background(), input); !errors.Is(err, application.ErrLegacyExpenseDependency) {
				t.Fatalf("error = %v, want dependency error", err)
			}
		})
	}
}

func TestLegacyCreditGuardPreservesValidReplayWithCanonicalInstant(t *testing.T) {
	input := application.RecordExpenseInput{Expense: application.CreateExpenseInput{
		UserID: "user-legacy", Description: "Compra", AmountMinor: 100, Currency: domain.CurrencyBRL,
		PaymentMethod: domain.PaymentMethodCredit, OccurredAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.FixedZone("fixture", -3*60*60)),
		FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS,
	}, IdempotencyKey: "legacy-canonical-key"}
	amount, _ := domain.NewMoney(100, domain.CurrencyBRL)
	expense, err := domain.NewExpense(domain.ExpenseParams{ID: "expense-canonical", Details: domain.ExpenseDetails{UserID: input.Expense.UserID, Description: input.Expense.Description, Amount: amount, PaymentMethod: domain.PaymentMethodCredit, OccurredAt: input.Expense.OccurredAt, FinancialTimezone: application.FinancialTimezone, Origin: input.Expense.Origin}, CreatedAt: time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	guard, err := application.NewLegacyExpenseCreditGuard(&legacyExpenseReplayFake{lookup: application.ExpenseReplayLookup{Expense: expense, Found: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Check(context.Background(), input); err != nil {
		t.Fatalf("canonical instant replay: %v", err)
	}
}

type legacyExpenseReplayFake struct {
	lookup application.ExpenseReplayLookup
}

func (replay *legacyExpenseReplayFake) FindExpenseReplay(context.Context, application.ExpenseReplayQuery) (application.ExpenseReplayLookup, error) {
	return replay.lookup, nil
}
