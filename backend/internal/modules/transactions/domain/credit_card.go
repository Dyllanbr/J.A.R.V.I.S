package domain

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	CreditCardIDPrefix     = "card_"
	CreditCardIDHexLength  = 32
	MaxCreditCardNameRunes = 200
)

var (
	ErrInvalidCreditCardID          = errors.New("credit card: invalid id")
	ErrInvalidCreditCardUserID      = errors.New("credit card: invalid user id")
	ErrInvalidCreditCardName        = errors.New("credit card: invalid name")
	ErrInvalidCreditCardLastFour    = errors.New("credit card: invalid last four")
	ErrInvalidCreditCardBrand       = errors.New("credit card: invalid brand")
	ErrInvalidDayOfMonthAnchor      = errors.New("credit card: invalid day-of-month anchor")
	ErrInvalidCreditCardCreditLimit = errors.New("credit card: credit limit must be positive")
	ErrInvalidCreditCardCreatedAt   = errors.New("credit card: invalid creation time")
	ErrInvalidCreditCardArchivedAt  = errors.New("credit card: invalid archive time")
	ErrInvalidCreditCardStatus      = errors.New("credit card: invalid status")
	ErrInvalidCreditCardState       = errors.New("credit card: invalid state")
	ErrCreditCardAlreadyArchived    = errors.New("credit card: already archived")
)

// CreditCardName is the human-facing, whitespace-canonical name of a card.
// It preserves case, punctuation and accents.
type CreditCardName struct {
	value string
}

func NewCreditCardName(value string) (CreditCardName, error) {
	if !utf8.ValidString(value) {
		return CreditCardName{}, ErrInvalidCreditCardName
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return CreditCardName{}, ErrInvalidCreditCardName
		}
	}

	fields := strings.FieldsFunc(norm.NFC.String(value), unicode.IsSpace)
	canonical := strings.Join(fields, " ")
	if canonical == "" || utf8.RuneCountInString(canonical) > MaxCreditCardNameRunes {
		return CreditCardName{}, ErrInvalidCreditCardName
	}
	return CreditCardName{value: canonical}, nil
}

func (name CreditCardName) String() string { return name.value }

// CreditCardLastFour is deliberately limited to the non-sensitive display
// suffix. It can never carry a complete card number.
type CreditCardLastFour struct {
	value string
}

func NewCreditCardLastFour(value string) (CreditCardLastFour, error) {
	if len(value) != 4 {
		return CreditCardLastFour{}, ErrInvalidCreditCardLastFour
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return CreditCardLastFour{}, ErrInvalidCreditCardLastFour
		}
	}
	return CreditCardLastFour{value: value}, nil
}

func (lastFour CreditCardLastFour) String() string { return lastFour.value }

// CreditCardBrand is optional classification metadata and has no effect on
// financial calculations or payment authorization.
type CreditCardBrand string

const (
	CreditCardBrandVisa            CreditCardBrand = "VISA"
	CreditCardBrandMastercard      CreditCardBrand = "MASTERCARD"
	CreditCardBrandElo             CreditCardBrand = "ELO"
	CreditCardBrandAmericanExpress CreditCardBrand = "AMERICAN_EXPRESS"
	CreditCardBrandHipercard       CreditCardBrand = "HIPERCARD"
	CreditCardBrandOther           CreditCardBrand = "OTHER"
)

func ValidateCreditCardBrand(brand CreditCardBrand) error {
	switch brand {
	case CreditCardBrandVisa,
		CreditCardBrandMastercard,
		CreditCardBrandElo,
		CreditCardBrandAmericanExpress,
		CreditCardBrandHipercard,
		CreditCardBrandOther:
		return nil
	default:
		return ErrInvalidCreditCardBrand
	}
}

// DayOfMonthAnchor represents a recurring calendar-day preference without a
// timezone or time of day. Cycle calculation belongs to a later increment.
type DayOfMonthAnchor struct {
	day uint8
}

func NewDayOfMonthAnchor(day int) (DayOfMonthAnchor, error) {
	if day < 1 || day > 31 {
		return DayOfMonthAnchor{}, ErrInvalidDayOfMonthAnchor
	}
	return DayOfMonthAnchor{day: uint8(day)}, nil
}

func (anchor DayOfMonthAnchor) Day() int { return int(anchor.day) }

// NewCreditCardCreditLimit creates optional declared-limit information. It
// does not authorize, reject or reserve any purchase.
func NewCreditCardCreditLimit(minorUnits int64, currency Currency) (Money, error) {
	limit, err := NewMoney(minorUnits, currency)
	if err != nil {
		return Money{}, err
	}
	if limit.MinorUnits() <= 0 {
		return Money{}, ErrInvalidCreditCardCreditLimit
	}
	return limit, nil
}

