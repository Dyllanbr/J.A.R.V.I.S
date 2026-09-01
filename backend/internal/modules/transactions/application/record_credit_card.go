package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

type CreditCardAuditEvent string

const CreditCardAuditEventCreated CreditCardAuditEvent = "CREDIT_CARD_CREATED"

var (
	ErrMissingCreditCardCommandStore = errors.New("record credit card: command store is required")
	ErrMissingCreditCardIDGenerator  = errors.New("record credit card: id generator is required")
	ErrMissingCreditCardClock        = errors.New("credit card: clock is required")
	ErrCreditCardIDGeneration        = errors.New("record credit card: id generation failed")
	ErrCreditCardCommandPersistence  = errors.New("record credit card: persistence failed")
)

type IdempotentCreditCardCommand struct {
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
	AuditEvent     CreditCardAuditEvent
	CreditCard     domain.CreditCard
}

type IdempotentCreditCardResult struct {
	CreditCard domain.CreditCard
	Replayed   bool
}

// CreditCardCommandStore atomically owns create idempotency, the card, audit
// intent and the historical ACTIVE result snapshot.
type CreditCardCommandStore interface {
	RecordCreditCard(context.Context, IdempotentCreditCardCommand) (IdempotentCreditCardResult, error)
}

type CreditCardIDGenerator interface {
	NewCreditCardID() (string, error)
}

type RecordCreditCardInput struct {
	CreditCard     CreditCardInput
	IdempotencyKey string
}

type RecordCreditCardResult struct {
	CreditCard domain.CreditCard
	Replayed   bool
}

type RecordCreditCard struct {
	store        CreditCardCommandStore
	replayReader CreditCardReplayReader
	idGenerator  CreditCardIDGenerator
	clock        Clock
}

func NewRecordCreditCard(
	store CreditCardCommandStore,
	replayReader CreditCardReplayReader,
	idGenerator CreditCardIDGenerator,
	clock Clock,
) (*RecordCreditCard, error) {
	if store == nil {
		return nil, ErrMissingCreditCardCommandStore
	}
	if replayReader == nil {
		return nil, ErrMissingCreditCardReplayReader
	}
	if idGenerator == nil {
		return nil, ErrMissingCreditCardIDGenerator
	}
	if clock == nil {
		return nil, ErrMissingCreditCardClock
	}
	return &RecordCreditCard{store: store, replayReader: replayReader, idGenerator: idGenerator, clock: clock}, nil
}

func (useCase *RecordCreditCard) Execute(
	ctx context.Context,
	input RecordCreditCardInput,
) (RecordCreditCardResult, error) {
	if err := ctx.Err(); err != nil {
		return RecordCreditCardResult{}, err
	}
	if err := validateCreditCardIdempotencyKey(input.IdempotencyKey); err != nil {
		return RecordCreditCardResult{}, err
	}
	details, err := normalizeCreditCardInput(ctx, input.CreditCard)
	if err != nil {
		return RecordCreditCardResult{}, err
	}
	fingerprint := fingerprintCreditCardCreation(details)
	persisted, found, err := findPersistedCreditCardReplay(ctx, useCase.replayReader, CreditCardReplayQuery{
		UserID:         details.UserID,
		Operation:      IdempotencyOperationCreateCreditCard,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
	})
	if err != nil {
		return RecordCreditCardResult{}, err
	}
	if found {
		active := domain.CreditCardStatusActive
		if err := validateCreditCardDependencySnapshot(persisted, details.UserID, "", &active); err != nil ||
			!creditCardMatchesDetails(persisted, details) {
			return RecordCreditCardResult{}, ErrCreditCardDependencyResult
		}
		return RecordCreditCardResult{CreditCard: persisted, Replayed: true}, nil
	}
	if err := ctx.Err(); err != nil {
		return RecordCreditCardResult{}, err
	}

	id, err := useCase.idGenerator.NewCreditCardID()
	if err != nil {
		return RecordCreditCardResult{}, newSafeOperationError(ErrCreditCardIDGeneration, err)
	}
	if err := domain.ValidateCreditCardID(id); err != nil {
		return RecordCreditCardResult{}, newSafeOperationError(ErrCreditCardIDGeneration, err)
	}
	if err := ctx.Err(); err != nil {
		return RecordCreditCardResult{}, err
	}
	createdAt := canonicalizeFinancialInstant(useCase.clock.Now())
	if createdAt.IsZero() {
		return RecordCreditCardResult{}, newSafeOperationError(ErrCreditCardDependencyResult, domain.ErrInvalidCreditCardCreatedAt)
	}
	card, err := domain.NewCreditCard(domain.CreditCardParams{ID: id, Details: details, CreatedAt: createdAt})
	if err != nil {
		return RecordCreditCardResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RecordCreditCardResult{}, err
	}
	stored, err := useCase.store.RecordCreditCard(ctx, IdempotentCreditCardCommand{
		Operation:      IdempotencyOperationCreateCreditCard,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
		AuditEvent:     CreditCardAuditEventCreated,
		CreditCard:     card,
	})
	if err != nil {
		if errors.Is(err, ErrCreditCardIdempotencyConflict) {
			return RecordCreditCardResult{}, ErrCreditCardIdempotencyConflict
		}
		return RecordCreditCardResult{}, newSafeOperationError(ErrCreditCardCommandPersistence, err)
	}
	active := domain.CreditCardStatusActive
	if err := validateCreditCardDependencySnapshot(stored.CreditCard, details.UserID, "", &active); err != nil ||
		!creditCardMatchesDetails(stored.CreditCard, details) {
		return RecordCreditCardResult{}, ErrCreditCardDependencyResult
	}
	if !stored.Replayed && !creditCardsEqual(stored.CreditCard, card) {
		return RecordCreditCardResult{}, ErrCreditCardDependencyResult
	}
	return RecordCreditCardResult{CreditCard: stored.CreditCard, Replayed: stored.Replayed}, nil
}
