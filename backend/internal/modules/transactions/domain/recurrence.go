package domain

import (
	"errors"
	"time"
)

const MaxRecurrenceDescriptionRunes = maxFinancialDescriptionRunes

var (
	ErrInvalidRecurrenceID             = errors.New("recurrence: invalid id")
	ErrInvalidRecurrenceUserID         = errors.New("recurrence: invalid user id")
	ErrInvalidRecurrenceType           = errors.New("recurrence: invalid transaction type")
	ErrInvalidRecurrenceDescription    = errors.New("recurrence: invalid description")
	ErrInvalidRecurrenceExpectedAmount = errors.New("recurrence: expected amount must be positive")
	ErrInvalidRecurrenceFrequency      = errors.New("recurrence: invalid frequency")
	ErrInvalidRecurrenceStartsOn       = errors.New("recurrence: invalid start date")
	ErrInvalidRecurrenceCreatedAt      = errors.New("recurrence: invalid creation time")
	ErrInvalidRecurrenceCancelledAt    = errors.New("recurrence: invalid cancellation time")
	ErrInvalidRecurrenceState          = errors.New("recurrence: invalid state")
	ErrRecurrenceAlreadyCancelled      = errors.New("recurrence: already cancelled")
	ErrInvalidRecurrenceMonthOffset    = errors.New("recurrence: invalid month offset")
)

// RecurrenceFrequency identifies the supported calendar cadence.
type RecurrenceFrequency string

const RecurrenceFrequencyMonthly RecurrenceFrequency = "MONTHLY"

// RecurrenceStatus identifies the lifecycle of a confirmed commitment.
type RecurrenceStatus string

const (
	RecurrenceStatusActive    RecurrenceStatus = "ACTIVE"
	RecurrenceStatusCancelled RecurrenceStatus = "CANCELLED"
)

// RecurrenceDetails contains confirmed recurring commitment data. It does not
// describe an occurred transaction and intentionally carries no payment,
// category, occurrence instant, timezone or origin.
type RecurrenceDetails struct {
	UserID          string
	TransactionType TransactionType
	Description     string
	ExpectedAmount  Money
	Frequency       RecurrenceFrequency
	StartsOn        CivilDate
}

// RecurrenceParams combines confirmed details with generated creation data.
type RecurrenceParams struct {
	ID        string
	Details   RecurrenceDetails
	CreatedAt time.Time
}

// Recurrence is a confirmed recurring expense commitment, not a financial
// transaction. Cancellation returns a new value and never creates or mutates
// Expense or Income.
type Recurrence struct {
	id              string
	userID          string
	transactionType TransactionType
	description     string
	expectedAmount  Money
	frequency       RecurrenceFrequency
	startsOn        CivilDate
	status          RecurrenceStatus
	createdAt       time.Time
	cancelledAt     time.Time
}

// NormalizeRecurrenceDetails validates channel-supplied data and returns its
// canonical form without generating an ID, reading a clock or causing I/O.
func NormalizeRecurrenceDetails(details RecurrenceDetails) (RecurrenceDetails, error) {
	if !isValidIdentifier(details.UserID) {
		return RecurrenceDetails{}, ErrInvalidRecurrenceUserID
	}
	if details.TransactionType != TransactionTypeExpense {
		return RecurrenceDetails{}, ErrInvalidRecurrenceType
	}
	description, valid := normalizeFinancialDescription(details.Description)
	if !valid {
		return RecurrenceDetails{}, ErrInvalidRecurrenceDescription
	}
	if details.ExpectedAmount.MinorUnits() <= 0 {
		return RecurrenceDetails{}, ErrInvalidRecurrenceExpectedAmount
	}
	if details.Frequency != RecurrenceFrequencyMonthly {
		return RecurrenceDetails{}, ErrInvalidRecurrenceFrequency
	}
	if !details.StartsOn.valid() {
		return RecurrenceDetails{}, ErrInvalidRecurrenceStartsOn
	}

	details.Description = description
	return details, nil
}

// NewRecurrence creates an ACTIVE recurrence after validating all invariants.
func NewRecurrence(params RecurrenceParams) (Recurrence, error) {
	if err := ValidateRecurrenceID(params.ID); err != nil {
		return Recurrence{}, err
	}
	details, err := NormalizeRecurrenceDetails(params.Details)
	if err != nil {
		return Recurrence{}, err
	}
	if params.CreatedAt.IsZero() {
		return Recurrence{}, ErrInvalidRecurrenceCreatedAt
	}

	return Recurrence{
		id:              params.ID,
		userID:          details.UserID,
		transactionType: details.TransactionType,
		description:     details.Description,
		expectedAmount:  details.ExpectedAmount,
		frequency:       details.Frequency,
		startsOn:        details.StartsOn,
		status:          RecurrenceStatusActive,
		createdAt:       normalizeInstant(params.CreatedAt),
	}, nil
}

