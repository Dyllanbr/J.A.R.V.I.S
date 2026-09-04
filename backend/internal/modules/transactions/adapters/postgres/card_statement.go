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

var (
	ErrMissingCardStatementPool    = errors.New("card statement postgres reader: pool is required")
	ErrInvalidCardStatementTimeout = errors.New("card statement postgres reader: operation timeout is invalid")
	ErrInvalidCardStatementQuery   = errors.New("card statement postgres reader: invalid query")
	ErrBeginCardStatementSnapshot  = errors.New("card statement postgres reader: read-only snapshot begin failed")
	ErrCommitCardStatementSnapshot = errors.New("card statement postgres reader: read-only snapshot commit failed")
	ErrReadCardStatementCard       = errors.New("card statement postgres reader: card read failed")
	ErrReadCardStatementExpenses   = errors.New("card statement postgres reader: expense read failed")
	ErrReadCardStatementPlans      = errors.New("card statement postgres reader: installment plan read failed")
	ErrInvalidStoredCardStatement  = errors.New("card statement postgres reader: stored snapshot is invalid")
	ErrReadCardStatementSnapshot   = errors.New("card statement postgres reader: snapshot read failed")
)

// CardStatementRepository reads the complete owner-scoped input for one card
// statement in a single PostgreSQL MVCC snapshot. It does not persist a
// statement or any derived line.
type CardStatementRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var _ application.CardStatementSnapshotReader = (*CardStatementRepository)(nil)

// NewCardStatementRepository creates a read-only statement snapshot reader
// using the application's existing PostgreSQL pool.
func NewCardStatementRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*CardStatementRepository, error) {
	if pool == nil {
		return nil, ErrMissingCardStatementPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidCardStatementTimeout
	}
	return &CardStatementRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

// Read loads the card, its linked expenses, and its installment plans from
// one owner-scoped RepeatableRead/ReadOnly transaction. The snapshot is only
// assigned after every query and rehydration succeeds, preventing partial
// results on any failure.
func (repository *CardStatementRepository) Read(
	ctx context.Context,
	query application.CardStatementSnapshotQuery,
) (application.CardStatementSnapshot, error) {
	if domain.ValidateUserID(query.OwnerID) != nil || domain.ValidateCreditCardID(query.CreditCardID) != nil || !validCardStatementDate(query.StatementDueOn) {
		return application.CardStatementSnapshot{}, ErrInvalidCardStatementQuery
	}

	var snapshot application.CardStatementSnapshot
	err := withCardStatementReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, transaction pgx.Tx) error {
		cardSnapshot, err := queryCardStatementCard(operationContext, transaction, query.OwnerID, query.CreditCardID)
		if errors.Is(err, pgx.ErrNoRows) {
			snapshot = application.CardStatementSnapshot{Found: false, Expenses: []domain.Expense{}, InstallmentPlans: []domain.InstallmentPlan{}}
			return nil
		}
		if err != nil {
			return newRepositoryError(ErrReadCardStatementCard, err)
		}
		card, err := cardSnapshot.rehydrate()
		if err != nil || card.UserID() != query.OwnerID || card.ID() != query.CreditCardID {
			if err == nil {
				err = ErrInvalidStoredCreditCard
			}
			return newRepositoryError(ErrInvalidStoredCardStatement, err)
		}

		expenses, err := readCardStatementExpenses(operationContext, transaction, query.OwnerID, query.CreditCardID)
		if err != nil {
			return err
		}
		plans, err := readCardStatementPlans(operationContext, transaction, query.OwnerID, query.CreditCardID)
		if err != nil {
			return err
		}

		snapshot = application.CardStatementSnapshot{
			CreditCard:       card,
			Expenses:         expenses,
			InstallmentPlans: plans,
			Found:            true,
		}
		return nil
	})
	if err != nil {
		return application.CardStatementSnapshot{}, newRepositoryError(ErrReadCardStatementSnapshot, err)
	}
	return snapshot, nil
}

func validCardStatementDate(value domain.CivilDate) bool {
	_, err := domain.NewCivilDate(value.Year(), value.Month(), value.Day())
	return err == nil
}

func withCardStatementReadOnlySnapshot(
	ctx context.Context,
	pool *pgxpool.Pool,
	timeout time.Duration,
	operation func(context.Context, pgx.Tx) error,
) error {
	operationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transaction, err := pool.BeginTx(operationContext, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return newRepositoryError(ErrBeginCardStatementSnapshot, err)
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

	if err := operation(operationContext, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(operationContext); err != nil {
		return newRepositoryError(ErrCommitCardStatementSnapshot, err)
	}
	committed = true
	return nil
}

func queryCardStatementCard(
	ctx context.Context,
	querier interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	ownerID, cardID string,
) (creditCardSnapshot, error) {
	return scanCreditCardSnapshot(querier.QueryRow(ctx, creditCardSelectSQL+`
		WHERE id = $1 AND user_id = $2
	`, cardID, ownerID))
}

func readCardStatementExpenses(
	ctx context.Context,
	querier interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	ownerID, cardID string,
) ([]domain.Expense, error) {
	rows, err := querier.Query(ctx, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, category_id, credit_card_id, statement_due_on,
			occurred_at, financial_timezone, origin, status, version,
			created_at, updated_at
		FROM transactions
		WHERE user_id = $1 AND credit_card_id = $2 AND type = 'EXPENSE'
		ORDER BY occurred_at ASC, id ASC
	`, ownerID, cardID)
	if err != nil {
		return nil, newRepositoryError(ErrReadCardStatementExpenses, err)
	}
	defer rows.Close()

	expenses := make([]domain.Expense, 0)
	for rows.Next() {
		expense, scanErr := scanExpense(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredCardStatement, scanErr)
		}
		linkedCard, linked := expense.CreditCardID()
		if !linked || linkedCard != cardID || expense.UserID() != ownerID {
			return nil, newRepositoryError(ErrInvalidStoredCardStatement, errors.New("expense owner or card mismatch"))
		}
		expenses = append(expenses, expense)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrReadCardStatementExpenses, err)
	}
	return expenses, nil
}

func readCardStatementPlans(
	ctx context.Context,
	querier interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	ownerID, cardID string,
) ([]domain.InstallmentPlan, error) {
	rows, err := querier.Query(ctx, `
		SELECT id, user_id, credit_card_id, expense_id, total_minor, total_currency,
		       installment_count, first_due_on, due_day, status, created_at, cancelled_on
		FROM installment_plans
		WHERE user_id = $1 AND credit_card_id = $2
		ORDER BY first_due_on ASC, created_at ASC, id ASC
	`, ownerID, cardID)
	if err != nil {
		return nil, newRepositoryError(ErrReadCardStatementPlans, err)
	}
	defer rows.Close()

	plans := make([]domain.InstallmentPlan, 0)
	for rows.Next() {
		snapshot, scanErr := scanInstallmentPlanSnapshot(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredCardStatement, scanErr)
		}
		plan, hydrateErr := snapshot.rehydrate(ownerID)
		if hydrateErr != nil || plan.OwnerID() != ownerID || plan.CreditCardID() != cardID {
			if hydrateErr == nil {
				hydrateErr = ErrInvalidStoredInstallmentPlan
			}
			return nil, newRepositoryError(ErrInvalidStoredCardStatement, hydrateErr)
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrReadCardStatementPlans, err)
	}
	return plans, nil
}
