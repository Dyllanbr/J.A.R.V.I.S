package application

import (
	"context"
	"errors"
	"sort"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingRecurrenceSuggestionExpenseReader     = errors.New("list recurrence suggestions: expense reader is required")
	ErrMissingRecurrenceSuggestionRecurrenceReader  = errors.New("list recurrence suggestions: recurrence reader is required")
	ErrMissingRecurrenceSuggestionSuppressionReader = errors.New("list recurrence suggestions: suppression reader is required")
	ErrMissingRecurrenceSuggestionClock             = errors.New("list recurrence suggestions: clock is required")
)

type ListRecurrenceSuggestionsResult struct {
	Items []RecurrenceSuggestion
}

type evaluatedRecurrenceSuggestions struct {
	items     []RecurrenceSuggestion
	dismissed map[RecurrenceSuggestionID]bool
}

type ListRecurrenceSuggestions struct {
	expenseReader     RecurrenceSuggestionExpenseReader
	recurrenceReader  RecurrenceReader
	suppressionReader RecurrenceSuggestionSuppressionReader
	clock             Clock
	location          *time.Location
}

func NewListRecurrenceSuggestions(
	expenseReader RecurrenceSuggestionExpenseReader,
	recurrenceReader RecurrenceReader,
	suppressionReader RecurrenceSuggestionSuppressionReader,
	clock Clock,
) (*ListRecurrenceSuggestions, error) {
	if expenseReader == nil {
		return nil, ErrMissingRecurrenceSuggestionExpenseReader
	}
	if recurrenceReader == nil {
		return nil, ErrMissingRecurrenceSuggestionRecurrenceReader
	}
	if suppressionReader == nil {
		return nil, ErrMissingRecurrenceSuggestionSuppressionReader
	}
	if clock == nil {
		return nil, ErrMissingRecurrenceSuggestionClock
	}
	location, err := time.LoadLocation(FinancialTimezone)
	if err != nil {
		return nil, newSafeOperationError(ErrFinancialTimezoneUnavailable, err)
	}
	return &ListRecurrenceSuggestions{
		expenseReader:     expenseReader,
		recurrenceReader:  recurrenceReader,
		suppressionReader: suppressionReader,
		clock:             clock,
		location:          location,
	}, nil
}

func (useCase *ListRecurrenceSuggestions) Execute(ctx context.Context, userID string) (ListRecurrenceSuggestionsResult, error) {
	evaluated, err := useCase.evaluate(ctx, userID)
	if err != nil {
		return ListRecurrenceSuggestionsResult{}, err
	}
	visible := make([]RecurrenceSuggestion, 0, len(evaluated.items))
	for _, item := range evaluated.items {
		if !evaluated.dismissed[item.ID] {
			visible = append(visible, item)
		}
	}
	return ListRecurrenceSuggestionsResult{Items: visible}, nil
}

func (useCase *ListRecurrenceSuggestions) evaluate(ctx context.Context, userID string) (evaluatedRecurrenceSuggestions, error) {
	if err := ctx.Err(); err != nil {
		return evaluatedRecurrenceSuggestions{}, err
	}
	if err := domain.ValidateUserID(userID); err != nil {
		return evaluatedRecurrenceSuggestions{}, err
	}
	now := useCase.clock.Now()
	if now.IsZero() {
		return evaluatedRecurrenceSuggestions{}, ErrInvalidRecurrenceSuggestionClock
	}
	evaluatedOn, start, end, err := recurrenceSuggestionObservationWindow(now, useCase.location)
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, err
	}

	expenses, err := useCase.expenseReader.ListRecurrenceSuggestionExpenses(ctx, RecurrenceSuggestionExpenseQuery{
		UserID:            userID,
		Start:             start,
		End:               end,
		FinancialTimezone: FinancialTimezone,
	})
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, preserveRecurrenceSuggestionDependencyError(ErrRecurrenceSuggestionExpenseQuery, err)
	}
	observations, err := useCase.buildObservations(userID, expenses)
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, err
	}

	recurrences, err := useCase.recurrenceReader.ListRecurrences(ctx, userID)
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, preserveRecurrenceSuggestionDependencyError(ErrRecurrenceSuggestionRecurrenceQuery, err)
	}
	exclusions, err := useCase.buildExclusions(userID, recurrences)
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, err
	}
	detected, err := domain.DetectRecurrenceSuggestions(domain.DetectRecurrenceSuggestionsParams{
		EvaluatedOn:  evaluatedOn,
		Observations: observations,
		Exclusions:   exclusions,
	})
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, err
	}

	items := make([]RecurrenceSuggestion, 0, len(detected))
	ids := make([]RecurrenceSuggestionID, 0, len(detected))
	for _, suggestion := range detected {
		id, fingerprint := deriveRecurrenceSuggestionIdentity(userID, suggestion)
		items = append(items, RecurrenceSuggestion{ID: id, Fingerprint: fingerprint, Suggestion: suggestion})
		ids = append(ids, id)
	}
	if len(items) == 0 {
		return evaluatedRecurrenceSuggestions{items: []RecurrenceSuggestion{}, dismissed: map[RecurrenceSuggestionID]bool{}}, nil
	}

	dismissedIDs, err := useCase.suppressionReader.ListDismissedRecurrenceSuggestionIDs(ctx, userID, append([]RecurrenceSuggestionID(nil), ids...))
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, preserveRecurrenceSuggestionDependencyError(ErrRecurrenceSuggestionSuppressionQuery, err)
	}
	dismissed, err := validateDismissedSuggestionIDs(ids, dismissedIDs)
	if err != nil {
		return evaluatedRecurrenceSuggestions{}, err
	}
	sort.Slice(items, func(left, right int) bool {
		return recurrenceSuggestionComesBefore(items[left], items[right])
	})
	return evaluatedRecurrenceSuggestions{items: items, dismissed: dismissed}, nil
}

