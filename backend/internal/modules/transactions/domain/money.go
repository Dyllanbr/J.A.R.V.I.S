package domain

import "errors"

// Currency identifies the monetary unit carried by Money.
type Currency string

const (
	// CurrencyBRL is the only currency supported in the first increment.
	CurrencyBRL Currency = "BRL"
)

var ErrUnsupportedCurrency = errors.New("money: unsupported currency")

// Money represents an exact BRL amount in integer minor units.
// BRL is the only supported currency, so the zero value is a valid BRL zero.
// Signed amounts are valid Money values; individual domain concepts, such as
// Expense, define their own sign invariants.
type Money struct {
	minorUnits int64
}

// NewMoney creates an exact monetary value in minor units.
func NewMoney(minorUnits int64, currency Currency) (Money, error) {
	if currency != CurrencyBRL {
		return Money{}, ErrUnsupportedCurrency
	}

	return Money{minorUnits: minorUnits}, nil
}

// MinorUnits returns the exact signed amount in the currency's minor unit.
func (m Money) MinorUnits() int64 {
	return m.minorUnits
}

// Currency returns the currency carried by the value.
func (m Money) Currency() Currency {
	return CurrencyBRL
}

// Equal reports monetary equality. Currency is implicit because every Money is BRL.
func (m Money) Equal(other Money) bool {
	return m.minorUnits == other.minorUnits
}
