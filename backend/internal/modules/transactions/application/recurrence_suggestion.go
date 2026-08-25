package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const (
	recurrenceSuggestionIDPrefix         = "rsg_"
	DismissRecurrenceSuggestionOperation = "DISMISS_RECURRENCE_SUGGESTION"
)

var (
	ErrInvalidRecurrenceSuggestionID        = errors.New("recurrence suggestion: invalid id")
	ErrRecurrenceSuggestionNotFound         = errors.New("recurrence suggestion: not found or stale")
	ErrRecurrenceSuggestionSuppressed       = errors.New("recurrence suggestion: suppressed")
	ErrRecurrenceSuggestionExpenseQuery     = errors.New("list recurrence suggestions: expense query failed")
	ErrRecurrenceSuggestionRecurrenceQuery  = errors.New("list recurrence suggestions: recurrence query failed")
	ErrRecurrenceSuggestionSuppressionQuery = errors.New("recurrence suggestion: suppression query failed")
	ErrRecurrenceSuggestionSuppressionWrite = errors.New("dismiss recurrence suggestion: persistence failed")
	ErrRecurrenceSuggestionOwnerIsolation   = errors.New("recurrence suggestion: owner isolation failed")
	ErrInvalidRecurrenceSuggestionClock     = errors.New("recurrence suggestion: invalid clock value")
)

type RecurrenceSuggestionID string

func ValidateRecurrenceSuggestionID(value RecurrenceSuggestionID) error {
	raw := string(value)
	if len(raw) != len(recurrenceSuggestionIDPrefix)+sha256.Size*2 || raw[:len(recurrenceSuggestionIDPrefix)] != recurrenceSuggestionIDPrefix {
		return ErrInvalidRecurrenceSuggestionID
	}
	for _, character := range raw[len(recurrenceSuggestionIDPrefix):] {
		isDigit := character >= '0' && character <= '9'
		isLowerHex := character >= 'a' && character <= 'f'
		if !isDigit && !isLowerHex {
			return ErrInvalidRecurrenceSuggestionID
		}
	}
	return nil
}

type RecurrenceSuggestionFingerprint [sha256.Size]byte

// RecurrenceSuggestion pairs a pure domain inference with its owner-scoped,
// opaque identity. The owner itself is never encoded directly in the ID.
type RecurrenceSuggestion struct {
	ID          RecurrenceSuggestionID
	Fingerprint RecurrenceSuggestionFingerprint
	Suggestion  domain.RecurrenceSuggestion
}

type RecurrenceSuggestionExpenseQuery struct {
	UserID            string
	Start             time.Time
	End               time.Time
	FinancialTimezone string
}

// RecurrenceSuggestionExpenseReader returns immutable occurred expenses in
// the requested owner-scoped observation window.
type RecurrenceSuggestionExpenseReader interface {
	ListRecurrenceSuggestionExpenses(context.Context, RecurrenceSuggestionExpenseQuery) ([]domain.Expense, error)
}

// RecurrenceSuggestionSuppressionReader is the owner-scoped read side of the
// future suppression persistence. Returned IDs must be a subset of the query.
type RecurrenceSuggestionSuppressionReader interface {
	ListDismissedRecurrenceSuggestionIDs(context.Context, string, []RecurrenceSuggestionID) ([]RecurrenceSuggestionID, error)
}

type DismissRecurrenceSuggestionCommand struct {
	Operation    string
	UserID       string
	SuggestionID RecurrenceSuggestionID
	Fingerprint  RecurrenceSuggestionFingerprint
	DismissedAt  time.Time
}

type DismissRecurrenceSuggestionStoreResult struct {
	Replayed bool
}

// RecurrenceSuggestionSuppressionStore is the atomic write authority. Its
// Stage 2 adapter must resolve concurrent repeats by owner and suggestion ID.
type RecurrenceSuggestionSuppressionStore interface {
	DismissRecurrenceSuggestion(context.Context, DismissRecurrenceSuggestionCommand) (DismissRecurrenceSuggestionStoreResult, error)
}

func deriveRecurrenceSuggestionIdentity(userID string, suggestion domain.RecurrenceSuggestion) (RecurrenceSuggestionID, RecurrenceSuggestionFingerprint) {
	fingerprint := sha256.Sum256(suggestion.Fingerprint().CanonicalBytes())
	return deriveRecurrenceSuggestionID(userID, fingerprint), fingerprint
}

// DeriveRecurrenceSuggestionID gives persistence adapters the application-owned
// identity invariant without duplicating its domain separation or exposing the
// owner in the resulting identifier.
func DeriveRecurrenceSuggestionID(
	userID string,
	fingerprint RecurrenceSuggestionFingerprint,
) (RecurrenceSuggestionID, error) {
	if err := domain.ValidateUserID(userID); err != nil {
		return "", err
	}
	return deriveRecurrenceSuggestionID(userID, fingerprint), nil
}

func deriveRecurrenceSuggestionID(
	userID string,
	fingerprint RecurrenceSuggestionFingerprint,
) RecurrenceSuggestionID {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, "RECURRENCE_SUGGESTION_ID")
	writeFingerprintString(digest, userID)
	writeFingerprintString(digest, string(fingerprint[:]))
	return RecurrenceSuggestionID(recurrenceSuggestionIDPrefix + hex.EncodeToString(digest.Sum(nil)))
}

func preserveRecurrenceSuggestionDependencyError(category, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return newSafeOperationError(category, err)
}
