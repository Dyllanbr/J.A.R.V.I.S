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

func TestCreateExpenseExecutesDeterministicFlow(t *testing.T) {
	repository := &recordingRepository{}
	idGenerator := &fixedIDGenerator{id: "expense-synthetic-001"}
	clock := &fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)}
	useCase := mustCreateExpense(t, repository, idGenerator, clock)

	result, err := useCase.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Execute() returned an error: %v", err)
	}

	if idGenerator.calls != 1 {
		t.Fatalf("ID generator calls = %d, want 1", idGenerator.calls)
	}
	if clock.calls != 1 {
		t.Fatalf("clock calls = %d, want 1", clock.calls)
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls = %d, want 1", repository.calls)
	}
	if result.Expense.ID() != idGenerator.id || repository.saved.ID() != idGenerator.id {
		t.Fatal("Execute() did not use the generated deterministic ID")
	}
	if !result.Expense.CreatedAt().Equal(clock.now) || !repository.saved.CreatedAt().Equal(clock.now) {
		t.Fatal("Execute() did not use the deterministic clock")
	}
	if !result.Expense.Amount().Equal(repository.saved.Amount()) {
		t.Fatal("Execute() returned an expense different from the saved expense")
	}
}

func TestCreateExpensePassesTheSameContextToRepository(t *testing.T) {
	type contextKey string
	const key contextKey = "synthetic-request"

	repository := &recordingRepository{}
	useCase := mustCreateExpense(
		t,
		repository,
		&fixedIDGenerator{id: "expense-synthetic-001"},
		&fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)},
	)
	ctx := context.WithValue(context.Background(), key, "synthetic-context-value")

	if _, err := useCase.Execute(ctx, validInput()); err != nil {
		t.Fatalf("Execute() returned an error: %v", err)
	}
	if repository.ctx != ctx {
		t.Fatal("Execute() did not pass the same context instance to the repository")
	}
	if repository.ctx.Value(key) != "synthetic-context-value" {
		t.Fatal("repository did not receive the context value")
	}
}

