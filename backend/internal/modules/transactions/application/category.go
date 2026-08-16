package application

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/domain"
)

const MaxCategoryDisplayNameRunes = 80

var (
	ErrInvalidCategoryDefinition  = errors.New("category: invalid definition")
	ErrCategoryNotFound           = errors.New("category: not found")
	ErrCategoryNotApplicable      = errors.New("category: not applicable")
	ErrCategoryCatalogUnavailable = errors.New("category catalog: unavailable")
	ErrMissingCategoryCatalog     = errors.New("category catalog: required")
)

// CategoryDefinition is an immutable catalog reference. Its localized display
// name is presentation metadata and never identifies a financial category.
type CategoryDefinition struct {
	id              domain.CategoryID
	transactionType domain.TransactionType
	displayName     string
	sortOrder       uint16
}

// NewCategoryDefinition validates trusted catalog data without deriving
// identity or applicability from localized display text.
func NewCategoryDefinition(
	id domain.CategoryID,
	transactionType domain.TransactionType,
	displayName string,
	sortOrder uint16,
) (CategoryDefinition, error) {
	if err := domain.ValidateCategoryID(id); err != nil {
		return CategoryDefinition{}, ErrInvalidCategoryDefinition
	}
	if transactionType != domain.TransactionTypeExpense && transactionType != domain.TransactionTypeIncome {
		return CategoryDefinition{}, ErrInvalidCategoryDefinition
	}
	if !isValidCategoryDisplayName(displayName) || sortOrder == 0 {
		return CategoryDefinition{}, ErrInvalidCategoryDefinition
	}

	return CategoryDefinition{
		id:              id,
		transactionType: transactionType,
		displayName:     displayName,
		sortOrder:       sortOrder,
	}, nil
}

func (definition CategoryDefinition) ID() domain.CategoryID { return definition.id }
func (definition CategoryDefinition) TransactionType() domain.TransactionType {
	return definition.transactionType
}
func (definition CategoryDefinition) DisplayName() string { return definition.displayName }
func (definition CategoryDefinition) SortOrder() uint16   { return definition.sortOrder }

// CategoryCatalog is the read-only system catalog capability used by
// transaction commands and, later, category discovery. It exposes no mutation.
type CategoryCatalog interface {
	FindCategory(context.Context, domain.CategoryID) (CategoryDefinition, error)
	ListCategories(context.Context, domain.TransactionType) ([]CategoryDefinition, error)
}

func validateCategoryForType(
	ctx context.Context,
	catalog CategoryCatalog,
	categoryID *domain.CategoryID,
	transactionType domain.TransactionType,
) (*domain.CategoryID, error) {
	if categoryID == nil {
		return nil, nil
	}
	if err := domain.ValidateCategoryID(*categoryID); err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, ErrCategoryCatalogUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	definition, err := catalog.FindCategory(ctx, *categoryID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, ErrCategoryNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, newSafeOperationError(ErrCategoryCatalogUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if definition.ID() != *categoryID {
		return nil, ErrCategoryCatalogUnavailable
	}
	if definition.TransactionType() != transactionType {
		return nil, ErrCategoryNotApplicable
	}

	validated := definition.ID()
	return &validated, nil
}

func isValidCategoryDisplayName(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxCategoryDisplayNameRunes {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
