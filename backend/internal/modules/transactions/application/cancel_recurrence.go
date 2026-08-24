package application

import (
	"context"
	"errors"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const IdempotencyOperationCancelRecurrence = "CANCEL_RECURRENCE"

const RecurrenceAuditEventCancelled RecurrenceAuditEvent = "RECURRENCE_CANCELLED"

var (
	ErrMissingRecurrenceCancellationStore = errors.New("cancel recurrence: command store is required")
	ErrRecurrenceNotFound                 = errors.New("recurrence: not found")
	ErrRecurrenceCancellationPersistence  = errors.New("cancel recurrence: persistence failed")
)

// IdempotentCancelRecurrenceCommand is an owner-scoped cancellation intent.
// The adapter must load and transition the recurrence atomically with audit
// and idempotency persistence.
type IdempotentCancelRecurrenceCommand struct {
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
	AuditEvent     RecurrenceAuditEvent
	UserID         string
	RecurrenceID   string
	CancelledAt    time.Time
}

type IdempotentCancelRecurrenceResult struct {
	Recurrence domain.Recurrence
	Replayed   bool
}

// RecurrenceCancellationStore is separate from creation so each use case
// depends only on its atomic command capability.
type RecurrenceCancellationStore interface {
	CancelRecurrence(context.Context, IdempotentCancelRecurrenceCommand) (IdempotentCancelRecurrenceResult, error)
}

type CancelRecurrenceInput struct {
	UserID         string
	RecurrenceID   string
	IdempotencyKey string
}

type CancelRecurrenceResult struct {
	Recurrence domain.Recurrence
	Replayed   bool
}

type CancelRecurrence struct {
	store        RecurrenceCancellationStore
	replayReader RecurrenceReplayReader
	clock        Clock
}

func NewCancelRecurrence(
	store RecurrenceCancellationStore,
	replayReader RecurrenceReplayReader,
	clock Clock,
) (*CancelRecurrence, error) {
	if store == nil {
		return nil, ErrMissingRecurrenceCancellationStore
	}
	if replayReader == nil {
		return nil, ErrMissingRecurrenceReplayReader
	}
	if clock == nil {
		return nil, ErrMissingRecurrenceClock
	}
	return &CancelRecurrence{store: store, replayReader: replayReader, clock: clock}, nil
}

func (useCase *CancelRecurrence) Execute(
	ctx context.Context,
	input CancelRecurrenceInput,
) (CancelRecurrenceResult, error) {
	if err := ctx.Err(); err != nil {
		return CancelRecurrenceResult{}, err
	}
	if err := domain.ValidateRecurrenceUserID(input.UserID); err != nil {
		return CancelRecurrenceResult{}, err
	}
	if err := domain.ValidateRecurrenceID(input.RecurrenceID); err != nil {
		return CancelRecurrenceResult{}, err
	}
	if err := validateRecurrenceIdempotencyKey(input.IdempotencyKey); err != nil {
		return CancelRecurrenceResult{}, err
	}

	fingerprint := fingerprintRecurrenceCancellation(input.UserID, input.RecurrenceID)
	persisted, found, err := findPersistedRecurrenceReplay(ctx, useCase.replayReader, RecurrenceReplayQuery{
		UserID:         input.UserID,
		Operation:      IdempotencyOperationCancelRecurrence,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
	})
	if err != nil {
		return CancelRecurrenceResult{}, err
	}
	if found {
		return CancelRecurrenceResult{Recurrence: persisted, Replayed: true}, nil
	}

	cancelledAt := canonicalizeFinancialInstant(useCase.clock.Now())
	if cancelledAt.IsZero() {
		return CancelRecurrenceResult{}, domain.ErrInvalidRecurrenceCancelledAt
	}
	stored, err := useCase.store.CancelRecurrence(ctx, IdempotentCancelRecurrenceCommand{
		Operation:      IdempotencyOperationCancelRecurrence,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
		AuditEvent:     RecurrenceAuditEventCancelled,
		UserID:         input.UserID,
		RecurrenceID:   input.RecurrenceID,
		CancelledAt:    cancelledAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrRecurrenceIdempotencyConflict):
			return CancelRecurrenceResult{}, ErrRecurrenceIdempotencyConflict
		case errors.Is(err, ErrRecurrenceNotFound):
			return CancelRecurrenceResult{}, ErrRecurrenceNotFound
		case errors.Is(err, domain.ErrRecurrenceAlreadyCancelled):
			return CancelRecurrenceResult{}, domain.ErrRecurrenceAlreadyCancelled
		default:
			return CancelRecurrenceResult{}, newSafeOperationError(ErrRecurrenceCancellationPersistence, err)
		}
	}
	return CancelRecurrenceResult{Recurrence: stored.Recurrence, Replayed: stored.Replayed}, nil
}
