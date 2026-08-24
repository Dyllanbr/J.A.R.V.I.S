package application

import (
	"context"

	"jarvis/backend/internal/modules/transactions/domain"
)

// PreviewRecurrenceResult is the canonical, non-persisted commitment shown
// before explicit confirmation.
type PreviewRecurrenceResult struct {
	Details domain.RecurrenceDetails
}

// PreviewRecurrence validates and normalizes without generated values or I/O.
type PreviewRecurrence struct{}

func (PreviewRecurrence) Execute(ctx context.Context, input RecurrenceInput) (PreviewRecurrenceResult, error) {
	details, err := normalizeRecurrenceInput(ctx, input)
	if err != nil {
		return PreviewRecurrenceResult{}, err
	}
	return PreviewRecurrenceResult{Details: details}, nil
}
