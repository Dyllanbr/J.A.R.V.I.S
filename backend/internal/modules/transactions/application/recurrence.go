package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingRecurrenceReplayReader = errors.New("recurrence: replay reader is required")
	ErrRecurrenceReplayLookup        = errors.New("recurrence: replay lookup failed")
)

// RecurrenceReplayQuery identifies a completed idempotent recurrence command.
// It is a read-only fast path; command stores must still perform the same
// check atomically while persisting a new command.
type RecurrenceReplayQuery struct {
	UserID         string
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
}

type RecurrenceReplayLookup struct {
	Recurrence domain.Recurrence
	Found      bool
}

// RecurrenceReplayReader reads already completed recurrence commands without
// generating command data. A mismatched fingerprint is reported as
// ErrRecurrenceIdempotencyConflict.
type RecurrenceReplayReader interface {
	FindRecurrenceReplay(context.Context, RecurrenceReplayQuery) (RecurrenceReplayLookup, error)
}

// RecurrenceInput is the semantic input shared by preview and confirmed
// recording. It intentionally has no transaction occurrence, payment method,
// category, timezone or origin.
type RecurrenceInput struct {
	UserID          string
	TransactionType domain.TransactionType
	Description     string
	ExpectedAmount  int64
	Currency        domain.Currency
	Frequency       domain.RecurrenceFrequency
	StartsOn        domain.CivilDate
}

func normalizeRecurrenceInput(ctx context.Context, input RecurrenceInput) (domain.RecurrenceDetails, error) {
	if err := ctx.Err(); err != nil {
		return domain.RecurrenceDetails{}, err
	}
	amount, err := domain.NewMoney(input.ExpectedAmount, input.Currency)
	if err != nil {
		return domain.RecurrenceDetails{}, err
	}
	return domain.NormalizeRecurrenceDetails(domain.RecurrenceDetails{
		UserID:          input.UserID,
		TransactionType: input.TransactionType,
		Description:     input.Description,
		ExpectedAmount:  amount,
		Frequency:       input.Frequency,
		StartsOn:        input.StartsOn,
	})
}

func fingerprintRecurrence(details domain.RecurrenceDetails) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, IdempotencyOperationCreateRecurrence)
	writeFingerprintString(digest, string(details.TransactionType))
	writeFingerprintString(digest, details.Description)
	writeFingerprintInt64(digest, details.ExpectedAmount.MinorUnits())
	writeFingerprintString(digest, string(details.ExpectedAmount.Currency()))
	writeFingerprintString(digest, string(details.Frequency))
	writeFingerprintString(digest, details.StartsOn.String())

	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}

func fingerprintRecurrenceCancellation(userID, recurrenceID string) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, IdempotencyOperationCancelRecurrence)
	writeFingerprintString(digest, userID)
	writeFingerprintString(digest, recurrenceID)

	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}

func validateRecurrenceIdempotencyKey(key string) error {
	if key == "" {
		return ErrRecurrenceIdempotencyKeyRequired
	}
	if !isValidIdempotencyKey(key) {
		return ErrRecurrenceIdempotencyKeyInvalid
	}
	return nil
}

func findPersistedRecurrenceReplay(
	ctx context.Context,
	reader RecurrenceReplayReader,
	query RecurrenceReplayQuery,
) (domain.Recurrence, bool, error) {
	lookup, err := reader.FindRecurrenceReplay(ctx, query)
	if err == nil {
		return lookup.Recurrence, lookup.Found, nil
	}
	switch {
	case errors.Is(err, ErrRecurrenceIdempotencyConflict):
		return domain.Recurrence{}, false, ErrRecurrenceIdempotencyConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return domain.Recurrence{}, false, err
	default:
		return domain.Recurrence{}, false, newSafeOperationError(ErrRecurrenceReplayLookup, err)
	}
}
