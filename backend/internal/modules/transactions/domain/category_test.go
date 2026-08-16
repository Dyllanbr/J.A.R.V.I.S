package domain_test

import (
	"errors"
	"strings"
	"testing"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestApprovedSystemCategoryIDsAreValid(t *testing.T) {
	t.Parallel()

	approved := []domain.CategoryID{
		domain.CategoryIDExpenseFood,
		domain.CategoryIDExpenseTransport,
		domain.CategoryIDExpenseHousing,
		domain.CategoryIDExpenseHealth,
		domain.CategoryIDExpenseLeisure,
		domain.CategoryIDExpenseEducation,
		domain.CategoryIDExpenseSubscriptions,
		domain.CategoryIDExpenseShopping,
		domain.CategoryIDExpenseTaxesFees,
		domain.CategoryIDExpenseOther,
		domain.CategoryIDIncomeSalary,
		domain.CategoryIDIncomeFreelance,
		domain.CategoryIDIncomeRefund,
		domain.CategoryIDIncomeSale,
		domain.CategoryIDIncomeInvestmentReturn,
		domain.CategoryIDIncomeBenefits,
		domain.CategoryIDIncomeOther,
	}

	for _, categoryID := range approved {
		categoryID := categoryID
		t.Run(categoryID.String(), func(t *testing.T) {
			t.Parallel()
			got, err := domain.NewCategoryID(categoryID.String())
			if err != nil {
				t.Fatalf("NewCategoryID() error = %v", err)
			}
			if got != categoryID {
				t.Fatalf("NewCategoryID() = %q, want %q", got, categoryID)
			}
		})
	}
}

func TestTechnicallyValidCategoryIDDoesNotImplyCatalogMembership(t *testing.T) {
	t.Parallel()

	const value = "expense.food2"
	categoryID, err := domain.NewCategoryID(value)
	if err != nil {
		t.Fatalf("NewCategoryID(%q) error = %v", value, err)
	}
	if categoryID.String() != value {
		t.Fatalf("NewCategoryID(%q) = %q", value, categoryID)
	}
}

func TestCategoryIDRejectsNonCanonicalTechnicalValues(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"Expense.food",
		"expense.FOOD",
		" expense.food",
		"expense.food ",
		"expense. saúde",
		"expense.saúde",
		"expense.\nfood",
		"expense-food",
		"expense..food",
		"expense._food",
		"expense.food_",
		"1expense.food",
		strings.Repeat("a", domain.MaxCategoryIDBytes+1),
	}

	for _, value := range invalid {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if _, err := domain.NewCategoryID(value); !errors.Is(err, domain.ErrInvalidCategoryID) {
				t.Fatalf("NewCategoryID(%q) error = %v, want ErrInvalidCategoryID", value, err)
			}
		})
	}
}

func TestExpenseCategoryIsOptionalAndImmutable(t *testing.T) {
	params := validExpenseParams(t)
	uncategorized, err := domain.NewExpense(params)
	if err != nil {
		t.Fatalf("NewExpense(uncategorized) error = %v", err)
	}
	if _, present := uncategorized.CategoryID(); present {
		t.Fatal("uncategorized Expense unexpectedly contains a CategoryID")
	}

	categoryID := domain.CategoryIDExpenseFood
	params.Details.CategoryID = &categoryID
	categorized, err := domain.NewExpense(params)
	if err != nil {
		t.Fatalf("NewExpense(categorized) error = %v", err)
	}
	categoryID = domain.CategoryIDExpenseOther
	got, present := categorized.CategoryID()
	if !present || got != domain.CategoryIDExpenseFood {
		t.Fatalf("CategoryID() = %q/%t, want expense.food/true", got, present)
	}

	invalid := domain.CategoryID("")
	params.Details.CategoryID = &invalid
	if _, err := domain.NewExpense(params); !errors.Is(err, domain.ErrInvalidCategoryID) {
		t.Fatalf("NewExpense(invalid category) error = %v, want ErrInvalidCategoryID", err)
	}
}

func TestIncomeCategoryIsOptionalAndDoesNotAddPaymentSemantics(t *testing.T) {
	params := validIncomeParams(t)
	uncategorized, err := domain.NewIncome(params)
	if err != nil {
		t.Fatalf("NewIncome(uncategorized) error = %v", err)
	}
	if _, present := uncategorized.CategoryID(); present {
		t.Fatal("uncategorized Income unexpectedly contains a CategoryID")
	}

	categoryID := domain.CategoryIDIncomeSalary
	params.Details.CategoryID = &categoryID
	categorized, err := domain.NewIncome(params)
	if err != nil {
		t.Fatalf("NewIncome(categorized) error = %v", err)
	}
	categoryID = domain.CategoryIDIncomeOther
	got, present := categorized.CategoryID()
	if !present || got != domain.CategoryIDIncomeSalary {
		t.Fatalf("CategoryID() = %q/%t, want income.salary/true", got, present)
	}

	invalid := domain.CategoryID("INCOME.SALARY")
	params.Details.CategoryID = &invalid
	if _, err := domain.NewIncome(params); !errors.Is(err, domain.ErrInvalidCategoryID) {
		t.Fatalf("NewIncome(invalid category) error = %v, want ErrInvalidCategoryID", err)
	}
}
