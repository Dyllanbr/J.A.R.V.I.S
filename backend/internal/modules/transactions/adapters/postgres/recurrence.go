package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingRecurrencePool             = errors.New("recurrence postgres repository: pool is required")
	ErrInvalidRecurrenceTimeout          = errors.New("recurrence postgres repository: operation timeout is invalid")
	ErrInvalidRecurrenceOperation        = errors.New("recurrence postgres repository: invalid operation")
	ErrInvalidRecurrenceAuditEvent       = errors.New("recurrence postgres repository: invalid audit event")
	ErrInvalidRecurrenceCommand          = errors.New("recurrence postgres repository: invalid command")
	ErrBeginRecurrenceTransaction        = errors.New("recurrence postgres repository: begin transaction failed")
	ErrReserveRecurrenceIdempotency      = errors.New("recurrence postgres repository: idempotency reservation failed")
	ErrReadRecurrenceIdempotency         = errors.New("recurrence postgres repository: idempotency lookup failed")
	ErrCompleteRecurrenceIdempotency     = errors.New("recurrence postgres repository: idempotency completion failed")
	ErrInvalidRecurrenceIdempotencyState = errors.New("recurrence postgres repository: idempotency state is invalid")
	ErrInsertRecurrence                  = errors.New("recurrence postgres repository: recurrence insert failed")
	ErrReadRecurrence                    = errors.New("recurrence postgres repository: recurrence lookup failed")
	ErrUpdateRecurrence                  = errors.New("recurrence postgres repository: recurrence update failed")
	ErrInsertRecurrenceAuditEvent        = errors.New("recurrence postgres repository: audit event insert failed")
	ErrListRecurrences                   = errors.New("recurrence postgres repository: recurrence list failed")
	ErrInvalidStoredRecurrence           = errors.New("recurrence postgres repository: stored recurrence is invalid")
	ErrCommitRecurrenceTransaction       = errors.New("recurrence postgres repository: commit failed")
)

// RecurrenceRepository owns PostgreSQL persistence for the recurrence
// aggregate. It deliberately has no transaction Expense/Income dependency.
type RecurrenceRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var (
	_ application.RecurrenceCommandStore      = (*RecurrenceRepository)(nil)
	_ application.RecurrenceCancellationStore = (*RecurrenceRepository)(nil)
	_ application.RecurrenceReplayReader      = (*RecurrenceRepository)(nil)
	_ application.RecurrenceReader            = (*RecurrenceRepository)(nil)
)

