package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestPreviewIncomeReturnsCanonicalDataWithoutDependencies(t *testing.T) {
	input := validIncomeInput()
	input.Description = "  Receita sintética  "
	input.OccurredAt = time.Date(2026, time.August, 14, 12, 0, 0, 123_456_789, time.FixedZone("minus-three", -3*60*60))

	result, err := (application.PreviewIncome{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Details.Description != "Receita sintética" {
		t.Fatalf("description = %q", result.Details.Description)
	}
	expected := time.Date(2026, time.August, 14, 15, 0, 0, 123_456_000, time.UTC)
	if result.Details.OccurredAt != expected || result.Details.OccurredAt.Location() != time.UTC {
		t.Fatalf("occurred-at = %s, want %s", result.Details.OccurredAt.Format(time.RFC3339Nano), expected.Format(time.RFC3339Nano))
	}
	if result.Details.Amount.MinorUnits() != input.AmountMinor || result.Details.Amount.Currency() != domain.CurrencyBRL {
		t.Fatal("preview did not preserve exact positive BRL minor units")
	}
}

func TestPreviewIncomeCanonicalizesFinancialInstantsToUTCMicroseconds(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{name: "no fraction", input: time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC), expected: time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC)},
		{name: "milliseconds", input: time.Date(2026, 8, 14, 15, 0, 0, 123_000_000, time.UTC), expected: time.Date(2026, 8, 14, 15, 0, 0, 123_000_000, time.UTC)},
		{name: "microseconds", input: time.Date(2026, 8, 14, 15, 0, 0, 123_456_000, time.UTC), expected: time.Date(2026, 8, 14, 15, 0, 0, 123_456_000, time.UTC)},
		{name: "sub-microsecond", input: time.Date(2026, 8, 14, 15, 0, 0, 999, time.UTC), expected: time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC)},
		{name: "nanoseconds", input: time.Date(2026, 8, 14, 15, 0, 0, 123_456_789, time.UTC), expected: time.Date(2026, 8, 14, 15, 0, 0, 123_456_000, time.UTC)},
		{name: "equivalent offset", input: time.Date(2026, 8, 14, 12, 0, 0, 123_456_789, time.FixedZone("minus-three", -3*60*60)), expected: time.Date(2026, 8, 14, 15, 0, 0, 123_456_000, time.UTC)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validIncomeInput()
			input.OccurredAt = test.input
			result, err := (application.PreviewIncome{}).Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.Details.OccurredAt != test.expected {
				t.Fatalf("occurred-at = %s, want %s", result.Details.OccurredAt.Format(time.RFC3339Nano), test.expected.Format(time.RFC3339Nano))
			}
		})
	}
}

