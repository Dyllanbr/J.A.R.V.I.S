package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

const CreditCardAuditEventArchived CreditCardAuditEvent = "CREDIT_CARD_ARCHIVED"

var (
	ErrMissingCreditCardArchiveStore = errors.New("archive credit card: command store is required")
	ErrCreditCardArchivePersistence  = errors.New("archive credit card: persistence failed")
)

type IdempotentArchiveCreditCardCommand struct {
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
	AuditEvent     CreditCardAuditEvent
	UserID         string
	CreditCardID   string
	CreditCard     domain.CreditCard
}

type IdempotentArchiveCreditCardResult struct {
	CreditCard domain.CreditCard
	Replayed   bool
}

// CreditCardArchiveStore atomically verifies current lifecycle state, archives
// once, writes audit/idempotency and preserves the original ARCHIVED snapshot.
type CreditCardArchiveStore interface {
	ArchiveCreditCard(context.Context, IdempotentArchiveCreditCardCommand) (IdempotentArchiveCreditCardResult, error)
}

type ArchiveCreditCardInput struct {
	UserID         string
	CreditCardID   string
	IdempotencyKey string
}

type ArchiveCreditCardResult struct {
	CreditCard domain.CreditCard
	Replayed   bool
}

type ArchiveCreditCard struct {
	store        CreditCardArchiveStore
	replayReader CreditCardReplayReader
	reader       CreditCardLookupReader
	clock        Clock
}

func NewArchiveCreditCard(
	store CreditCardArchiveStore,
	replayReader CreditCardReplayReader,
	reader CreditCardLookupReader,
	clock Clock,
) (*ArchiveCreditCard, error) {
	if store == nil {
		return nil, ErrMissingCreditCardArchiveStore
	}
	if replayReader == nil {
		return nil, ErrMissingCreditCardReplayReader
	}
	if reader == nil {
		return nil, ErrMissingCreditCardLookupReader
	}
	if clock == nil {
		return nil, ErrMissingCreditCardClock
	}
	return &ArchiveCreditCard{store: store, replayReader: replayReader, reader: reader, clock: clock}, nil
}

func (useCase *ArchiveCreditCard) Execute(
	ctx context.Context,
	input ArchiveCreditCardInput,
) (ArchiveCreditCardResult, error) {
	if err := ctx.Err(); err != nil {
		return ArchiveCreditCardResult{}, err
	}
	if err := domain.ValidateCreditCardUserID(input.UserID); err != nil {
		return ArchiveCreditCardResult{}, err
	}
	if err := domain.ValidateCreditCardID(input.CreditCardID); err != nil {
		return ArchiveCreditCardResult{}, err
	}
	if err := validateCreditCardIdempotencyKey(input.IdempotencyKey); err != nil {
		return ArchiveCreditCardResult{}, err
	}

	fingerprint := fingerprintCreditCardArchive(input.UserID, input.CreditCardID)
	persisted, found, err := findPersistedCreditCardReplay(ctx, useCase.replayReader, CreditCardReplayQuery{
		UserID:         input.UserID,
		Operation:      IdempotencyOperationArchiveCreditCard,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
	})
	if err != nil {
		return ArchiveCreditCardResult{}, err
	}
	if found {
		archived := domain.CreditCardStatusArchived
		if err := validateCreditCardDependencySnapshot(persisted, input.UserID, input.CreditCardID, &archived); err != nil {
			return ArchiveCreditCardResult{}, err
		}
		return ArchiveCreditCardResult{CreditCard: persisted, Replayed: true}, nil
	}
	if err := ctx.Err(); err != nil {
		return ArchiveCreditCardResult{}, err
	}

	lookup, err := useCase.reader.FindCreditCard(ctx, input.UserID, input.CreditCardID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ArchiveCreditCardResult{}, err
		}
		return ArchiveCreditCardResult{}, newSafeOperationError(ErrCreditCardLookup, err)
	}
	if err := ctx.Err(); err != nil {
		return ArchiveCreditCardResult{}, err
	}
	if !lookup.Found {
		return ArchiveCreditCardResult{}, ErrCreditCardNotFound
	}
	if err := validateCreditCardDependencySnapshot(lookup.CreditCard, input.UserID, input.CreditCardID, nil); err != nil {
		return ArchiveCreditCardResult{}, err
	}
	if lookup.CreditCard.Status() == domain.CreditCardStatusArchived {
		return ArchiveCreditCardResult{}, domain.ErrCreditCardAlreadyArchived
	}

	archivedAt := canonicalizeFinancialInstant(useCase.clock.Now())
	card, err := lookup.CreditCard.Archive(archivedAt)
	if err != nil {
		return ArchiveCreditCardResult{}, newSafeOperationError(ErrCreditCardDependencyResult, err)
	}
	if err := ctx.Err(); err != nil {
		return ArchiveCreditCardResult{}, err
	}
	stored, err := useCase.store.ArchiveCreditCard(ctx, IdempotentArchiveCreditCardCommand{
		Operation:      IdempotencyOperationArchiveCreditCard,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
		AuditEvent:     CreditCardAuditEventArchived,
		UserID:         input.UserID,
		CreditCardID:   input.CreditCardID,
		CreditCard:     card,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrCreditCardIdempotencyConflict):
			return ArchiveCreditCardResult{}, ErrCreditCardIdempotencyConflict
		case errors.Is(err, ErrCreditCardNotFound):
			return ArchiveCreditCardResult{}, ErrCreditCardNotFound
		case errors.Is(err, domain.ErrCreditCardAlreadyArchived):
			return ArchiveCreditCardResult{}, domain.ErrCreditCardAlreadyArchived
		default:
			return ArchiveCreditCardResult{}, newSafeOperationError(ErrCreditCardArchivePersistence, err)
		}
	}
	archived := domain.CreditCardStatusArchived
	if err := validateCreditCardDependencySnapshot(stored.CreditCard, input.UserID, input.CreditCardID, &archived); err != nil {
		return ArchiveCreditCardResult{}, err
	}
	if !stored.Replayed && !creditCardsEqual(stored.CreditCard, card) {
		return ArchiveCreditCardResult{}, ErrCreditCardDependencyResult
	}
	return ArchiveCreditCardResult{CreditCard: stored.CreditCard, Replayed: stored.Replayed}, nil
}
