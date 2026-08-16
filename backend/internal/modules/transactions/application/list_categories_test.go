package application_test

import (
	"context"
	"errors"
	"testing"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestListCategoriesRequiresCatalog(t *testing.T) {
	if _, err := application.NewListCategories(nil); !errors.Is(err, application.ErrMissingCategoryCatalog) {
		t.Fatalf("NewListCategories(nil) error = %v", err)
	}
}

func TestListCategoriesReturnsExpenseThenIncomeWithoutReordering(t *testing.T) {
	expense := mustListCategory(t, "expense.second", domain.TransactionTypeExpense, "Segunda", 20)
	expenseFirst := mustListCategory(t, "expense.first", domain.TransactionTypeExpense, "Primeira", 10)
	income := mustListCategory(t, "income.first", domain.TransactionTypeIncome, "Primeira receita", 10)
	catalog := &listCategoryCatalog{byType: map[domain.TransactionType][]application.CategoryDefinition{
		domain.TransactionTypeExpense: {expenseFirst, expense},
		domain.TransactionTypeIncome:  {income},
	}}
	useCase, err := application.NewListCategories(catalog)
	if err != nil {
		t.Fatalf("NewListCategories() error = %v", err)
	}

	result, err := useCase.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Items) != 3 || result.Items[0].ID().String() != "expense.first" ||
		result.Items[1].ID().String() != "expense.second" || result.Items[2].ID().String() != "income.first" {
		t.Fatalf("items = %#v", result.Items)
	}
	if len(catalog.calls) != 2 || catalog.calls[0] != domain.TransactionTypeExpense || catalog.calls[1] != domain.TransactionTypeIncome {
		t.Fatalf("catalog calls = %v", catalog.calls)
	}
}

func TestListCategoriesReturnsNoPartialResultAndPreservesCancellation(t *testing.T) {
	expense := mustListCategory(t, "expense.food", domain.TransactionTypeExpense, "Alimentação", 10)
	catalog := &listCategoryCatalog{
		byType: map[domain.TransactionType][]application.CategoryDefinition{
			domain.TransactionTypeExpense: {expense},
		},
		incomeErr: errors.New("private catalog failure"),
	}
	useCase, err := application.NewListCategories(catalog)
	if err != nil {
		t.Fatalf("NewListCategories() error = %v", err)
	}

	result, err := useCase.Execute(context.Background())
	if !errors.Is(err, application.ErrListCategories) || len(result.Items) != 0 || err.Error() != application.ErrListCategories.Error() {
		t.Fatalf("partial/failure result = %#v, error = %v", result, err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Execute() error = %v", err)
	}
}

type listCategoryCatalog struct {
	byType    map[domain.TransactionType][]application.CategoryDefinition
	incomeErr error
	calls     []domain.TransactionType
}

func (catalog *listCategoryCatalog) FindCategory(context.Context, domain.CategoryID) (application.CategoryDefinition, error) {
	return application.CategoryDefinition{}, application.ErrCategoryNotFound
}

func (catalog *listCategoryCatalog) ListCategories(
	_ context.Context,
	transactionType domain.TransactionType,
) ([]application.CategoryDefinition, error) {
	catalog.calls = append(catalog.calls, transactionType)
	if transactionType == domain.TransactionTypeIncome && catalog.incomeErr != nil {
		return nil, catalog.incomeErr
	}
	return catalog.byType[transactionType], nil
}

func mustListCategory(
	t *testing.T,
	id string,
	transactionType domain.TransactionType,
	displayName string,
	sortOrder uint16,
) application.CategoryDefinition {
	t.Helper()
	categoryID, err := domain.NewCategoryID(id)
	if err != nil {
		t.Fatalf("NewCategoryID(%q) error = %v", id, err)
	}
	definition, err := application.NewCategoryDefinition(categoryID, transactionType, displayName, sortOrder)
	if err != nil {
		t.Fatalf("NewCategoryDefinition(%q) error = %v", id, err)
	}
	return definition
}
