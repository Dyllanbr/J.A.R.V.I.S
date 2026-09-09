package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingFinancialGoalStore  = errors.New("financial goals: goal store is required")
	ErrMissingProtectedValueStore = errors.New("financial goals: protected value store is required")
	ErrFinancialGoalPersistence   = errors.New("financial goals: goal persistence failed")
	ErrProtectedValuePersistence  = errors.New("financial goals: protected value persistence failed")
	ErrInvalidFinancialGoalInput  = errors.New("financial goals: invalid goal input")
	ErrInvalidProtectedValueInput = errors.New("financial goals: invalid protected value input")
)

// FinancialGoalStore is the persistence boundary for replacing one
// owner-scoped financial goal declaration. It has no progress or payment
// semantics and does not write any financial transaction.
type FinancialGoalStore interface {
	ReplaceFinancialGoal(context.Context, domain.FinancialGoal) error
}

// ProtectedValueStore is the persistence boundary for replacing one
// owner-scoped protected-value declaration. A protected value is a
// declaration only; it is not applied to Safe Available in this slice.
type ProtectedValueStore interface {
	ReplaceProtectedValue(context.Context, domain.ProtectedValue) error
}

type ReplaceFinancialGoalInput struct {
	OwnerID      string
	ID           string
	Title        string
	TargetAmount domain.Money
}

type ReplaceFinancialGoal struct {
	store FinancialGoalStore
}

func NewReplaceFinancialGoal(store FinancialGoalStore) (*ReplaceFinancialGoal, error) {
	if store == nil {
		return nil, ErrMissingFinancialGoalStore
	}
	return &ReplaceFinancialGoal{store: store}, nil
}

func (useCase *ReplaceFinancialGoal) Execute(ctx context.Context, input ReplaceFinancialGoalInput) (domain.FinancialGoal, error) {
	if err := ctx.Err(); err != nil {
		return domain.FinancialGoal{}, err
	}
	goal, err := domain.NewFinancialGoal(domain.FinancialGoalParams{
		ID: input.ID, OwnerID: input.OwnerID, Title: input.Title, TargetAmount: input.TargetAmount,
	})
	if err != nil {
		return domain.FinancialGoal{}, ErrInvalidFinancialGoalInput
	}
	if err := useCase.store.ReplaceFinancialGoal(ctx, goal); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return domain.FinancialGoal{}, err
		}
		return domain.FinancialGoal{}, newSafeOperationError(ErrFinancialGoalPersistence, err)
	}
	if err := ctx.Err(); err != nil {
		return domain.FinancialGoal{}, err
	}
	return goal, nil
}

type ReplaceProtectedValueInput struct {
	OwnerID string
	ID      string
	Label   string
	Amount  domain.Money
}

type ReplaceProtectedValue struct {
	store ProtectedValueStore
}

func NewReplaceProtectedValue(store ProtectedValueStore) (*ReplaceProtectedValue, error) {
	if store == nil {
		return nil, ErrMissingProtectedValueStore
	}
	return &ReplaceProtectedValue{store: store}, nil
}

func (useCase *ReplaceProtectedValue) Execute(ctx context.Context, input ReplaceProtectedValueInput) (domain.ProtectedValue, error) {
	if err := ctx.Err(); err != nil {
		return domain.ProtectedValue{}, err
	}
	value, err := domain.NewProtectedValue(domain.ProtectedValueParams{
		ID: input.ID, OwnerID: input.OwnerID, Label: input.Label, Amount: input.Amount,
	})
	if err != nil {
		return domain.ProtectedValue{}, ErrInvalidProtectedValueInput
	}
	if err := useCase.store.ReplaceProtectedValue(ctx, value); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return domain.ProtectedValue{}, err
		}
		return domain.ProtectedValue{}, newSafeOperationError(ErrProtectedValuePersistence, err)
	}
	if err := ctx.Err(); err != nil {
		return domain.ProtectedValue{}, err
	}
	return value, nil
}
