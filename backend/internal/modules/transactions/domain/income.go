package domain

import (
	"errors"
	"time"
)

const (
	// MaxIncomeDescriptionRunes matches the financial description policy shared
	// with Expense without making the two aggregates share write semantics.
	MaxIncomeDescriptionRunes = maxFinancialDescriptionRunes
	initialIncomeVersion      = uint64(1)
)

var (
	ErrInvalidIncomeID                = errors.New("income: invalid id")
	ErrInvalidIncomeUserID            = errors.New("income: invalid user id")
	ErrInvalidIncomeDescription       = errors.New("income: invalid description")
	ErrInvalidIncomeAmount            = errors.New("income: amount must be positive")
	ErrInvalidIncomeOccurredAt        = errors.New("income: invalid occurrence time")
	ErrInvalidIncomeFinancialTimezone = errors.New("income: invalid financial timezone")
	ErrInvalidIncomeOrigin            = errors.New("income: invalid origin")
	ErrInvalidIncomeCreatedAt         = errors.New("income: invalid creation time")
)

// IncomeStatus identifies the lifecycle state of an income.
type IncomeStatus string

const (
	// IncomeStatusRecorded is the initial state because Income is created only
	// after the calling channel obtains explicit user confirmation.
	IncomeStatusRecorded IncomeStatus = "RECORDED"
)

// IncomeDetails contains channel-supplied data validated before application
// dependencies generate an ID and timestamps. Income intentionally has no
// payment method.
type IncomeDetails struct {
	UserID            string
	Description       string
	Amount            Money
	OccurredAt        time.Time
	FinancialTimezone string
	Origin            Origin
}

// IncomeParams carries channel details and generated data required to create
// an Income.
type IncomeParams struct {
	ID        string
	Details   IncomeDetails
	CreatedAt time.Time
}

// Income is an immutable financial transaction with income semantics.
type Income struct {
	id                string
	userID            string
	transactionType   TransactionType
	description       string
	amount            Money
	occurredAt        time.Time
	financialTimezone string
	origin            Origin
	status            IncomeStatus
	version           uint64
	createdAt         time.Time
	updatedAt         time.Time
}

// ValidateIncomeDetails applies the domain rules for channel-supplied data.
func ValidateIncomeDetails(details IncomeDetails) error {
	_, err := NormalizeIncomeDetails(details)
	return err
}

// NormalizeIncomeDetails returns the canonical data used by preview and
// recording without generating an ID or timestamps.
func NormalizeIncomeDetails(details IncomeDetails) (IncomeDetails, error) {
	if !isValidIdentifier(details.UserID) {
		return IncomeDetails{}, ErrInvalidIncomeUserID
	}

	description, valid := normalizeFinancialDescription(details.Description)
	if !valid {
		return IncomeDetails{}, ErrInvalidIncomeDescription
	}
	if details.Amount.MinorUnits() <= 0 {
		return IncomeDetails{}, ErrInvalidIncomeAmount
	}
	if details.OccurredAt.IsZero() {
		return IncomeDetails{}, ErrInvalidIncomeOccurredAt
	}
	if !isValidTimezone(details.FinancialTimezone) {
		return IncomeDetails{}, ErrInvalidIncomeFinancialTimezone
	}
	if !details.Origin.valid() {
		return IncomeDetails{}, ErrInvalidIncomeOrigin
	}

	details.Description = description
	details.OccurredAt = normalizeInstant(details.OccurredAt)
	return details, nil
}

// NewIncome validates all invariants and creates an income in RECORDED state.
func NewIncome(params IncomeParams) (Income, error) {
	if !isValidIdentifier(params.ID) {
		return Income{}, ErrInvalidIncomeID
	}

	details, err := NormalizeIncomeDetails(params.Details)
	if err != nil {
		return Income{}, err
	}
	if params.CreatedAt.IsZero() {
		return Income{}, ErrInvalidIncomeCreatedAt
	}

	createdAt := normalizeInstant(params.CreatedAt)
	return Income{
		id:                params.ID,
		userID:            details.UserID,
		transactionType:   TransactionTypeIncome,
		description:       details.Description,
		amount:            details.Amount,
		occurredAt:        details.OccurredAt,
		financialTimezone: details.FinancialTimezone,
		origin:            details.Origin,
		status:            IncomeStatusRecorded,
		version:           initialIncomeVersion,
		createdAt:         createdAt,
		updatedAt:         createdAt,
	}, nil
}

func (income Income) ID() string                { return income.id }
func (income Income) UserID() string            { return income.userID }
func (income Income) Type() TransactionType     { return income.transactionType }
func (income Income) Description() string       { return income.description }
func (income Income) Amount() Money             { return income.amount }
func (income Income) OccurredAt() time.Time     { return income.occurredAt }
func (income Income) FinancialTimezone() string { return income.financialTimezone }
func (income Income) Origin() Origin            { return income.origin }
func (income Income) Status() IncomeStatus      { return income.status }
func (income Income) Version() uint64           { return income.version }
func (income Income) CreatedAt() time.Time      { return income.createdAt }
func (income Income) UpdatedAt() time.Time      { return income.updatedAt }
