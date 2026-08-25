package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var ErrMissingPrepareSuggestedRecurrenceList = errors.New("prepare suggested recurrence: list use case is required")

type PrepareSuggestedRecurrenceInput struct {
	UserID       string
	SuggestionID RecurrenceSuggestionID
}

type PrepareSuggestedRecurrenceResult struct {
	SuggestionID RecurrenceSuggestionID
	Preview      PreviewRecurrenceResult
}

// PrepareSuggestedRecurrence recalculates the current server-side inference
// and reuses PreviewRecurrence. It cannot persist a confirmed recurrence.
type PrepareSuggestedRecurrence struct {
	list    *ListRecurrenceSuggestions
	preview PreviewRecurrence
}

func NewPrepareSuggestedRecurrence(list *ListRecurrenceSuggestions) (*PrepareSuggestedRecurrence, error) {
	if list == nil {
		return nil, ErrMissingPrepareSuggestedRecurrenceList
	}
	return &PrepareSuggestedRecurrence{list: list, preview: PreviewRecurrence{}}, nil
}

func (useCase *PrepareSuggestedRecurrence) Execute(ctx context.Context, input PrepareSuggestedRecurrenceInput) (PrepareSuggestedRecurrenceResult, error) {
	if err := ctx.Err(); err != nil {
		return PrepareSuggestedRecurrenceResult{}, err
	}
	if err := domain.ValidateUserID(input.UserID); err != nil {
		return PrepareSuggestedRecurrenceResult{}, err
	}
	if err := ValidateRecurrenceSuggestionID(input.SuggestionID); err != nil {
		return PrepareSuggestedRecurrenceResult{}, err
	}
	evaluated, err := useCase.list.evaluate(ctx, input.UserID)
	if err != nil {
		return PrepareSuggestedRecurrenceResult{}, err
	}
	suggestion, found := findRecurrenceSuggestion(evaluated.items, input.SuggestionID)
	if !found {
		return PrepareSuggestedRecurrenceResult{}, ErrRecurrenceSuggestionNotFound
	}
	if evaluated.dismissed[input.SuggestionID] {
		return PrepareSuggestedRecurrenceResult{}, ErrRecurrenceSuggestionSuppressed
	}
	preview, err := useCase.preview.Execute(ctx, RecurrenceInput{
		UserID:          input.UserID,
		TransactionType: domain.TransactionTypeExpense,
		Description:     suggestion.Suggestion.Description(),
		ExpectedAmount:  suggestion.Suggestion.ExpectedAmount().MinorUnits(),
		Currency:        suggestion.Suggestion.ExpectedAmount().Currency(),
		Frequency:       domain.RecurrenceFrequencyMonthly,
		StartsOn:        suggestion.Suggestion.ProposedStartsOn(),
	})
	if err != nil {
		return PrepareSuggestedRecurrenceResult{}, err
	}
	return PrepareSuggestedRecurrenceResult{SuggestionID: suggestion.ID, Preview: preview}, nil
}
