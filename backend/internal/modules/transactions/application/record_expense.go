package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

const IdempotencyOperationCreateExpense = "CREATE_EXPENSE"

var (
	ErrMissingCommandStore       = errors.New("record expense: command store is required")
	ErrIdempotencyKeyRequired    = errors.New("record expense: idempotency key is required")
	ErrIdempotencyKeyInvalid     = errors.New("record expense: idempotency key is invalid")
	ErrIdempotencyConflict       = errors.New("record expense: idempotency key was reused")
	ErrExpenseCommandPersistence = errors.New("record expense: persistence failed")
)

// IdempotentExpenseCommand carries the minimum data an atomic write adapter
// needs to reserve an operation and persist its Expense and audit event.
type IdempotentExpenseCommand struct {
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
	Expense        domain.Expense
}

// IdempotentExpenseResult returns either the newly stored Expense or the
// original resource loaded during a replay.
type IdempotentExpenseResult struct {
	Expense  domain.Expense
	Replayed bool
}

// ExpenseCommandStore atomically owns idempotency metadata, Expense and audit
// persistence for the create-expense command.
type ExpenseCommandStore interface {
	Record(context.Context, IdempotentExpenseCommand) (IdempotentExpenseResult, error)
}

// RecordExpenseInput is the already-confirmed channel input plus its operation
// idempotency key.
type RecordExpenseInput struct {
	Expense        CreateExpenseInput
	IdempotencyKey string
}

// RecordExpenseResult is returned only when the atomic command store succeeds
// or safely replays the original resource.
type RecordExpenseResult struct {
	Expense  domain.Expense
	Replayed bool
}

// RecordExpense executes the mutating command after explicit confirmation by
// the caller. Confirmation itself is intentionally not represented here.
type RecordExpense struct {
	store       ExpenseCommandStore
	idGenerator ExpenseIDGenerator
	clock       Clock
}

// NewRecordExpense composes the command with its concrete required ports.
func NewRecordExpense(
	store ExpenseCommandStore,
	idGenerator ExpenseIDGenerator,
	clock Clock,
) (*RecordExpense, error) {
	if store == nil {
		return nil, ErrMissingCommandStore
	}
	if idGenerator == nil {
		return nil, ErrMissingIDGenerator
	}
	if clock == nil {
		return nil, ErrMissingClock
	}
	return &RecordExpense{store: store, idGenerator: idGenerator, clock: clock}, nil
}

// Execute validates command semantics before consuming generated values and
// delegates the entire atomic write to the command store.
func (useCase *RecordExpense) Execute(ctx context.Context, input RecordExpenseInput) (RecordExpenseResult, error) {
	if err := ctx.Err(); err != nil {
		return RecordExpenseResult{}, err
	}
	if err := ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return RecordExpenseResult{}, err
	}

	details, err := normalizeExpenseInput(input.Expense)
	if err != nil {
		return RecordExpenseResult{}, err
	}
	fingerprint := fingerprintExpense(details)

	id, err := useCase.idGenerator.NewExpenseID()
	if err != nil {
		return RecordExpenseResult{}, newSafeOperationError(ErrExpenseIDGeneration, err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID:        id,
		Details:   details,
		CreatedAt: canonicalizeFinancialInstant(useCase.clock.Now()),
	})
	if err != nil {
		return RecordExpenseResult{}, err
	}

	stored, err := useCase.store.Record(ctx, IdempotentExpenseCommand{
		Operation:      IdempotencyOperationCreateExpense,
		IdempotencyKey: input.IdempotencyKey,
		Fingerprint:    fingerprint,
		Expense:        expense,
	})
	if err != nil {
		if errors.Is(err, ErrIdempotencyConflict) {
			return RecordExpenseResult{}, ErrIdempotencyConflict
		}
		return RecordExpenseResult{}, newSafeOperationError(ErrExpenseCommandPersistence, err)
	}

	return RecordExpenseResult{Expense: stored.Expense, Replayed: stored.Replayed}, nil
}

// ValidateIdempotencyKey accepts only visible ASCII characters and a bounded
// byte length so values are safe and predictable across HTTP and PostgreSQL.
func ValidateIdempotencyKey(key string) error {
	if key == "" {
		return ErrIdempotencyKeyRequired
	}
	if !isValidIdempotencyKey(key) {
		return ErrIdempotencyKeyInvalid
	}
	return nil
}

func fingerprintExpense(details domain.ExpenseDetails) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, string(domain.TransactionTypeExpense))
	writeFingerprintString(digest, details.Description)
	writeFingerprintInt64(digest, details.Amount.MinorUnits())
	writeFingerprintString(digest, string(details.Amount.Currency()))
	writeFingerprintString(digest, string(details.PaymentMethod))
	writeFingerprintString(digest, details.OccurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	writeFingerprintString(digest, details.FinancialTimezone)
	writeFingerprintString(digest, string(details.Origin))

	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}
