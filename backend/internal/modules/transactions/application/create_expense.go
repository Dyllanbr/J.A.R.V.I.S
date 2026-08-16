package application

import (
	"context"
	"errors"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingRepository   = errors.New("create expense: repository is required")
	ErrMissingIDGenerator  = errors.New("create expense: id generator is required")
	ErrMissingClock        = errors.New("create expense: clock is required")
	ErrExpenseIDGeneration = errors.New("create expense: id generation failed")
	ErrExpensePersistence  = errors.New("create expense: persistence failed")
)

// ExpenseRepository is the persistence capability required by CreateExpense.
type ExpenseRepository interface {
	Save(context.Context, domain.Expense) error
}

// ExpenseIDGenerator creates identifiers without coupling the use case to an
// identifier format or external library.
type ExpenseIDGenerator interface {
	NewExpenseID() (string, error)
}

// Clock provides the current instant to deterministic application code.
type Clock interface {
	Now() time.Time
}

// CreateExpenseInput carries only information already reviewed and confirmed
// by the calling channel.
type CreateExpenseInput struct {
	UserID            string
	Description       string
	AmountMinor       int64
	Currency          domain.Currency
	PaymentMethod     domain.PaymentMethod
	CategoryID        *domain.CategoryID
	OccurredAt        time.Time
	FinancialTimezone string
	Origin            domain.Origin
}

// CreateExpenseResult is returned only after the repository accepts the expense.
type CreateExpenseResult struct {
	Expense domain.Expense
}

// CreateExpense executes creation after explicit confirmation by the caller.
type CreateExpense struct {
	repository      ExpenseRepository
	idGenerator     ExpenseIDGenerator
	clock           Clock
	categoryCatalog CategoryCatalog
}

// NewCreateExpense builds the use case with explicit dependencies.
func NewCreateExpense(repository ExpenseRepository, idGenerator ExpenseIDGenerator, clock Clock) (*CreateExpense, error) {
	return newCreateExpense(repository, idGenerator, clock, nil)
}

// NewCreateExpenseWithCategoryCatalog composes category validation while the
// legacy constructor remains compatible with uncategorized commands.
func NewCreateExpenseWithCategoryCatalog(
	repository ExpenseRepository,
	idGenerator ExpenseIDGenerator,
	clock Clock,
	categoryCatalog CategoryCatalog,
) (*CreateExpense, error) {
	if categoryCatalog == nil {
		return nil, ErrMissingCategoryCatalog
	}
	return newCreateExpense(repository, idGenerator, clock, categoryCatalog)
}

func newCreateExpense(
	repository ExpenseRepository,
	idGenerator ExpenseIDGenerator,
	clock Clock,
	categoryCatalog CategoryCatalog,
) (*CreateExpense, error) {
	if repository == nil {
		return nil, ErrMissingRepository
	}
	if idGenerator == nil {
		return nil, ErrMissingIDGenerator
	}
	if clock == nil {
		return nil, ErrMissingClock
	}

	return &CreateExpense{
		repository:      repository,
		idGenerator:     idGenerator,
		clock:           clock,
		categoryCatalog: categoryCatalog,
	}, nil
}

// Execute validates input, creates an expense and requests its persistence.
func (useCase *CreateExpense) Execute(ctx context.Context, input CreateExpenseInput) (CreateExpenseResult, error) {
	if err := ctx.Err(); err != nil {
		return CreateExpenseResult{}, err
	}

	details, err := normalizeExpenseInput(ctx, useCase.categoryCatalog, input)
	if err != nil {
		return CreateExpenseResult{}, err
	}

	id, err := useCase.idGenerator.NewExpenseID()
	if err != nil {
		return CreateExpenseResult{}, newSafeOperationError(ErrExpenseIDGeneration, err)
	}

	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID:        id,
		Details:   details,
		CreatedAt: canonicalizeFinancialInstant(useCase.clock.Now()),
	})
	if err != nil {
		return CreateExpenseResult{}, err
	}

	if err := useCase.repository.Save(ctx, expense); err != nil {
		return CreateExpenseResult{}, newSafeOperationError(ErrExpensePersistence, err)
	}

	return CreateExpenseResult{Expense: expense}, nil
}

func normalizeExpenseInput(
	ctx context.Context,
	categoryCatalog CategoryCatalog,
	input CreateExpenseInput,
) (domain.ExpenseDetails, error) {
	amount, err := domain.NewMoney(input.AmountMinor, input.Currency)
	if err != nil {
		return domain.ExpenseDetails{}, err
	}

	details, err := domain.NormalizeExpenseDetails(domain.ExpenseDetails{
		UserID:            input.UserID,
		Description:       input.Description,
		Amount:            amount,
		PaymentMethod:     input.PaymentMethod,
		CategoryID:        input.CategoryID,
		OccurredAt:        input.OccurredAt,
		FinancialTimezone: input.FinancialTimezone,
		Origin:            input.Origin,
	})
	if err != nil {
		return domain.ExpenseDetails{}, err
	}
	details.CategoryID, err = validateCategoryForType(ctx, categoryCatalog, details.CategoryID, domain.TransactionTypeExpense)
	if err != nil {
		return domain.ExpenseDetails{}, err
	}

	details.OccurredAt = canonicalizeFinancialInstant(details.OccurredAt)
	return details, nil
}

// canonicalizeFinancialInstant defines the single time representation used by
// application commands, previews, fingerprints and persisted responses. It
// discards precision below one microsecond instead of relying on an adapter to
// choose how an otherwise valid instant is represented.
func canonicalizeFinancialInstant(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

type safeOperationError struct {
	category error
	cause    error
}

func newSafeOperationError(category, cause error) error {
	return safeOperationError{category: category, cause: cause}
}

func (err safeOperationError) Error() string {
	return err.category.Error()
}

func (err safeOperationError) Unwrap() []error {
	return []error{err.category, err.cause}
}