// CreditCardStatus identifies the terminal lifecycle of an organizational
// card reference.
type CreditCardStatus string

const (
	CreditCardStatusActive   CreditCardStatus = "ACTIVE"
	CreditCardStatusArchived CreditCardStatus = "ARCHIVED"
)

// CreditCardDetails contains immutable owner-provided card metadata. Optional
// fields deliberately contain no payment credential or complete PAN.
type CreditCardDetails struct {
	UserID           string
	Name             CreditCardName
	LastFour         *CreditCardLastFour
	Brand            *CreditCardBrand
	ClosingDayAnchor DayOfMonthAnchor
	DueDayAnchor     DayOfMonthAnchor
	CreditLimit      *Money
}

type CreditCardParams struct {
	ID        string
	Details   CreditCardDetails
	CreatedAt time.Time
}

type CreditCardRehydrationParams struct {
	ID         string
	Details    CreditCardDetails
	Status     CreditCardStatus
	CreatedAt  time.Time
	ArchivedAt *time.Time
}

// CreditCard is an organizational reference used by future deterministic
// calculations. It cannot execute or authorize a payment.
type CreditCard struct {
	id               string
	userID           string
	name             CreditCardName
	lastFour         CreditCardLastFour
	hasLastFour      bool
	brand            CreditCardBrand
	hasBrand         bool
	closingDayAnchor DayOfMonthAnchor
	dueDayAnchor     DayOfMonthAnchor
	creditLimit      Money
	hasCreditLimit   bool
	status           CreditCardStatus
	createdAt        time.Time
	archivedAt       time.Time
}

func NormalizeCreditCardDetails(details CreditCardDetails) (CreditCardDetails, error) {
	if err := ValidateCreditCardUserID(details.UserID); err != nil {
		return CreditCardDetails{}, err
	}
	if details.Name.value == "" {
		return CreditCardDetails{}, ErrInvalidCreditCardName
	}
	canonicalName, err := NewCreditCardName(details.Name.value)
	if err != nil || canonicalName != details.Name {
		return CreditCardDetails{}, ErrInvalidCreditCardName
	}
	if details.LastFour != nil {
		lastFour, err := NewCreditCardLastFour(details.LastFour.value)
		if err != nil || lastFour != *details.LastFour {
			return CreditCardDetails{}, ErrInvalidCreditCardLastFour
		}
		details.LastFour = &lastFour
	}
	if details.Brand != nil {
		if err := ValidateCreditCardBrand(*details.Brand); err != nil {
			return CreditCardDetails{}, err
		}
		brand := *details.Brand
		details.Brand = &brand
	}
	if details.ClosingDayAnchor.day < 1 || details.ClosingDayAnchor.day > 31 {
		return CreditCardDetails{}, ErrInvalidDayOfMonthAnchor
	}
	if details.DueDayAnchor.day < 1 || details.DueDayAnchor.day > 31 {
		return CreditCardDetails{}, ErrInvalidDayOfMonthAnchor
	}
	if details.CreditLimit != nil {
		if details.CreditLimit.Currency() != CurrencyBRL || details.CreditLimit.MinorUnits() <= 0 {
			return CreditCardDetails{}, ErrInvalidCreditCardCreditLimit
		}
		limit := *details.CreditLimit
		details.CreditLimit = &limit
	}
	return details, nil
}

func NewCreditCard(params CreditCardParams) (CreditCard, error) {
	return RehydrateCreditCard(CreditCardRehydrationParams{
		ID:        params.ID,
		Details:   params.Details,
		Status:    CreditCardStatusActive,
		CreatedAt: params.CreatedAt,
	})
}

