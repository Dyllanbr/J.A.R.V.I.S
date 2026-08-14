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

func TestPreviewExpenseNormalizesWithoutGeneratedValuesOrPersistence(t *testing.T) {
	input := validInput()
	input.Description = "  Mercado sintético  "
	input.OccurredAt = time.Date(2026, time.August, 14, 12, 0, 0, 123, time.FixedZone("synthetic", -3*60*60))

	result, err := (application.PreviewExpense{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Details.Description != "Mercado sintético" {
		t.Fatalf("description = %q, want trimmed canonical value", result.Details.Description)
	}
	if result.Details.OccurredAt.Location() != time.UTC {
		t.Fatalf("occurred-at location = %v, want UTC", result.Details.OccurredAt.Location())
	}
	if result.Details.OccurredAt.Nanosecond() != 0 {
		t.Fatalf("occurred-at nanoseconds = %d, want sub-microsecond precision discarded", result.Details.OccurredAt.Nanosecond())
	}
}

func TestPreviewExpenseCanonicalizesFinancialInstantsToUTCMicroseconds(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "no fraction",
			input:    time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
			expected: time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
		},
		{
			name:     "milliseconds",
			input:    time.Date(2026, time.August, 14, 15, 0, 0, 123_000_000, time.UTC),
			expected: time.Date(2026, time.August, 14, 15, 0, 0, 123_000_000, time.UTC),
		},
		{
			name:     "exact microseconds",
			input:    time.Date(2026, time.August, 14, 15, 0, 0, 123_456_000, time.UTC),
			expected: time.Date(2026, time.August, 14, 15, 0, 0, 123_456_000, time.UTC),
		},
		{
			name:     "below one microsecond",
			input:    time.Date(2026, time.August, 14, 15, 0, 0, 999, time.UTC),
			expected: time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
		},
		{
			name:     "nine fractional digits",
			input:    time.Date(2026, time.August, 14, 15, 0, 0, 123_456_789, time.UTC),
			expected: time.Date(2026, time.August, 14, 15, 0, 0, 123_456_000, time.UTC),
		},
		{
			name:     "equivalent offset instant",
			input:    time.Date(2026, time.August, 14, 12, 0, 0, 123_456_789, time.FixedZone("minus-three", -3*60*60)),
			expected: time.Date(2026, time.August, 14, 15, 0, 0, 123_456_000, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validInput()
			input.OccurredAt = test.input
			result, err := (application.PreviewExpense{}).Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.Details.OccurredAt != test.expected {
				t.Fatalf("occurred-at = %s, want %s", result.Details.OccurredAt.Format(time.RFC3339Nano), test.expected.Format(time.RFC3339Nano))
			}
			if result.Details.OccurredAt.Location() != time.UTC {
				t.Fatalf("occurred-at location = %v, want UTC", result.Details.OccurredAt.Location())
			}
		})
	}
}

func TestPreviewExpenseRejectsDomainInvalidInput(t *testing.T) {
	input := validInput()
	input.AmountMinor = 0

	_, err := (application.PreviewExpense{}).Execute(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidExpenseAmount) {
		t.Fatalf("Execute() error = %v, want ErrInvalidExpenseAmount", err)
	}
}

func TestRecordExpenseBuildsCanonicalIdempotentCommand(t *testing.T) {
	store := &recordingCommandStore{}
	idGenerator := &fixedIDGenerator{id: "expense-synthetic-record-001"}
	clock := &fixedClock{now: time.Date(2026, time.August, 14, 18, 0, 0, 123_456_789, time.UTC)}
	useCase := mustRecordExpense(t, store, idGenerator, clock)
	input := application.RecordExpenseInput{
		Expense:        validInput(),
		IdempotencyKey: "synthetic-create-001",
	}
	input.Expense.Description = "  Mercado sintético  "

	result, err := useCase.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.calls != 1 || idGenerator.calls != 1 || clock.calls != 1 {
		t.Fatalf("calls store/id/clock = %d/%d/%d, want 1/1/1", store.calls, idGenerator.calls, clock.calls)
	}
	if store.command.Operation != application.IdempotencyOperationCreateExpense {
		t.Fatalf("operation = %q", store.command.Operation)
	}
	if store.command.IdempotencyKey != input.IdempotencyKey {
		t.Fatal("command did not carry the validated idempotency key")
	}
	if store.command.Expense.Description() != "Mercado sintético" {
		t.Fatal("command did not carry the normalized description")
	}
	expectedCreatedAt := time.Date(2026, time.August, 14, 18, 0, 0, 123_456_000, time.UTC)
	if store.command.Expense.CreatedAt() != expectedCreatedAt || store.command.Expense.UpdatedAt() != expectedCreatedAt {
		t.Fatal("command timestamps were not canonicalized to UTC microseconds")
	}
	if result.Expense.ID() != idGenerator.id || result.Replayed {
		t.Fatal("result did not return the newly persisted Expense")
	}
}

