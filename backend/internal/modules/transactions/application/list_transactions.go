package application

import (
	"context"
	"errors"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingMonthlyTransactionReader = errors.New("list transactions: reader is required")
	ErrMonthlyTransactionQuery         = errors.New("list transactions: query failed")
)

// MonthlyTransactionReader is the minimum read capability required by the
// mixed monthly history. It does not participate in transaction writes.
type MonthlyTransactionReader interface {
	ListMonthlyTransactions(context.Context, ExpenseMonthQuery) ([]MonthlyTransaction, error)
}

// ListTransactionsByMonthResult contains the requested canonical month and
// its owner-isolated read projections.
type ListTransactionsByMonthResult struct {
	Month string
	Items []MonthlyTransaction
}

// ListTransactionsByMonth computes financial calendar bounds and delegates
// the mixed Expense/Income query to its read port.
type ListTransactionsByMonth struct {
	reader   MonthlyTransactionReader
	location *time.Location
}

// NewListTransactionsByMonth loads the financial timezone once.
func NewListTransactionsByMonth(reader MonthlyTransactionReader) (*ListTransactionsByMonth, error) {
	if reader == nil {
		return nil, ErrMissingMonthlyTransactionReader
	}
	location, err := time.LoadLocation(FinancialTimezone)
	if err != nil {
		return nil, newSafeOperationError(ErrFinancialTimezoneUnavailable, err)
	}
	return &ListTransactionsByMonth{reader: reader, location: location}, nil
}

// Execute validates owner/month and returns a stable empty collection.
func (useCase *ListTransactionsByMonth) Execute(
	ctx context.Context,
	userID string,
	month string,
) (ListTransactionsByMonthResult, error) {
	if err := ctx.Err(); err != nil {
		return ListTransactionsByMonthResult{}, err
	}
	if err := domain.ValidateUserID(userID); err != nil {
		return ListTransactionsByMonthResult{}, err
	}

	year, monthNumber, err := parseMonth(month)
	if err != nil {
		return ListTransactionsByMonthResult{}, err
	}
	startLocal := time.Date(year, monthNumber, 1, 0, 0, 0, 0, useCase.location)
	endLocal := startLocal.AddDate(0, 1, 0)
	items, err := useCase.reader.ListMonthlyTransactions(ctx, ExpenseMonthQuery{
		UserID:            userID,
		Start:             startLocal.UTC(),
		End:               endLocal.UTC(),
		FinancialTimezone: FinancialTimezone,
	})
	if err != nil {
		return ListTransactionsByMonthResult{}, newSafeOperationError(ErrMonthlyTransactionQuery, err)
	}
	if items == nil {
		items = []MonthlyTransaction{}
	}
	return ListTransactionsByMonthResult{Month: month, Items: items}, nil
}
