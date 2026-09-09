package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxFinancialGoalTitleRunes = 200

var (
	ErrInvalidFinancialGoalID       = errors.New("financial goal: invalid id")
	ErrInvalidFinancialGoalOwnerID  = errors.New("financial goal: invalid owner id")
	ErrInvalidFinancialGoalTitle    = errors.New("financial goal: invalid title")
	ErrInvalidFinancialGoalTarget   = errors.New("financial goal: invalid target amount")
	ErrInvalidProtectedValueID      = errors.New("protected value: invalid id")
	ErrInvalidProtectedValueOwnerID = errors.New("protected value: invalid owner id")
	ErrInvalidProtectedValueLabel   = errors.New("protected value: invalid label")
	ErrInvalidProtectedValueAmount  = errors.New("protected value: invalid amount")
)

// FinancialGoal is an immutable declaration of a user's objective. It is a
// read model foundation only: it has no progress, payment, alert, rollover or
// persistence behavior in this stage.
type FinancialGoal struct {
	id           string
	ownerID      string
	title        string
	targetAmount Money
}

type FinancialGoalParams struct {
	ID           string
	OwnerID      string
	Title        string
	TargetAmount Money
}

// NewFinancialGoal validates a goal without reading a clock, generating an
// identifier or performing I/O. A target is a positive BRL amount; protected
// values are modeled separately because their integration with Safe Available
// still requires a later, explicit product decision.
func NewFinancialGoal(params FinancialGoalParams) (FinancialGoal, error) {
	if !isValidIdentifier(params.ID) {
		return FinancialGoal{}, ErrInvalidFinancialGoalID
	}
	if err := ValidateUserID(params.OwnerID); err != nil {
		return FinancialGoal{}, ErrInvalidFinancialGoalOwnerID
	}
	title, ok := normalizeFinancialGoalLabel(params.Title)
	if !ok {
		return FinancialGoal{}, ErrInvalidFinancialGoalTitle
	}
	if params.TargetAmount.Currency() != CurrencyBRL || params.TargetAmount.MinorUnits() <= 0 {
		return FinancialGoal{}, ErrInvalidFinancialGoalTarget
	}
	return FinancialGoal{id: params.ID, ownerID: params.OwnerID, title: title, targetAmount: params.TargetAmount}, nil
}

func (goal FinancialGoal) ID() string          { return goal.id }
func (goal FinancialGoal) OwnerID() string     { return goal.ownerID }
func (goal FinancialGoal) Title() string       { return goal.title }
func (goal FinancialGoal) TargetAmount() Money { return goal.targetAmount }

func (goal FinancialGoal) Equal(other FinancialGoal) bool {
	return goal.id == other.id && goal.ownerID == other.ownerID && goal.title == other.title && goal.targetAmount.Equal(other.targetAmount)
}

func (goal FinancialGoal) structurallyValid() bool {
	if !isValidIdentifier(goal.id) || ValidateUserID(goal.ownerID) != nil {
		return false
	}
	if _, ok := normalizeFinancialGoalLabel(goal.title); !ok {
		return false
	}
	return goal.targetAmount.Currency() == CurrencyBRL && goal.targetAmount.MinorUnits() > 0
}

// ProtectedValue is an immutable, explicitly named amount the user wants to
// preserve. It is not yet applied to Safe Available and does not move or
// reserve money; its future financial effect requires an approved formula.
type ProtectedValue struct {
	id      string
	ownerID string
	label   string
	amount  Money
}

type ProtectedValueParams struct {
	ID      string
	OwnerID string
	Label   string
	Amount  Money
}

// NewProtectedValue validates a non-negative BRL amount. Zero is accepted so
// a user can explicitly keep a protection rule at zero without inventing a
// hidden default.
func NewProtectedValue(params ProtectedValueParams) (ProtectedValue, error) {
	if !isValidIdentifier(params.ID) {
		return ProtectedValue{}, ErrInvalidProtectedValueID
	}
	if err := ValidateUserID(params.OwnerID); err != nil {
		return ProtectedValue{}, ErrInvalidProtectedValueOwnerID
	}
	label, ok := normalizeFinancialGoalLabel(params.Label)
	if !ok {
		return ProtectedValue{}, ErrInvalidProtectedValueLabel
	}
	if params.Amount.Currency() != CurrencyBRL || params.Amount.MinorUnits() < 0 {
		return ProtectedValue{}, ErrInvalidProtectedValueAmount
	}
	return ProtectedValue{id: params.ID, ownerID: params.OwnerID, label: label, amount: params.Amount}, nil
}

func (value ProtectedValue) ID() string      { return value.id }
func (value ProtectedValue) OwnerID() string { return value.ownerID }
func (value ProtectedValue) Label() string   { return value.label }
func (value ProtectedValue) Amount() Money   { return value.amount }

func (value ProtectedValue) Equal(other ProtectedValue) bool {
	return value.id == other.id && value.ownerID == other.ownerID && value.label == other.label && value.amount.Equal(other.amount)
}

func (value ProtectedValue) structurallyValid() bool {
	if !isValidIdentifier(value.id) || ValidateUserID(value.ownerID) != nil {
		return false
	}
	if _, ok := normalizeFinancialGoalLabel(value.label); !ok {
		return false
	}
	return value.amount.Currency() == CurrencyBRL && value.amount.MinorUnits() >= 0
}

func normalizeFinancialGoalLabel(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxFinancialGoalTitleRunes {
		return "", false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", false
		}
	}
	return value, true
}
