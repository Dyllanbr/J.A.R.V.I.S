package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
)

func TestNewListTransactionsByMonthRequiresReader(t *testing.T) {
	if _, err := application.NewListTransactionsByMonth(nil); !errors.Is(err, application.ErrMissingMonthlyTransactionReader) {
		t.Fatalf("NewListTransactionsByMonth(nil) error = %v", err)
	}
}

func TestListTransactionsByMonthUsesCanonicalBoundsAndStableEmptyItems(t *testing.T) {
	reader := &recordingMonthlyTransactionReader{}
	useCase, err := application.NewListTransactionsByMonth(reader)
	if err != nil {
		t.Fatalf("NewListTransactionsByMonth() error = %v", err)
	}
	result, err := useCase.Execute(context.Background(), "user-synthetic-001", "2026-08")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	location, _ := time.LoadLocation(application.FinancialTimezone)
	if reader.calls != 1 || reader.query.UserID != "user-synthetic-001" ||
		reader.query.Start.In(location).Format(time.RFC3339) != "2026-08-01T00:00:00-03:00" ||
		reader.query.End.In(location).Format(time.RFC3339) != "2026-09-01T00:00:00-03:00" ||
		reader.query.Start.Location() != time.UTC || reader.query.End.Location() != time.UTC ||
		reader.query.FinancialTimezone != application.FinancialTimezone {
		t.Fatal("mixed monthly query did not preserve owner, timezone and [start,end) bounds")
	}
	if result.Month != "2026-08" || result.Items == nil || len(result.Items) != 0 {
		t.Fatal("mixed monthly result did not preserve month and stable empty items")
	}
}

func TestListTransactionsByMonthRejectsInvalidInputBeforeReader(t *testing.T) {
	for _, month := range []string{"", "2026-8", "2026-00", "2026-13", "0000-01", "2026-08-extra"} {
		reader := &recordingMonthlyTransactionReader{}
		useCase, err := application.NewListTransactionsByMonth(reader)
		if err != nil {
			t.Fatalf("NewListTransactionsByMonth() error = %v", err)
		}
		if _, err := useCase.Execute(context.Background(), "user-synthetic-001", month); !errors.Is(err, application.ErrInvalidMonth) {
			t.Fatalf("Execute(%q) error = %v, want ErrInvalidMonth", month, err)
		}
		if reader.calls != 0 {
			t.Fatal("invalid month reached mixed reader")
		}
	}
}

func TestListTransactionsByMonthPropagatesCancellationAndSafelyWrapsReaderFailure(t *testing.T) {
	reader := &recordingMonthlyTransactionReader{err: errors.New("SUPER_SECRET_MONTHLY_QUERY")}
	useCase, err := application.NewListTransactionsByMonth(reader)
	if err != nil {
		t.Fatalf("NewListTransactionsByMonth() error = %v", err)
	}
	_, err = useCase.Execute(context.Background(), "user-synthetic-001", "2026-08")
	if !errors.Is(err, application.ErrMonthlyTransactionQuery) || err.Error() != application.ErrMonthlyTransactionQuery.Error() {
		t.Fatalf("reader error = %v, want safe query category", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	reader.calls = 0
	if _, err := useCase.Execute(canceled, "user-synthetic-001", "2026-08"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Execute() error = %v", err)
	}
	if reader.calls != 0 {
		t.Fatal("canceled request reached mixed reader")
	}
}

type recordingMonthlyTransactionReader struct {
	calls int
	query application.ExpenseMonthQuery
	items []application.MonthlyTransaction
	err   error
}

func (reader *recordingMonthlyTransactionReader) ListMonthlyTransactions(
	_ context.Context,
	query application.ExpenseMonthQuery,
) ([]application.MonthlyTransaction, error) {
	reader.calls++
	reader.query = query
	return reader.items, reader.err
}
