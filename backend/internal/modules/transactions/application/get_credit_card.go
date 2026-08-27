package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingCreditCardLookupReader = errors.New("get credit card: reader is required")
	ErrCreditCardLookup              = errors.New("get credit card: query failed")
)

type CreditCardLookup struct {
	CreditCard domain.CreditCard
	Found      bool
}

// CreditCardLookupReader must scope every lookup by owner. Found=false is the
// common result for unknown and cross-owner identifiers.
type CreditCardLookupReader interface {
	FindCreditCard(context.Context, string, string) (CreditCardLookup, error)
}

type GetCreditCardInput struct {
	UserID       string
	CreditCardID string
}

type GetCreditCardResult struct {
	CreditCard domain.CreditCard
}

type GetCreditCard struct {
	reader CreditCardLookupReader
}

func NewGetCreditCard(reader CreditCardLookupReader) (*GetCreditCard, error) {
	if reader == nil {
		return nil, ErrMissingCreditCardLookupReader
	}
	return &GetCreditCard{reader: reader}, nil
}

func (useCase *GetCreditCard) Execute(ctx context.Context, input GetCreditCardInput) (GetCreditCardResult, error) {
	if err := ctx.Err(); err != nil {
		return GetCreditCardResult{}, err
	}
	if err := domain.ValidateCreditCardUserID(input.UserID); err != nil {
		return GetCreditCardResult{}, err
	}
	if err := domain.ValidateCreditCardID(input.CreditCardID); err != nil {
		return GetCreditCardResult{}, err
	}
	lookup, err := useCase.reader.FindCreditCard(ctx, input.UserID, input.CreditCardID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return GetCreditCardResult{}, err
		}
		return GetCreditCardResult{}, newSafeOperationError(ErrCreditCardLookup, err)
	}
	if err := ctx.Err(); err != nil {
		return GetCreditCardResult{}, err
	}
	if !lookup.Found {
		return GetCreditCardResult{}, ErrCreditCardNotFound
	}
	if err := validateCreditCardDependencySnapshot(lookup.CreditCard, input.UserID, input.CreditCardID, nil); err != nil {
		return GetCreditCardResult{}, err
	}
	return GetCreditCardResult{CreditCard: lookup.CreditCard}, nil
}