// NewRecurrenceRepository creates the recurrence-specific PostgreSQL adapter.
func NewRecurrenceRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*RecurrenceRepository, error) {
	if pool == nil {
		return nil, ErrMissingRecurrencePool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidRecurrenceTimeout
	}
	return &RecurrenceRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

// RecordRecurrence atomically reserves idempotency, inserts a recurrence and
// its audit event, and stores a historical ACTIVE replay snapshot.
func (repository *RecurrenceRepository) RecordRecurrence(
	ctx context.Context,
	command application.IdempotentRecurrenceCommand,
) (application.IdempotentRecurrenceResult, error) {
	if command.Operation != application.IdempotencyOperationCreateRecurrence {
		return application.IdempotentRecurrenceResult{}, ErrInvalidRecurrenceOperation
	}
	if command.AuditEvent != application.RecurrenceAuditEventCreated {
		return application.IdempotentRecurrenceResult{}, ErrInvalidRecurrenceAuditEvent
	}
	if command.Recurrence.Status() != domain.RecurrenceStatusActive ||
		!isCanonicalRecurrenceInstant(command.Recurrence.CreatedAt()) {
		return application.IdempotentRecurrenceResult{}, ErrInvalidRecurrenceCommand
	}
	if _, err := snapshotFromRecurrence(command.Recurrence).rehydrate(); err != nil {
		return application.IdempotentRecurrenceResult{}, ErrInvalidRecurrenceCommand
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		return application.IdempotentRecurrenceResult{}, newRepositoryError(ErrBeginRecurrenceTransaction, err)
	}
	committed := false
	defer rollbackRecurrenceTransaction(transaction, &committed)()

	reserved, err := reserveRecurrenceIdempotency(
		operationContext,
		transaction,
		command.Recurrence.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.Fingerprint,
		command.Recurrence.CreatedAt(),
	)
	if err != nil {
		return application.IdempotentRecurrenceResult{}, err
	}
	if !reserved {
		recurrence, replayErr := replayRecurrence(
			operationContext,
			transaction,
			command.Recurrence.UserID(),
			command.Operation,
			command.IdempotencyKey,
			command.Fingerprint,
		)
		if replayErr != nil {
			return application.IdempotentRecurrenceResult{}, replayErr
		}
		if err := transaction.Commit(operationContext); err != nil {
			return application.IdempotentRecurrenceResult{}, newRepositoryError(ErrCommitRecurrenceTransaction, err)
		}
		committed = true
		return application.IdempotentRecurrenceResult{Recurrence: recurrence, Replayed: true}, nil
	}

	if err := insertRecurrence(operationContext, transaction, command.Recurrence); err != nil {
		return application.IdempotentRecurrenceResult{}, err
	}
	if err := insertRecurrenceAuditEvent(
		operationContext,
		transaction,
		command.Recurrence,
		command.AuditEvent,
		command.Recurrence.CreatedAt(),
	); err != nil {
		return application.IdempotentRecurrenceResult{}, err
	}
	if err := completeRecurrenceIdempotency(
		operationContext,
		transaction,
		command.Recurrence.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.Recurrence,
		command.Recurrence.CreatedAt(),
	); err != nil {
		return application.IdempotentRecurrenceResult{}, err
	}

	if err := transaction.Commit(operationContext); err != nil {
		return application.IdempotentRecurrenceResult{}, newRepositoryError(ErrCommitRecurrenceTransaction, err)
	}
	committed = true
	return application.IdempotentRecurrenceResult{Recurrence: command.Recurrence}, nil
}

// CancelRecurrence atomically resolves idempotency, locks the owner-scoped
// recurrence row, applies the domain transition, audits it, and stores the
// historical CANCELLED replay snapshot.
func (repository *RecurrenceRepository) CancelRecurrence(
	ctx context.Context,
	command application.IdempotentCancelRecurrenceCommand,
) (application.IdempotentCancelRecurrenceResult, error) {
	if command.Operation != application.IdempotencyOperationCancelRecurrence {
		return application.IdempotentCancelRecurrenceResult{}, ErrInvalidRecurrenceOperation
	}
	if command.AuditEvent != application.RecurrenceAuditEventCancelled {
		return application.IdempotentCancelRecurrenceResult{}, ErrInvalidRecurrenceAuditEvent
	}
	if domain.ValidateRecurrenceUserID(command.UserID) != nil ||
		domain.ValidateRecurrenceID(command.RecurrenceID) != nil ||
		!isCanonicalRecurrenceInstant(command.CancelledAt) {
		return application.IdempotentCancelRecurrenceResult{}, ErrInvalidRecurrenceCommand
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		return application.IdempotentCancelRecurrenceResult{}, newRepositoryError(ErrBeginRecurrenceTransaction, err)
	}
	committed := false
	defer rollbackRecurrenceTransaction(transaction, &committed)()

	reserved, err := reserveRecurrenceIdempotency(
		operationContext,
		transaction,
		command.UserID,
		command.Operation,
		command.IdempotencyKey,
		command.Fingerprint,
		command.CancelledAt,
	)
	if err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}
	if !reserved {
		recurrence, replayErr := replayRecurrence(
			operationContext,
			transaction,
			command.UserID,
			command.Operation,
			command.IdempotencyKey,
			command.Fingerprint,
		)
		if replayErr != nil {
			return application.IdempotentCancelRecurrenceResult{}, replayErr
		}
		if err := transaction.Commit(operationContext); err != nil {
			return application.IdempotentCancelRecurrenceResult{}, newRepositoryError(ErrCommitRecurrenceTransaction, err)
		}
		committed = true
		return application.IdempotentCancelRecurrenceResult{Recurrence: recurrence, Replayed: true}, nil
	}

	recurrence, err := loadRecurrenceForUpdate(
		operationContext,
		transaction,
		command.RecurrenceID,
		command.UserID,
	)
	if err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}
	cancelled, err := recurrence.Cancel(command.CancelledAt)
	if err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}
	if err := updateCancelledRecurrence(operationContext, transaction, cancelled); err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}
	if err := insertRecurrenceAuditEvent(
		operationContext,
		transaction,
		cancelled,
		command.AuditEvent,
		command.CancelledAt,
	); err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}
	if err := completeRecurrenceIdempotency(
		operationContext,
		transaction,
		command.UserID,
		command.Operation,
		command.IdempotencyKey,
		cancelled,
		command.CancelledAt,
	); err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}

	if err := transaction.Commit(operationContext); err != nil {
		return application.IdempotentCancelRecurrenceResult{}, newRepositoryError(ErrCommitRecurrenceTransaction, err)
	}
	committed = true
	return application.IdempotentCancelRecurrenceResult{Recurrence: cancelled}, nil
}

