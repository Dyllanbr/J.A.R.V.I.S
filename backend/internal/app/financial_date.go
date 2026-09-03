package app

import (
	"context"
	"errors"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type financialDateProvider struct{}

var _ application.FinancialDateProvider = financialDateProvider{}

func (financialDateProvider) CurrentFinancialDate(ctx context.Context) (domain.CivilDate, error) {
	if err := ctx.Err(); err != nil {
		return domain.CivilDate{}, err
	}
	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		return domain.CivilDate{}, errors.New("financial date timezone unavailable")
	}
	now := time.Now().In(location)
	return domain.NewCivilDate(now.Year(), now.Month(), now.Day())
}
