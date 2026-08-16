package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var ErrListCategories = errors.New("list categories: query failed")

// ListCategoriesResult contains the complete system catalog in presentation
// order: Expense categories first, followed by Income categories.
type ListCategoriesResult struct {
	Items []CategoryDefinition
}

// ListCategories exposes the read-only catalog without coupling HTTP to its
// PostgreSQL implementation.
type ListCategories struct {
	catalog CategoryCatalog
}

// NewListCategories composes the catalog discovery query.
func NewListCategories(catalog CategoryCatalog) (*ListCategories, error) {
	if catalog == nil {
		return nil, ErrMissingCategoryCatalog
	}
	return &ListCategories{catalog: catalog}, nil
}

// Execute returns the complete catalog or no items at all when either query
// fails. The catalog adapter owns ordering within each transaction type.
func (useCase *ListCategories) Execute(ctx context.Context) (ListCategoriesResult, error) {
	if err := ctx.Err(); err != nil {
		return ListCategoriesResult{}, err
	}

	expense, err := useCase.catalog.ListCategories(ctx, domain.TransactionTypeExpense)
	if err != nil {
		return ListCategoriesResult{}, listCategoriesError(err)
	}
	income, err := useCase.catalog.ListCategories(ctx, domain.TransactionTypeIncome)
	if err != nil {
		return ListCategoriesResult{}, listCategoriesError(err)
	}

	items := make([]CategoryDefinition, 0, len(expense)+len(income))
	items = append(items, expense...)
	items = append(items, income...)
	return ListCategoriesResult{Items: items}, nil
}

func listCategoriesError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return newSafeOperationError(ErrListCategories, err)
}
