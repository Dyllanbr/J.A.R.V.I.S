package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

const IdempotencyOperationCreateRecurrence = "CREATE_RECURRENCE"

// RecurrenceAuditEvent identifies the minimal audit intent owned atomically by
// a recurrence command store.
type RecurrenceAuditEvent string

const RecurrenceAuditEventCreated RecurrenceAuditEvent = "RECURRENCE_CREATED"

var (
	ErrMissingRecurrenceCommandStore    = errors.New("record recurrence: command store is required")
	ErrMissingRecurrenceIDGenerator     = errors.New("record recurrence: id generator is required")
	ErrMissingRecurrenceClock           = errors.New("record recurrence: clock is required")
	ErrRecurrenceIdempotencyKeyRequired = errors.New("recurrence: idempotency key is required")
	ErrRecurrenceIdempotencyKeyInvalid  = errors.New("recurrence: idempotency key is invalid")
	ErrRecurrenceIdempotencyConflict    = errors.New("recurrence: idempotency key was reused")
	ErrRecurrenceIDGeneration           = errors.New("record recurrence: id generation failed")
	ErrRecurrenceCommandPersistence     = errors.New("record recurrence: persistence failed")
)

// IdempotentRecurrenceCommand carries everything an adapter needs to persist
// recurrence, audit intent and idempotency metadata atomically.
type IdempotentRecurrenceCommand struct {
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
	AuditEvent     RecurrenceAuditEvent
	Recurrence     domain.Recurrence
}

type IdempotentRecurrenceResult struct {
	Recurrence domain.Recurrence
	Replayed   bool
}

// RecurrenceCommandStore is the create-specific atomic persistence port.
type RecurrenceCommandStore interface {
	RecordRecurrence(context.Context, IdempotentRecurrenceCommand) (IdempotentRecurrenceResult, error)
}

// RecurrenceIDGenerator creates opaque recurrence identifiers.
type RecurrenceIDGenerator interface {
	NewRecurrenceID() (string, error)
}

type RecordRecurrenceInput struct {
	Recurrence     RecurrenceInput
	IdempotencyKey string
}

type RecordRecurrenceResult struct {
	Recurrence domain.Recurrence
	Replayed   bool
}

// RecordRecurrence prepares a confirmed recurring commitment. It has no
// transaction write dependency and cannot create Expense or Income.
type RecordRecurrence struct {
	store        RecurrenceCommandStore
	replayReader RecurrenceReplayReader
	idGenerator  RecurrenceIDGenerator
	clock        Clock
}

func NewRecordRecurrence(
	store RecurrenceCommandStore,
	replayReader RecurrenceReplayReader,
	idGenerator RecurrenceIDGenerator,
	clock Clock,
) (*RecordRecurrence, error) {
	if store == nil {
		return nil, ErrMissingRecurrenceCommandStore
	}
	if replayReader == nil {
		return nil, ErrMissingRecurrenceReplayReader
	}
	if idGenerator == nil {
		return nil, ErrMissingRecurrenceIDGenerator
	}
	if clock == nil {
		return nil, ErrMissingRecurrenceClock
	}
	return &RecordRecurrence{store: store, replayReader: replayReader, idGenerator: idGenerator, clock: clock}, nil
}

func (useCase *RecordRecurrence) Execute(
	ctx context.Context,
	input RecordRecurrenceInput,
) (RecordRecurrenceResult, error) {
	if err := ctx.Err(); err != nil {
		return RecordRecurrenceResult{}, err
	}
	if err := validateRecurrenceIdempotencyKey(input.IdempotencyKey); err != nil {
		return RecordRecurrenceResult{}, err
	}
	details, err := normalizeRecurrenceInput(ctx, input.Recurrence)
	if err != nil {
		return RecordRecurrenceResult{}, err
	}
	fingerprint := fingerprintRecurrence(details)
	persisted, found, err := findPersistedRecurrenceReplay(ctx, useCase.replayReader, RecurrenceReplayQuery{
		UserID:         details.UserID,
		Operation:      IdempotencyOperationCreateRecurrence,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
	})
	if err != nil {
		return RecordRecurrenceResult{}, err
	}
	if found {
		return RecordRecurrenceResult{Recurrence: persisted, Replayed: true}, nil
	}

	id, err := useCase.idGenerator.NewRecurrenceID()
	if err != nil {
		return RecordRecurrenceResult{}, newSafeOperationError(ErrRecurrenceIDGeneration, err)
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID:        id,
		Details:   details,
		CreatedAt: canonicalizeFinancialInstant(useCase.clock.Now()),
	})
	if err != nil {
		return RecordRecurrenceResult{}, err
	}

	stored, err := useCase.store.RecordRecurrence(ctx, IdempotentRecurrenceCommand{
		Operation:      IdempotencyOperationCreateRecurrence,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
		AuditEvent:     RecurrenceAuditEventCreated,
		Recurrence:     recurrence,
	})
	if err != nil {
		if errors.Is(err, ErrRecurrenceIdempotencyConflict) {
			return RecordRecurrenceResult{}, ErrRecurrenceIdempotencyConflict
		}
		return RecordRecurrenceResult{}, newSafeOperationError(ErrRecurrenceCommandPersistence, err)
	}
	return RecordRecurrenceResult{Recurrence: stored.Recurrence, Replayed: stored.Replayed}, nil
}
