package randomid_test

import (
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/randomid"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestGeneratorCreatesDomainCompatibleDistinctIDs(t *testing.T) {
	generator := randomid.Generator{}
	first, err := generator.NewExpenseID()
	if err != nil {
		t.Fatalf("first NewExpenseID() error = %v", err)
	}
	second, err := generator.NewExpenseID()
	if err != nil {
		t.Fatalf("second NewExpenseID() error = %v", err)
	}
	if first == second || len(first) != len("exp_")+32 {
		t.Fatal("generator did not create distinct 128-bit opaque IDs")
	}

	amount, _ := domain.NewMoney(1, domain.CurrencyBRL)
	if _, err := domain.NewExpense(domain.ExpenseParams{
		ID: first,
		Details: domain.ExpenseDetails{
			UserID:            "user-synthetic-001",
			Description:       "Synthetic",
			Amount:            amount,
			PaymentMethod:     domain.PaymentMethodCash,
			OccurredAt:        time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC),
			FinancialTimezone: "America/Sao_Paulo",
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.August, 14, 13, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("generated ID is not domain compatible: %v", err)
	}
}

func TestGeneratorCreatesDomainCompatibleDistinctIncomeIDs(t *testing.T) {
	generator := randomid.Generator{}
	first, err := generator.NewIncomeID()
	if err != nil {
		t.Fatalf("first NewIncomeID() error = %v", err)
	}
	second, err := generator.NewIncomeID()
	if err != nil {
		t.Fatalf("second NewIncomeID() error = %v", err)
	}
	if first == second || len(first) != len("inc_")+32 {
		t.Fatal("generator did not create distinct 128-bit opaque Income IDs")
	}

	amount, _ := domain.NewMoney(1, domain.CurrencyBRL)
	if _, err := domain.NewIncome(domain.IncomeParams{
		ID: first,
		Details: domain.IncomeDetails{
			UserID:            "user-synthetic-001",
			Description:       "Synthetic income",
			Amount:            amount,
			OccurredAt:        time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC),
			FinancialTimezone: "America/Sao_Paulo",
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.August, 14, 13, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("generated Income ID is not domain compatible: %v", err)
	}
}

func TestGeneratorCreatesDomainCompatibleDistinctRecurrenceIDs(t *testing.T) {
	generator := randomid.Generator{}
	first, err := generator.NewRecurrenceID()
	if err != nil {
		t.Fatalf("first NewRecurrenceID() error = %v", err)
	}
	second, err := generator.NewRecurrenceID()
	if err != nil {
		t.Fatalf("second NewRecurrenceID() error = %v", err)
	}
	if first == second || len(first) != len("rec_")+32 {
		t.Fatal("generator did not create distinct 128-bit opaque Recurrence IDs")
	}
	if err := domain.ValidateRecurrenceID(first); err != nil {
		t.Fatalf("generated Recurrence ID is not domain compatible: %v", err)
	}
}

func TestCreditCardGeneratorCreatesDomainCompatibleDistinctIDs(t *testing.T) {
	generator := randomid.NewCreditCardGenerator()
	first, err := generator.NewCreditCardID()
	if err != nil {
		t.Fatalf("first NewCreditCardID() error = %v", err)
	}
	second, err := generator.NewCreditCardID()
	if err != nil {
		t.Fatalf("second NewCreditCardID() error = %v", err)
	}
	if first == second || len(first) != len("card_")+32 {
		t.Fatal("generator did not create distinct 128-bit opaque CreditCard IDs")
	}
	if err := domain.ValidateCreditCardID(first); err != nil {
		t.Fatalf("generated CreditCard ID is not domain compatible: %v", err)
	}
}
