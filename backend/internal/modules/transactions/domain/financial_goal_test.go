package domain_test

import (
	"errors"
	"strings"
	"testing"

	"jarvis/backend/internal/modules/transactions/domain"
)

const financialGoalID = "goal_00000000000000000000000000000001"
const protectedValueID = "protected_00000000000000000000000000000001"

func TestNewFinancialGoalValidatesImmutableOwnerScopedDeclaration(t *testing.T) {
	amount, err := domain.NewMoney(25_000, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	goal, err := domain.NewFinancialGoal(domain.FinancialGoalParams{ID: financialGoalID, OwnerID: "owner-1", Title: "Viagem", TargetAmount: amount})
	if err != nil {
		t.Fatal(err)
	}
	if goal.ID() != financialGoalID || goal.OwnerID() != "owner-1" || goal.Title() != "Viagem" || goal.TargetAmount().MinorUnits() != 25_000 {
		t.Fatalf("goal = %#v", goal)
	}
	if !goal.Equal(goal) {
		t.Fatal("goal should equal itself")
	}
}

func TestNewFinancialGoalRejectsInvalidFields(t *testing.T) {
	amount, _ := domain.NewMoney(1, domain.CurrencyBRL)
	cases := []struct {
		name   string
		params domain.FinancialGoalParams
		want   error
	}{
		{"id", domain.FinancialGoalParams{OwnerID: "owner-1", Title: "x", TargetAmount: amount}, domain.ErrInvalidFinancialGoalID},
		{"owner", domain.FinancialGoalParams{ID: financialGoalID, Title: "x", TargetAmount: amount}, domain.ErrInvalidFinancialGoalOwnerID},
		{"title", domain.FinancialGoalParams{ID: financialGoalID, OwnerID: "owner-1", Title: " \t ", TargetAmount: amount}, domain.ErrInvalidFinancialGoalTitle},
		{"control", domain.FinancialGoalParams{ID: financialGoalID, OwnerID: "owner-1", Title: "x\u0001", TargetAmount: amount}, domain.ErrInvalidFinancialGoalTitle},
		{"zero", domain.FinancialGoalParams{ID: financialGoalID, OwnerID: "owner-1", Title: "x", TargetAmount: goalMoney(t, 0)}, domain.ErrInvalidFinancialGoalTarget},
		{"negative", domain.FinancialGoalParams{ID: financialGoalID, OwnerID: "owner-1", Title: "x", TargetAmount: goalMoney(t, -1)}, domain.ErrInvalidFinancialGoalTarget},
		{"currency", domain.FinancialGoalParams{ID: financialGoalID, OwnerID: "owner-1", Title: "x", TargetAmount: domain.Money{}}, domain.ErrInvalidFinancialGoalTarget},
		{"long", domain.FinancialGoalParams{ID: financialGoalID, OwnerID: "owner-1", Title: strings.Repeat("a", domain.MaxFinancialGoalTitleRunes+1), TargetAmount: amount}, domain.ErrInvalidFinancialGoalTitle},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := domain.NewFinancialGoal(testCase.params); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestNewProtectedValueAllowsZeroAndRejectsNegativeOrInvalidData(t *testing.T) {
	zero := money(t, 0)
	value, err := domain.NewProtectedValue(domain.ProtectedValueParams{ID: protectedValueID, OwnerID: "owner-1", Label: "Reserva", Amount: zero})
	if err != nil {
		t.Fatal(err)
	}
	if value.Amount().MinorUnits() != 0 || value.Label() != "Reserva" || value.OwnerID() != "owner-1" {
		t.Fatalf("value = %#v", value)
	}
	invalid := []struct {
		name   string
		params domain.ProtectedValueParams
		want   error
	}{
		{"id", domain.ProtectedValueParams{OwnerID: "owner-1", Label: "x", Amount: zero}, domain.ErrInvalidProtectedValueID},
		{"owner", domain.ProtectedValueParams{ID: protectedValueID, Label: "x", Amount: zero}, domain.ErrInvalidProtectedValueOwnerID},
		{"label", domain.ProtectedValueParams{ID: protectedValueID, OwnerID: "owner-1", Label: " ", Amount: zero}, domain.ErrInvalidProtectedValueLabel},
		{"negative", domain.ProtectedValueParams{ID: protectedValueID, OwnerID: "owner-1", Label: "x", Amount: money(t, -1)}, domain.ErrInvalidProtectedValueAmount},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := domain.NewProtectedValue(testCase.params); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func goalMoney(t *testing.T, minor int64) domain.Money {
	t.Helper()
	value, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
