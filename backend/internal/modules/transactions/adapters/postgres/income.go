package postgres

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/jackc/pgx/v5"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrInvalidIncomeOperation        = errors.New("income postgres repository: invalid operation")
	ErrBeginIncomeTransaction        = errors.New("income postgres repository: begin transaction failed")
	ErrReserveIncomeIdempotency      = errors.New("income postgres repository: idempotency reservation failed")
	ErrReadIncomeIdempotency         = errors.New("income postgres repository: idempotency lookup failed")
	ErrCompleteIncomeIdempotency     = errors.New("income postgres repository: idempotency completion failed")
	ErrInvalidIncomeIdempotencyState = errors.New("income postgres repository: idempotency state is invalid")
	ErrInsertIncome                  = errors.New("income postgres repository: income insert failed")
	ErrInsertIncomeAuditEvent        = errors.New("income postgres repository: audit event insert failed")
	ErrCommitIncomeTransaction       = errors.New("income postgres repository: commit failed")
)

var _ application.IncomeCommandStore = (*ExpenseRepository)(nil)

// RecordIncome atomically reserves the operation key, persists Income and its
// audit event, and completes the idempotency record. Matching committed
// operations are reconstructed from PostgreSQL.
func (repository *ExpenseRepository) RecordIncome(
	ctx context.Context,
	command application.IdempotentIncomeCommand,
) (application.IdempotentIncomeResult, error) {
	if command.Operation != application.IdempotencyOperationCreateIncome {
		return application.IdempotentIncomeResult{}, ErrInvalidIncomeOperation
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		return application.IdempotentIncomeResult{}, newRepositoryError(ErrBeginIncomeTransaction, err)
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
		command.Income.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.Fingerprint[:],
		command.Income.CreatedAt(),
	).Scan(&reserved)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return application.IdempotentIncomeResult{}, newRepositoryError(ErrReserveIncomeIdempotency, err)
	}

	if !reserved {
		result, replayErr := replayIncome(operationContext, transaction, command)
		if replayErr != nil {
			return application.IdempotentIncomeResult{}, replayErr
		}
		if err := transaction.Commit(operationContext); err != nil {
			return application.IdempotentIncomeResult{}, newRepositoryError(ErrCommitIncomeTransaction, err)
		}
		committed = true
		return result, nil
	}

	if err := insertIncome(operationContext, transaction, command.Income); err != nil {
		return application.IdempotentIncomeResult{}, err
	}
	if err := insertIncomeAuditEvent(operationContext, transaction, command.Income); err != nil {
		return application.IdempotentIncomeResult{}, err
	}

	tag, err := transaction.Exec(operationContext, `
		UPDATE idempotency_records
		SET state = 'COMPLETED', transaction_id = $4, completed_at = $5
		WHERE user_id = $1
		  AND operation = $2
		  AND idempotency_key = $3
		  AND state = 'PENDING'
	`,
		command.Income.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.Income.ID(),
		command.Income.CreatedAt(),
	)
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidIncomeIdempotencyState
		}
		return application.IdempotentIncomeResult{}, newRepositoryError(ErrCompleteIncomeIdempotency, err)
	}

	if err := transaction.Commit(operationContext); err != nil {
		return application.IdempotentIncomeResult{}, newRepositoryError(ErrCommitIncomeTransaction, err)
	}
	committed = true
	return application.IdempotentIncomeResult{Income: command.Income}, nil
}

func replayIncome(
	ctx context.Context,
	transaction pgx.Tx,
	command application.IdempotentIncomeCommand,
) (application.IdempotentIncomeResult, error) {
	var (
		fingerprint   []byte
		state         string
		transactionID *string
	)
	err := transaction.QueryRow(ctx, `
		SELECT request_fingerprint, state, transaction_id
		FROM idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, command.Income.UserID(), command.Operation, command.IdempotencyKey).Scan(
		&fingerprint,
		&state,
		&transactionID,
	)
	if err != nil {
		return application.IdempotentIncomeResult{}, newRepositoryError(ErrReadIncomeIdempotency, err)
	}
	if subtle.ConstantTimeCompare(fingerprint, command.Fingerprint[:]) != 1 {
		return application.IdempotentIncomeResult{}, application.ErrIncomeIdempotencyConflict
	}
	if state != "COMPLETED" || transactionID == nil {
		return application.IdempotentIncomeResult{}, ErrInvalidIncomeIdempotencyState
	}

	income, err := loadIncome(ctx, transaction, *transactionID, command.Income.UserID())
	if err != nil {
		return application.IdempotentIncomeResult{}, err
	}
	return application.IdempotentIncomeResult{Income: income, Replayed: true}, nil
}

func insertIncome(ctx context.Context, transaction pgx.Tx, income domain.Income) error {
	if _, err := transaction.Exec(ctx, `
		INSERT INTO transactions (
			id,
			user_id,
			type,
			description,
			amount_minor,
			currency,
			payment_method,
			occurred_at,
			financial_timezone,
			origin,
			status,
			version,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $8, $9, $10, $11, $12, $13)
	`,
		income.ID(),
		income.UserID(),
		income.Type(),
		income.Description(),
		income.Amount().MinorUnits(),
		income.Amount().Currency(),
		income.OccurredAt(),
		income.FinancialTimezone(),
		income.Origin(),
		income.Status(),
		income.Version(),
		income.CreatedAt(),
		income.UpdatedAt(),
	); err != nil {
		return newRepositoryError(ErrInsertIncome, err)
	}
	return nil
}

func insertIncomeAuditEvent(ctx context.Context, transaction pgx.Tx, income domain.Income) error {
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
		income.UserID(),
		income.Type(),
		income.ID(),
		income.Version(),
		"INCOME_RECORDED",
		income.CreatedAt(),
	); err != nil {
		return newRepositoryError(ErrInsertIncomeAuditEvent, err)
	}
	return nil
}
