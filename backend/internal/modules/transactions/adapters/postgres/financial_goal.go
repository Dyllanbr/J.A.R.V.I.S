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
	ErrMissingFinancialGoalsPool      = errors.New("financial goals postgres repository: pool is required")
	ErrInvalidFinancialGoalsTimeout   = errors.New("financial goals postgres repository: operation timeout is invalid")
	ErrInvalidFinancialGoalsOwner     = errors.New("financial goals postgres repository: owner is invalid")
	ErrInvalidFinancialGoalsInput     = errors.New("financial goals postgres repository: input is invalid")
	ErrBeginFinancialGoalsTransaction = errors.New("financial goals postgres repository: transaction begin failed")
	ErrReadFinancialGoals             = errors.New("financial goals postgres repository: goals read failed")
	ErrReadProtectedValues            = errors.New("financial goals postgres repository: protected values read failed")
	ErrInvalidStoredFinancialGoal     = errors.New("financial goals postgres repository: stored goal is invalid")
	ErrInvalidStoredProtectedValue    = errors.New("financial goals postgres repository: stored protected value is invalid")
	ErrCommitFinancialGoalsSnapshot   = errors.New("financial goals postgres repository: snapshot commit failed")
	ErrUpsertFinancialGoal            = errors.New("financial goals postgres repository: goal replacement failed")
	ErrUpsertProtectedValue           = errors.New("financial goals postgres repository: protected value replacement failed")
	ErrCommitFinancialGoal            = errors.New("financial goals postgres repository: goal commit failed")
	ErrCommitProtectedValue           = errors.New("financial goals postgres repository: protected value commit failed")
)

// FinancialGoalsRepository reads both declarations in one MVCC snapshot and
// replaces them by their owner-scoped identifiers. It never touches the
// financial ledger, audit or idempotency tables.
type FinancialGoalsRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var _ application.FinancialGoalsSnapshotReader = (*FinancialGoalsRepository)(nil)
var _ application.FinancialGoalStore = (*FinancialGoalsRepository)(nil)
var _ application.ProtectedValueStore = (*FinancialGoalsRepository)(nil)

func NewFinancialGoalsRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*FinancialGoalsRepository, error) {
	if pool == nil {
		return nil, ErrMissingFinancialGoalsPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidFinancialGoalsTimeout
	}
	return &FinancialGoalsRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

func (repository *FinancialGoalsRepository) Read(ctx context.Context, ownerID string) (application.FinancialGoalsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return application.FinancialGoalsSnapshot{}, err
	}
	if repository == nil || repository.pool == nil {
		return application.FinancialGoalsSnapshot{}, ErrMissingFinancialGoalsPool
	}
	if domain.ValidateUserID(ownerID) != nil {
		return application.FinancialGoalsSnapshot{}, ErrInvalidFinancialGoalsOwner
	}

	var result application.FinancialGoalsSnapshot
	err := withFinancialGoalsReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, tx pgx.Tx) error {
		goals, err := readFinancialGoals(operationContext, tx, ownerID)
		if err != nil {
			return err
		}
		protectedValues, err := readProtectedValues(operationContext, tx, ownerID)
		if err != nil {
			return err
		}
		result = application.FinancialGoalsSnapshot{Goals: goals, ProtectedValues: protectedValues}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.FinancialGoalsSnapshot{}, err
		}
		return application.FinancialGoalsSnapshot{}, newRepositoryError(ErrReadFinancialGoals, err)
	}
	if err := ctx.Err(); err != nil {
		return application.FinancialGoalsSnapshot{}, err
	}
	return result, nil
}