// RehydrateCreditCard validates the complete persisted state and rejects
// impossible lifecycle combinations instead of repairing them silently.
func RehydrateCreditCard(params CreditCardRehydrationParams) (CreditCard, error) {
	if err := ValidateCreditCardID(params.ID); err != nil {
		return CreditCard{}, err
	}
	details, err := NormalizeCreditCardDetails(params.Details)
	if err != nil {
		return CreditCard{}, err
	}
	if params.CreatedAt.IsZero() {
		return CreditCard{}, ErrInvalidCreditCardCreatedAt
	}
	createdAt := normalizeInstant(params.CreatedAt)

	var archivedAt time.Time
	switch params.Status {
	case CreditCardStatusActive:
		if params.ArchivedAt != nil {
			return CreditCard{}, ErrInvalidCreditCardState
		}
	case CreditCardStatusArchived:
		if params.ArchivedAt == nil || params.ArchivedAt.IsZero() {
			return CreditCard{}, ErrInvalidCreditCardState
		}
		archivedAt = normalizeInstant(*params.ArchivedAt)
		if archivedAt.Before(createdAt) {
			return CreditCard{}, ErrInvalidCreditCardArchivedAt
		}
	default:
		return CreditCard{}, ErrInvalidCreditCardStatus
	}

	card := CreditCard{
		id:               params.ID,
		userID:           details.UserID,
		name:             details.Name,
		closingDayAnchor: details.ClosingDayAnchor,
		dueDayAnchor:     details.DueDayAnchor,
		status:           params.Status,
		createdAt:        createdAt,
		archivedAt:       archivedAt,
	}
	if details.LastFour != nil {
		card.lastFour = *details.LastFour
		card.hasLastFour = true
	}
	if details.Brand != nil {
		card.brand = *details.Brand
		card.hasBrand = true
	}
	if details.CreditLimit != nil {
		card.creditLimit = *details.CreditLimit
		card.hasCreditLimit = true
	}
	return card, nil
}

// Archive applies the only lifecycle transition and returns a new value.
func (card CreditCard) Archive(at time.Time) (CreditCard, error) {
	if !card.structurallyValid() {
		return CreditCard{}, ErrInvalidCreditCardState
	}
	if card.status == CreditCardStatusArchived {
		return CreditCard{}, ErrCreditCardAlreadyArchived
	}
	if at.IsZero() {
		return CreditCard{}, ErrInvalidCreditCardArchivedAt
	}
	at = normalizeInstant(at)
	if at.Before(card.createdAt) {
		return CreditCard{}, ErrInvalidCreditCardArchivedAt
	}
	card.status = CreditCardStatusArchived
	card.archivedAt = at
	return card, nil
}

func (card CreditCard) structurallyValid() bool {
	params := CreditCardRehydrationParams{
		ID:        card.id,
		Details:   card.Details(),
		Status:    card.status,
		CreatedAt: card.createdAt,
	}
	if card.hasArchivedAt() {
		archivedAt := card.archivedAt
		params.ArchivedAt = &archivedAt
	}
	_, err := RehydrateCreditCard(params)
	return err == nil && card.createdAt == normalizeInstant(card.createdAt) &&
		(card.archivedAt.IsZero() || card.archivedAt == normalizeInstant(card.archivedAt))
}

func ValidateCreditCardID(value string) error {
	if len(value) != len(CreditCardIDPrefix)+CreditCardIDHexLength || !strings.HasPrefix(value, CreditCardIDPrefix) {
		return ErrInvalidCreditCardID
	}
	for index := len(CreditCardIDPrefix); index < len(value); index++ {
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return ErrInvalidCreditCardID
		}
	}
	return nil
}

func ValidateCreditCardUserID(value string) error {
	if !isValidIdentifier(value) {
		return ErrInvalidCreditCardUserID
	}
	return nil
}

func (card CreditCard) Details() CreditCardDetails {
	details := CreditCardDetails{
		UserID:           card.userID,
		Name:             card.name,
		ClosingDayAnchor: card.closingDayAnchor,
		DueDayAnchor:     card.dueDayAnchor,
	}
	if card.hasLastFour {
		lastFour := card.lastFour
		details.LastFour = &lastFour
	}
	if card.hasBrand {
		brand := card.brand
		details.Brand = &brand
	}
	if card.hasCreditLimit {
		limit := card.creditLimit
		details.CreditLimit = &limit
	}
	return details
}

func (card CreditCard) ID() string                         { return card.id }
func (card CreditCard) UserID() string                     { return card.userID }
func (card CreditCard) Name() CreditCardName               { return card.name }
func (card CreditCard) ClosingDayAnchor() DayOfMonthAnchor { return card.closingDayAnchor }
func (card CreditCard) DueDayAnchor() DayOfMonthAnchor     { return card.dueDayAnchor }
func (card CreditCard) Status() CreditCardStatus           { return card.status }
func (card CreditCard) CreatedAt() time.Time               { return card.createdAt }

func (card CreditCard) LastFour() (CreditCardLastFour, bool) {
	return card.lastFour, card.hasLastFour
}

func (card CreditCard) Brand() (CreditCardBrand, bool) {
	return card.brand, card.hasBrand
}

func (card CreditCard) CreditLimit() (Money, bool) {
	return card.creditLimit, card.hasCreditLimit
}

func (card CreditCard) ArchivedAt() (time.Time, bool) {
	return card.archivedAt, card.hasArchivedAt()
}

func (card CreditCard) hasArchivedAt() bool { return !card.archivedAt.IsZero() }
