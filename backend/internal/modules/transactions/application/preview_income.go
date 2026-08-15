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
type PreviewIncome struct{}

// Execute returns the same canonical details used by RecordIncome.
func (PreviewIncome) Execute(ctx context.Context, input CreateIncomeInput) (PreviewIncomeResult, error) {
	if err := ctx.Err(); err != nil {
		return PreviewIncomeResult{}, err
	}

	details, err := normalizeIncomeInput(input)
	if err != nil {
		return PreviewIncomeResult{}, err
	}
	return PreviewIncomeResult{Details: details}, nil
}
