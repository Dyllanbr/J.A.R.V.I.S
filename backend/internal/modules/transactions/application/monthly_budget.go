package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingMonthlyBudgetStore   = errors.New("monthly budget: store is required")
	ErrInvalidMonthlyBudgetOwnerID = errors.New("monthly budget: invalid owner id")
	ErrInvalidMonthlyBudgetMonth   = errors.New("monthly budget: invalid month")
	ErrInvalidMonthlyBudgetAmount  = errors.New("monthly budget: invalid amount")
	ErrMonthlyBudgetPersistence    = errors.New("monthly budget: persistence failed")
)

// MonthlyBudgetStore is the future persistence boundary for replacing the
// natural owner/month key. Stage 1 keeps this port free of PostgreSQL details;
// no financial transaction or idempotency record is involved.
type MonthlyBudgetStore interface {
	ReplaceMonthlyBudget(context.Context, domain.MonthlyBudget) error
}

type SetMonthlyBudgetInput struct {
	OwnerID string
	Month   domain.CivilMonth
	Amount  domain.Money
}

type SetMonthlyBudgetResult struct {
	Budget domain.MonthlyBudget
}

// SetMonthlyBudget validates an explicit civil-month budget and delegates
// replacement to the dedicated store. Repeating the same owner/month call is
// naturally idempotent at that boundary; this use case has no generated ID,
// clock or financial idempotency dependency.
type SetMonthlyBudget struct {
	store MonthlyBudgetStore
}

func NewSetMonthlyBudget(store MonthlyBudgetStore) (*SetMonthlyBudget, error) {
	if store == nil {
		return nil, ErrMissingMonthlyBudgetStore
	}
	return &SetMonthlyBudget{store: store}, nil
}

func (useCase *SetMonthlyBudget) Execute(ctx context.Context, input SetMonthlyBudgetInput) (SetMonthlyBudgetResult, error) {
	if err := ctx.Err(); err != nil {
		return SetMonthlyBudgetResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return SetMonthlyBudgetResult{}, ErrInvalidMonthlyBudgetOwnerID
	}
	if input.Month.String() == "" {
		return SetMonthlyBudgetResult{}, ErrInvalidMonthlyBudgetMonth
	}
	if input.Amount.Currency() != domain.CurrencyBRL || input.Amount.MinorUnits() < 0 {
		return SetMonthlyBudgetResult{}, ErrInvalidMonthlyBudgetAmount
	}
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{
		OwnerID: input.OwnerID,
		Month:   input.Month,
		Amount:  input.Amount,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidMonthlyBudgetOwnerID):
			return SetMonthlyBudgetResult{}, ErrInvalidMonthlyBudgetOwnerID
		case errors.Is(err, domain.ErrInvalidMonthlyBudgetMonth):
			return SetMonthlyBudgetResult{}, ErrInvalidMonthlyBudgetMonth
		default:
			return SetMonthlyBudgetResult{}, ErrInvalidMonthlyBudgetAmount
		}
	}
	if err := useCase.store.ReplaceMonthlyBudget(ctx, budget); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return SetMonthlyBudgetResult{}, err
		}
		return SetMonthlyBudgetResult{}, newSafeOperationError(ErrMonthlyBudgetPersistence, err)
	}
	if err := ctx.Err(); err != nil {
		return SetMonthlyBudgetResult{}, err
	}
	return SetMonthlyBudgetResult{Budget: budget}, nil
}