func readFinancialGoals(ctx context.Context, tx pgx.Tx, ownerID string) ([]domain.FinancialGoal, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, user_id, title, target_amount_minor, currency
		FROM financial_goals
		WHERE user_id = $1
		ORDER BY id ASC
	`, ownerID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, newRepositoryError(ErrReadFinancialGoals, err)
	}
	defer rows.Close()
	goals := make([]domain.FinancialGoal, 0)
	for rows.Next() {
		var id, storedOwner, title, currency string
		var targetMinor int64
		if err := rows.Scan(&id, &storedOwner, &title, &targetMinor, &currency); err != nil {
			return nil, newRepositoryError(ErrInvalidStoredFinancialGoal, err)
		}
		amount, err := domain.NewMoney(targetMinor, domain.Currency(currency))
		if err != nil {
			return nil, newRepositoryError(ErrInvalidStoredFinancialGoal, err)
		}
		goal, err := domain.NewFinancialGoal(domain.FinancialGoalParams{ID: id, OwnerID: storedOwner, Title: title, TargetAmount: amount})
		if err != nil || storedOwner != ownerID {
			if err == nil {
				err = ErrInvalidFinancialGoalsOwner
			}
			return nil, newRepositoryError(ErrInvalidStoredFinancialGoal, err)
		}
		goals = append(goals, goal)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, newRepositoryError(ErrReadFinancialGoals, err)
	}
	return goals, nil
}

func readProtectedValues(ctx context.Context, tx pgx.Tx, ownerID string) ([]domain.ProtectedValue, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, user_id, label, amount_minor, currency
		FROM protected_values
		WHERE user_id = $1
		ORDER BY id ASC
	`, ownerID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, newRepositoryError(ErrReadProtectedValues, err)
	}
	defer rows.Close()
	values := make([]domain.ProtectedValue, 0)
	for rows.Next() {
		var id, storedOwner, label, currency string
		var amountMinor int64
		if err := rows.Scan(&id, &storedOwner, &label, &amountMinor, &currency); err != nil {
			return nil, newRepositoryError(ErrInvalidStoredProtectedValue, err)
		}
		amount, err := domain.NewMoney(amountMinor, domain.Currency(currency))
		if err != nil {
			return nil, newRepositoryError(ErrInvalidStoredProtectedValue, err)
		}
		value, err := domain.NewProtectedValue(domain.ProtectedValueParams{ID: id, OwnerID: storedOwner, Label: label, Amount: amount})
		if err != nil || storedOwner != ownerID {
			if err == nil {
				err = ErrInvalidFinancialGoalsOwner
			}
			return nil, newRepositoryError(ErrInvalidStoredProtectedValue, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, newRepositoryError(ErrReadProtectedValues, err)
	}
	return values, nil
}

func withFinancialGoalsReadOnlySnapshot(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration, operation func(context.Context, pgx.Tx) error) error {
	operationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := pool.BeginTx(operationContext, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		if ctxErr := operationContext.Err(); ctxErr != nil {
			return ctxErr
		}
		return newRepositoryError(ErrBeginFinancialGoalsTransaction, err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackContext, cancelRollback := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancelRollback()
		_ = tx.Rollback(rollbackContext)
	}()
	if err := operation(operationContext, tx); err != nil {
		return err
	}
	if err := tx.Commit(operationContext); err != nil {
		if ctxErr := operationContext.Err(); ctxErr != nil {
			return ctxErr
		}
		return newRepositoryError(ErrCommitFinancialGoalsSnapshot, err)
	}
	committed = true
	return nil
}

func (repository *FinancialGoalsRepository) ReplaceFinancialGoal(ctx context.Context, goal domain.FinancialGoal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.pool == nil {
		return ErrMissingFinancialGoalsPool
	}
	validated, err := domain.NewFinancialGoal(domain.FinancialGoalParams{ID: goal.ID(), OwnerID: goal.OwnerID(), Title: goal.Title(), TargetAmount: goal.TargetAmount()})
	if err != nil || !validated.Equal(goal) {
		return ErrInvalidFinancialGoalsInput
	}
	return repository.replaceDeclaration(ctx, goal.OwnerID(), goal.ID(), goal.Title(), goal.TargetAmount(), `
		INSERT INTO financial_goals (user_id, id, title, target_amount_minor, currency)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, id) DO UPDATE
		SET title = EXCLUDED.title,
		    target_amount_minor = EXCLUDED.target_amount_minor,
		    currency = EXCLUDED.currency
	`, ErrUpsertFinancialGoal, ErrCommitFinancialGoal)
}

func (repository *FinancialGoalsRepository) ReplaceProtectedValue(ctx context.Context, value domain.ProtectedValue) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.pool == nil {
		return ErrMissingFinancialGoalsPool
	}
	validated, err := domain.NewProtectedValue(domain.ProtectedValueParams{ID: value.ID(), OwnerID: value.OwnerID(), Label: value.Label(), Amount: value.Amount()})
	if err != nil || !validated.Equal(value) {
		return ErrInvalidFinancialGoalsInput
	}
	return repository.replaceDeclaration(ctx, value.OwnerID(), value.ID(), value.Label(), value.Amount(), `
		INSERT INTO protected_values (user_id, id, label, amount_minor, currency)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, id) DO UPDATE
		SET label = EXCLUDED.label,
		    amount_minor = EXCLUDED.amount_minor,
		    currency = EXCLUDED.currency
	`, ErrUpsertProtectedValue, ErrCommitProtectedValue)
}

func (repository *FinancialGoalsRepository) replaceDeclaration(ctx context.Context, ownerID, id, label string, amount domain.Money, statement string, upsertErr, commitErr error) error {
	if domain.ValidateUserID(ownerID) != nil || !validFinancialGoalIdentifier(id) {
		return ErrInvalidFinancialGoalsInput
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	tx, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		if ctxErr := operationContext.Err(); ctxErr != nil {
			return ctxErr
		}
		return newRepositoryError(ErrBeginFinancialGoalsTransaction, err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackContext, cancelRollback := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancelRollback()
		_ = tx.Rollback(rollbackContext)
	}()
	if _, err := tx.Exec(operationContext, statement, ownerID, id, label, amount.MinorUnits(), amount.Currency()); err != nil {
		if ctxErr := operationContext.Err(); ctxErr != nil {
			return ctxErr
		}
		return newRepositoryError(upsertErr, err)
	}
	if err := tx.Commit(operationContext); err != nil {
		if ctxErr := operationContext.Err(); ctxErr != nil {
			return ctxErr
		}
		return newRepositoryError(commitErr, err)
	}
	committed = true
	return nil
}

func validFinancialGoalIdentifier(value string) bool {
	return domain.ValidateUserID(value) == nil
}
