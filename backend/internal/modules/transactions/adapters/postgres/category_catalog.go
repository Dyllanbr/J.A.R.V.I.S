package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrInvalidCategoryTransactionType = errors.New("category postgres catalog: invalid transaction type")
	ErrQueryCategory                  = errors.New("category postgres catalog: category query failed")
	ErrQueryCategories                = errors.New("category postgres catalog: categories query failed")
	ErrLoadCategoryDefinition         = errors.New("category postgres catalog: stored category is invalid")
)

var _ application.CategoryCatalog = (*ExpenseRepository)(nil)

// FindCategory resolves one system category without deriving applicability
// from its technical identifier.
func (repository *ExpenseRepository) FindCategory(
	ctx context.Context,
	categoryID domain.CategoryID,
) (application.CategoryDefinition, error) {
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	var (
		storedID, transactionType, displayName string
		sortOrder                              int64
	)
	err := repository.pool.QueryRow(operationContext, `
		SELECT id, transaction_type, display_name_pt_br, sort_order
		FROM categories
		WHERE id = $1
	`, categoryID.String()).Scan(&storedID, &transactionType, &displayName, &sortOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.CategoryDefinition{}, application.ErrCategoryNotFound
	}
	if err != nil {
		return application.CategoryDefinition{}, newRepositoryError(ErrQueryCategory, err)
	}
	definition, err := categoryDefinitionFromDatabase(storedID, transactionType, displayName, sortOrder)
	if err != nil {
		return application.CategoryDefinition{}, newRepositoryError(ErrLoadCategoryDefinition, err)
	}
	return definition, nil
}

// ListCategories returns the system catalog for one financial transaction
// type in stable presentation order.
func (repository *ExpenseRepository) ListCategories(
	ctx context.Context,
	transactionType domain.TransactionType,
) ([]application.CategoryDefinition, error) {
	if transactionType != domain.TransactionTypeExpense && transactionType != domain.TransactionTypeIncome {
		return nil, ErrInvalidCategoryTransactionType
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()

	rows, err := repository.pool.Query(operationContext, `
		SELECT id, transaction_type, display_name_pt_br, sort_order
		FROM categories
		WHERE transaction_type = $1
		ORDER BY sort_order, id
	`, transactionType)
	if err != nil {
		return nil, newRepositoryError(ErrQueryCategories, err)
	}
	defer rows.Close()

	definitions := make([]application.CategoryDefinition, 0)
	for rows.Next() {
		var (
			id, storedType, displayName string
			sortOrder                   int64
		)
		if err := rows.Scan(&id, &storedType, &displayName, &sortOrder); err != nil {
			return nil, newRepositoryError(ErrLoadCategoryDefinition, err)
		}
		definition, err := categoryDefinitionFromDatabase(id, storedType, displayName, sortOrder)
		if err != nil {
			return nil, newRepositoryError(ErrLoadCategoryDefinition, err)
		}
		definitions = append(definitions, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrQueryCategories, err)
	}
	return definitions, nil
}

func categoryDefinitionFromDatabase(
	id string,
	transactionType string,
	displayName string,
	sortOrder int64,
) (application.CategoryDefinition, error) {
	if sortOrder <= 0 || sortOrder > int64(^uint16(0)) {
		return application.CategoryDefinition{}, ErrLoadCategoryDefinition
	}

	categoryID, err := domain.NewCategoryID(id)
	if err != nil {
		return application.CategoryDefinition{}, err
	}
	definition, err := application.NewCategoryDefinition(
		categoryID,
		domain.TransactionType(transactionType),
		displayName,
		uint16(sortOrder),
	)
	if err != nil {
		return application.CategoryDefinition{}, err
	}
	return definition, nil
}
