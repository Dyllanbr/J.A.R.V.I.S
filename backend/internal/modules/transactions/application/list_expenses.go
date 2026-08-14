package application

import (
	"context"
	"errors"
	"strconv"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const FinancialTimezone = "America/Sao_Paulo"

var (
	ErrMissingExpenseReader         = errors.New("list expenses: reader is required")
	ErrInvalidMonth                 = errors.New("list expenses: month is invalid")
	ErrFinancialTimezoneUnavailable = errors.New("list expenses: financial timezone is unavailable")
	ErrExpenseQuery                 = errors.New("list expenses: query failed")
)

// ExpenseMonthQuery is the exact owner/time range needed by the current
// monthly listing. The interval is inclusive at Start and exclusive at End.
type ExpenseMonthQuery struct {
	UserID            string
	Start             time.Time
	End               time.Time
	FinancialTimezone string
}

// ExpenseReader is the minimum read capability required by the monthly use
// case.
type ExpenseReader interface {
	ListByFinancialMonth(context.Context, ExpenseMonthQuery) ([]domain.Expense, error)
}

// ListExpensesByMonthResult contains the requested canonical month and its
// owner-isolated expenses.
type ListExpensesByMonthResult struct {
	Month    string
	Expenses []domain.Expense
}

// ListExpensesByMonth computes financial calendar bounds independently of the
// process-local timezone and delegates the bounded query to its read port.
type ListExpensesByMonth struct {
	reader   ExpenseReader
	location *time.Location
}

// NewListExpensesByMonth loads the current IANA financial timezone once.
func NewListExpensesByMonth(reader ExpenseReader) (*ListExpensesByMonth, error) {
	if reader == nil {
		return nil, ErrMissingExpenseReader
	}
	location, err := time.LoadLocation(FinancialTimezone)
	if err != nil {
		return nil, newSafeOperationError(ErrFinancialTimezoneUnavailable, err)
	}
	return &ListExpensesByMonth{reader: reader, location: location}, nil
}

// Execute validates the owner and strict YYYY-MM input and returns a stable
// empty slice when no expenses exist.
func (useCase *ListExpensesByMonth) Execute(
	ctx context.Context,
	userID string,
	month string,
) (ListExpensesByMonthResult, error) {
	if err := ctx.Err(); err != nil {
		return ListExpensesByMonthResult{}, err
	}
	if err := domain.ValidateUserID(userID); err != nil {
		return ListExpensesByMonthResult{}, err
	}

	year, monthNumber, err := parseMonth(month)
	if err != nil {
		return ListExpensesByMonthResult{}, err
	}
	startLocal := time.Date(year, monthNumber, 1, 0, 0, 0, 0, useCase.location)
	endLocal := startLocal.AddDate(0, 1, 0)
	expenses, err := useCase.reader.ListByFinancialMonth(ctx, ExpenseMonthQuery{
		UserID:            userID,
		Start:             startLocal.UTC(),
		End:               endLocal.UTC(),
		FinancialTimezone: FinancialTimezone,
	})
	if err != nil {
		return ListExpensesByMonthResult{}, newSafeOperationError(ErrExpenseQuery, err)
	}
	if expenses == nil {
		expenses = []domain.Expense{}
	}
	return ListExpensesByMonthResult{Month: month, Expenses: expenses}, nil
}

func parseMonth(value string) (int, time.Month, error) {
	if len(value) != len("2006-01") || value[4] != '-' {
		return 0, 0, ErrInvalidMonth
	}
	for index, character := range value {
		if index == 4 {
			continue
		}
		if character < '0' || character > '9' {
			return 0, 0, ErrInvalidMonth
		}
	}
	year, err := strconv.Atoi(value[:4])
	if err != nil || year < 1 {
		return 0, 0, ErrInvalidMonth
	}
	month, err := strconv.Atoi(value[5:])
	if err != nil || month < 1 || month > 12 {
		return 0, 0, ErrInvalidMonth
	}
	return year, time.Month(month), nil
}
