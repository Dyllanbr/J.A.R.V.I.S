package application

import (
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

// MonthlyTransaction is a query-only projection for the future mixed monthly
// history. It is not a writable aggregate. PaymentMethod is present only for
// Expense projections.
type MonthlyTransaction struct {
	ID                string
	UserID            string
	Type              domain.TransactionType
	Description       string
	Amount            domain.Money
	PaymentMethod     *domain.PaymentMethod
	OccurredAt        time.Time
	FinancialTimezone string
	Origin            domain.Origin
	Status            string
	Version           uint64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NewMonthlyTransactionFromExpense creates a read projection without changing
// the Expense aggregate.
func NewMonthlyTransactionFromExpense(expense domain.Expense) MonthlyTransaction {
	paymentMethod := expense.PaymentMethod()
	return MonthlyTransaction{
		ID:                expense.ID(),
		UserID:            expense.UserID(),
		Type:              expense.Type(),
		Description:       expense.Description(),
		Amount:            expense.Amount(),
		PaymentMethod:     &paymentMethod,
		OccurredAt:        expense.OccurredAt(),
		FinancialTimezone: expense.FinancialTimezone(),
		Origin:            expense.Origin(),
		Status:            string(expense.Status()),
		Version:           expense.Version(),
		CreatedAt:         expense.CreatedAt(),
		UpdatedAt:         expense.UpdatedAt(),
	}
}

// NewMonthlyTransactionFromIncome creates a read projection with no payment
// method.
func NewMonthlyTransactionFromIncome(income domain.Income) MonthlyTransaction {
	return MonthlyTransaction{
		ID:                income.ID(),
		UserID:            income.UserID(),
		Type:              income.Type(),
		Description:       income.Description(),
		Amount:            income.Amount(),
		OccurredAt:        income.OccurredAt(),
		FinancialTimezone: income.FinancialTimezone(),
		Origin:            income.Origin(),
		Status:            string(income.Status()),
		Version:           income.Version(),
		CreatedAt:         income.CreatedAt(),
		UpdatedAt:         income.UpdatedAt(),
	}
}