func TestRecordExpenseFingerprintUsesNormalizedSemantics(t *testing.T) {
	firstStore := &recordingCommandStore{}
	first := mustRecordExpense(t, firstStore, &fixedIDGenerator{id: "expense-fingerprint-a"}, &fixedClock{
		now: time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC),
	})
	firstInput := application.RecordExpenseInput{Expense: validInput(), IdempotencyKey: "same-key"}
	firstInput.Expense.Description = " Mercado sintético "
	firstInput.Expense.OccurredAt = time.Date(2026, time.August, 14, 12, 0, 0, 0, time.FixedZone("minus-three", -3*60*60))
	if _, err := first.Execute(context.Background(), firstInput); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}

	secondStore := &recordingCommandStore{}
	second := mustRecordExpense(t, secondStore, &fixedIDGenerator{id: "expense-fingerprint-b"}, &fixedClock{
		now: time.Date(2026, time.August, 14, 19, 0, 0, 0, time.UTC),
	})
	secondInput := application.RecordExpenseInput{Expense: validInput(), IdempotencyKey: "same-key"}
	secondInput.Expense.Description = "Mercado sintético"
	secondInput.Expense.OccurredAt = time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)
	if _, err := second.Execute(context.Background(), secondInput); err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}

	if firstStore.command.Fingerprint != secondStore.command.Fingerprint {
		t.Fatal("semantically equivalent inputs produced different fingerprints")
	}
}

func TestRecordExpenseFingerprintUsesCanonicalMicrosecondInstant(t *testing.T) {
	fingerprintFor := func(t *testing.T, id string, nanoseconds int) application.RequestFingerprint {
		t.Helper()
		store := &recordingCommandStore{}
		useCase := mustRecordExpense(t, store, &fixedIDGenerator{id: id}, &fixedClock{
			now: time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC),
		})
		input := application.RecordExpenseInput{Expense: validInput(), IdempotencyKey: "same-key"}
		input.Expense.OccurredAt = time.Date(2026, time.August, 14, 15, 0, 0, nanoseconds, time.UTC)
		if _, err := useCase.Execute(context.Background(), input); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		return store.command.Fingerprint
	}

	first := fingerprintFor(t, "expense-fingerprint-micro-a", 123)
	equivalent := fingerprintFor(t, "expense-fingerprint-micro-b", 999)
	different := fingerprintFor(t, "expense-fingerprint-micro-c", 1_000)
	if first != equivalent {
		t.Fatal("sub-microsecond differences that canonicalize equally changed the fingerprint")
	}
	if first == different {
		t.Fatal("different canonical microsecond instants produced the same fingerprint")
	}
}

func TestRecordExpenseRejectsBeforeGeneratedValuesAndStore(t *testing.T) {
	invalidAmount := validInput()
	invalidAmount.AmountMinor = 0
	tests := []struct {
		name   string
		input  application.RecordExpenseInput
		wanted error
	}{
		{name: "missing key", input: application.RecordExpenseInput{Expense: validInput()}, wanted: application.ErrIdempotencyKeyRequired},
		{name: "invalid key", input: application.RecordExpenseInput{Expense: validInput(), IdempotencyKey: "invalid key"}, wanted: application.ErrIdempotencyKeyInvalid},
		{name: "invalid expense", input: application.RecordExpenseInput{Expense: invalidAmount, IdempotencyKey: "valid-key"}, wanted: domain.ErrInvalidExpenseAmount},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingCommandStore{}
			idGenerator := &fixedIDGenerator{id: "unused-id"}
			clock := &fixedClock{now: time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)}
			useCase := mustRecordExpense(t, store, idGenerator, clock)

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

func TestRecordExpenseReturnsReplayAndConflict(t *testing.T) {
	replayedExpense := mustSyntheticExpense(t, "expense-original")
	store := &recordingCommandStore{result: application.IdempotentExpenseResult{Expense: replayedExpense, Replayed: true}}
	useCase := mustRecordExpense(t, store, &fixedIDGenerator{id: "expense-unused-replay"}, &fixedClock{
		now: time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC),
	})
	result, err := useCase.Execute(context.Background(), application.RecordExpenseInput{
		Expense:        validInput(),
		IdempotencyKey: "replay-key",
	})
	if err != nil {
		t.Fatalf("Execute() replay error = %v", err)
	}
	if !result.Replayed || result.Expense.ID() != replayedExpense.ID() {
		t.Fatal("Execute() did not return the original replayed resource")
	}

	store.err = application.ErrIdempotencyConflict
	_, err = useCase.Execute(context.Background(), application.RecordExpenseInput{
		Expense:        validInput(),
		IdempotencyKey: "reused-key",
	})
	if !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("Execute() conflict error = %v", err)
	}
}

