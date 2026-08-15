package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrQueryExpenses = errors.New("expense postgres repository: expense query failed")
	ErrLoadExpense   = errors.New("expense postgres repository: stored expense is invalid")
)

var _ application.ExpenseReader = (*ExpenseRepository)(nil)

// ListByFinancialMonth returns one owner's expenses in a caller-computed
// inclusive/exclusive interval with deterministic total ordering.
func (repository *ExpenseRepository) ListByFinancialMonth(
	ctx context.Context,
	query application.ExpenseMonthQuery,
) ([]domain.Expense, error) {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	rows, err := repository.pool.Query(operationContext, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, occurred_at, financial_timezone, origin,
			status, version, created_at, updated_at
		FROM transactions
		WHERE user_id = $1
		  AND type = 'EXPENSE'
		  AND occurred_at >= $2
		  AND occurred_at < $3
		  AND financial_timezone = $4
		ORDER BY occurred_at DESC, id DESC
	`, query.UserID, query.Start, query.End, query.FinancialTimezone)
	if err != nil {
		return nil, newRepositoryError(ErrQueryExpenses, err)
	}
	defer rows.Close()

	expenses := make([]domain.Expense, 0)
	for rows.Next() {
		expense, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		expenses = append(expenses, expense)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrQueryExpenses, err)
	}
	return expenses, nil
}

type rowScanner interface {
	Scan(...any) error
}

func loadExpense(ctx context.Context, transaction pgx.Tx, id, userID string) (domain.Expense, error) {
	return scanExpense(transaction.QueryRow(ctx, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, occurred_at, financial_timezone, origin,
			status, version, created_at, updated_at
		FROM transactions
		WHERE id = $1 AND user_id = $2 AND type = 'EXPENSE'
	`, id, userID))
}

func scanExpense(row rowScanner) (domain.Expense, error) {
	var (
		id, userID, transactionType, description string
		currency, paymentMethod, timezone        string
		origin, status                           string
		amountMinor, version                     int64
		occurredAt, createdAt, updatedAt         time.Time
	)
	if err := row.Scan(
		&id, &userID, &transactionType, &description, &amountMinor, &currency,
		&paymentMethod, &occurredAt, &timezone, &origin, &status, &version,
		&createdAt, &updatedAt,
	); err != nil {
		return domain.Expense{}, newRepositoryError(ErrLoadExpense, err)
	}
	if transactionType != string(domain.TransactionTypeExpense) ||
		status != string(domain.ExpenseStatusRecorded) || version != 1 ||
		!updatedAt.Equal(createdAt) {
		return domain.Expense{}, ErrLoadExpense
	}

	amount, err := domain.NewMoney(amountMinor, domain.Currency(currency))
	if err != nil {
		return domain.Expense{}, newRepositoryError(ErrLoadExpense, err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID:            userID,
			Description:       description,
			Amount:            amount,
			PaymentMethod:     domain.PaymentMethod(paymentMethod),
			OccurredAt:        occurredAt,
			FinancialTimezone: timezone,
			Origin:            domain.Origin(origin),
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		return domain.Expense{}, newRepositoryError(ErrLoadExpense, err)
	}
	return expense, nil
}