// Cancel applies the only supported lifecycle transition and returns a new
// recurrence value. Cancellation is terminal.
func (recurrence Recurrence) Cancel(cancelledAt time.Time) (Recurrence, error) {
	if !recurrence.structurallyValid() {
		return Recurrence{}, ErrInvalidRecurrenceState
	}
	if recurrence.status == RecurrenceStatusCancelled {
		return Recurrence{}, ErrRecurrenceAlreadyCancelled
	}
	if cancelledAt.IsZero() {
		return Recurrence{}, ErrInvalidRecurrenceCancelledAt
	}
	cancelledAt = normalizeInstant(cancelledAt)
	if cancelledAt.Before(recurrence.createdAt) {
		return Recurrence{}, ErrInvalidRecurrenceCancelledAt
	}

	recurrence.status = RecurrenceStatusCancelled
	recurrence.cancelledAt = cancelledAt
	return recurrence, nil
}

// structurallyValid protects lifecycle operations from zero values and
// invalid states. Constructors remain the only public way to create the
// aggregate; adapters can safely rehydrate by creating the ACTIVE value and
// applying its persisted cancellation once.
func (recurrence Recurrence) structurallyValid() bool {
	if ValidateRecurrenceID(recurrence.id) != nil || recurrence.createdAt.IsZero() || recurrence.createdAt != normalizeInstant(recurrence.createdAt) {
		return false
	}
	details, err := NormalizeRecurrenceDetails(RecurrenceDetails{
		UserID:          recurrence.userID,
		TransactionType: recurrence.transactionType,
		Description:     recurrence.description,
		ExpectedAmount:  recurrence.expectedAmount,
		Frequency:       recurrence.frequency,
		StartsOn:        recurrence.startsOn,
	})
	if err != nil || details.Description != recurrence.description {
		return false
	}

	switch recurrence.status {
	case RecurrenceStatusActive:
		return recurrence.cancelledAt.IsZero()
	case RecurrenceStatusCancelled:
		return !recurrence.cancelledAt.IsZero() &&
			recurrence.cancelledAt == normalizeInstant(recurrence.cancelledAt) &&
			!recurrence.cancelledAt.Before(recurrence.createdAt)
	default:
		return false
	}
}

// OccurrenceDate returns the conceptual monthly date at the given offset from
// StartsOn. When the anchor day does not exist, it uses that month's last day.
func (recurrence Recurrence) OccurrenceDate(monthOffset int) (CivilDate, error) {
	if monthOffset < 0 || !recurrence.startsOn.valid() {
		return CivilDate{}, ErrInvalidRecurrenceMonthOffset
	}

	monthIndex := int64(recurrence.startsOn.year-1)*12 + int64(recurrence.startsOn.month-1) + int64(monthOffset)
	if monthIndex < 0 {
		return CivilDate{}, ErrInvalidRecurrenceMonthOffset
	}
	year := monthIndex/12 + 1
	if year > 9999 {
		return CivilDate{}, ErrInvalidRecurrenceMonthOffset
	}
	month := time.Month(monthIndex%12 + 1)
	day := recurrence.startsOn.day
	if lastDay := daysInMonth(int(year), month); day > lastDay {
		day = lastDay
	}
	return NewCivilDate(int(year), month, day)
}

// ValidateRecurrenceID applies the existing opaque identifier policy.
func ValidateRecurrenceID(value string) error {
	if !isValidIdentifier(value) {
		return ErrInvalidRecurrenceID
	}
	return nil
}

// ValidateRecurrenceUserID applies owner validation without exposing input.
func ValidateRecurrenceUserID(value string) error {
	if !isValidIdentifier(value) {
		return ErrInvalidRecurrenceUserID
	}
	return nil
}

func (recurrence Recurrence) ID() string                     { return recurrence.id }
func (recurrence Recurrence) UserID() string                 { return recurrence.userID }
func (recurrence Recurrence) Type() TransactionType          { return recurrence.transactionType }
func (recurrence Recurrence) Description() string            { return recurrence.description }
func (recurrence Recurrence) ExpectedAmount() Money          { return recurrence.expectedAmount }
func (recurrence Recurrence) Frequency() RecurrenceFrequency { return recurrence.frequency }
func (recurrence Recurrence) StartsOn() CivilDate            { return recurrence.startsOn }
func (recurrence Recurrence) Status() RecurrenceStatus       { return recurrence.status }
func (recurrence Recurrence) CreatedAt() time.Time           { return recurrence.createdAt }
func (recurrence Recurrence) CancelledAt() (time.Time, bool) {
	if recurrence.cancelledAt.IsZero() {
		return time.Time{}, false
	}
	return recurrence.cancelledAt, true
}
