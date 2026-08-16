package application

import (
	"context"

	"jarvis/backend/internal/modules/transactions/domain"
)

// PreviewIncomeResult contains canonical, non-persisted income details for
// review before explicit confirmation.
type PreviewIncomeResult struct {
	Details domain.IncomeDetails
}

// PreviewIncome validates and normalizes income without generated values or
// persistence dependencies.
type PreviewIncome struct {
	categoryCatalog CategoryCatalog
}

// NewPreviewIncomeWithCategoryCatalog composes category validation while the
// zero value remains compatible with uncategorized previews.
func NewPreviewIncomeWithCategoryCatalog(categoryCatalog CategoryCatalog) (PreviewIncome, error) {
	if categoryCatalog == nil {
		return PreviewIncome{}, ErrMissingCategoryCatalog
	}
	return PreviewIncome{categoryCatalog: categoryCatalog}, nil
}

// Execute returns the same canonical details used by RecordIncome.
func (useCase PreviewIncome) Execute(ctx context.Context, input CreateIncomeInput) (PreviewIncomeResult, error) {
	if err := ctx.Err(); err != nil {
		return PreviewIncomeResult{}, err
	}

	details, err := normalizeIncomeInput(ctx, useCase.categoryCatalog, input)
	if err != nil {
		return PreviewIncomeResult{}, err
	}
	return PreviewIncomeResult{Details: details}, nil
}
