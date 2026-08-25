package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingDismissRecurrenceSuggestionList  = errors.New("dismiss recurrence suggestion: list use case is required")
	ErrMissingRecurrenceSuggestionStore        = errors.New("dismiss recurrence suggestion: store is required")
	ErrMissingDismissRecurrenceSuggestionClock = errors.New("dismiss recurrence suggestion: clock is required")
)

type DismissRecurrenceSuggestionInput struct {
	UserID       string
	SuggestionID RecurrenceSuggestionID
}

type DismissRecurrenceSuggestionResult struct {
	SuggestionID RecurrenceSuggestionID
	Replayed     bool
}

type DismissRecurrenceSuggestion struct {
	list              *ListRecurrenceSuggestions
	suppressionReader RecurrenceSuggestionSuppressionReader
	store             RecurrenceSuggestionSuppressionStore
	clock             Clock
}

func NewDismissRecurrenceSuggestion(
	list *ListRecurrenceSuggestions,
	suppressionReader RecurrenceSuggestionSuppressionReader,
	store RecurrenceSuggestionSuppressionStore,
	clock Clock,
) (*DismissRecurrenceSuggestion, error) {
	if list == nil {
		return nil, ErrMissingDismissRecurrenceSuggestionList
	}
	if suppressionReader == nil {
		return nil, ErrMissingRecurrenceSuggestionSuppressionReader
	}
	if store == nil {
		return nil, ErrMissingRecurrenceSuggestionStore
	}
	if clock == nil {
		return nil, ErrMissingDismissRecurrenceSuggestionClock
	}
	return &DismissRecurrenceSuggestion{list: list, suppressionReader: suppressionReader, store: store, clock: clock}, nil
}

func (useCase *DismissRecurrenceSuggestion) Execute(ctx context.Context, input DismissRecurrenceSuggestionInput) (DismissRecurrenceSuggestionResult, error) {
	if err := ctx.Err(); err != nil {
		return DismissRecurrenceSuggestionResult{}, err
	}
	if err := domain.ValidateUserID(input.UserID); err != nil {
		return DismissRecurrenceSuggestionResult{}, err
	}
	if err := ValidateRecurrenceSuggestionID(input.SuggestionID); err != nil {
		return DismissRecurrenceSuggestionResult{}, err
	}
	dismissed, err := useCase.suppressionReader.ListDismissedRecurrenceSuggestionIDs(ctx, input.UserID, []RecurrenceSuggestionID{input.SuggestionID})
	if err != nil {
		return DismissRecurrenceSuggestionResult{}, preserveRecurrenceSuggestionDependencyError(ErrRecurrenceSuggestionSuppressionQuery, err)
	}
	dismissedSet, err := validateDismissedSuggestionIDs([]RecurrenceSuggestionID{input.SuggestionID}, dismissed)
	if err != nil {
		return DismissRecurrenceSuggestionResult{}, err
	}
	if dismissedSet[input.SuggestionID] {
		return DismissRecurrenceSuggestionResult{SuggestionID: input.SuggestionID, Replayed: true}, nil
	}

	evaluated, err := useCase.list.evaluate(ctx, input.UserID)
	if err != nil {
		return DismissRecurrenceSuggestionResult{}, err
	}
	suggestion, found := findRecurrenceSuggestion(evaluated.items, input.SuggestionID)
	if !found {
		return DismissRecurrenceSuggestionResult{}, ErrRecurrenceSuggestionNotFound
	}
	if evaluated.dismissed[input.SuggestionID] {
		return DismissRecurrenceSuggestionResult{SuggestionID: input.SuggestionID, Replayed: true}, nil
	}
	dismissedAt := canonicalizeFinancialInstant(useCase.clock.Now())
	if dismissedAt.IsZero() {
		return DismissRecurrenceSuggestionResult{}, ErrInvalidRecurrenceSuggestionClock
	}
	stored, err := useCase.store.DismissRecurrenceSuggestion(ctx, DismissRecurrenceSuggestionCommand{
		Operation:    DismissRecurrenceSuggestionOperation,
		UserID:       input.UserID,
		SuggestionID: suggestion.ID,
		Fingerprint:  suggestion.Fingerprint,
		DismissedAt:  dismissedAt,
	})
	if err != nil {
		return DismissRecurrenceSuggestionResult{}, preserveRecurrenceSuggestionDependencyError(ErrRecurrenceSuggestionSuppressionWrite, err)
	}
	return DismissRecurrenceSuggestionResult{SuggestionID: suggestion.ID, Replayed: stored.Replayed}, nil
}

func findRecurrenceSuggestion(items []RecurrenceSuggestion, id RecurrenceSuggestionID) (RecurrenceSuggestion, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return RecurrenceSuggestion{}, false
}