func (useCase *ListRecurrenceSuggestions) buildObservations(userID string, expenses []domain.Expense) ([]domain.RecurrenceSuggestionObservation, error) {
	observations := make([]domain.RecurrenceSuggestionObservation, 0, len(expenses))
	for _, expense := range expenses {
		if expense.UserID() != userID || expense.Type() != domain.TransactionTypeExpense {
			return nil, ErrRecurrenceSuggestionOwnerIsolation
		}
		occurred := expense.OccurredAt().In(useCase.location)
		occurredOn, err := domain.NewCivilDate(occurred.Year(), occurred.Month(), occurred.Day())
		if err != nil {
			return nil, err
		}
		observation, err := domain.NewRecurrenceSuggestionObservation(domain.RecurrenceSuggestionObservationParams{
			ExpenseID:   expense.ID(),
			Description: expense.Description(),
			Amount:      expense.Amount(),
			OccurredOn:  occurredOn,
		})
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func (useCase *ListRecurrenceSuggestions) buildExclusions(userID string, recurrences []domain.Recurrence) ([]domain.RecurrenceSuggestionExclusion, error) {
	exclusions := make([]domain.RecurrenceSuggestionExclusion, 0, len(recurrences))
	for _, recurrence := range recurrences {
		if recurrence.UserID() != userID || recurrence.Type() != domain.TransactionTypeExpense {
			return nil, ErrRecurrenceSuggestionOwnerIsolation
		}
		params := domain.RecurrenceSuggestionExclusionParams{
			Description: recurrence.Description(),
			Amount:      recurrence.ExpectedAmount(),
			Status:      recurrence.Status(),
		}
		if cancelledAt, ok := recurrence.CancelledAt(); ok {
			local := cancelledAt.In(useCase.location)
			cancelledOn, err := domain.NewCivilDate(local.Year(), local.Month(), local.Day())
			if err != nil {
				return nil, err
			}
			params.CancelledOn = &cancelledOn
		}
		exclusion, err := domain.NewRecurrenceSuggestionExclusion(params)
		if err != nil {
			return nil, err
		}
		exclusions = append(exclusions, exclusion)
	}
	return exclusions, nil
}

func recurrenceSuggestionObservationWindow(now time.Time, location *time.Location) (domain.CivilDate, time.Time, time.Time, error) {
	local := now.In(location)
	evaluatedOn, err := domain.NewCivilDate(local.Year(), local.Month(), local.Day())
	if err != nil {
		return domain.CivilDate{}, time.Time{}, time.Time{}, err
	}
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location).AddDate(0, -5, 0)
	end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
	return evaluatedOn, start.UTC(), end.UTC(), nil
}

func validateDismissedSuggestionIDs(requested, returned []RecurrenceSuggestionID) (map[RecurrenceSuggestionID]bool, error) {
	requestedSet := make(map[RecurrenceSuggestionID]bool, len(requested))
	for _, id := range requested {
		requestedSet[id] = true
	}
	dismissed := make(map[RecurrenceSuggestionID]bool, len(returned))
	for _, id := range returned {
		if ValidateRecurrenceSuggestionID(id) != nil || !requestedSet[id] {
			return nil, ErrRecurrenceSuggestionOwnerIsolation
		}
		dismissed[id] = true
	}
	return dismissed, nil
}

func recurrenceSuggestionComesBefore(left, right RecurrenceSuggestion) bool {
	leftSuggestion := left.Suggestion
	rightSuggestion := right.Suggestion
	if !leftSuggestion.ProposedStartsOn().Equal(rightSuggestion.ProposedStartsOn()) {
		return leftSuggestion.ProposedStartsOn().Before(rightSuggestion.ProposedStartsOn())
	}
	if leftSuggestion.NormalizedDescription() != rightSuggestion.NormalizedDescription() {
		return leftSuggestion.NormalizedDescription() < rightSuggestion.NormalizedDescription()
	}
	if leftSuggestion.ExpectedAmount().MinorUnits() != rightSuggestion.ExpectedAmount().MinorUnits() {
		return leftSuggestion.ExpectedAmount().MinorUnits() < rightSuggestion.ExpectedAmount().MinorUnits()
	}
	return left.ID < right.ID
}
