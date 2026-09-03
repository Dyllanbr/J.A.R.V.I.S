package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrCreditCardRequired         = errors.New("expense: credit card is required")
	ErrLegacyExpenseReplayLookup  = errors.New("expense: replay lookup failed")
	ErrLegacyExpenseDependency    = errors.New("expense: invalid replay snapshot")
	ErrMissingExpenseReplayReader = errors.New("expense: replay reader is required")
)

// ExpenseReplayQuery is the legacy CREATE_EXPENSE identity before any newly
// prohibited CREDIT write is attempted.
type ExpenseReplayQuery struct {
	UserID         string
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
}

type ExpenseReplayLookup struct {
	Expense domain.Expense
	Found   bool
}

// ExpenseReplayReader is intentionally read-only. Stage 2 can implement it
// against persisted idempotency records without changing the legacy digest.
type ExpenseReplayReader interface {
	FindExpenseReplay(context.Context, ExpenseReplayQuery) (ExpenseReplayLookup, error)
}

type LegacyExpenseCreditGuard struct{ replayReader ExpenseReplayReader }

func NewLegacyExpenseCreditGuard(replayReader ExpenseReplayReader) (*LegacyExpenseCreditGuard, error) {
	if replayReader == nil {
		return nil, ErrMissingExpenseReplayReader
	}
	return &LegacyExpenseCreditGuard{replayReader: replayReader}, nil
}

// Check preserves historical replay while refusing a new CREDIT command. It
// does not participate in RecordExpense until a persistent replay reader is
// wired by a later stage.
func (guard *LegacyExpenseCreditGuard) Check(ctx context.Context, input RecordExpenseInput) (ExpenseReplayLookup, error) {
	if err := ctx.Err(); err != nil {
		return ExpenseReplayLookup{}, err
	}
	if err := ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ExpenseReplayLookup{}, err
	}
	amount, err := domain.NewMoney(input.Expense.AmountMinor, input.Expense.Currency)
	if err != nil {
		return ExpenseReplayLookup{}, err
	}
	details, err := domain.NormalizeExpenseDetails(domain.ExpenseDetails{
		UserID: input.Expense.UserID, Description: input.Expense.Description, Amount: amount,
		PaymentMethod: input.Expense.PaymentMethod, CategoryID: input.Expense.CategoryID,
		OccurredAt: input.Expense.OccurredAt, FinancialTimezone: input.Expense.FinancialTimezone,
		Origin: input.Expense.Origin,
	})
	if err != nil {
		return ExpenseReplayLookup{}, err
	}
	details.OccurredAt = canonicalizeFinancialInstant(details.OccurredAt)
	lookup, err := guard.replayReader.FindExpenseReplay(ctx, ExpenseReplayQuery{UserID: details.UserID, Operation: IdempotencyOperationCreateExpense, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprintExpense(details)})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ExpenseReplayLookup{}, err
		}
		if errors.Is(err, ErrIdempotencyConflict) {
			return ExpenseReplayLookup{}, ErrIdempotencyConflict
		}
		return ExpenseReplayLookup{}, newSafeOperationError(ErrLegacyExpenseReplayLookup, err)
	}
	if lookup.Found {
		if err := validateLegacyExpenseReplaySnapshot(lookup.Expense, details); err != nil {
			return ExpenseReplayLookup{}, err
		}
		return lookup, nil
	}
	if details.PaymentMethod == domain.PaymentMethodCredit {
		return ExpenseReplayLookup{}, ErrCreditCardRequired
	}
	return lookup, nil
}

func validateLegacyExpenseReplaySnapshot(expense domain.Expense, expected domain.ExpenseDetails) error {
	if expense.ID() == "" || expense.UserID() != expected.UserID || expense.Type() != domain.TransactionTypeExpense || expense.Status() != domain.ExpenseStatusRecorded || expense.Version() == 0 || expense.CreatedAt().IsZero() || expense.UpdatedAt().IsZero() || expense.UpdatedAt() != expense.CreatedAt() {
		return ErrLegacyExpenseDependency
	}
	if expense.PaymentMethod() != domain.PaymentMethodCredit || expense.Description() != expected.Description || !expense.Amount().Equal(expected.Amount) || expense.Amount().Currency() != expected.Amount.Currency() || expense.OccurredAt() != expected.OccurredAt || expense.FinancialTimezone() != expected.FinancialTimezone || expense.Origin() != expected.Origin {
		return ErrLegacyExpenseDependency
	}
	if categoryID, present := expense.CategoryID(); present != (expected.CategoryID != nil) || present && categoryID != *expected.CategoryID {
		return ErrLegacyExpenseDependency
	}
	if _, linked := expense.CreditCardID(); linked {
		return ErrLegacyExpenseDependency
	}
	if _, hasDueDate := expense.StatementDueOn(); hasDueDate {
		return ErrLegacyExpenseDependency
	}
	return nil
}