func TestCreateExpenseDoesNotCallDependenciesForInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*application.CreateExpenseInput)
		wantErr error
	}{
		{name: "invalid user", mutate: func(input *application.CreateExpenseInput) { input.UserID = "" }, wantErr: domain.ErrInvalidUserID},
		{name: "user with surrounding spaces", mutate: func(input *application.CreateExpenseInput) { input.UserID = " user-id " }, wantErr: domain.ErrInvalidUserID},
		{name: "user with control", mutate: func(input *application.CreateExpenseInput) { input.UserID = "user\nid" }, wantErr: domain.ErrInvalidUserID},
		{name: "user above limit", mutate: func(input *application.CreateExpenseInput) {
			input.UserID = strings.Repeat("u", domain.MaxIdentifierBytes+1)
		}, wantErr: domain.ErrInvalidUserID},
		{name: "empty description", mutate: func(input *application.CreateExpenseInput) { input.Description = "" }, wantErr: domain.ErrInvalidDescription},
		{name: "blank description", mutate: func(input *application.CreateExpenseInput) { input.Description = " \t\n " }, wantErr: domain.ErrInvalidDescription},
		{name: "description above limit", mutate: func(input *application.CreateExpenseInput) {
			input.Description = strings.Repeat("a", domain.MaxExpenseDescriptionRunes+1)
		}, wantErr: domain.ErrInvalidDescription},
		{name: "invalid UTF-8 description", mutate: func(input *application.CreateExpenseInput) { input.Description = string([]byte{0xff}) }, wantErr: domain.ErrInvalidDescription},
		{name: "zero amount", mutate: func(input *application.CreateExpenseInput) { input.AmountMinor = 0 }, wantErr: domain.ErrInvalidExpenseAmount},
		{name: "negative amount", mutate: func(input *application.CreateExpenseInput) { input.AmountMinor = -1 }, wantErr: domain.ErrInvalidExpenseAmount},
		{name: "unsupported currency", mutate: func(input *application.CreateExpenseInput) { input.Currency = domain.Currency("USD") }, wantErr: domain.ErrUnsupportedCurrency},
		{name: "invalid payment method", mutate: func(input *application.CreateExpenseInput) { input.PaymentMethod = domain.PaymentMethod("SYNTHETIC") }, wantErr: domain.ErrInvalidPaymentMethod},
		{name: "invalid occurrence", mutate: func(input *application.CreateExpenseInput) { input.OccurredAt = time.Time{} }, wantErr: domain.ErrInvalidOccurredAt},
		{name: "empty timezone", mutate: func(input *application.CreateExpenseInput) { input.FinancialTimezone = "" }, wantErr: domain.ErrInvalidFinancialTimezone},
		{name: "invalid timezone", mutate: func(input *application.CreateExpenseInput) { input.FinancialTimezone = "Synthetic/Invalid" }, wantErr: domain.ErrInvalidFinancialTimezone},
		{name: "process local timezone", mutate: func(input *application.CreateExpenseInput) { input.FinancialTimezone = "Local" }, wantErr: domain.ErrInvalidFinancialTimezone},
		{name: "invalid origin", mutate: func(input *application.CreateExpenseInput) { input.Origin = domain.Origin("SYNTHETIC") }, wantErr: domain.ErrInvalidOrigin},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &recordingRepository{}
			idGenerator := &fixedIDGenerator{id: "expense-synthetic-invalid"}
			clock := &fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)}
			useCase := mustCreateExpense(
				t,
				repository,
				idGenerator,
				clock,
			)
			input := validInput()
			test.mutate(&input)

			_, err := useCase.Execute(context.Background(), input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, test.wantErr)
			}
			if idGenerator.calls != 0 || clock.calls != 0 || repository.calls != 0 {
				t.Fatalf(
					"dependency calls for invalid input: ID generator=%d, clock=%d, repository=%d; want all 0",
					idGenerator.calls,
					clock.calls,
					repository.calls,
				)
			}
		})
	}
}

func TestCreateExpenseDomainErrorPrecedesIDGenerationFailure(t *testing.T) {
	idGenerator := &fixedIDGenerator{err: errors.New("synthetic-id-provider-failure")}
	clock := &fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)}
	repository := &recordingRepository{}
	useCase := mustCreateExpense(t, repository, idGenerator, clock)
	input := validInput()
	input.Description = ""

	_, err := useCase.Execute(context.Background(), input)
	if !errors.Is(err, domain.ErrInvalidDescription) {
		t.Fatalf("Execute() error = %v, want ErrInvalidDescription", err)
	}
	if errors.Is(err, application.ErrExpenseIDGeneration) {
		t.Fatal("Execute() masked the domain error with an ID generation failure")
	}
	if idGenerator.calls != 0 || clock.calls != 0 || repository.calls != 0 {
		t.Fatal("Execute() called dependencies before returning the domain error")
	}
}

func TestCreateExpensePropagatesRepositoryFailureSafely(t *testing.T) {
	const sensitiveMarker = "synthetic-financial-repository-marker"
	repositoryFailure := errors.New(sensitiveMarker)
	repository := &recordingRepository{saveErr: repositoryFailure}
	useCase := mustCreateExpense(
		t,
		repository,
		&fixedIDGenerator{id: "expense-synthetic-001"},
		&fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)},
	)

	_, err := useCase.Execute(context.Background(), validInput())
	if !errors.Is(err, application.ErrExpensePersistence) {
		t.Fatalf("Execute() error = %v, want ErrExpensePersistence", err)
	}
	if !errors.Is(err, repositoryFailure) {
		t.Fatal("Execute() did not preserve the repository cause for programmatic inspection")
	}
	if strings.Contains(err.Error(), sensitiveMarker) {
		t.Fatal("Execute() exposed the raw repository error")
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls = %d, want 1", repository.calls)
	}
}

