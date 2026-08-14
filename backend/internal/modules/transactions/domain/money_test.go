package domain_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestNewMoneyPreservesExactMinorUnits(t *testing.T) {
	tests := []struct {
		name       string
		minorUnits int64
	}{
		{name: "zero", minorUnits: 0},
		{name: "positive BRL amount", minorUnits: 4250},
		{name: "negative generic amount", minorUnits: -1},
		{name: "largest supported amount", minorUnits: math.MaxInt64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			money, err := domain.NewMoney(test.minorUnits, domain.CurrencyBRL)
			if err != nil {
				t.Fatalf("NewMoney() returned an error: %v", err)
			}
			if money.MinorUnits() != test.minorUnits {
				t.Fatalf("MinorUnits() = %d, want %d", money.MinorUnits(), test.minorUnits)
			}
			if money.Currency() != domain.CurrencyBRL {
				t.Fatalf("Currency() = %q, want %q", money.Currency(), domain.CurrencyBRL)
			}
		})
	}
}

func TestNewMoneyRejectsUnsupportedCurrencyWithoutEchoingInput(t *testing.T) {
	const unsupported = domain.Currency("SYNTHETIC-SENSITIVE-CURRENCY")

	_, err := domain.NewMoney(100, unsupported)
	if !errors.Is(err, domain.ErrUnsupportedCurrency) {
		t.Fatalf("NewMoney() error = %v, want ErrUnsupportedCurrency", err)
	}
	if strings.Contains(err.Error(), string(unsupported)) {
		t.Fatal("NewMoney() exposed the raw unsupported currency")
	}
}

func TestMoneyZeroValueIsBRLZero(t *testing.T) {
	var declaredZero domain.Money
	literalZero := domain.Money{}
	constructedZero, err := domain.NewMoney(0, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() returned an error: %v", err)
	}

	zeros := []struct {
		name  string
		money domain.Money
	}{
		{name: "declared zero value", money: declaredZero},
		{name: "literal zero value", money: literalZero},
	}

	for _, test := range zeros {
		t.Run(test.name, func(t *testing.T) {
			if test.money.MinorUnits() != 0 {
				t.Fatalf("MinorUnits() = %d, want 0", test.money.MinorUnits())
			}
			if test.money.Currency() != domain.CurrencyBRL {
				t.Fatalf("Currency() = %q, want %q", test.money.Currency(), domain.CurrencyBRL)
			}
			if !test.money.Equal(constructedZero) {
				t.Fatal("zero value is not equal to NewMoney(0, BRL)")
			}
		})
	}
}

func TestMoneyEqual(t *testing.T) {
	amount, err := domain.NewMoney(4250, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() returned an error: %v", err)
	}
	equalAmount, err := domain.NewMoney(4250, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() returned an error: %v", err)
	}
	differentAmount, err := domain.NewMoney(4251, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() returned an error: %v", err)
	}

	if !amount.Equal(equalAmount) {
		t.Fatal("Equal() = false for identical amounts")
	}
	if amount.Equal(differentAmount) {
		t.Fatal("Equal() = true for different amounts")
	}
}

func FuzzNewMoney(f *testing.F) {
	f.Add(int64(0), "BRL")
	f.Add(int64(4250), "BRL")
	f.Add(int64(math.MaxInt64), "BRL")
	f.Add(int64(math.MinInt64), "BRL")
	f.Add(int64(100), "USD")
	f.Add(int64(100), "")

	f.Fuzz(func(t *testing.T, minorUnits int64, currency string) {
		money, err := domain.NewMoney(minorUnits, domain.Currency(currency))
		if currency != string(domain.CurrencyBRL) {
			if !errors.Is(err, domain.ErrUnsupportedCurrency) {
				t.Fatalf("NewMoney(%d, %q) error = %v, want ErrUnsupportedCurrency", minorUnits, currency, err)
			}
			return
		}

		if err != nil {
			t.Fatalf("NewMoney(%d, BRL) returned an error: %v", minorUnits, err)
		}
		if money.MinorUnits() != minorUnits || money.Currency() != domain.CurrencyBRL {
			t.Fatal("NewMoney() did not preserve the exact amount and currency")
		}
	})
}