// FindRecurrenceReplay is the read-only fast path for already committed
// operations. The command stores repeat the decision atomically.
func (repository *RecurrenceRepository) FindRecurrenceReplay(
	ctx context.Context,
	query application.RecurrenceReplayQuery,
) (application.RecurrenceReplayLookup, error) {
	if !validRecurrenceOperation(query.Operation) {
		return application.RecurrenceReplayLookup{}, ErrInvalidRecurrenceOperation
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	recurrence, found, err := readRecurrenceReplay(
		operationContext,
		repository.pool,
		query.UserID,
		query.Operation,
		query.IdempotencyKey,
		query.Fingerprint,
	)
	if err != nil {
		return application.RecurrenceReplayLookup{}, err
	}
	return application.RecurrenceReplayLookup{Recurrence: recurrence, Found: found}, nil
}

// ListRecurrences reads only the requested owner's recurrence rows. Domain
// reconstruction fails closed if persisted data violates aggregate rules.
func (repository *RecurrenceRepository) ListRecurrences(
	ctx context.Context,
	userID string,
) ([]domain.Recurrence, error) {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	rows, err := repository.pool.Query(operationContext, `
		SELECT
			id,
			user_id,
			transaction_type,
			description,
			expected_amount_minor,
			currency,
			frequency,
			starts_on,
			status,
			created_at,
			cancelled_at
		FROM recurrences
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, newRepositoryError(ErrListRecurrences, err)
	}
	defer rows.Close()

	items := make([]domain.Recurrence, 0)
	for rows.Next() {
		snapshot, scanErr := scanRecurrenceSnapshot(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrListRecurrences, scanErr)
		}
		recurrence, hydrateErr := snapshot.rehydrate()
		if hydrateErr != nil || recurrence.UserID() != userID {
			if hydrateErr == nil {
				hydrateErr = ErrInvalidStoredRecurrence
			}
			return nil, newRepositoryError(ErrInvalidStoredRecurrence, hydrateErr)
		}
		items = append(items, recurrence)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrListRecurrences, err)
	}
	return items, nil
}

func rollbackRecurrenceTransaction(transaction pgx.Tx, committed *bool) func() {
	return func() {
		if *committed {
			return
		}
		rollbackContext, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()
		_ = transaction.Rollback(rollbackContext)
	}
}

func reserveRecurrenceIdempotency(
	ctx context.Context,
	transaction pgx.Tx,
	userID, operation, key string,
	fingerprint application.RequestFingerprint,
	createdAt time.Time,
) (bool, error) {
	var reserved bool
	err := transaction.QueryRow(ctx, `
		INSERT INTO recurrence_idempotency_records (
			user_id,
			operation,
			idempotency_key,
			request_fingerprint,
			state,
			created_at
		) VALUES ($1, $2, $3, $4, 'PENDING', $5)
		ON CONFLICT (user_id, operation, idempotency_key) DO NOTHING
		RETURNING TRUE
	`, userID, operation, key, fingerprint[:], createdAt).Scan(&reserved)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, newRepositoryError(ErrReserveRecurrenceIdempotency, err)
	}
	return reserved, nil
}

func completeRecurrenceIdempotency(
	ctx context.Context,
	transaction pgx.Tx,
	userID, operation, key string,
	recurrence domain.Recurrence,
	completedAt time.Time,
) error {
	snapshot := snapshotFromRecurrence(recurrence)
	tag, err := transaction.Exec(ctx, `
		UPDATE recurrence_idempotency_records
		SET
			state = 'COMPLETED',
			recurrence_id = $4,
			result_transaction_type = $5,
			result_description = $6,
			result_expected_amount_minor = $7,
			result_currency = $8,
			result_frequency = $9,
			result_starts_on = $10,
			result_status = $11,
			result_created_at = $12,
			result_cancelled_at = $13,
			completed_at = $14
		WHERE user_id = $1
		  AND operation = $2
		  AND idempotency_key = $3
		  AND state = 'PENDING'
	`,
		userID,
		operation,
		key,
		snapshot.id,
		snapshot.transactionType,
		snapshot.description,
		snapshot.expectedAmountMinor,
		snapshot.currency,
		snapshot.frequency,
		snapshot.startsOn,
		snapshot.status,
		snapshot.createdAt,
		snapshot.cancelledAt,
		completedAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidRecurrenceIdempotencyState
		}
		return newRepositoryError(ErrCompleteRecurrenceIdempotency, err)
	}
	return nil
}

func replayRecurrence(
	ctx context.Context,
	querier recurrenceRowQuerier,
	userID, operation, key string,
	fingerprint application.RequestFingerprint,
) (domain.Recurrence, error) {
	recurrence, found, err := readRecurrenceReplay(ctx, querier, userID, operation, key, fingerprint)
	if err != nil {
		return domain.Recurrence{}, err
	}
	if !found {
		return domain.Recurrence{}, ErrInvalidRecurrenceIdempotencyState
	}
	return recurrence, nil
}

func readRecurrenceReplay(
	ctx context.Context,
	querier recurrenceRowQuerier,
	userID, operation, key string,
	fingerprint application.RequestFingerprint,
) (domain.Recurrence, bool, error) {
	var (
		storedFingerprint []byte
		state             string
		snapshot          recurrenceSnapshot
	)
	err := querier.QueryRow(ctx, `
		SELECT
			request_fingerprint,
			state,
			recurrence_id,
			user_id,
			result_transaction_type,
			result_description,
			result_expected_amount_minor,
			result_currency,
			result_frequency,
			result_starts_on,
			result_status,
			result_created_at,
			result_cancelled_at
		FROM recurrence_idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, userID, operation, key).Scan(
		&storedFingerprint,
		&state,
		&snapshot.id,
		&snapshot.userID,
		&snapshot.transactionType,
		&snapshot.description,
		&snapshot.expectedAmountMinor,
		&snapshot.currency,
		&snapshot.frequency,
		&snapshot.startsOn,
		&snapshot.status,
		&snapshot.createdAt,
		&snapshot.cancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Recurrence{}, false, nil
	}
	if err != nil {
		return domain.Recurrence{}, false, newRepositoryError(ErrReadRecurrenceIdempotency, err)
	}
	if subtle.ConstantTimeCompare(storedFingerprint, fingerprint[:]) != 1 {
		return domain.Recurrence{}, false, application.ErrRecurrenceIdempotencyConflict
	}
	if state != "COMPLETED" || snapshot.id == nil || snapshot.userID == nil {
		return domain.Recurrence{}, false, ErrInvalidRecurrenceIdempotencyState
	}
	recurrence, err := snapshot.rehydrate()
	if err != nil || recurrence.UserID() != userID {
		if err == nil {
			err = ErrInvalidStoredRecurrence
		}
		return domain.Recurrence{}, false, newRepositoryError(ErrInvalidStoredRecurrence, err)
	}
	if (operation == application.IdempotencyOperationCreateRecurrence && recurrence.Status() != domain.RecurrenceStatusActive) ||
		(operation == application.IdempotencyOperationCancelRecurrence && recurrence.Status() != domain.RecurrenceStatusCancelled) {
		return domain.Recurrence{}, false, ErrInvalidRecurrenceIdempotencyState
	}
	return recurrence, true, nil
}

func insertRecurrence(ctx context.Context, transaction pgx.Tx, recurrence domain.Recurrence) error {
	snapshot := snapshotFromRecurrence(recurrence)
	_, err := transaction.Exec(ctx, `
		INSERT INTO recurrences (
			id,
			user_id,
			transaction_type,
			description,
			expected_amount_minor,
			currency,
			frequency,
			starts_on,
			status,
			created_at,
			cancelled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		snapshot.id,
		snapshot.userID,
		snapshot.transactionType,
		snapshot.description,
		snapshot.expectedAmountMinor,
		snapshot.currency,
		snapshot.frequency,
		snapshot.startsOn,
		snapshot.status,
		snapshot.createdAt,
		snapshot.cancelledAt,
	)
	if err != nil {
		return newRepositoryError(ErrInsertRecurrence, err)
	}
	return nil
}

func insertRecurrenceAuditEvent(
	ctx context.Context,
	transaction pgx.Tx,
	recurrence domain.Recurrence,
	event application.RecurrenceAuditEvent,
	createdAt time.Time,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO recurrence_audit_events (
			user_id,
			recurrence_id,
			event_type,
			created_at
		) VALUES ($1, $2, $3, $4)
	`, recurrence.UserID(), recurrence.ID(), event, createdAt)
	if err != nil {
		return newRepositoryError(ErrInsertRecurrenceAuditEvent, err)
	}
	return nil
}

func loadRecurrenceForUpdate(
	ctx context.Context,
	transaction pgx.Tx,
	recurrenceID, userID string,
) (domain.Recurrence, error) {
	row := transaction.QueryRow(ctx, `
		SELECT
			id,
			user_id,
			transaction_type,
			description,
			expected_amount_minor,
			currency,
			frequency,
			starts_on,
			status,
			created_at,
			cancelled_at
		FROM recurrences
		WHERE id = $1 AND user_id = $2
		FOR UPDATE
	`, recurrenceID, userID)
	snapshot, err := scanRecurrenceSnapshot(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Recurrence{}, application.ErrRecurrenceNotFound
	}
	if err != nil {
		return domain.Recurrence{}, newRepositoryError(ErrReadRecurrence, err)
	}
	recurrence, err := snapshot.rehydrate()
	if err != nil {
		return domain.Recurrence{}, newRepositoryError(ErrInvalidStoredRecurrence, err)
	}
	return recurrence, nil
}

func updateCancelledRecurrence(ctx context.Context, transaction pgx.Tx, recurrence domain.Recurrence) error {
	cancelledAt, ok := recurrence.CancelledAt()
	if !ok || recurrence.Status() != domain.RecurrenceStatusCancelled {
		return ErrInvalidRecurrenceCommand
	}
	tag, err := transaction.Exec(ctx, `
		UPDATE recurrences
		SET status = 'CANCELLED', cancelled_at = $3
		WHERE id = $1 AND user_id = $2 AND status = 'ACTIVE' AND cancelled_at IS NULL
	`, recurrence.ID(), recurrence.UserID(), cancelledAt)
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidStoredRecurrence
		}
		return newRepositoryError(ErrUpdateRecurrence, err)
	}
	return nil
}

type recurrenceRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type recurrenceScanner interface {
	Scan(...any) error
}

type recurrenceSnapshot struct {
	id                  *string
	userID              *string
	transactionType     *string
	description         *string
	expectedAmountMinor *int64
	currency            *string
	frequency           *string
	startsOn            pgtype.Date
	status              *string
	createdAt           *time.Time
	cancelledAt         *time.Time
}

func snapshotFromRecurrence(recurrence domain.Recurrence) recurrenceSnapshot {
	id := recurrence.ID()
	userID := recurrence.UserID()
	transactionType := string(recurrence.Type())
	description := recurrence.Description()
	expectedAmountMinor := recurrence.ExpectedAmount().MinorUnits()
	currency := string(recurrence.ExpectedAmount().Currency())
	frequency := string(recurrence.Frequency())
	startsOn := pgtype.Date{
		Time: time.Date(
			recurrence.StartsOn().Year(),
			recurrence.StartsOn().Month(),
			recurrence.StartsOn().Day(),
			0, 0, 0, 0,
			time.UTC,
		),
		Valid: true,
	}
	status := string(recurrence.Status())
	createdAt := recurrence.CreatedAt()
	cancelledAt, cancelled := recurrence.CancelledAt()
	snapshot := recurrenceSnapshot{
		id:                  &id,
		userID:              &userID,
		transactionType:     &transactionType,
		description:         &description,
		expectedAmountMinor: &expectedAmountMinor,
		currency:            &currency,
		frequency:           &frequency,
		startsOn:            startsOn,
		status:              &status,
		createdAt:           &createdAt,
	}
	if cancelled {
		snapshot.cancelledAt = &cancelledAt
	}
	return snapshot
}

