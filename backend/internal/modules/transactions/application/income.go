package application

import (
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

// CreateIncomeInput carries the semantic data used by both preview and the
// confirmed idempotent record command. Income intentionally has no payment
// method.
type CreateIncomeInput struct {
	UserID            string
	Description       string
	AmountMinor       int64
	Currency          domain.Currency
	OccurredAt        time.Time
	FinancialTimezone string
	Origin            domain.Origin
}

func normalizeIncomeInput(input CreateIncomeInput) (domain.IncomeDetails, error) {
	amount, err := domain.NewMoney(input.AmountMinor, input.Currency)
	if err != nil {
		return domain.IncomeDetails{}, err
	}

	details, err := domain.NormalizeIncomeDetails(domain.IncomeDetails{
		UserID:            input.UserID,
		Description:       input.Description,
		Amount:            amount,
		OccurredAt:        input.OccurredAt,
		FinancialTimezone: input.FinancialTimezone,
		Origin:            input.Origin,
	})
	if err != nil {
		return domain.IncomeDetails{}, err
	}

	details.OccurredAt = canonicalizeFinancialInstant(details.OccurredAt)
	return details, nil
}
