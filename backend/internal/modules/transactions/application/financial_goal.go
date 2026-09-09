package application

import (
	"context"
	"errors"
	"sort"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingFinancialGoalsSnapshotReader = errors.New("financial goals: snapshot reader is required")
	ErrInvalidFinancialGoalsOwnerID        = errors.New("financial goals: invalid owner id")
	ErrFinancialGoalsSnapshotQuery         = errors.New("financial goals: snapshot query failed")
	ErrFinancialGoalsDependency            = errors.New("financial goals: invalid dependency result")
	ErrFinancialGoalDuplicate              = errors.New("financial goals: duplicate id")
)

// FinancialGoalsSnapshot is an in-memory, owner-scoped read model. It is
// intentionally separate from SafeAvailable until the product defines how a
// protected value affects that calculation.
type FinancialGoalsSnapshot struct {
	Goals           []domain.FinancialGoal
	ProtectedValues []domain.ProtectedValue
}

// FinancialGoalsSnapshotReader is the single read boundary for goals and
// protected values. A future adapter may populate both collections in one
// consistent snapshot; this stage performs no persistence.
type FinancialGoalsSnapshotReader interface {
	Read(context.Context, string) (FinancialGoalsSnapshot, error)
}

type ListFinancialGoalsResult struct {
	goals           []domain.FinancialGoal
	protectedValues []domain.ProtectedValue
}

func (result ListFinancialGoalsResult) Goals() []domain.FinancialGoal {
	copyOfGoals := make([]domain.FinancialGoal, len(result.goals))
	copy(copyOfGoals, result.goals)
	return copyOfGoals
}

func (result ListFinancialGoalsResult) ProtectedValues() []domain.ProtectedValue {
	copyOfValues := make([]domain.ProtectedValue, len(result.protectedValues))
	copy(copyOfValues, result.protectedValues)
	return copyOfValues
}

// ListFinancialGoals validates the server-supplied owner, performs exactly
// one snapshot read and returns deterministic defensive copies. It has no
// Clock, ID generator, write store or financial idempotency dependency.
type ListFinancialGoals struct {
	reader FinancialGoalsSnapshotReader
}

func NewListFinancialGoals(reader FinancialGoalsSnapshotReader) (*ListFinancialGoals, error) {
	if reader == nil {
		return nil, ErrMissingFinancialGoalsSnapshotReader
	}
	return &ListFinancialGoals{reader: reader}, nil
}

func (useCase *ListFinancialGoals) Execute(ctx context.Context, ownerID string) (ListFinancialGoalsResult, error) {
	if err := ctx.Err(); err != nil {
		return ListFinancialGoalsResult{}, err
	}
	if err := domain.ValidateUserID(ownerID); err != nil {
		return ListFinancialGoalsResult{}, ErrInvalidFinancialGoalsOwnerID
	}
	snapshot, err := useCase.reader.Read(ctx, ownerID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ListFinancialGoalsResult{}, err
		}
		return ListFinancialGoalsResult{}, newSafeOperationError(ErrFinancialGoalsSnapshotQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return ListFinancialGoalsResult{}, err
	}
	goals := append([]domain.FinancialGoal(nil), snapshot.Goals...)
	protectedValues := append([]domain.ProtectedValue(nil), snapshot.ProtectedValues...)
	seen := make(map[string]struct{}, len(goals)+len(protectedValues))
	for _, goal := range goals {
		if goal.OwnerID() != ownerID || !financialGoalStructurallyValid(goal) {
			return ListFinancialGoalsResult{}, newSafeOperationError(ErrFinancialGoalsDependency, domain.ErrInvalidFinancialGoalOwnerID)
		}
		if _, exists := seen[goal.ID()]; exists {
			return ListFinancialGoalsResult{}, ErrFinancialGoalDuplicate
		}
		seen[goal.ID()] = struct{}{}
	}
	for _, value := range protectedValues {
		if value.OwnerID() != ownerID || !protectedValueStructurallyValid(value) {
			return ListFinancialGoalsResult{}, newSafeOperationError(ErrFinancialGoalsDependency, domain.ErrInvalidProtectedValueOwnerID)
		}
		if _, exists := seen[value.ID()]; exists {
			return ListFinancialGoalsResult{}, ErrFinancialGoalDuplicate
		}
		seen[value.ID()] = struct{}{}
	}
	sort.Slice(goals, func(left, right int) bool { return goals[left].ID() < goals[right].ID() })
	sort.Slice(protectedValues, func(left, right int) bool { return protectedValues[left].ID() < protectedValues[right].ID() })
	if goals == nil {
		goals = []domain.FinancialGoal{}
	}
	if protectedValues == nil {
		protectedValues = []domain.ProtectedValue{}
	}
	return ListFinancialGoalsResult{goals: goals, protectedValues: protectedValues}, nil
}

// The domain constructors keep fields private. These narrow checks avoid
// exposing a generic rehydration API before persistence is designed.
func financialGoalStructurallyValid(goal domain.FinancialGoal) bool {
	_, err := domain.NewFinancialGoal(domain.FinancialGoalParams{ID: goal.ID(), OwnerID: goal.OwnerID(), Title: goal.Title(), TargetAmount: goal.TargetAmount()})
	return err == nil
}

func protectedValueStructurallyValid(value domain.ProtectedValue) bool {
	_, err := domain.NewProtectedValue(domain.ProtectedValueParams{ID: value.ID(), OwnerID: value.OwnerID(), Label: value.Label(), Amount: value.Amount()})
	return err == nil
}
