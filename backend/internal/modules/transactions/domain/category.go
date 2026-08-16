package domain

import "errors"

const MaxCategoryIDBytes = 64

var ErrInvalidCategoryID = errors.New("category: invalid id")

// CategoryID is the stable technical identity of a system category. It is
// intentionally separate from localized display text.
type CategoryID string

const (
	CategoryIDExpenseFood          CategoryID = "expense.food"
	CategoryIDExpenseTransport     CategoryID = "expense.transport"
	CategoryIDExpenseHousing       CategoryID = "expense.housing"
	CategoryIDExpenseHealth        CategoryID = "expense.health"
	CategoryIDExpenseLeisure       CategoryID = "expense.leisure"
	CategoryIDExpenseEducation     CategoryID = "expense.education"
	CategoryIDExpenseSubscriptions CategoryID = "expense.subscriptions"
	CategoryIDExpenseShopping      CategoryID = "expense.shopping"
	CategoryIDExpenseTaxesFees     CategoryID = "expense.taxes_fees"
	CategoryIDExpenseOther         CategoryID = "expense.other"

	CategoryIDIncomeSalary           CategoryID = "income.salary"
	CategoryIDIncomeFreelance        CategoryID = "income.freelance"
	CategoryIDIncomeRefund           CategoryID = "income.refund"
	CategoryIDIncomeSale             CategoryID = "income.sale"
	CategoryIDIncomeInvestmentReturn CategoryID = "income.investment_return"
	CategoryIDIncomeBenefits         CategoryID = "income.benefits"
	CategoryIDIncomeOther            CategoryID = "income.other"
)

// NewCategoryID validates a client-supplied technical category identity.
func NewCategoryID(value string) (CategoryID, error) {
	categoryID := CategoryID(value)
	if err := ValidateCategoryID(categoryID); err != nil {
		return "", err
	}
	return categoryID, nil
}

// ValidateCategoryID accepts bounded lowercase ASCII identifiers composed of
// alphanumeric segments separated by dots or underscores. Hyphens are not
// currently needed by the system catalog and are therefore not accepted.
func ValidateCategoryID(categoryID CategoryID) error {
	value := string(categoryID)
	if value == "" || len(value) > MaxCategoryIDBytes {
		return ErrInvalidCategoryID
	}

	previousWasSeparator := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isSeparator := character == '.' || character == '_'

		if index == 0 && !isLetter {
			return ErrInvalidCategoryID
		}
		if !isLetter && !isDigit && !isSeparator {
			return ErrInvalidCategoryID
		}
		if isSeparator && (previousWasSeparator || index == len(value)-1) {
			return ErrInvalidCategoryID
		}
		previousWasSeparator = isSeparator
	}
	return nil
}

func (categoryID CategoryID) String() string {
	return string(categoryID)
}

func normalizeOptionalCategoryID(categoryID *CategoryID) (*CategoryID, error) {
	if categoryID == nil {
		return nil, nil
	}
	if err := ValidateCategoryID(*categoryID); err != nil {
		return nil, err
	}
	validated := *categoryID
	return &validated, nil
}
