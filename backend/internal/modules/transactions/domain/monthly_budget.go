package domain

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

var (
	ErrInvalidCivilMonth           = errors.New("monthly budget: invalid civil month")
	ErrInvalidMonthlyBudgetOwnerID = errors.New("monthly budget: invalid owner id")
	ErrInvalidMonthlyBudgetMonth   = errors.New("monthly budget: invalid month")
	ErrInvalidMonthlyBudgetAmount  = errors.New("monthly budget: invalid amount")
	ErrMonthlyBudgetDuplicate      = errors.New("monthly budget: duplicate owner and month")
)

// CivilMonth is a valid Gregorian calendar month with no timezone or implicit
// current-month behavior. Its canonical transport representation is YYYY-MM.
type CivilMonth struct {
	year  int
	month time.Month
}

// NewCivilMonth validates and constructs a civil month.
func NewCivilMonth(year int, month time.Month) (CivilMonth, error) {
	value := CivilMonth{year: year, month: month}
	if !value.valid() {
		return CivilMonth{}, ErrInvalidCivilMonth
	}
	return value, nil
}

// ParseCivilMonth accepts only the exact ASCII representation YYYY-MM.
func ParseCivilMonth(value string) (CivilMonth, error) {
	if len(value) != 7 || value[4] != '-' {
		return CivilMonth{}, ErrInvalidCivilMonth
	}
	for index := 0; index < len(value); index++ {
		if index == 4 {
			continue
		}
		if value[index] < '0' || value[index] > '9' {
			return CivilMonth{}, ErrInvalidCivilMonth
		}
	}
	year, err := strconv.Atoi(value[:4])
	if err != nil {
		return CivilMonth{}, ErrInvalidCivilMonth
	}
	month, err := strconv.Atoi(value[5:])
	if err != nil {
		return CivilMonth{}, ErrInvalidCivilMonth
	}
	return NewCivilMonth(year, time.Month(month))
}

func (month CivilMonth) Year() int         { return month.year }
func (month CivilMonth) Month() time.Month { return month.month }

// String returns the canonical YYYY-MM representation or an empty string for
// an invalid zero value.
func (month CivilMonth) String() string {
	if !month.valid() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", month.year, month.month)
}

func (month CivilMonth) Equal(other CivilMonth) bool { return month == other }

func (month CivilMonth) StartOn() CivilDate {
	date, _ := NewCivilDate(month.year, month.month, 1)
	return date
}

func (month CivilMonth) EndOn() CivilDate {
	date, _ := NewCivilDate(month.year, month.month, daysInMonth(month.year, month.month))
	return date
}

func (month CivilMonth) Contains(date CivilDate) bool {
	return month.valid() && date.valid() && !date.Before(month.StartOn()) && !month.EndOn().Before(date)
}

func (month CivilMonth) valid() bool {
	return month.year >= 1 && month.year <= 9999 && month.month >= time.January && month.month <= time.December
}

// MonthlyBudget is an immutable owner/month budget read model. It is not a
// persisted aggregate and has no category, rollover, payment or alert state.
type MonthlyBudget struct {
	ownerID string
	month   CivilMonth
	amount  Money
}

type MonthlyBudgetParams struct {
	OwnerID string
	Month   CivilMonth
	Amount  Money
}

func NewMonthlyBudget(params MonthlyBudgetParams) (MonthlyBudget, error) {
	if err := ValidateUserID(params.OwnerID); err != nil {
		return MonthlyBudget{}, ErrInvalidMonthlyBudgetOwnerID
	}
	if !params.Month.valid() {
		return MonthlyBudget{}, ErrInvalidMonthlyBudgetMonth
	}
	if params.Amount.Currency() != CurrencyBRL || params.Amount.MinorUnits() < 0 {
		return MonthlyBudget{}, ErrInvalidMonthlyBudgetAmount
	}
	return MonthlyBudget{ownerID: params.OwnerID, month: params.Month, amount: params.Amount}, nil
}

func (budget MonthlyBudget) OwnerID() string   { return budget.ownerID }
func (budget MonthlyBudget) Month() CivilMonth { return budget.month }
func (budget MonthlyBudget) Amount() Money     { return budget.amount }
func (budget MonthlyBudget) Equal(other MonthlyBudget) bool {
	return budget.ownerID == other.ownerID && budget.month.Equal(other.month) && budget.amount.Equal(other.amount)
}

func (budget MonthlyBudget) valid() bool {
	return ValidateUserID(budget.ownerID) == nil && budget.month.valid() && budget.amount.Currency() == CurrencyBRL && budget.amount.MinorUnits() >= 0
}

// ReplaceMonthlyBudget returns a fresh collection in deterministic order,
// replacing an existing value with the same owner/month natural key. It is a
// pure operation; persistence belongs to a later adapter stage.
func ReplaceMonthlyBudget(existing []MonthlyBudget, replacement MonthlyBudget) ([]MonthlyBudget, error) {
	if err := validateMonthlyBudgetValue(replacement); err != nil {
		return nil, err
	}
	result := append([]MonthlyBudget(nil), existing...)
	keyOwner, keyMonth := replacement.ownerID, replacement.month
	seen := make(map[monthlyBudgetKey]struct{}, len(result))
	replaced := false
	for index, budget := range result {
		if err := validateMonthlyBudgetValue(budget); err != nil {
			return nil, err
		}
		key := monthlyBudgetKey{ownerID: budget.ownerID, month: budget.month}
		if _, exists := seen[key]; exists {
			return nil, ErrMonthlyBudgetDuplicate
		}
		seen[key] = struct{}{}
		if budget.ownerID != keyOwner || !budget.month.Equal(keyMonth) {
			continue
		}
		result[index] = replacement
		replaced = true
	}
	if !replaced {
		result = append(result, replacement)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].ownerID != result[right].ownerID {
			return result[left].ownerID < result[right].ownerID
		}
		return result[left].month.String() < result[right].month.String()
	})
	return result, nil
}

type monthlyBudgetKey struct {
	ownerID string
	month   CivilMonth
}

func validateMonthlyBudgetValue(budget MonthlyBudget) error {
	if ValidateUserID(budget.ownerID) != nil {
		return ErrInvalidMonthlyBudgetOwnerID
	}
	if !budget.month.valid() {
		return ErrInvalidMonthlyBudgetMonth
	}
	if budget.amount.Currency() != CurrencyBRL || budget.amount.MinorUnits() < 0 {
		return ErrInvalidMonthlyBudgetAmount
	}
	return nil
}