func TestPreviewIncomeRejectsInvalidInputAndCanceledContext(t *testing.T) {
	invalid := validIncomeInput()
	invalid.AmountMinor = 0
	if _, err := (application.PreviewIncome{}).Execute(context.Background(), invalid); !errors.Is(err, domain.ErrInvalidIncomeAmount) {
		t.Fatalf("Execute() error = %v, want ErrInvalidIncomeAmount", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (application.PreviewIncome{}).Execute(ctx, validIncomeInput()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(canceled) error = %v, want context.Canceled", err)
	}
}

func TestPreviewAndRecordIncomeUseTheSameCanonicalSemantics(t *testing.T) {
	input := validIncomeInput()
	input.Description = "  Receita sintética canônica  "
	input.OccurredAt = time.Date(2026, 8, 14, 12, 0, 0, 123_456_789, time.FixedZone("minus-three", -3*60*60))
	preview, err := (application.PreviewIncome{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("PreviewIncome.Execute() error = %v", err)
	}

	store := &recordingIncomeCommandStore{}
	useCase := mustRecordIncome(t, store, &fixedIncomeIDGenerator{id: "income-preview-record"}, &fixedClock{
		now: time.Date(2026, 8, 14, 18, 0, 0, 123_456_789, time.UTC),
	})
	if _, err := useCase.Execute(context.Background(), application.RecordIncomeInput{Income: input, IdempotencyKey: "preview-record-key"}); err != nil {
		t.Fatalf("RecordIncome.Execute() error = %v", err)
	}

	income := store.command.Income
	if income.Description() != preview.Details.Description ||
		!income.Amount().Equal(preview.Details.Amount) ||
		income.OccurredAt() != preview.Details.OccurredAt ||
		income.FinancialTimezone() != preview.Details.FinancialTimezone ||
		income.Origin() != preview.Details.Origin {
		t.Fatal("preview and record produced different canonical Income semantics")
	}
}

func TestRecordIncomeBuildsCanonicalIdempotentCommand(t *testing.T) {
	store := &recordingIncomeCommandStore{}
	idGenerator := &fixedIncomeIDGenerator{id: "income-synthetic-record-001"}
	clock := &fixedClock{now: time.Date(2026, time.August, 14, 18, 0, 0, 123_456_789, time.UTC)}
	useCase := mustRecordIncome(t, store, idGenerator, clock)
	input := application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "synthetic-income-001"}
	input.Income.Description = "  Receita sintética  "

	result, err := useCase.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.calls != 1 || idGenerator.calls != 1 || clock.calls != 1 {
		t.Fatalf("calls store/id/clock = %d/%d/%d, want 1/1/1", store.calls, idGenerator.calls, clock.calls)
	}
	if store.command.Operation != application.IdempotencyOperationCreateIncome {
		t.Fatalf("operation = %q", store.command.Operation)
	}
	if store.command.IdempotencyKey != input.IdempotencyKey || store.command.Income.Type() != domain.TransactionTypeIncome {
		t.Fatal("command lost idempotency key or Income type")
	}
	if store.command.Income.Description() != "Receita sintética" {
		t.Fatal("command did not carry the normalized description")
	}
	expectedCreatedAt := time.Date(2026, time.August, 14, 18, 0, 0, 123_456_000, time.UTC)
	if store.command.Income.CreatedAt() != expectedCreatedAt || store.command.Income.UpdatedAt() != expectedCreatedAt {
		t.Fatal("command timestamps were not canonicalized to UTC microseconds")
	}
	if result.Income.ID() != idGenerator.id || result.Replayed {
		t.Fatal("result did not return the newly prepared Income")
	}
}

func TestRecordIncomeFingerprintUsesNormalizedSemantics(t *testing.T) {
	first := validIncomeInput()
	first.Description = " Receita sintética "
	first.OccurredAt = time.Date(2026, 8, 14, 12, 0, 0, 123, time.FixedZone("minus-three", -3*60*60))
	second := validIncomeInput()
	second.Description = "Receita sintética"
	second.OccurredAt = time.Date(2026, 8, 14, 15, 0, 0, 999, time.UTC)

	firstFingerprint := incomeFingerprintFor(t, "income-fingerprint-a", first)
	secondFingerprint := incomeFingerprintFor(t, "income-fingerprint-b", second)
	if firstFingerprint != secondFingerprint {
		t.Fatal("semantically equivalent Income inputs produced different fingerprints")
	}
}

func TestRecordIncomeFingerprintChangesWithEverySemanticField(t *testing.T) {
	base := validIncomeInput()
	baseFingerprint := incomeFingerprintFor(t, "income-fingerprint-base", base)
	tests := []struct {
		name   string
		mutate func(*application.CreateIncomeInput)
	}{
		{name: "description", mutate: func(input *application.CreateIncomeInput) { input.Description = "Outra receita sintética" }},
		{name: "amount", mutate: func(input *application.CreateIncomeInput) { input.AmountMinor++ }},
		{name: "occurred at", mutate: func(input *application.CreateIncomeInput) { input.OccurredAt = input.OccurredAt.Add(time.Microsecond) }},
		{name: "timezone", mutate: func(input *application.CreateIncomeInput) { input.FinancialTimezone = "UTC" }},
		{name: "origin", mutate: func(input *application.CreateIncomeInput) { input.Origin = domain.OriginWhatsApp }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.mutate(&changed)
			if got := incomeFingerprintFor(t, "income-fingerprint-"+test.name, changed); got == baseFingerprint {
				t.Fatal("semantic change did not change the Income fingerprint")
			}
		})
	}
}

func TestIncomeAndExpenseFingerprintsAreDomainSeparated(t *testing.T) {
	incomeInput := validIncomeInput()
	incomeFingerprint := incomeFingerprintFor(t, "income-domain-separated", incomeInput)

	expenseStore := &recordingCommandStore{}
	expenseUseCase := mustRecordExpense(t, expenseStore, &fixedIDGenerator{id: "expense-domain-separated"}, &fixedClock{
		now: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC),
	})
	expenseInput := validInput()
	expenseInput.Description = incomeInput.Description
	expenseInput.AmountMinor = incomeInput.AmountMinor
	expenseInput.OccurredAt = incomeInput.OccurredAt
	if _, err := expenseUseCase.Execute(context.Background(), application.RecordExpenseInput{
		Expense: expenseInput, IdempotencyKey: "same-textual-key",
	}); err != nil {
		t.Fatalf("RecordExpense.Execute() error = %v", err)
	}

	if incomeFingerprint == expenseStore.command.Fingerprint {
		t.Fatal("Income and Expense produced the same fingerprint")
	}
}

