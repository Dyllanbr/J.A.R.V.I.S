package postgres

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/jackc/pgx/v5"

	"jarvis/backend/internal/modules/transactions/application"
)

var (
	ErrReserveIdempotency      = errors.New("expense postgres repository: idempotency reservation failed")
	ErrReadIdempotency         = errors.New("expense postgres repository: idempotency lookup failed")
	ErrCompleteIdempotency     = errors.New("expense postgres repository: idempotency completion failed")
	ErrInvalidIdempotencyState = errors.New("expense postgres repository: idempotency state is invalid")
)

var _ application.ExpenseCommandStore = (*ExpenseRepository)(nil)

// Record atomically reserves the command key, persists Expense + audit event,
// and completes the idempotency record. A committed matching key is replayed
// from PostgreSQL, so behavior does not depend on process memory.
func (repository *ExpenseRepository) Record(
	ctx context.Context,
	command application.IdempotentExpenseCommand,
) (application.IdempotentExpenseResult, error) {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		return application.IdempotentExpenseResult{}, newRepositoryError(ErrBeginTransaction, err)
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

	var reserved bool
	err = transaction.QueryRow(operationContext, `
		INSERT INTO idempotency_records (
			user_id,
			operation,
			idempotency_key,
			request_fingerprint,
			state,
			transaction_id,
			created_at,
			completed_at
		) VALUES ($1, $2, $3, $4, 'PENDING', NULL, $5, NULL)
		ON CONFLICT (user_id, operation, idempotency_key) DO NOTHING
		RETURNING TRUE
	`,
		command.Expense.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.Fingerprint[:],
		command.Expense.CreatedAt(),
	).Scan(&reserved)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return application.IdempotentExpenseResult{}, newRepositoryError(ErrReserveIdempotency, err)
	}

	if !reserved {
		result, replayErr := replayExpense(operationContext, transaction, command)
		if replayErr != nil {
			return application.IdempotentExpenseResult{}, replayErr
		}
		if err := transaction.Commit(operationContext); err != nil {
			return application.IdempotentExpenseResult{}, newRepositoryError(ErrCommitTransaction, err)
		}
		committed = true
		return result, nil
	}

	if err := insertExpense(operationContext, transaction, command.Expense); err != nil {
		return application.IdempotentExpenseResult{}, err
	}
	if err := insertAuditEvent(operationContext, transaction, command.Expense); err != nil {
		return application.IdempotentExpenseResult{}, err
	}

	tag, err := transaction.Exec(operationContext, `
		UPDATE idempotency_records
		SET state = 'COMPLETED', transaction_id = $4, completed_at = $5
		WHERE user_id = $1
		  AND operation = $2
		  AND idempotency_key = $3
		  AND state = 'PENDING'
	`,
		command.Expense.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.Expense.ID(),
		command.Expense.CreatedAt(),
	)
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidIdempotencyState
		}
		return application.IdempotentExpenseResult{}, newRepositoryError(ErrCompleteIdempotency, err)
	}

	if err := transaction.Commit(operationContext); err != nil {
		return application.IdempotentExpenseResult{}, newRepositoryError(ErrCommitTransaction, err)
	}
	committed = true
	return application.IdempotentExpenseResult{Expense: command.Expense}, nil
}

func replayExpense(
	ctx context.Context,
	transaction pgx.Tx,
	command application.IdempotentExpenseCommand,
) (application.IdempotentExpenseResult, error) {
	var (
		fingerprint   []byte
		state         string
		transactionID *string
	)
	err := transaction.QueryRow(ctx, `
		SELECT request_fingerprint, state, transaction_id
		FROM idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, command.Expense.UserID(), command.Operation, command.IdempotencyKey).Scan(
		&fingerprint,
		&state,
		&transactionID,
	)
	if err != nil {
		return application.IdempotentExpenseResult{}, newRepositoryError(ErrReadIdempotency, err)
	}
	if subtle.ConstantTimeCompare(fingerprint, command.Fingerprint[:]) != 1 {
		return application.IdempotentExpenseResult{}, application.ErrIdempotencyConflict
	}
	if state != "COMPLETED" || transactionID == nil {
		return application.IdempotentExpenseResult{}, ErrInvalidIdempotencyState
	}

	expense, err := loadExpense(ctx, transaction, *transactionID, command.Expense.UserID())
	if err != nil {
		return application.IdempotentExpenseResult{}, err
	}
	return application.IdempotentExpenseResult{Expense: expense, Replayed: true}, nil
}
