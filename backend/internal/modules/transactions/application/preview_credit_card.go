package application

import (
	"context"

	"jarvis/backend/internal/modules/transactions/domain"
)

// CreditCardPreview is the canonical review representation. It intentionally
// excludes owner, ID, lifecycle, timestamps and fingerprint data.
type CreditCardPreview struct {
	Name             string
	LastFour         *string
	Brand            *domain.CreditCardBrand
	ClosingDayAnchor int
	DueDayAnchor     int
	CreditLimit      *domain.Money
}

type PreviewCreditCardResult struct {
	Preview CreditCardPreview
}

// PreviewCreditCard validates and canonicalizes without dependencies or I/O.
type PreviewCreditCard struct{}

func (PreviewCreditCard) Execute(ctx context.Context, input CreditCardInput) (PreviewCreditCardResult, error) {
	details, err := normalizeCreditCardInput(ctx, input)
	if err != nil {
		return PreviewCreditCardResult{}, err
	}
	return PreviewCreditCardResult{Preview: creditCardPreviewFromDetails(details)}, nil
}

func creditCardPreviewFromDetails(details domain.CreditCardDetails) CreditCardPreview {
	preview := CreditCardPreview{
		Name:             details.Name.String(),
		ClosingDayAnchor: details.ClosingDayAnchor.Day(),
		DueDayAnchor:     details.DueDayAnchor.Day(),
	}
	if details.LastFour != nil {
		lastFour := details.LastFour.String()
		preview.LastFour = &lastFour
	}
	if details.Brand != nil {
		brand := *details.Brand
		preview.Brand = &brand
	}
	if details.CreditLimit != nil {
		limit := *details.CreditLimit
		preview.CreditLimit = &limit
	}
	return preview
}