func TestRecordIncomeRejectsBeforeGeneratedValuesAndStore(t *testing.T) {
	invalidAmount := validIncomeInput()
	invalidAmount.AmountMinor = 0
	invalidCurrency := validIncomeInput()
	invalidCurrency.Currency = domain.Currency("USD")
	tests := []struct {
		name   string
		input  application.RecordIncomeInput
		wanted error
	}{
		{name: "missing key", input: application.RecordIncomeInput{Income: validIncomeInput()}, wanted: application.ErrIncomeIdempotencyKeyRequired},
		{name: "invalid key", input: application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "invalid key"}, wanted: application.ErrIncomeIdempotencyKeyInvalid},
		{name: "key above limit", input: application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: strings.Repeat("k", domain.MaxIdentifierBytes+1)}, wanted: application.ErrIncomeIdempotencyKeyInvalid},
		{name: "invalid income", input: application.RecordIncomeInput{Income: invalidAmount, IdempotencyKey: "valid-key"}, wanted: domain.ErrInvalidIncomeAmount},
		{name: "unsupported currency", input: application.RecordIncomeInput{Income: invalidCurrency, IdempotencyKey: "valid-key"}, wanted: domain.ErrUnsupportedCurrency},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingIncomeCommandStore{}
			idGenerator := &fixedIncomeIDGenerator{id: "unused-income-id"}
			clock := &fixedClock{now: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)}
			useCase := mustRecordIncome(t, store, idGenerator, clock)
			_, err := useCase.Execute(context.Background(), test.input)
			if !errors.Is(err, test.wanted) {
				t.Fatalf("Execute() error = %v, want %v", err, test.wanted)
			}
			if store.calls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
				t.Fatalf("calls store/id/clock = %d/%d/%d, want zero", store.calls, idGenerator.calls, clock.calls)
			}
		})
	}
}

func TestRecordIncomeReturnsReplayAndConflict(t *testing.T) {
	replayedIncome := mustSyntheticIncome(t, "income-original")
	store := &recordingIncomeCommandStore{result: application.IdempotentIncomeResult{Income: replayedIncome, Replayed: true}}
	useCase := mustRecordIncome(t, store, &fixedIncomeIDGenerator{id: "income-unused-replay"}, &fixedClock{
		now: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC),
	})
	result, err := useCase.Execute(context.Background(), application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "replay-key"})
	if err != nil {
		t.Fatalf("Execute() replay error = %v", err)
	}
	if !result.Replayed || result.Income.ID() != replayedIncome.ID() {
		t.Fatal("Execute() did not return the original replayed Income")
	}

	store.err = application.ErrIncomeIdempotencyConflict
	_, err = useCase.Execute(context.Background(), application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "reused-key"})
	if !errors.Is(err, application.ErrIncomeIdempotencyConflict) {
		t.Fatalf("Execute() conflict error = %v", err)
	}
}

func TestRecordIncomeWrapsPortAndIDFailuresSafely(t *testing.T) {
	const marker = "SUPER_SECRET_INCOME_DEPENDENCY_98431"
	portFailure := errors.New(marker)
	store := &recordingIncomeCommandStore{err: portFailure}
	useCase := mustRecordIncome(t, store, &fixedIncomeIDGenerator{id: "income-port-failure"}, &fixedClock{
		now: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC),
	})
	_, err := useCase.Execute(context.Background(), application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "safe-key"})
	if !errors.Is(err, application.ErrIncomeCommandPersistence) || !errors.Is(err, portFailure) {
		t.Fatalf("Execute() error = %v, want categorized port failure", err)
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatal("Execute() exposed the raw port error")
	}

	idFailure := errors.New(marker)
	clock := &fixedClock{now: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)}
	store = &recordingIncomeCommandStore{}
	useCase = mustRecordIncome(t, store, &fixedIncomeIDGenerator{err: idFailure}, clock)
	_, err = useCase.Execute(context.Background(), application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "safe-key"})
	if !errors.Is(err, application.ErrIncomeIDGeneration) || !errors.Is(err, idFailure) {
		t.Fatalf("Execute() error = %v, want categorized ID failure", err)
	}
	if strings.Contains(err.Error(), marker) || store.calls != 0 || clock.calls != 0 {
		t.Fatal("ID failure exposed its cause or consumed later dependencies")
	}
}

