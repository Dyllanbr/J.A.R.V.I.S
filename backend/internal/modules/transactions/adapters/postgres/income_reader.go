package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"jarvis/backend/internal/modules/transactions/domain"
)

var ErrLoadIncome = errors.New("income postgres repository: stored income is invalid")

func loadIncome(ctx context.Context, transaction pgx.Tx, id, userID string) (domain.Income, error) {
	return scanIncome(transaction.QueryRow(ctx, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, occurred_at, financial_timezone, origin,
			status, version, created_at, updated_at
		FROM transactions
		WHERE id = $1 AND user_id = $2 AND type = 'INCOME'
	`, id, userID))
}

func scanIncome(row rowScanner) (domain.Income, error) {
	var (
		id, userID, transactionType, description string
		currency, timezone, origin, status       string
		paymentMethod                            *string
		amountMinor, version                     int64
		occurredAt, createdAt, updatedAt         time.Time
	)
	if err := row.Scan(
		&id, &userID, &transactionType, &description, &amountMinor, &currency,
		&paymentMethod, &occurredAt, &timezone, &origin, &status, &version,
		&createdAt, &updatedAt,
	); err != nil {
		return domain.Income{}, newRepositoryError(ErrLoadIncome, err)
	}
	if transactionType != string(domain.TransactionTypeIncome) || paymentMethod != nil ||
		status != string(domain.IncomeStatusRecorded) || version != 1 ||
		!updatedAt.Equal(createdAt) {
		return domain.Income{}, ErrLoadIncome
	}

	amount, err := domain.NewMoney(amountMinor, domain.Currency(currency))
	if err != nil {
		return domain.Income{}, newRepositoryError(ErrLoadIncome, err)
	}
	income, err := domain.NewIncome(domain.IncomeParams{
		ID: id,
		Details: domain.IncomeDetails{
			UserID:            userID,
			Description:       description,
			Amount:            amount,
			OccurredAt:        occurredAt,
			FinancialTimezone: timezone,
			Origin:            domain.Origin(origin),
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		return domain.Income{}, newRepositoryError(ErrLoadIncome, err)
	}
	return income, nil
}
