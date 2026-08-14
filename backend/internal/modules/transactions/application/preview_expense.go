package application

import (
	"context"

	"jarvis/backend/internal/modules/transactions/domain"
)

// PreviewExpenseResult contains the canonical, non-persisted expense details
// that a channel must present before explicit confirmation.
type PreviewExpenseResult struct {
	Details domain.ExpenseDetails
}

// PreviewExpense validates and normalizes an expense without invoking any
// generated-value or persistence dependency.
type PreviewExpense struct{}

// Execute returns the same canonical details used by the mutating use case.
func (PreviewExpense) Execute(ctx context.Context, input CreateExpenseInput) (PreviewExpenseResult, error) {
	if err := ctx.Err(); err != nil {
		return PreviewExpenseResult{}, err
	}

	details, err := normalizeExpenseInput(input)
	if err != nil {
		return PreviewExpenseResult{}, err
	}
	return PreviewExpenseResult{Details: details}, nil
}
