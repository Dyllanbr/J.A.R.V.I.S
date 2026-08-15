package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

const IdempotencyOperationCreateIncome = "CREATE_INCOME"

var (
	ErrMissingIncomeCommandStore    = errors.New("record income: command store is required")
	ErrMissingIncomeIDGenerator     = errors.New("record income: id generator is required")
	ErrMissingIncomeClock           = errors.New("record income: clock is required")
	ErrIncomeIdempotencyKeyRequired = errors.New("record income: idempotency key is required")
	ErrIncomeIdempotencyKeyInvalid  = errors.New("record income: idempotency key is invalid")
	ErrIncomeIdempotencyConflict    = errors.New("record income: idempotency key was reused")
	ErrIncomeIDGeneration           = errors.New("record income: id generation failed")
	ErrIncomeCommandPersistence     = errors.New("record income: persistence failed")
)

// IdempotentIncomeCommand carries the minimum data required by a future
// adapter to persist idempotency metadata, Income and its audit event atomically.
type IdempotentIncomeCommand struct {
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
	Income         domain.Income
}

// IdempotentIncomeResult represents a new record or a persisted replay.
type IdempotentIncomeResult struct {
	Income   domain.Income
	Replayed bool
}

// IncomeCommandStore is the specific atomic write capability consumed by
// RecordIncome. It is not a generic unit of work.
type IncomeCommandStore interface {
	RecordIncome(context.Context, IdempotentIncomeCommand) (IdempotentIncomeResult, error)
}

// IncomeIDGenerator creates opaque income identifiers without coupling the
// use case to their representation.
type IncomeIDGenerator interface {
	NewIncomeID() (string, error)
}

// RecordIncomeInput is confirmed channel input plus its operation key.
type RecordIncomeInput struct {
	Income         CreateIncomeInput
	IdempotencyKey string
}

// RecordIncomeResult returns the newly stored or replayed Income.
type RecordIncomeResult struct {
	Income   domain.Income
	Replayed bool
}

// RecordIncome prepares a confirmed, idempotent income command.
type RecordIncome struct {
	store       IncomeCommandStore
	idGenerator IncomeIDGenerator
	clock       Clock
}

// NewRecordIncome composes the command with only its required ports.
func NewRecordIncome(
	store IncomeCommandStore,
	idGenerator IncomeIDGenerator,
	clock Clock,
) (*RecordIncome, error) {
	if store == nil {
		return nil, ErrMissingIncomeCommandStore
	}
	if idGenerator == nil {
		return nil, ErrMissingIncomeIDGenerator
	}
	if clock == nil {
		return nil, ErrMissingIncomeClock
	}
	return &RecordIncome{store: store, idGenerator: idGenerator, clock: clock}, nil
}

// Execute validates command semantics before consuming generated values and
// delegates the future atomic write to IncomeCommandStore.
func (useCase *RecordIncome) Execute(ctx context.Context, input RecordIncomeInput) (RecordIncomeResult, error) {
	if err := ctx.Err(); err != nil {
		return RecordIncomeResult{}, err
	}
	if err := validateIncomeIdempotencyKey(input.IdempotencyKey); err != nil {
		return RecordIncomeResult{}, err
	}

	details, err := normalizeIncomeInput(input.Income)
	if err != nil {
		return RecordIncomeResult{}, err
	}
	fingerprint := fingerprintIncome(details)

	id, err := useCase.idGenerator.NewIncomeID()
	if err != nil {
		return RecordIncomeResult{}, newSafeOperationError(ErrIncomeIDGeneration, err)
	}
	income, err := domain.NewIncome(domain.IncomeParams{
		ID:        id,
		Details:   details,
		CreatedAt: canonicalizeFinancialInstant(useCase.clock.Now()),
	})
	if err != nil {
		return RecordIncomeResult{}, err
	}

	stored, err := useCase.store.RecordIncome(ctx, IdempotentIncomeCommand{
		Operation:      IdempotencyOperationCreateIncome,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
		Income:         income,
	})
	if err != nil {
		if errors.Is(err, ErrIncomeIdempotencyConflict) {
			return RecordIncomeResult{}, ErrIncomeIdempotencyConflict
		}
		return RecordIncomeResult{}, newSafeOperationError(ErrIncomeCommandPersistence, err)
	}

	return RecordIncomeResult{Income: stored.Income, Replayed: stored.Replayed}, nil
}

func validateIncomeIdempotencyKey(key string) error {
	if key == "" {
		return ErrIncomeIdempotencyKeyRequired
	}
	if !isValidIdempotencyKey(key) {
		return ErrIncomeIdempotencyKeyInvalid
	}
	return nil
}

func fingerprintIncome(details domain.IncomeDetails) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, string(domain.TransactionTypeIncome))
	writeFingerprintString(digest, details.Description)
	writeFingerprintInt64(digest, details.Amount.MinorUnits())
	writeFingerprintString(digest, string(details.Amount.Currency()))
	writeFingerprintString(digest, details.OccurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	writeFingerprintString(digest, details.FinancialTimezone)
	writeFingerprintString(digest, string(details.Origin))

	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}
