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
type PreviewExpense struct {
	categoryCatalog CategoryCatalog
}

// NewPreviewExpenseWithCategoryCatalog composes the catalog used only when a
// category is present. The zero value remains valid for legacy uncategorized
// previews.
func NewPreviewExpenseWithCategoryCatalog(categoryCatalog CategoryCatalog) (PreviewExpense, error) {
	if categoryCatalog == nil {
		return PreviewExpense{}, ErrMissingCategoryCatalog
	}
	return PreviewExpense{categoryCatalog: categoryCatalog}, nil
}

// Execute returns the same canonical details used by the mutating use case.
func (useCase PreviewExpense) Execute(ctx context.Context, input CreateExpenseInput) (PreviewExpenseResult, error) {
	if err := ctx.Err(); err != nil {
		return PreviewExpenseResult{}, err
	}

	details, err := normalizeExpenseInput(ctx, useCase.categoryCatalog, input)
	if err != nil {
		return PreviewExpenseResult{}, err
	}
	return PreviewExpenseResult{Details: details}, nil
}