func TestValidateIdempotencyKey(t *testing.T) {
	valid := []string{"a", strings.Repeat("x", 128), "synthetic-key:ABC_123"}
	for _, key := range valid {
		if err := application.ValidateIdempotencyKey(key); err != nil {
			t.Fatalf("ValidateIdempotencyKey(valid) error = %v", err)
		}
	}

	invalid := []string{"", strings.Repeat("x", 129), " leading", "trailing ", "internal space", "line\nfeed", "não-ascii"}
	for _, key := range invalid {
		if err := application.ValidateIdempotencyKey(key); err == nil {
			t.Fatalf("ValidateIdempotencyKey(%q) returned nil", key)
		}
	}
}

func TestListExpensesByMonthUsesSaoPauloInclusiveExclusiveBounds(t *testing.T) {
	tests := []struct {
		month          string
		startLocalDate string
		endLocalDate   string
	}{
		{month: "2024-02", startLocalDate: "2024-02-01", endLocalDate: "2024-03-01"},
		{month: "2025-02", startLocalDate: "2025-02-01", endLocalDate: "2025-03-01"},
		{month: "2026-04", startLocalDate: "2026-04-01", endLocalDate: "2026-05-01"},
		{month: "2026-07", startLocalDate: "2026-07-01", endLocalDate: "2026-08-01"},
		{month: "2026-12", startLocalDate: "2026-12-01", endLocalDate: "2027-01-01"},
	}

	for _, test := range tests {
		t.Run(test.month, func(t *testing.T) {
			reader := &recordingExpenseReader{}
			useCase, err := application.NewListExpensesByMonth(reader)
			if err != nil {
				t.Fatalf("NewListExpensesByMonth() error = %v", err)
			}
			result, err := useCase.Execute(context.Background(), "user-synthetic-001", test.month)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			location, _ := time.LoadLocation(application.FinancialTimezone)
			if got := reader.query.Start.In(location).Format("2006-01-02"); got != test.startLocalDate {
				t.Fatalf("start local date = %s, want %s", got, test.startLocalDate)
			}
			if got := reader.query.End.In(location).Format("2006-01-02"); got != test.endLocalDate {
				t.Fatalf("end local date = %s, want %s", got, test.endLocalDate)
			}
			if reader.query.Start.Location() != time.UTC || reader.query.End.Location() != time.UTC {
				t.Fatal("query boundaries are not normalized to UTC")
			}
			if reader.query.FinancialTimezone != application.FinancialTimezone || result.Month != test.month {
				t.Fatal("monthly query lost its canonical timezone or month")
			}
			if result.Expenses == nil {
				t.Fatal("empty result must use an empty slice, not nil")
			}
		})
	}
}

func TestListExpensesByMonthRejectsMalformedMonthWithoutQuery(t *testing.T) {
	invalid := []string{"", "2026-8", "08-2026", "2026-00", "2026-13", "0000-01", "2026/08"}
	for _, month := range invalid {
		reader := &recordingExpenseReader{}
		useCase, err := application.NewListExpensesByMonth(reader)
		if err != nil {
			t.Fatalf("NewListExpensesByMonth() error = %v", err)
		}
		_, err = useCase.Execute(context.Background(), "user-synthetic-001", month)
		if !errors.Is(err, application.ErrInvalidMonth) {
			t.Fatalf("Execute(%q) error = %v, want ErrInvalidMonth", month, err)
		}
		if reader.calls != 0 {
			t.Fatalf("reader calls = %d, want 0", reader.calls)
		}
	}
}

type recordingCommandStore struct {
	calls   int
	command application.IdempotentExpenseCommand
	result  application.IdempotentExpenseResult
	err     error
}

func (store *recordingCommandStore) Record(
	_ context.Context,
	command application.IdempotentExpenseCommand,
) (application.IdempotentExpenseResult, error) {
	store.calls++
	store.command = command
	if store.err != nil {
		return application.IdempotentExpenseResult{}, store.err
	}
	if store.result.Expense.ID() != "" {
		return store.result, nil
	}
	return application.IdempotentExpenseResult{Expense: command.Expense}, nil
}

type recordingExpenseReader struct {
	calls int
	query application.ExpenseMonthQuery
	items []domain.Expense
	err   error
}

func (reader *recordingExpenseReader) ListByFinancialMonth(
	_ context.Context,
	query application.ExpenseMonthQuery,
) ([]domain.Expense, error) {
	reader.calls++
	reader.query = query
	return reader.items, reader.err
}

func mustRecordExpense(
	t testing.TB,
	store application.ExpenseCommandStore,
	idGenerator application.ExpenseIDGenerator,
	clock application.Clock,
) *application.RecordExpense {
	t.Helper()
	useCase, err := application.NewRecordExpense(store, idGenerator, clock)
	if err != nil {
		t.Fatalf("NewRecordExpense() error = %v", err)
	}
	return useCase
}

func mustSyntheticExpense(t testing.TB, id string) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(4250, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID:            "user-synthetic-001",
			Description:       "Mercado sintético",
			Amount:            amount,
			PaymentMethod:     domain.PaymentMethodPIX,
			OccurredAt:        time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewExpense() error = %v", err)
	}
	return expense
}
