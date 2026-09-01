package application

import (
	"context"
	"errors"
	"sort"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingCreditCardListReader = errors.New("list credit cards: reader is required")
	ErrCreditCardListQuery         = errors.New("list credit cards: query failed")
)

type CreditCardListReader interface {
	ListCreditCards(context.Context, string) ([]domain.CreditCard, error)
}

type ListCreditCardsResult struct {
	Items []domain.CreditCard
}

type ListCreditCards struct {
	reader CreditCardListReader
}

func NewListCreditCards(reader CreditCardListReader) (*ListCreditCards, error) {
	if reader == nil {
		return nil, ErrMissingCreditCardListReader
	}
	return &ListCreditCards{reader: reader}, nil
}

// Execute orders ACTIVE before ARCHIVED, then canonical name ascending,
// creation instant descending and opaque ID ascending as the final tie-break.
func (useCase *ListCreditCards) Execute(ctx context.Context, userID string) (ListCreditCardsResult, error) {
	if err := ctx.Err(); err != nil {
		return ListCreditCardsResult{}, err
	}
	if err := domain.ValidateCreditCardUserID(userID); err != nil {
		return ListCreditCardsResult{}, err
	}
	items, err := useCase.reader.ListCreditCards(ctx, userID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ListCreditCardsResult{}, err
		}
		return ListCreditCardsResult{}, newSafeOperationError(ErrCreditCardListQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return ListCreditCardsResult{}, err
	}
	if items == nil {
		items = []domain.CreditCard{}
	} else {
		items = append([]domain.CreditCard(nil), items...)
	}
	seen := make(map[string]struct{}, len(items))
	for _, card := range items {
		if err := validateCreditCardDependencySnapshot(card, userID, "", nil); err != nil {
			return ListCreditCardsResult{}, err
		}
		if _, duplicated := seen[card.ID()]; duplicated {
			return ListCreditCardsResult{}, ErrCreditCardDependencyResult
		}
		seen[card.ID()] = struct{}{}
	}
	sort.Slice(items, func(left, right int) bool {
		return creditCardComesBefore(items[left], items[right])
	})
	return ListCreditCardsResult{Items: items}, nil
}

func creditCardComesBefore(left, right domain.CreditCard) bool {
	if left.Status() != right.Status() {
		return left.Status() == domain.CreditCardStatusActive
	}
	if left.Name().String() != right.Name().String() {
		return left.Name().String() < right.Name().String()
	}
	if !left.CreatedAt().Equal(right.CreatedAt()) {
		return left.CreatedAt().After(right.CreatedAt())
	}
	return left.ID() < right.ID()
}
