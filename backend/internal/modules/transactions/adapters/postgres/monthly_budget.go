package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingMonthlyBudgetPool       = errors.New("monthly budget postgres store: pool is required")
	ErrInvalidMonthlyBudgetTimeout    = errors.New("monthly budget postgres store: operation timeout is invalid")
	ErrInvalidMonthlyBudgetInput      = errors.New("monthly budget postgres store: budget is invalid")
	ErrBeginMonthlyBudgetTransaction  = errors.New("monthly budget postgres store: transaction begin failed")
	ErrUpsertMonthlyBudget            = errors.New("monthly budget postgres store: budget replacement failed")
	ErrCommitMonthlyBudgetTransaction = errors.New("monthly budget postgres store: transaction commit failed")
	ErrReadMonthlyBudget              = errors.New("monthly budget postgres store: budget read failed")
	ErrInvalidStoredMonthlyBudget     = errors.New("monthly budget postgres store: stored budget is invalid")
)

// MonthlyBudgetRepository persists the owner/month natural key without
// introducing a financial aggregate, ledger write or idempotency record.
type MonthlyBudgetRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var _ application.MonthlyBudgetStore = (*MonthlyBudgetRepository)(nil)

// NewMonthlyBudgetRepository creates the PostgreSQL adapter for monthly
// budgets. The pool is the same bounded pool used by the other transaction
// adapters.
func NewMonthlyBudgetRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*MonthlyBudgetRepository, error) {
	if pool == nil {
		return nil, ErrMissingMonthlyBudgetPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidMonthlyBudgetTimeout
	}
	return &MonthlyBudgetRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

// ReplaceMonthlyBudget atomically inserts or replaces the budget identified
// by (owner, month). PostgreSQL's primary-key conflict arbiter makes
// concurrent replacements safe without a read-before-write race.
func (repository *MonthlyBudgetRepository) ReplaceMonthlyBudget(ctx context.Context, budget domain.MonthlyBudget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.pool == nil {
		return ErrMissingMonthlyBudgetPool
	}
	validated, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{
		OwnerID: budget.OwnerID(),
		Month:   budget.Month(),
		Amount:  budget.Amount(),
	})
	if err != nil || !validated.Equal(budget) {
		return ErrInvalidMonthlyBudgetInput
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return contextErr
		}
		return newRepositoryError(ErrBeginMonthlyBudgetTransaction, err)
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

	_, err = transaction.Exec(operationContext, `
		INSERT INTO monthly_budgets (user_id, month, amount_minor, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (user_id, month) DO UPDATE
		SET amount_minor = EXCLUDED.amount_minor,
		    currency = EXCLUDED.currency,
		    updated_at = CURRENT_TIMESTAMP
	`, budget.OwnerID(), postgresDate(budget.Month().StartOn()), budget.Amount().MinorUnits(), budget.Amount().Currency())
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return contextErr
		}
		return newRepositoryError(ErrUpsertMonthlyBudget, err)
	}
	if err := transaction.Commit(operationContext); err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return contextErr
		}
		return newRepositoryError(ErrCommitMonthlyBudgetTransaction, err)
	}
	committed = true
	return nil
}

// ReadMonthlyBudget returns the requested owner's budget. A missing row is a
// normal empty result (found=false), not a persistence error.
func (repository *MonthlyBudgetRepository) ReadMonthlyBudget(
	ctx context.Context,
	ownerID string,
	month domain.CivilMonth,
) (domain.MonthlyBudget, bool, error) {
	if err := ctx.Err(); err != nil {
		return domain.MonthlyBudget{}, false, err
	}
	if repository == nil || repository.pool == nil {
		return domain.MonthlyBudget{}, false, ErrMissingMonthlyBudgetPool
	}
	if domain.ValidateUserID(ownerID) != nil || month.String() == "" {
		return domain.MonthlyBudget{}, false, ErrInvalidMonthlyBudgetInput
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	var (
		storedMonth pgtype.Date
		minor       int64
		currency    string
	)
	err := repository.pool.QueryRow(operationContext, `
		SELECT month, amount_minor, currency
		FROM monthly_budgets
		WHERE user_id = $1 AND month = $2
	`, ownerID, postgresDate(month.StartOn())).Scan(&storedMonth, &minor, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MonthlyBudget{}, false, nil
	}
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return domain.MonthlyBudget{}, false, contextErr
		}
		return domain.MonthlyBudget{}, false, newRepositoryError(ErrReadMonthlyBudget, err)
	}
	if !storedMonth.Valid || storedMonth.InfinityModifier != pgtype.Finite {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredMonthlyBudget
	}
	storedDate, err := civilDateFromPostgres(storedMonth)
	if err != nil || storedDate.Day() != 1 {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredMonthlyBudget
	}
	storedCivilMonth, err := domain.NewCivilMonth(storedDate.Year(), storedDate.Month())
	if err != nil || !storedCivilMonth.Equal(month) {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredMonthlyBudget
	}
	storedAmount, err := domain.NewMoney(minor, domain.Currency(currency))
	if err != nil {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredMonthlyBudget
	}
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{OwnerID: ownerID, Month: storedCivilMonth, Amount: storedAmount})
	if err != nil {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredMonthlyBudget
	}
	return budget, true, nil
}
