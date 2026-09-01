package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingCreditCardPool             = errors.New("credit card postgres repository: pool is required")
	ErrInvalidCreditCardTimeout          = errors.New("credit card postgres repository: operation timeout is invalid")
	ErrInvalidCreditCardOperation        = errors.New("credit card postgres repository: invalid operation")
	ErrInvalidCreditCardAuditEvent       = errors.New("credit card postgres repository: invalid audit event")
	ErrInvalidCreditCardCommand          = errors.New("credit card postgres repository: invalid command")
	ErrInvalidCreditCardQuery            = errors.New("credit card postgres repository: invalid query")
	ErrBeginCreditCardTransaction        = errors.New("credit card postgres repository: begin transaction failed")
	ErrReserveCreditCardIdempotency      = errors.New("credit card postgres repository: idempotency reservation failed")
	ErrReadCreditCardIdempotency         = errors.New("credit card postgres repository: idempotency lookup failed")
	ErrCompleteCreditCardIdempotency     = errors.New("credit card postgres repository: idempotency completion failed")
	ErrInvalidCreditCardIdempotencyState = errors.New("credit card postgres repository: idempotency state is invalid")
	ErrInsertCreditCard                  = errors.New("credit card postgres repository: card insert failed")
	ErrReadCreditCard                    = errors.New("credit card postgres repository: card lookup failed")
	ErrUpdateCreditCard                  = errors.New("credit card postgres repository: card update failed")
	ErrInsertCreditCardAuditEvent        = errors.New("credit card postgres repository: audit event insert failed")
	ErrListCreditCards                   = errors.New("credit card postgres repository: card list failed")
	ErrInvalidStoredCreditCard           = errors.New("credit card postgres repository: stored card is invalid")
	ErrCommitCreditCardTransaction       = errors.New("credit card postgres repository: commit failed")
)

// CreditCardRepository owns PostgreSQL persistence for organizational card
// references. It has no transaction, purchase or payment dependency.
type CreditCardRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var (
	_ application.CreditCardCommandStore = (*CreditCardRepository)(nil)
	_ application.CreditCardArchiveStore = (*CreditCardRepository)(nil)
	_ application.CreditCardReplayReader = (*CreditCardRepository)(nil)
	_ application.CreditCardLookupReader = (*CreditCardRepository)(nil)
	_ application.CreditCardListReader   = (*CreditCardRepository)(nil)
)

func NewCreditCardRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*CreditCardRepository, error) {
	if pool == nil {
		return nil, ErrMissingCreditCardPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidCreditCardTimeout
	}
	return &CreditCardRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

// RecordCreditCard atomically reserves idempotency, inserts the aggregate and
// audit event, and completes the immutable ACTIVE replay snapshot.
func (repository *CreditCardRepository) RecordCreditCard(
	ctx context.Context,
	command application.IdempotentCreditCardCommand,
) (application.IdempotentCreditCardResult, error) {
	if command.Operation != application.IdempotencyOperationCreateCreditCard {
		return application.IdempotentCreditCardResult{}, ErrInvalidCreditCardOperation
	}
	if command.AuditEvent != application.CreditCardAuditEventCreated {
		return application.IdempotentCreditCardResult{}, ErrInvalidCreditCardAuditEvent
	}
	if !validCreditCardIdempotencyKey(command.IdempotencyKey) ||
		command.CreditCard.Status() != domain.CreditCardStatusActive ||
		!isCanonicalCreditCardInstant(command.CreditCard.CreatedAt()) {
		return application.IdempotentCreditCardResult{}, ErrInvalidCreditCardCommand
	}
	if _, err := snapshotFromCreditCard(command.CreditCard).rehydrate(); err != nil {
		return application.IdempotentCreditCardResult{}, ErrInvalidCreditCardCommand
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		return application.IdempotentCreditCardResult{}, newRepositoryError(ErrBeginCreditCardTransaction, err)
	}
	committed := false
	defer rollbackCreditCardTransaction(transaction, &committed)()

	reserved, err := reserveCreditCardIdempotency(
		operationContext,
		transaction,
		command.CreditCard.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.Fingerprint,
		command.CreditCard.CreatedAt(),
	)
	if err != nil {
		return application.IdempotentCreditCardResult{}, err
	}
	if !reserved {
		card, replayErr := replayCreditCard(
			operationContext,
			transaction,
			command.CreditCard.UserID(),
			command.Operation,
			command.IdempotencyKey,
			command.Fingerprint,
		)
		if replayErr != nil {
			return application.IdempotentCreditCardResult{}, replayErr
		}
		if err := transaction.Commit(operationContext); err != nil {
			return application.IdempotentCreditCardResult{}, newRepositoryError(ErrCommitCreditCardTransaction, err)
		}
		committed = true
		return application.IdempotentCreditCardResult{CreditCard: card, Replayed: true}, nil
	}

	if err := insertCreditCard(operationContext, transaction, command.CreditCard); err != nil {
		return application.IdempotentCreditCardResult{}, err
	}
	if err := insertCreditCardAuditEvent(
		operationContext,
		transaction,
		command.CreditCard,
		command.AuditEvent,
		command.CreditCard.CreatedAt(),
	); err != nil {
		return application.IdempotentCreditCardResult{}, err
	}
	if err := completeCreditCardIdempotency(
		operationContext,
		transaction,
		command.CreditCard.UserID(),
		command.Operation,
		command.IdempotencyKey,
		command.CreditCard,
		command.CreditCard.CreatedAt(),
	); err != nil {
		return application.IdempotentCreditCardResult{}, err
	}
	if err := transaction.Commit(operationContext); err != nil {
		return application.IdempotentCreditCardResult{}, newRepositoryError(ErrCommitCreditCardTransaction, err)
	}
	committed = true
	return application.IdempotentCreditCardResult{CreditCard: command.CreditCard}, nil
}

// ArchiveCreditCard atomically reserves idempotency, locks the owner-scoped
// aggregate, performs the terminal transition, audits it and stores its
// historical ARCHIVED replay snapshot.
func (repository *CreditCardRepository) ArchiveCreditCard(
	ctx context.Context,
	command application.IdempotentArchiveCreditCardCommand,
) (application.IdempotentArchiveCreditCardResult, error) {
	if command.Operation != application.IdempotencyOperationArchiveCreditCard {
		return application.IdempotentArchiveCreditCardResult{}, ErrInvalidCreditCardOperation
	}
	if command.AuditEvent != application.CreditCardAuditEventArchived {
		return application.IdempotentArchiveCreditCardResult{}, ErrInvalidCreditCardAuditEvent
	}
	archivedAt, hasArchivedAt := command.CreditCard.ArchivedAt()
	if !validCreditCardIdempotencyKey(command.IdempotencyKey) ||
		domain.ValidateCreditCardUserID(command.UserID) != nil ||
		domain.ValidateCreditCardID(command.CreditCardID) != nil ||
		command.CreditCard.UserID() != command.UserID ||
		command.CreditCard.ID() != command.CreditCardID ||
		command.CreditCard.Status() != domain.CreditCardStatusArchived ||
		!hasArchivedAt || !isCanonicalCreditCardInstant(archivedAt) ||
		!isCanonicalCreditCardInstant(command.CreditCard.CreatedAt()) {
		return application.IdempotentArchiveCreditCardResult{}, ErrInvalidCreditCardCommand
	}
	if _, err := snapshotFromCreditCard(command.CreditCard).rehydrate(); err != nil {
		return application.IdempotentArchiveCreditCardResult{}, ErrInvalidCreditCardCommand
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{})
	if err != nil {
		return application.IdempotentArchiveCreditCardResult{}, newRepositoryError(ErrBeginCreditCardTransaction, err)
	}
	committed := false
	defer rollbackCreditCardTransaction(transaction, &committed)()

	reserved, err := reserveCreditCardIdempotency(
		operationContext,
		transaction,
		command.UserID,
		command.Operation,
		command.IdempotencyKey,
		command.Fingerprint,
		archivedAt,
	)
	if err != nil {
		return application.IdempotentArchiveCreditCardResult{}, err
	}
	if !reserved {
		card, replayErr := replayCreditCard(
			operationContext,
			transaction,
			command.UserID,
			command.Operation,
			command.IdempotencyKey,
			command.Fingerprint,
		)
		if replayErr != nil {
			return application.IdempotentArchiveCreditCardResult{}, replayErr
		}
		if err := transaction.Commit(operationContext); err != nil {
			return application.IdempotentArchiveCreditCardResult{}, newRepositoryError(ErrCommitCreditCardTransaction, err)
		}
		committed = true
		return application.IdempotentArchiveCreditCardResult{CreditCard: card, Replayed: true}, nil
	}

	current, err := loadCreditCardForUpdate(operationContext, transaction, command.CreditCardID, command.UserID)
	if err != nil {
		return application.IdempotentArchiveCreditCardResult{}, err
	}
	archived, err := current.Archive(archivedAt)
	if err != nil {
		return application.IdempotentArchiveCreditCardResult{}, err
	}
	if !creditCardSnapshotsEqual(snapshotFromCreditCard(archived), snapshotFromCreditCard(command.CreditCard)) {
		return application.IdempotentArchiveCreditCardResult{}, ErrInvalidCreditCardCommand
	}
	if err := updateArchivedCreditCard(operationContext, transaction, archived); err != nil {
		return application.IdempotentArchiveCreditCardResult{}, err
	}
	if err := insertCreditCardAuditEvent(
		operationContext,
		transaction,
		archived,
		command.AuditEvent,
		archivedAt,
	); err != nil {
		return application.IdempotentArchiveCreditCardResult{}, err
	}
	if err := completeCreditCardIdempotency(
		operationContext,
		transaction,
		command.UserID,
		command.Operation,
		command.IdempotencyKey,
		archived,
		archivedAt,
	); err != nil {
		return application.IdempotentArchiveCreditCardResult{}, err
	}
	if err := transaction.Commit(operationContext); err != nil {
		return application.IdempotentArchiveCreditCardResult{}, newRepositoryError(ErrCommitCreditCardTransaction, err)
	}
	committed = true
	return application.IdempotentArchiveCreditCardResult{CreditCard: archived}, nil
}

func (repository *CreditCardRepository) FindCreditCardReplay(
	ctx context.Context,
	query application.CreditCardReplayQuery,
) (application.CreditCardReplayLookup, error) {
	if domain.ValidateCreditCardUserID(query.UserID) != nil ||
		!validCreditCardOperation(query.Operation) ||
		!validCreditCardIdempotencyKey(query.IdempotencyKey) {
		return application.CreditCardReplayLookup{}, ErrInvalidCreditCardQuery
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	card, found, err := readCreditCardReplay(
		operationContext,
		repository.pool,
		query.UserID,
		query.Operation,
		query.IdempotencyKey,
		query.Fingerprint,
	)
	if err != nil {
		return application.CreditCardReplayLookup{}, err
	}
	return application.CreditCardReplayLookup{CreditCard: card, Found: found}, nil
}

func (repository *CreditCardRepository) FindCreditCard(
	ctx context.Context,
	userID, creditCardID string,
) (application.CreditCardLookup, error) {
	if domain.ValidateCreditCardUserID(userID) != nil || domain.ValidateCreditCardID(creditCardID) != nil {
		return application.CreditCardLookup{}, ErrInvalidCreditCardQuery
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	row := repository.pool.QueryRow(operationContext, creditCardSelectSQL+`
		WHERE id = $1 AND user_id = $2
	`, creditCardID, userID)
	snapshot, err := scanCreditCardSnapshot(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.CreditCardLookup{}, nil
	}
	if err != nil {
		return application.CreditCardLookup{}, newRepositoryError(ErrReadCreditCard, err)
	}
	card, err := snapshot.rehydrate()
	if err != nil || card.UserID() != userID {
		if err == nil {
			err = ErrInvalidStoredCreditCard
		}
		return application.CreditCardLookup{}, newRepositoryError(ErrInvalidStoredCreditCard, err)
	}
	return application.CreditCardLookup{CreditCard: card, Found: true}, nil
}

func (repository *CreditCardRepository) ListCreditCards(ctx context.Context, userID string) ([]domain.CreditCard, error) {
	if domain.ValidateCreditCardUserID(userID) != nil {
		return nil, ErrInvalidCreditCardQuery
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	rows, err := repository.pool.Query(operationContext, creditCardSelectSQL+`
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, newRepositoryError(ErrListCreditCards, err)
	}
	defer rows.Close()
	items := make([]domain.CreditCard, 0)
	for rows.Next() {
		snapshot, scanErr := scanCreditCardSnapshot(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrListCreditCards, scanErr)
		}
		card, hydrateErr := snapshot.rehydrate()
		if hydrateErr != nil || card.UserID() != userID {
			if hydrateErr == nil {
				hydrateErr = ErrInvalidStoredCreditCard
			}
			return nil, newRepositoryError(ErrInvalidStoredCreditCard, hydrateErr)
		}
		items = append(items, card)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrListCreditCards, err)
	}
	return items, nil
}

const creditCardSelectSQL = `
	SELECT
		id,
		user_id,
		name,
		last_four,
		brand,
		closing_day,
		due_day,
		credit_limit_minor,
		credit_limit_currency,
		status,
		created_at,
		archived_at
	FROM credit_cards
`

func rollbackCreditCardTransaction(transaction pgx.Tx, committed *bool) func() {
	return func() {
		if *committed {
			return
		}
		rollbackContext, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()
		_ = transaction.Rollback(rollbackContext)
	}
}

func reserveCreditCardIdempotency(
	ctx context.Context,
	transaction pgx.Tx,
	userID, operation, key string,
	fingerprint application.RequestFingerprint,
	createdAt time.Time,
) (bool, error) {
	var reserved bool
	err := transaction.QueryRow(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, $2, $3, $4, 'PENDING', $5)
		ON CONFLICT (user_id, operation, idempotency_key) DO NOTHING
		RETURNING TRUE
	`, userID, operation, key, fingerprint[:], createdAt).Scan(&reserved)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, newRepositoryError(ErrReserveCreditCardIdempotency, err)
	}
	return reserved, nil
}

func completeCreditCardIdempotency(
	ctx context.Context,
	transaction pgx.Tx,
	userID, operation, key string,
	card domain.CreditCard,
	completedAt time.Time,
) error {
	snapshot := snapshotFromCreditCard(card)
	tag, err := transaction.Exec(ctx, `
		UPDATE credit_card_idempotency_records
		SET
			state = 'COMPLETED',
			credit_card_id = $4,
			result_name = $5,
			result_last_four = $6,
			result_brand = $7,
			result_closing_day = $8,
			result_due_day = $9,
			result_credit_limit_minor = $10,
			result_credit_limit_currency = $11,
			result_status = $12,
			result_created_at = $13,
			result_archived_at = $14,
			completed_at = $15
		WHERE user_id = $1
		  AND operation = $2
		  AND idempotency_key = $3
		  AND state = 'PENDING'
	`,
		userID,
		operation,
		key,
		snapshot.id,
		snapshot.name,
		snapshot.lastFour,
		snapshot.brand,
		snapshot.closingDay,
		snapshot.dueDay,
		snapshot.creditLimitMinor,
		snapshot.creditLimitCurrency,
		snapshot.status,
		snapshot.createdAt,
		snapshot.archivedAt,
		completedAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidCreditCardIdempotencyState
		}
		return newRepositoryError(ErrCompleteCreditCardIdempotency, err)
	}
	return nil
}

func replayCreditCard(
	ctx context.Context,
	querier creditCardRowQuerier,
	userID, operation, key string,
	fingerprint application.RequestFingerprint,
) (domain.CreditCard, error) {
	card, found, err := readCreditCardReplay(ctx, querier, userID, operation, key, fingerprint)
	if err != nil {
		return domain.CreditCard{}, err
	}
	if !found {
		return domain.CreditCard{}, ErrInvalidCreditCardIdempotencyState
	}
	return card, nil
}

func readCreditCardReplay(
	ctx context.Context,
	querier creditCardRowQuerier,
	userID, operation, key string,
	fingerprint application.RequestFingerprint,
) (domain.CreditCard, bool, error) {
	var (
		storedFingerprint []byte
		state             string
		snapshot          creditCardSnapshot
	)
	err := querier.QueryRow(ctx, `
		SELECT
			request_fingerprint,
			state,
			credit_card_id,
			user_id,
			result_name,
			result_last_four,
			result_brand,
			result_closing_day,
			result_due_day,
			result_credit_limit_minor,
			result_credit_limit_currency,
			result_status,
			result_created_at,
			result_archived_at
		FROM credit_card_idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, userID, operation, key).Scan(
		&storedFingerprint,
		&state,
		&snapshot.id,
		&snapshot.userID,
		&snapshot.name,
		&snapshot.lastFour,
		&snapshot.brand,
		&snapshot.closingDay,
		&snapshot.dueDay,
		&snapshot.creditLimitMinor,
		&snapshot.creditLimitCurrency,
		&snapshot.status,
		&snapshot.createdAt,
		&snapshot.archivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreditCard{}, false, nil
	}
	if err != nil {
		return domain.CreditCard{}, false, newRepositoryError(ErrReadCreditCardIdempotency, err)
	}
	if subtle.ConstantTimeCompare(storedFingerprint, fingerprint[:]) != 1 {
		return domain.CreditCard{}, false, application.ErrCreditCardIdempotencyConflict
	}
	if state != "COMPLETED" || snapshot.id == nil || snapshot.userID == nil {
		return domain.CreditCard{}, false, ErrInvalidCreditCardIdempotencyState
	}
	card, err := snapshot.rehydrate()
	if err != nil || card.UserID() != userID {
		if err == nil {
			err = ErrInvalidStoredCreditCard
		}
		return domain.CreditCard{}, false, newRepositoryError(ErrInvalidStoredCreditCard, err)
	}
	if (operation == application.IdempotencyOperationCreateCreditCard && card.Status() != domain.CreditCardStatusActive) ||
		(operation == application.IdempotencyOperationArchiveCreditCard && card.Status() != domain.CreditCardStatusArchived) {
		return domain.CreditCard{}, false, ErrInvalidCreditCardIdempotencyState
	}
	return card, true, nil
}

func insertCreditCard(ctx context.Context, transaction pgx.Tx, card domain.CreditCard) error {
	snapshot := snapshotFromCreditCard(card)
	_, err := transaction.Exec(ctx, `
		INSERT INTO credit_cards (
			id, user_id, name, last_four, brand, closing_day, due_day,
			credit_limit_minor, credit_limit_currency, status, created_at, archived_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		snapshot.id,
		snapshot.userID,
		snapshot.name,
		snapshot.lastFour,
		snapshot.brand,
		snapshot.closingDay,
		snapshot.dueDay,
		snapshot.creditLimitMinor,
		snapshot.creditLimitCurrency,
		snapshot.status,
		snapshot.createdAt,
		snapshot.archivedAt,
	)
	if err != nil {
		return newRepositoryError(ErrInsertCreditCard, err)
	}
	return nil
}

func insertCreditCardAuditEvent(
	ctx context.Context,
	transaction pgx.Tx,
	card domain.CreditCard,
	event application.CreditCardAuditEvent,
	createdAt time.Time,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO credit_card_audit_events (
			user_id, credit_card_id, event_type, created_at
		) VALUES ($1, $2, $3, $4)
	`, card.UserID(), card.ID(), event, createdAt)
	if err != nil {
		return newRepositoryError(ErrInsertCreditCardAuditEvent, err)
	}
	return nil
}

func loadCreditCardForUpdate(
	ctx context.Context,
	transaction pgx.Tx,
	creditCardID, userID string,
) (domain.CreditCard, error) {
	row := transaction.QueryRow(ctx, creditCardSelectSQL+`
		WHERE id = $1 AND user_id = $2
		FOR UPDATE
	`, creditCardID, userID)
	snapshot, err := scanCreditCardSnapshot(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CreditCard{}, application.ErrCreditCardNotFound
	}
	if err != nil {
		return domain.CreditCard{}, newRepositoryError(ErrReadCreditCard, err)
	}
	card, err := snapshot.rehydrate()
	if err != nil {
		return domain.CreditCard{}, newRepositoryError(ErrInvalidStoredCreditCard, err)
	}
	return card, nil
}

func updateArchivedCreditCard(ctx context.Context, transaction pgx.Tx, card domain.CreditCard) error {
	archivedAt, ok := card.ArchivedAt()
	if !ok || card.Status() != domain.CreditCardStatusArchived {
		return ErrInvalidCreditCardCommand
	}
	tag, err := transaction.Exec(ctx, `
		UPDATE credit_cards
		SET status = 'ARCHIVED', archived_at = $3
		WHERE id = $1 AND user_id = $2 AND status = 'ACTIVE' AND archived_at IS NULL
	`, card.ID(), card.UserID(), archivedAt)
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidStoredCreditCard
		}
		return newRepositoryError(ErrUpdateCreditCard, err)
	}
	return nil
}

type creditCardRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type creditCardScanner interface {
	Scan(...any) error
}

type creditCardSnapshot struct {
	id                  *string
	userID              *string
	name                *string
	lastFour            *string
	brand               *string
	closingDay          *int16
	dueDay              *int16
	creditLimitMinor    *int64
	creditLimitCurrency *string
	status              *string
	createdAt           *time.Time
	archivedAt          *time.Time
}

func snapshotFromCreditCard(card domain.CreditCard) creditCardSnapshot {
	id := card.ID()
	userID := card.UserID()
	name := card.Name().String()
	closingDay := int16(card.ClosingDayAnchor().Day())
	dueDay := int16(card.DueDayAnchor().Day())
	status := string(card.Status())
	createdAt := card.CreatedAt()
	snapshot := creditCardSnapshot{
		id:         &id,
		userID:     &userID,
		name:       &name,
		closingDay: &closingDay,
		dueDay:     &dueDay,
		status:     &status,
		createdAt:  &createdAt,
	}
	if lastFour, ok := card.LastFour(); ok {
		value := lastFour.String()
		snapshot.lastFour = &value
	}
	if brand, ok := card.Brand(); ok {
		value := string(brand)
		snapshot.brand = &value
	}
	if limit, ok := card.CreditLimit(); ok {
		minor := limit.MinorUnits()
		currency := string(limit.Currency())
		snapshot.creditLimitMinor = &minor
		snapshot.creditLimitCurrency = &currency
	}
	if archivedAt, ok := card.ArchivedAt(); ok {
		snapshot.archivedAt = &archivedAt
	}
	return snapshot
}

func scanCreditCardSnapshot(scanner creditCardScanner) (creditCardSnapshot, error) {
	var snapshot creditCardSnapshot
	err := scanner.Scan(
		&snapshot.id,
		&snapshot.userID,
		&snapshot.name,
		&snapshot.lastFour,
		&snapshot.brand,
		&snapshot.closingDay,
		&snapshot.dueDay,
		&snapshot.creditLimitMinor,
		&snapshot.creditLimitCurrency,
		&snapshot.status,
		&snapshot.createdAt,
		&snapshot.archivedAt,
	)
	return snapshot, err
}

func (snapshot creditCardSnapshot) rehydrate() (domain.CreditCard, error) {
	if snapshot.id == nil || snapshot.userID == nil || snapshot.name == nil ||
		snapshot.closingDay == nil || snapshot.dueDay == nil || snapshot.status == nil ||
		snapshot.createdAt == nil ||
		(snapshot.creditLimitMinor == nil) != (snapshot.creditLimitCurrency == nil) ||
		!isCanonicalCreditCardInstant(*snapshot.createdAt) ||
		(snapshot.archivedAt != nil && !isCanonicalCreditCardInstant(*snapshot.archivedAt)) {
		return domain.CreditCard{}, ErrInvalidStoredCreditCard
	}
	name, err := domain.NewCreditCardName(*snapshot.name)
	if err != nil || name.String() != *snapshot.name {
		return domain.CreditCard{}, ErrInvalidStoredCreditCard
	}
	closingDay, err := domain.NewDayOfMonthAnchor(int(*snapshot.closingDay))
	if err != nil {
		return domain.CreditCard{}, err
	}
	dueDay, err := domain.NewDayOfMonthAnchor(int(*snapshot.dueDay))
	if err != nil {
		return domain.CreditCard{}, err
	}
	details := domain.CreditCardDetails{
		UserID:           *snapshot.userID,
		Name:             name,
		ClosingDayAnchor: closingDay,
		DueDayAnchor:     dueDay,
	}
	if snapshot.lastFour != nil {
		lastFour, err := domain.NewCreditCardLastFour(*snapshot.lastFour)
		if err != nil {
			return domain.CreditCard{}, err
		}
		details.LastFour = &lastFour
	}
	if snapshot.brand != nil {
		brand := domain.CreditCardBrand(*snapshot.brand)
		if err := domain.ValidateCreditCardBrand(brand); err != nil {
			return domain.CreditCard{}, err
		}
		details.Brand = &brand
	}
	if snapshot.creditLimitMinor != nil {
		limit, err := domain.NewCreditCardCreditLimit(
			*snapshot.creditLimitMinor,
			domain.Currency(*snapshot.creditLimitCurrency),
		)
		if err != nil {
			return domain.CreditCard{}, err
		}
		details.CreditLimit = &limit
	}
	return domain.RehydrateCreditCard(domain.CreditCardRehydrationParams{
		ID:         *snapshot.id,
		Details:    details,
		Status:     domain.CreditCardStatus(*snapshot.status),
		CreatedAt:  *snapshot.createdAt,
		ArchivedAt: snapshot.archivedAt,
	})
}

func creditCardSnapshotsEqual(left, right creditCardSnapshot) bool {
	return equalOptionalString(left.id, right.id) &&
		equalOptionalString(left.userID, right.userID) &&
		equalOptionalString(left.name, right.name) &&
		equalOptionalString(left.lastFour, right.lastFour) &&
		equalOptionalString(left.brand, right.brand) &&
		equalOptionalInt16(left.closingDay, right.closingDay) &&
		equalOptionalInt16(left.dueDay, right.dueDay) &&
		equalOptionalInt64(left.creditLimitMinor, right.creditLimitMinor) &&
		equalOptionalString(left.creditLimitCurrency, right.creditLimitCurrency) &&
		equalOptionalString(left.status, right.status) &&
		equalOptionalTime(left.createdAt, right.createdAt) &&
		equalOptionalTime(left.archivedAt, right.archivedAt)
}

func equalOptionalString(left, right *string) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalOptionalInt16(left, right *int16) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalOptionalInt64(left, right *int64) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func equalOptionalTime(left, right *time.Time) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && left.Equal(*right))
}

func validCreditCardOperation(operation string) bool {
	return operation == application.IdempotencyOperationCreateCreditCard ||
		operation == application.IdempotencyOperationArchiveCreditCard
}

func validCreditCardIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for index := 0; index < len(key); index++ {
		if key[index] < '!' || key[index] > '~' {
			return false
		}
	}
	return true
}

func isCanonicalCreditCardInstant(value time.Time) bool {
	return !value.IsZero() && value.Equal(value.UTC().Truncate(time.Microsecond))
}