func TestRecordIncomeHonorsCancellationAndRequiresDependencies(t *testing.T) {
	store := &recordingIncomeCommandStore{}
	idGenerator := &fixedIncomeIDGenerator{id: "income-canceled"}
	clock := &fixedClock{now: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)}
	useCase := mustRecordIncome(t, store, idGenerator, clock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := useCase.Execute(ctx, application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "valid-key"})
	if !errors.Is(err, context.Canceled) || store.calls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
		t.Fatal("canceled command consumed dependencies")
	}

	tests := []struct {
		name        string
		store       application.IncomeCommandStore
		idGenerator application.IncomeIDGenerator
		clock       application.Clock
		wanted      error
	}{
		{name: "store", idGenerator: idGenerator, clock: clock, wanted: application.ErrMissingIncomeCommandStore},
		{name: "id generator", store: store, clock: clock, wanted: application.ErrMissingIncomeIDGenerator},
		{name: "clock", store: store, idGenerator: idGenerator, wanted: application.ErrMissingIncomeClock},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := application.NewRecordIncome(test.store, test.idGenerator, test.clock)
			if !errors.Is(err, test.wanted) {
				t.Fatalf("NewRecordIncome() error = %v, want %v", err, test.wanted)
			}
		})
	}
}

func TestMonthlyTransactionProjectionKeepsTypeSpecificPaymentMethod(t *testing.T) {
	expense := mustSyntheticExpense(t, "expense-monthly-projection")
	income := mustSyntheticIncome(t, "income-monthly-projection")

	expenseProjection := application.NewMonthlyTransactionFromExpense(expense)
	if expenseProjection.Type != domain.TransactionTypeExpense || expenseProjection.PaymentMethod == nil || *expenseProjection.PaymentMethod != domain.PaymentMethodPIX {
		t.Fatal("Expense projection lost its type-specific payment method")
	}
	incomeProjection := application.NewMonthlyTransactionFromIncome(income)
	if incomeProjection.Type != domain.TransactionTypeIncome || incomeProjection.PaymentMethod != nil {
		t.Fatal("Income projection must not contain a payment method")
	}
	if incomeProjection.ID != income.ID() || incomeProjection.UserID != income.UserID() || !incomeProjection.Amount.Equal(income.Amount()) {
		t.Fatal("Income projection lost common read fields")
	}
}

type recordingIncomeCommandStore struct {
	calls   int
	command application.IdempotentIncomeCommand
	result  application.IdempotentIncomeResult
	err     error
}

func (store *recordingIncomeCommandStore) RecordIncome(
	_ context.Context,
	command application.IdempotentIncomeCommand,
) (application.IdempotentIncomeResult, error) {
	store.calls++
	store.command = command
	if store.err != nil {
		return application.IdempotentIncomeResult{}, store.err
	}
	if store.result.Income.ID() != "" {
		return store.result, nil
	}
	return application.IdempotentIncomeResult{Income: command.Income}, nil
}

type fixedIncomeIDGenerator struct {
	id    string
	err   error
	calls int
}

func (generator *fixedIncomeIDGenerator) NewIncomeID() (string, error) {
	generator.calls++
	return generator.id, generator.err
}

func mustRecordIncome(
	t testing.TB,
	store application.IncomeCommandStore,
	idGenerator application.IncomeIDGenerator,
	clock application.Clock,
) *application.RecordIncome {
	t.Helper()
	useCase, err := application.NewRecordIncome(store, idGenerator, clock)
	if err != nil {
		t.Fatalf("NewRecordIncome() error = %v", err)
	}
	return useCase
}

func validIncomeInput() application.CreateIncomeInput {
	return application.CreateIncomeInput{
		UserID:            "user-synthetic-001",
		Description:       "Synthetic income",
		AmountMinor:       725000,
		Currency:          domain.CurrencyBRL,
		OccurredAt:        time.Date(2026, 8, 14, 15, 0, 0, 123_456_000, time.UTC),
		FinancialTimezone: application.FinancialTimezone,
		Origin:            domain.OriginIOS,
	}
}

func mustSyntheticIncome(t testing.TB, id string) domain.Income {
	t.Helper()
	amount, err := domain.NewMoney(725000, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	income, err := domain.NewIncome(domain.IncomeParams{
		ID: id,
		Details: domain.IncomeDetails{
			UserID:            "user-synthetic-001",
			Description:       "Receita sintética",
			Amount:            amount,
			OccurredAt:        time.Date(2026, 8, 14, 15, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewIncome() error = %v", err)
	}
	return income
}

func incomeFingerprintFor(t testing.TB, id string, input application.CreateIncomeInput) application.RequestFingerprint {
	t.Helper()
	store := &recordingIncomeCommandStore{}
	useCase := mustRecordIncome(t, store, &fixedIncomeIDGenerator{id: id}, &fixedClock{
		now: time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC),
	})
	if _, err := useCase.Execute(context.Background(), application.RecordIncomeInput{Income: input, IdempotencyKey: "same-key"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	return store.command.Fingerprint
}
