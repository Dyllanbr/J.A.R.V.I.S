package application

import (
	"context"
	"errors"
	"sort"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingRecurrenceReader = errors.New("list recurrences: reader is required")
	ErrRecurrenceQuery         = errors.New("list recurrences: query failed")
)

// RecurrenceReader is the owner-scoped read capability required by the list
// use case. Implementations must not return another owner's commitments.
type RecurrenceReader interface {
	ListRecurrences(context.Context, string) ([]domain.Recurrence, error)
}

type ListRecurrencesResult struct {
	Items []domain.Recurrence
}

type ListRecurrences struct {
	reader RecurrenceReader
}

func NewListRecurrences(reader RecurrenceReader) (*ListRecurrences, error) {
	if reader == nil {
		return nil, ErrMissingRecurrenceReader
	}
	return &ListRecurrences{reader: reader}, nil
}

// Execute lists ACTIVE commitments before CANCELLED commitments, then uses
// StartsOn, CreatedAt and ID descending for deterministic recency ordering.
func (useCase *ListRecurrences) Execute(ctx context.Context, userID string) (ListRecurrencesResult, error) {
	if err := ctx.Err(); err != nil {
		return ListRecurrencesResult{}, err
	}
	if err := domain.ValidateRecurrenceUserID(userID); err != nil {
		return ListRecurrencesResult{}, err
	}
	items, err := useCase.reader.ListRecurrences(ctx, userID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ListRecurrencesResult{}, err
		}
		return ListRecurrencesResult{}, newSafeOperationError(ErrRecurrenceQuery, err)
	}
	if items == nil {
		items = []domain.Recurrence{}
	} else {
		items = append([]domain.Recurrence(nil), items...)
	}
	sort.SliceStable(items, func(left, right int) bool {
		return recurrenceComesBefore(items[left], items[right])
	})
	return ListRecurrencesResult{Items: items}, nil
}

func recurrenceComesBefore(left, right domain.Recurrence) bool {
	if left.Status() != right.Status() {
		return left.Status() == domain.RecurrenceStatusActive
	}
	if !left.StartsOn().Equal(right.StartsOn()) {
		return right.StartsOn().Before(left.StartsOn())
	}
	if !left.CreatedAt().Equal(right.CreatedAt()) {
		return left.CreatedAt().After(right.CreatedAt())
	}
	return left.ID() > right.ID()
}