func TestCreateExpensePropagatesIDGenerationFailureSafely(t *testing.T) {
	const sensitiveMarker = "synthetic-id-provider-marker"
	idFailure := errors.New(sensitiveMarker)
	repository := &recordingRepository{}
	clock := &fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)}
	useCase := mustCreateExpense(
		t,
		repository,
		&fixedIDGenerator{err: idFailure},
		clock,
	)

	_, err := useCase.Execute(context.Background(), validInput())
	if !errors.Is(err, application.ErrExpenseIDGeneration) || !errors.Is(err, idFailure) {
		t.Fatalf("Execute() error = %v, want categorized ID generation failure", err)
	}
	if strings.Contains(err.Error(), sensitiveMarker) {
		t.Fatal("Execute() exposed the raw ID provider error")
	}
	if repository.calls != 0 {
		t.Fatalf("repository calls = %d, want 0", repository.calls)
	}
	if clock.calls != 0 {
		t.Fatalf("clock calls = %d, want 0 after ID generation failure", clock.calls)
	}
}

func TestCreateExpenseHonorsCanceledContext(t *testing.T) {
	repository := &recordingRepository{}
	idGenerator := &fixedIDGenerator{id: "expense-synthetic-001"}
	clock := &fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)}
	useCase := mustCreateExpense(t, repository, idGenerator, clock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := useCase.Execute(ctx, validInput())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
	if idGenerator.calls != 0 || clock.calls != 0 || repository.calls != 0 {
		t.Fatal("Execute() called dependencies after cancellation")
	}
}

func TestNewCreateExpenseRequiresExplicitDependencies(t *testing.T) {
	repository := &recordingRepository{}
	idGenerator := &fixedIDGenerator{id: "expense-synthetic-001"}
	clock := &fixedClock{now: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)}

	tests := []struct {
		name        string
		repository  application.ExpenseRepository
		idGenerator application.ExpenseIDGenerator
		clock       application.Clock
		wantErr     error
	}{
		{name: "repository", idGenerator: idGenerator, clock: clock, wantErr: application.ErrMissingRepository},
		{name: "id generator", repository: repository, clock: clock, wantErr: application.ErrMissingIDGenerator},
		{name: "clock", repository: repository, idGenerator: idGenerator, wantErr: application.ErrMissingClock},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := application.NewCreateExpense(test.repository, test.idGenerator, test.clock)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewCreateExpense() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

type recordingRepository struct {
	calls   int
	ctx     context.Context
	saved   domain.Expense
	saveErr error
}

func (repository *recordingRepository) Save(ctx context.Context, expense domain.Expense) error {
	repository.calls++
	repository.ctx = ctx
	repository.saved = expense
	return repository.saveErr
}

type fixedIDGenerator struct {
	id    string
	err   error
	calls int
}

func (generator *fixedIDGenerator) NewExpenseID() (string, error) {
	generator.calls++
	return generator.id, generator.err
}

type fixedClock struct {
	now   time.Time
	calls int
}

func (clock *fixedClock) Now() time.Time {
	clock.calls++
	return clock.now
}

func validInput() application.CreateExpenseInput {
	return application.CreateExpenseInput{
		UserID:            "user-synthetic-001",
		Description:       "Synthetic expense",
		AmountMinor:       4250,
		Currency:          domain.CurrencyBRL,
		PaymentMethod:     domain.PaymentMethodPIX,
		OccurredAt:        time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC),
		FinancialTimezone: "America/Sao_Paulo",
		Origin:            domain.OriginIOS,
	}
}

func mustCreateExpense(
	t testing.TB,
	repository application.ExpenseRepository,
	idGenerator application.ExpenseIDGenerator,
	clock application.Clock,
) *application.CreateExpense {
	t.Helper()
	useCase, err := application.NewCreateExpense(repository, idGenerator, clock)
	if err != nil {
		t.Fatalf("NewCreateExpense() returned an error: %v", err)
	}
	return useCase
}
