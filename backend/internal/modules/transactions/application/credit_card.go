package application

import (
	"context"
	"errors"
	"hash"

	"jarvis/backend/internal/modules/transactions/domain"
)

const (
	IdempotencyOperationCreateCreditCard  = "CREATE_CREDIT_CARD"
	IdempotencyOperationArchiveCreditCard = "ARCHIVE_CREDIT_CARD"

	creditCardCreateFingerprintVersion  = "credit-card-create-v1"
	creditCardArchiveFingerprintVersion = "credit-card-archive-v1"
)

var (
	ErrCreditCardIdempotencyKeyRequired = errors.New("credit card: idempotency key is required")
	ErrCreditCardIdempotencyKeyInvalid  = errors.New("credit card: idempotency key is invalid")
	ErrCreditCardIdempotencyConflict    = errors.New("credit card: idempotency key was reused")
	ErrCreditCardNotFound               = errors.New("credit card: not found")
	ErrCreditCardDependencyResult       = errors.New("credit card: invalid dependency result")
	ErrMissingCreditCardReplayReader    = errors.New("credit card: replay reader is required")
	ErrCreditCardReplayLookup           = errors.New("credit card: replay lookup failed")
)

type CreditCardLimitInput struct {
	MinorUnits int64
	Currency   domain.Currency
}

// CreditCardInput carries only reviewable card metadata. It contains no PAN,
// credential, payment token or transaction behavior.
type CreditCardInput struct {
	UserID           string
	Name             string
	LastFour         *string
	Brand            *domain.CreditCardBrand
	ClosingDayAnchor int
	DueDayAnchor     int
	CreditLimit      *CreditCardLimitInput
}

type CreditCardReplayQuery struct {
	UserID         string
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
}

type CreditCardReplayLookup struct {
	CreditCard domain.CreditCard
	Found      bool
}

// CreditCardReplayReader is a committed-state fast path. Command stores must
// repeat the decision atomically and remain authoritative for races.
type CreditCardReplayReader interface {
	FindCreditCardReplay(context.Context, CreditCardReplayQuery) (CreditCardReplayLookup, error)
}

func normalizeCreditCardInput(ctx context.Context, input CreditCardInput) (domain.CreditCardDetails, error) {
	if err := ctx.Err(); err != nil {
		return domain.CreditCardDetails{}, err
	}
	if err := domain.ValidateCreditCardUserID(input.UserID); err != nil {
		return domain.CreditCardDetails{}, err
	}
	name, err := domain.NewCreditCardName(input.Name)
	if err != nil {
		return domain.CreditCardDetails{}, err
	}
	closingDay, err := domain.NewDayOfMonthAnchor(input.ClosingDayAnchor)
	if err != nil {
		return domain.CreditCardDetails{}, err
	}
	dueDay, err := domain.NewDayOfMonthAnchor(input.DueDayAnchor)
	if err != nil {
		return domain.CreditCardDetails{}, err
	}

	details := domain.CreditCardDetails{
		UserID:           input.UserID,
		Name:             name,
		ClosingDayAnchor: closingDay,
		DueDayAnchor:     dueDay,
	}
	if input.LastFour != nil {
		lastFour, err := domain.NewCreditCardLastFour(*input.LastFour)
		if err != nil {
			return domain.CreditCardDetails{}, err
		}
		details.LastFour = &lastFour
	}
	if input.Brand != nil {
		if err := domain.ValidateCreditCardBrand(*input.Brand); err != nil {
			return domain.CreditCardDetails{}, err
		}
		brand := *input.Brand
		details.Brand = &brand
	}
	if input.CreditLimit != nil {
		limit, err := domain.NewCreditCardCreditLimit(input.CreditLimit.MinorUnits, input.CreditLimit.Currency)
		if err != nil {
			return domain.CreditCardDetails{}, err
		}
		details.CreditLimit = &limit
	}
	return domain.NormalizeCreditCardDetails(details)
}

