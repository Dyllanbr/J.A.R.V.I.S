package domain

import (
	"errors"
	"time"
)

const (
	// MaxExpenseDescriptionRunes limits descriptions by Unicode characters,
	// not encoded bytes, without changing their internal content.
	MaxExpenseDescriptionRunes = maxFinancialDescriptionRunes
	initialExpenseVersion      = uint64(1)
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

// PaymentMethod identifies how an expense was paid.
type PaymentMethod string

const (
	PaymentMethodPIX    PaymentMethod = "PIX"
	PaymentMethodDebit  PaymentMethod = "DEBIT"
	PaymentMethodCredit PaymentMethod = "CREDIT"
	PaymentMethodCash   PaymentMethod = "CASH"
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
	CategoryID        *CategoryID
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
	categoryID        *CategoryID
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
	_, err := NormalizeExpenseDetails(details)
	return err
}

// NormalizeExpenseDetails validates channel-supplied data and returns the
// canonical representation used to create an Expense. It does not generate an
// ID or timestamps and is therefore safe to use for a non-persisting preview.
func NormalizeExpenseDetails(details ExpenseDetails) (ExpenseDetails, error) {
	if !isValidIdentifier(details.UserID) {
		return ExpenseDetails{}, ErrInvalidUserID
	}

	description, valid := normalizeFinancialDescription(details.Description)
	if !valid {
		return ExpenseDetails{}, ErrInvalidDescription
	}

	if details.Amount.MinorUnits() <= 0 {
		return ExpenseDetails{}, ErrInvalidExpenseAmount
	}
	if !details.PaymentMethod.valid() {
		return ExpenseDetails{}, ErrInvalidPaymentMethod
	}
	categoryID, err := normalizeOptionalCategoryID(details.CategoryID)
	if err != nil {
		return ExpenseDetails{}, err
	}
	if details.OccurredAt.IsZero() {
		return ExpenseDetails{}, ErrInvalidOccurredAt
	}
	if !isValidTimezone(details.FinancialTimezone) {
		return ExpenseDetails{}, ErrInvalidFinancialTimezone
	}
	if !details.Origin.valid() {
		return ExpenseDetails{}, ErrInvalidOrigin
	}

	details.Description = description
	details.CategoryID = categoryID
	details.OccurredAt = normalizeInstant(details.OccurredAt)
	return details, nil
}

// NewExpense validates all invariants and creates an expense in RECORDED state.
func NewExpense(params ExpenseParams) (Expense, error) {
	if !isValidIdentifier(params.ID) {
		return Expense{}, ErrInvalidExpenseID
	}

	details, err := NormalizeExpenseDetails(params.Details)
	if err != nil {
		return Expense{}, err
	}
	if params.CreatedAt.IsZero() {
		return Expense{}, ErrInvalidCreatedAt
	}

	createdAt := normalizeInstant(params.CreatedAt)
	return Expense{
		id:                params.ID,
		userID:            details.UserID,
		transactionType:   TransactionTypeExpense,
		description:       details.Description,
		amount:            details.Amount,
		paymentMethod:     details.PaymentMethod,
		categoryID:        details.CategoryID,
		occurredAt:        details.OccurredAt,
		financialTimezone: details.FinancialTimezone,
		origin:            details.Origin,
		status:            ExpenseStatusRecorded,
		version:           initialExpenseVersion,
		createdAt:         createdAt,
		updatedAt:         createdAt,
	}, nil
}

// ValidateUserID applies the opaque owner identifier policy without exposing
// the rejected value.
func ValidateUserID(value string) error {
	if !isValidIdentifier(value) {
		return ErrInvalidUserID
	}
	return nil
}

func (e Expense) ID() string                   { return e.id }
func (e Expense) UserID() string               { return e.userID }
func (e Expense) Type() TransactionType        { return e.transactionType }
func (e Expense) Description() string          { return e.description }
func (e Expense) Amount() Money                { return e.amount }
func (e Expense) PaymentMethod() PaymentMethod { return e.paymentMethod }
func (e Expense) CategoryID() (CategoryID, bool) {
	if e.categoryID == nil {
		return "", false
	}
	return *e.categoryID, true
}
func (e Expense) OccurredAt() time.Time     { return e.occurredAt }
func (e Expense) FinancialTimezone() string { return e.financialTimezone }
func (e Expense) Origin() Origin            { return e.origin }
func (e Expense) Status() ExpenseStatus     { return e.status }
func (e Expense) Version() uint64           { return e.version }
func (e Expense) CreatedAt() time.Time      { return e.createdAt }
func (e Expense) UpdatedAt() time.Time      { return e.updatedAt }

func (method PaymentMethod) valid() bool {
	switch method {
	case PaymentMethodPIX, PaymentMethodDebit, PaymentMethodCredit, PaymentMethodCash:
		return true
	default:
		return false
	}
}
