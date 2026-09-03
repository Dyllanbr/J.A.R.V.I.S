package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrQueryMonthlyTransactions = errors.New("transactions postgres repository: monthly query failed")
	ErrLoadMonthlyTransaction   = errors.New("transactions postgres repository: stored monthly transaction is invalid")
)

var _ application.MonthlyTransactionReader = (*ExpenseRepository)(nil)

// ListMonthlyTransactions returns a mixed read projection for one owner and
// an already computed inclusive/exclusive financial-month interval.
func (repository *ExpenseRepository) ListMonthlyTransactions(
	ctx context.Context,
	query application.ExpenseMonthQuery,
) ([]application.MonthlyTransaction, error) {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	rows, err := repository.pool.Query(operationContext, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, category_id, credit_card_id, statement_due_on, occurred_at, financial_timezone, origin,
			status, version, created_at, updated_at
		FROM transactions
		WHERE user_id = $1
		  AND occurred_at >= $2
		  AND occurred_at < $3
		  AND financial_timezone = $4
		ORDER BY occurred_at DESC, id DESC
	`, query.UserID, query.Start, query.End, query.FinancialTimezone)
	if err != nil {
		return nil, newRepositoryError(ErrQueryMonthlyTransactions, err)
	}
	defer rows.Close()

	transactions := make([]application.MonthlyTransaction, 0)
	for rows.Next() {
		transaction, err := scanMonthlyTransaction(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrQueryMonthlyTransactions, err)
	}
	return transactions, nil
}

func scanMonthlyTransaction(row rowScanner) (application.MonthlyTransaction, error) {
	var (
		id, userID, transactionType, description string
		currency, timezone, origin, status       string
		paymentMethod                            *string
		categoryID                               *string
		creditCardID                             *string
		statementDueOn                           pgtype.Date
		amountMinor, version                     int64
		occurredAt, createdAt, updatedAt         time.Time
	)
	if err := row.Scan(
		&id, &userID, &transactionType, &description, &amountMinor, &currency,
		&paymentMethod, &categoryID, &creditCardID, &statementDueOn, &occurredAt, &timezone, &origin, &status, &version,
		&createdAt, &updatedAt,
	); err != nil {
		return application.MonthlyTransaction{}, newRepositoryError(ErrLoadMonthlyTransaction, err)
	}
	if version != 1 || !updatedAt.Equal(createdAt) {
		return application.MonthlyTransaction{}, ErrLoadMonthlyTransaction
	}

	amount, err := domain.NewMoney(amountMinor, domain.Currency(currency))
	if err != nil {
		return application.MonthlyTransaction{}, newRepositoryError(ErrLoadMonthlyTransaction, err)
	}
	storedCategoryID, err := categoryIDFromDatabase(categoryID)
	if err != nil {
		return application.MonthlyTransaction{}, newRepositoryError(ErrLoadMonthlyTransaction, err)
	}

	switch domain.TransactionType(transactionType) {
	case domain.TransactionTypeExpense:
		if paymentMethod == nil || status != string(domain.ExpenseStatusRecorded) {
			return application.MonthlyTransaction{}, ErrLoadMonthlyTransaction
		}
		var dueOn *domain.CivilDate
		if statementDueOn.Valid || statementDueOn.InfinityModifier != pgtype.Finite {
			value, dateErr := civilDateFromPostgres(statementDueOn)
			if dateErr != nil {
				return application.MonthlyTransaction{}, newRepositoryError(ErrLoadMonthlyTransaction, dateErr)
			}
			dueOn = &value
		}
		if (creditCardID == nil) != (dueOn == nil) {
			return application.MonthlyTransaction{}, ErrLoadMonthlyTransaction
		}
		expense, err := domain.NewExpense(domain.ExpenseParams{
			ID: id,
			Details: domain.ExpenseDetails{
				UserID:            userID,
				Description:       description,
				Amount:            amount,
				PaymentMethod:     domain.PaymentMethod(*paymentMethod),
				CategoryID:        storedCategoryID,
				CreditCardID:      creditCardID,
				StatementDueOn:    dueOn,
				OccurredAt:        occurredAt,
				FinancialTimezone: timezone,
				Origin:            domain.Origin(origin),
			},
			CreatedAt: createdAt,
		})
		if err != nil {
			return application.MonthlyTransaction{}, newRepositoryError(ErrLoadMonthlyTransaction, err)
		}
		return application.NewMonthlyTransactionFromExpense(expense), nil

	case domain.TransactionTypeIncome:
		if paymentMethod != nil || status != string(domain.IncomeStatusRecorded) {
			return application.MonthlyTransaction{}, ErrLoadMonthlyTransaction
		}
		income, err := domain.NewIncome(domain.IncomeParams{
			ID: id,
			Details: domain.IncomeDetails{
				UserID:            userID,
				Description:       description,
				Amount:            amount,
				CategoryID:        storedCategoryID,
				OccurredAt:        occurredAt,
				FinancialTimezone: timezone,
				Origin:            domain.Origin(origin),
			},
			CreatedAt: createdAt,
		})
		if err != nil {
			return application.MonthlyTransaction{}, newRepositoryError(ErrLoadMonthlyTransaction, err)
		}
		return application.NewMonthlyTransactionFromIncome(income), nil

	default:
		return application.MonthlyTransaction{}, ErrLoadMonthlyTransaction
	}
}
