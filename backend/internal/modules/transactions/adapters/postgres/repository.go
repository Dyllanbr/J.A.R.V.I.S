package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

const (
	rollbackTimeout         = 2 * time.Second
	maximumOperationTimeout = 30 * time.Second
)

var (
	ErrMissingPool       = errors.New("expense postgres repository: pool is required")
	ErrInvalidTimeout    = errors.New("expense postgres repository: operation timeout is invalid")
	ErrBeginTransaction  = errors.New("expense postgres repository: begin transaction failed")
	ErrInsertExpense     = errors.New("expense postgres repository: expense insert failed")
	ErrInsertAuditEvent  = errors.New("expense postgres repository: audit event insert failed")
	ErrCommitTransaction = errors.New("expense postgres repository: commit failed")
)

// ExpenseRepository persists an Expense and its audit event atomically.
type ExpenseRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var _ application.ExpenseRepository = (*ExpenseRepository)(nil)

// NewExpenseRepository creates the PostgreSQL adapter for the existing port.
func NewExpenseRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*ExpenseRepository, error) {
	if pool == nil {
		return nil, ErrMissingPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidTimeout
	}

	return &ExpenseRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

// Save writes the expense and EXPENSE_RECORDED audit event in one transaction.
func (repository *ExpenseRepository) Save(ctx context.Context, expense domain.Expense) error {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		return newRepositoryError(ErrBeginTransaction, err)
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackContext, cancelRollback := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancelRollback()
		_ = transaction.Rollback(rollbackContext)
	}()

	if err := insertExpense(operationContext, transaction, expense); err != nil {
		return err
	}

	if err := insertAuditEvent(operationContext, transaction, expense); err != nil {
		return err
	}

	if err := transaction.Commit(operationContext); err != nil {
		return newRepositoryError(ErrCommitTransaction, err)
	}
	committed = true

	return nil
}

func insertExpense(ctx context.Context, transaction pgx.Tx, expense domain.Expense) error {
	categoryID, hasCategory := expense.CategoryID()
	var storedCategoryID any
	if hasCategory {
		storedCategoryID = categoryID.String()
	}
	creditCardID, hasCreditCard := expense.CreditCardID()
	var storedCreditCardID any
	var statementDueOn any
	if hasCreditCard {
		storedCreditCardID = creditCardID
		dueOn, ok := expense.StatementDueOn()
		if !ok {
			return ErrInsertExpense
		}
		statementDueOn = postgresDate(dueOn)
	}

	if _, err := transaction.Exec(ctx, `
		INSERT INTO transactions (
			id,
			user_id,
			type,
			description,
			amount_minor,
			currency,
			payment_method,
			category_id,
			credit_card_id,
			statement_due_on,
			occurred_at,
			financial_timezone,
			origin,
			status,
			version,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`,
		expense.ID(),
		expense.UserID(),
		expense.Type(),
		expense.Description(),
		expense.Amount().MinorUnits(),
		expense.Amount().Currency(),
		expense.PaymentMethod(),
		storedCategoryID,
		storedCreditCardID,
		statementDueOn,
		expense.OccurredAt(),
		expense.FinancialTimezone(),
		expense.Origin(),
		expense.Status(),
		expense.Version(),
		expense.CreatedAt(),
		expense.UpdatedAt(),
	); err != nil {
		return newRepositoryError(ErrInsertExpense, err)
	}
	return nil
}

func insertAuditEvent(ctx context.Context, transaction pgx.Tx, expense domain.Expense) error {
	if _, err := transaction.Exec(ctx, `
		INSERT INTO audit_events (
			user_id,
			aggregate_type,
			aggregate_id,
			aggregate_version,
			event_type,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`,
		expense.UserID(),
		"EXPENSE",
		expense.ID(),
		expense.Version(),
		"EXPENSE_RECORDED",
		expense.CreatedAt(),
	); err != nil {
		return newRepositoryError(ErrInsertAuditEvent, err)
	}
	return nil
}

type repositoryError struct {
	category error
	cause    error
}

func newRepositoryError(category, cause error) error {
	return repositoryError{category: category, cause: cause}
}

func (err repositoryError) Error() string {
	return err.category.Error()
}

func (err repositoryError) Unwrap() []error {
	return []error{err.category, err.cause}
}