func fingerprintCreditCardCreation(details domain.CreditCardDetails) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, IdempotencyOperationCreateCreditCard)
	writeFingerprintString(digest, creditCardCreateFingerprintVersion)
	writeFingerprintString(digest, details.Name.String())
	writeOptionalFingerprintString(digest, details.LastFour != nil, func() string {
		if details.LastFour == nil {
			return ""
		}
		return details.LastFour.String()
	}())
	writeOptionalFingerprintString(digest, details.Brand != nil, func() string {
		if details.Brand == nil {
			return ""
		}
		return string(*details.Brand)
	}())
	writeFingerprintInt64(digest, int64(details.ClosingDayAnchor.Day()))
	writeFingerprintInt64(digest, int64(details.DueDayAnchor.Day()))
	if details.CreditLimit == nil {
		writeFingerprintString(digest, "absent")
	} else {
		writeFingerprintString(digest, "present")
		writeFingerprintInt64(digest, details.CreditLimit.MinorUnits())
		writeFingerprintString(digest, string(details.CreditLimit.Currency()))
	}

	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}

func fingerprintCreditCardArchive(userID, creditCardID string) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, IdempotencyOperationArchiveCreditCard)
	writeFingerprintString(digest, creditCardArchiveFingerprintVersion)
	writeFingerprintString(digest, userID)
	writeFingerprintString(digest, creditCardID)

	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}

func writeOptionalFingerprintString(digest hash.Hash, present bool, value string) {
	if !present {
		writeFingerprintString(digest, "absent")
		return
	}
	writeFingerprintString(digest, "present")
	writeFingerprintString(digest, value)
}

func validateCreditCardIdempotencyKey(key string) error {
	if key == "" {
		return ErrCreditCardIdempotencyKeyRequired
	}
	if !isValidIdempotencyKey(key) {
		return ErrCreditCardIdempotencyKeyInvalid
	}
	return nil
}

func findPersistedCreditCardReplay(
	ctx context.Context,
	reader CreditCardReplayReader,
	query CreditCardReplayQuery,
) (domain.CreditCard, bool, error) {
	lookup, err := reader.FindCreditCardReplay(ctx, query)
	if err == nil {
		if err := ctx.Err(); err != nil {
			return domain.CreditCard{}, false, err
		}
		return lookup.CreditCard, lookup.Found, nil
	}
	switch {
	case errors.Is(err, ErrCreditCardIdempotencyConflict):
		return domain.CreditCard{}, false, ErrCreditCardIdempotencyConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return domain.CreditCard{}, false, err
	default:
		return domain.CreditCard{}, false, newSafeOperationError(ErrCreditCardReplayLookup, err)
	}
}

func validateCreditCardDependencySnapshot(
	card domain.CreditCard,
	userID string,
	creditCardID string,
	wantedStatus *domain.CreditCardStatus,
) error {
	if card.UserID() != userID || (creditCardID != "" && card.ID() != creditCardID) {
		return ErrCreditCardDependencyResult
	}
	archivedAt, hasArchivedAt := card.ArchivedAt()
	params := domain.CreditCardRehydrationParams{
		ID:        card.ID(),
		Details:   card.Details(),
		Status:    card.Status(),
		CreatedAt: card.CreatedAt(),
	}
	if hasArchivedAt {
		params.ArchivedAt = &archivedAt
	}
	if _, err := domain.RehydrateCreditCard(params); err != nil {
		return newSafeOperationError(ErrCreditCardDependencyResult, err)
	}
	if wantedStatus != nil && card.Status() != *wantedStatus {
		return ErrCreditCardDependencyResult
	}
	return nil
}

func creditCardMatchesDetails(card domain.CreditCard, details domain.CreditCardDetails) bool {
	actual := card.Details()
	if actual.UserID != details.UserID || actual.Name != details.Name ||
		actual.ClosingDayAnchor != details.ClosingDayAnchor || actual.DueDayAnchor != details.DueDayAnchor {
		return false
	}
	if !optionalLastFourEqual(actual.LastFour, details.LastFour) || !optionalBrandEqual(actual.Brand, details.Brand) {
		return false
	}
	return optionalMoneyEqual(actual.CreditLimit, details.CreditLimit)
}

func creditCardsEqual(left, right domain.CreditCard) bool {
	if left.ID() != right.ID() || left.UserID() != right.UserID() || left.Status() != right.Status() ||
		left.CreatedAt() != right.CreatedAt() || !creditCardMatchesDetails(left, right.Details()) {
		return false
	}
	leftArchivedAt, leftArchived := left.ArchivedAt()
	rightArchivedAt, rightArchived := right.ArchivedAt()
	return leftArchived == rightArchived && (!leftArchived || leftArchivedAt == rightArchivedAt)
}

func optionalLastFourEqual(left, right *domain.CreditCardLastFour) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalBrandEqual(left, right *domain.CreditCardBrand) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func optionalMoneyEqual(left, right *domain.Money) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right) && left.Currency() == right.Currency()
}