func scanRecurrenceSnapshot(scanner recurrenceScanner) (recurrenceSnapshot, error) {
	var snapshot recurrenceSnapshot
	err := scanner.Scan(
		&snapshot.id,
		&snapshot.userID,
		&snapshot.transactionType,
		&snapshot.description,
		&snapshot.expectedAmountMinor,
		&snapshot.currency,
		&snapshot.frequency,
		&snapshot.startsOn,
		&snapshot.status,
		&snapshot.createdAt,
		&snapshot.cancelledAt,
	)
	return snapshot, err
}

func (snapshot recurrenceSnapshot) rehydrate() (domain.Recurrence, error) {
	if snapshot.id == nil || snapshot.userID == nil || snapshot.transactionType == nil ||
		snapshot.description == nil || snapshot.expectedAmountMinor == nil || snapshot.currency == nil ||
		snapshot.frequency == nil || snapshot.status == nil || snapshot.createdAt == nil {
		return domain.Recurrence{}, ErrInvalidStoredRecurrence
	}
	amount, err := domain.NewMoney(*snapshot.expectedAmountMinor, domain.Currency(*snapshot.currency))
	if err != nil {
		return domain.Recurrence{}, err
	}
	startsOn, err := civilDateFromPostgres(snapshot.startsOn)
	if err != nil {
		return domain.Recurrence{}, err
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID: *snapshot.id,
		Details: domain.RecurrenceDetails{
			UserID:          *snapshot.userID,
			TransactionType: domain.TransactionType(*snapshot.transactionType),
			Description:     *snapshot.description,
			ExpectedAmount:  amount,
			Frequency:       domain.RecurrenceFrequency(*snapshot.frequency),
			StartsOn:        startsOn,
		},
		CreatedAt: *snapshot.createdAt,
	})
	if err != nil {
		return domain.Recurrence{}, err
	}
	switch domain.RecurrenceStatus(*snapshot.status) {
	case domain.RecurrenceStatusActive:
		if snapshot.cancelledAt != nil {
			return domain.Recurrence{}, ErrInvalidStoredRecurrence
		}
		return recurrence, nil
	case domain.RecurrenceStatusCancelled:
		if snapshot.cancelledAt == nil {
			return domain.Recurrence{}, ErrInvalidStoredRecurrence
		}
		return recurrence.Cancel(*snapshot.cancelledAt)
	default:
		return domain.Recurrence{}, ErrInvalidStoredRecurrence
	}
}

func civilDateFromPostgres(value pgtype.Date) (domain.CivilDate, error) {
	if !value.Valid || value.InfinityModifier != pgtype.Finite {
		return domain.CivilDate{}, domain.ErrInvalidCivilDate
	}
	year, month, day := value.Time.Date()
	return domain.NewCivilDate(year, month, day)
}

func validRecurrenceOperation(operation string) bool {
	return operation == application.IdempotencyOperationCreateRecurrence ||
		operation == application.IdempotencyOperationCancelRecurrence
}

func isCanonicalRecurrenceInstant(value time.Time) bool {
	return !value.IsZero() && value == value.UTC().Truncate(time.Microsecond)
}
