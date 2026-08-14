package domain

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxExpenseDescriptionRunes limits descriptions by Unicode characters,
	// not encoded bytes, without changing their internal content.
	MaxExpenseDescriptionRunes = 200
	// MaxIdentifierBytes bounds opaque technical identifiers by UTF-8 bytes.
	MaxIdentifierBytes    = 128
	initialExpenseVersion = uint64(1)
)

var (
	ErrInvalidExpenseID         = errors.New("expense: invalid id")
	ErrInvalidUserID            = errors.New("expense: invalid user id")
	ErrInvalidDescription       = errors.New("expense: invalid description")
	ErrInvalidExpenseAmount     = errors.New("expense: amount must be positive")
	ErrInvalidPaymentMethod     = errors.New("expense: invalid payment method")
	ErrInvalidOccurredAt        = errors.New("expense: invalid occurrence time")
	ErrInvalidFinancialTimezone = errors.New("expense: invalid financial timezone")
	ErrInvalidOrigin            = errors.New("expense: invalid origin")
	ErrInvalidCreatedAt         = errors.New("expense: invalid creation time")
)

// TransactionType identifies the financial meaning of a transaction.
type TransactionType string

const TransactionTypeExpense TransactionType = "EXPENSE"

// PaymentMethod identifies how an expense was paid.
type PaymentMethod string

const (
	PaymentMethodPIX    PaymentMethod = "PIX"
	PaymentMethodDebit  PaymentMethod = "DEBIT"
	PaymentMethodCredit PaymentMethod = "CREDIT"
	PaymentMethodCash   PaymentMethod = "CASH"
)

// Origin identifies the channel that confirmed the expense command.
type Origin string

const (
	OriginIOS      Origin = "IOS"
	OriginWhatsApp Origin = "WHATSAPP"
)

// ExpenseStatus identifies the lifecycle state of an expense.
type ExpenseStatus string

const (
	// ExpenseStatusRecorded is the initial state because CreateExpense is only
	// invoked after the calling channel obtains explicit user confirmation.
	ExpenseStatusRecorded ExpenseStatus = "RECORDED"
)

// ExpenseDetails contains the channel-supplied data whose invariants are
// validated before application dependencies generate an ID and timestamps.
type ExpenseDetails struct {
	UserID            string
	Description       string
	Amount            Money
	PaymentMethod     PaymentMethod
	OccurredAt        time.Time
	FinancialTimezone string
	Origin            Origin
}

// ExpenseParams carries channel details and generated data required to create
// an Expense. NewExpense validates both before constructing the entity.
type ExpenseParams struct {
	ID        string
	Details   ExpenseDetails
	CreatedAt time.Time
}

// Expense is an immutable financial transaction with expense semantics.
type Expense struct {
	id                string
	userID            string
	transactionType   TransactionType
	description       string
	amount            Money
	paymentMethod     PaymentMethod
	occurredAt        time.Time
	financialTimezone string
	origin            Origin
	status            ExpenseStatus
	version           uint64
	createdAt         time.Time
	updatedAt         time.Time
}

// ValidateExpenseDetails applies the domain rules for channel-supplied data.
func ValidateExpenseDetails(details ExpenseDetails) error {
	_, err := validateExpenseDetails(details)
	return err
}

func validateExpenseDetails(details ExpenseDetails) (string, error) {
	if !isValidIdentifier(details.UserID) {
		return "", ErrInvalidUserID
	}

	description := strings.TrimSpace(details.Description)
	if description == "" || !utf8.ValidString(description) || utf8.RuneCountInString(description) > MaxExpenseDescriptionRunes {
		return "", ErrInvalidDescription
	}

	if details.Amount.MinorUnits() <= 0 {
		return "", ErrInvalidExpenseAmount
	}
	if !details.PaymentMethod.valid() {
		return "", ErrInvalidPaymentMethod
	}
	if details.OccurredAt.IsZero() {
		return "", ErrInvalidOccurredAt
	}
	if !isValidTimezone(details.FinancialTimezone) {
		return "", ErrInvalidFinancialTimezone
	}
	if !details.Origin.valid() {
		return "", ErrInvalidOrigin
	}

	return description, nil
}

// NewExpense validates all invariants and creates an expense in RECORDED state.
func NewExpense(params ExpenseParams) (Expense, error) {
	if !isValidIdentifier(params.ID) {
		return Expense{}, ErrInvalidExpenseID
	}

	description, err := validateExpenseDetails(params.Details)
	if err != nil {
		return Expense{}, err
	}
	if params.CreatedAt.IsZero() {
		return Expense{}, ErrInvalidCreatedAt
	}

	createdAt := normalizeInstant(params.CreatedAt)
	details := params.Details

	return Expense{
		id:                params.ID,
		userID:            details.UserID,
		transactionType:   TransactionTypeExpense,
		description:       description,
		amount:            details.Amount,
		paymentMethod:     details.PaymentMethod,
		occurredAt:        normalizeInstant(details.OccurredAt),
		financialTimezone: details.FinancialTimezone,
		origin:            details.Origin,
		status:            ExpenseStatusRecorded,
		version:           initialExpenseVersion,
		createdAt:         createdAt,
		updatedAt:         createdAt,
	}, nil
}

func (e Expense) ID() string                   { return e.id }
func (e Expense) UserID() string               { return e.userID }
func (e Expense) Type() TransactionType        { return e.transactionType }
func (e Expense) Description() string          { return e.description }
func (e Expense) Amount() Money                { return e.amount }
func (e Expense) PaymentMethod() PaymentMethod { return e.paymentMethod }
func (e Expense) OccurredAt() time.Time        { return e.occurredAt }
func (e Expense) FinancialTimezone() string    { return e.financialTimezone }
func (e Expense) Origin() Origin               { return e.origin }
func (e Expense) Status() ExpenseStatus        { return e.status }
func (e Expense) Version() uint64              { return e.version }
func (e Expense) CreatedAt() time.Time         { return e.createdAt }
func (e Expense) UpdatedAt() time.Time         { return e.updatedAt }

func isValidIdentifier(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > MaxIdentifierBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func isValidTimezone(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || value == "Local" {
		return false
	}
	_, err := time.LoadLocation(value)
	return err == nil
}

func (method PaymentMethod) valid() bool {
	switch method {
	case PaymentMethodPIX, PaymentMethodDebit, PaymentMethodCredit, PaymentMethodCash:
		return true
	default:
		return false
	}
}

func (origin Origin) valid() bool {
	switch origin {
	case OriginIOS, OriginWhatsApp:
		return true
	default:
		return false
	}
}

func normalizeInstant(value time.Time) time.Time {
	return value.UTC().Round(0)
}
