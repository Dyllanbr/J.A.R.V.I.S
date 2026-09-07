package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type safeAvailableReaderFake struct {
	snapshot domain.SafeAvailableSnapshot
	err      error
	calls    int
	query    application.SafeAvailableSnapshotQuery
}

func (reader *safeAvailableReaderFake) Read(_ context.Context, query application.SafeAvailableSnapshotQuery) (domain.SafeAvailableSnapshot, error) {
	reader.calls++
	reader.query = query
	return reader.snapshot, reader.err
}

func TestCalculateSafeAvailableUsesOneExplicitSnapshotRead(t *testing.T) {
	period := appSafeAvailablePeriod(t, 2026, time.January, 1, 2026, time.January, 31)
	entry := appSafeAvailableEntry(t, domain.SafeAvailableEntryKindCommitment, "ipl_00000000000000000000000000000001", 1, period.EndOn(), 30_000)
	snapshot := appSafeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: appSafeAvailableMoney(t, 100_000),
		Commitments: []domain.SafeAvailableEntry{entry},
	})
	reader := &safeAvailableReaderFake{snapshot: snapshot}
	useCase, err := application.NewCalculateSafeAvailable(reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Execute(context.Background(), application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if reader.calls != 1 {
		t.Fatalf("reader calls = %d, want 1", reader.calls)
	}
	if reader.query.OwnerID != "owner-1" || !reader.query.Period.StartOn().Equal(period.StartOn()) || !reader.query.Period.EndOn().Equal(period.EndOn()) {
		t.Fatalf("reader query = %#v, want owner and period propagated exactly", reader.query)
	}
	if result.Available.FinalAmount().MinorUnits() != 70_000 {
		t.Fatalf("final amount = %d, want 70000", result.Available.FinalAmount().MinorUnits())
	}
}

func TestCalculateSafeAvailableReturnsStableEmptyReadModelWithExplicitMissingData(t *testing.T) {
	period := appSafeAvailablePeriod(t, 2026, time.February, 1, 2026, time.February, 28)
	reader := &safeAvailableReaderFake{snapshot: appSafeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: appSafeAvailableMoney(t, 0),
	})}
	useCase := appMustCalculateSafeAvailable(t, reader)
	result, err := useCase.Execute(context.Background(), application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Available.FinalAmount().MinorUnits() != 0 || len(result.Available.Breakdown()) != 1 {
		t.Fatalf("empty result = amount %d, breakdown %d; want zero and balance explanation", result.Available.FinalAmount().MinorUnits(), len(result.Available.Breakdown()))
	}
	missing := result.Available.MissingData()
	if len(missing) != 1 || missing[0] != domain.SafeAvailableMissingBudget {
		t.Fatalf("missing data = %#v, want explicit BUDGET", missing)
	}
	if result.Available.Breakdown() == nil || result.Available.MissingData() == nil {
		t.Fatal("empty read model collections must be stable, non-nil collections")
	}
}

func TestCalculateSafeAvailableValidatesBeforeReading(t *testing.T) {
	reader := &safeAvailableReaderFake{}
	useCase := appMustCalculateSafeAvailable(t, reader)
	validPeriod := appSafeAvailablePeriod(t, 2026, time.March, 1, 2026, time.March, 31)
	cases := []struct {
		name  string
		input application.CalculateSafeAvailableInput
		want  error
	}{
		{name: "invalid owner", input: application.CalculateSafeAvailableInput{Period: validPeriod}, want: application.ErrInvalidSafeAvailableOwnerID},
		{name: "invalid period", input: application.CalculateSafeAvailableInput{OwnerID: "owner-1"}, want: application.ErrInvalidSafeAvailablePeriod},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := useCase.Execute(context.Background(), testCase.input)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Execute() error = %v, want %v", err, testCase.want)
			}
		})
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls = %d, want 0 for invalid inputs", reader.calls)
	}
}

func TestCalculateSafeAvailableRejectsCrossOwnerSnapshotWithoutLeakingCause(t *testing.T) {
	period := appSafeAvailablePeriod(t, 2026, time.April, 1, 2026, time.April, 30)
	foreign := appSafeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-2", Period: period, AvailableBalance: appSafeAvailableMoney(t, 100),
	})
	reader := &safeAvailableReaderFake{snapshot: foreign}
	useCase := appMustCalculateSafeAvailable(t, reader)
	marker := errors.New("owner=owner-2 SQL=SELECT secret")
	reader.err = marker
	// Reader errors are sanitized before any dependency detail can escape.
	_, err := useCase.Execute(context.Background(), application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period})
	if !errors.Is(err, application.ErrSafeAvailableSnapshotQuery) || err.Error() != application.ErrSafeAvailableSnapshotQuery.Error() {
		t.Fatalf("sanitized read error = %v", err)
	}
	reader.err = nil
	reader.snapshot = foreign
	_, err = useCase.Execute(context.Background(), application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period})
	if !errors.Is(err, application.ErrSafeAvailableDependency) || err.Error() != application.ErrSafeAvailableDependency.Error() {
		t.Fatalf("cross-owner error = %v, want sanitized dependency error", err)
	}
}

func TestCalculateSafeAvailablePreservesContextCancellation(t *testing.T) {
	period := appSafeAvailablePeriod(t, 2026, time.May, 1, 2026, time.May, 31)
	reader := &safeAvailableReaderFake{snapshot: appSafeAvailableSnapshot(t, domain.SafeAvailableSnapshotParams{
		OwnerID: "owner-1", Period: period, AvailableBalance: appSafeAvailableMoney(t, 1),
	})}
	useCase := appMustCalculateSafeAvailable(t, reader)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(ctx, application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Execute() error = %v, want context.Canceled", err)
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls after cancellation = %d, want 0", reader.calls)
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer deadlineCancel()
	if _, err := useCase.Execute(deadlineCtx, application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline Execute() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestCalculateSafeAvailablePreservesReaderContextErrors(t *testing.T) {
	period := appSafeAvailablePeriod(t, 2026, time.June, 1, 2026, time.June, 30)
	for _, expected := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(expected.Error(), func(t *testing.T) {
			reader := &safeAvailableReaderFake{err: expected}
			useCase := appMustCalculateSafeAvailable(t, reader)
			_, err := useCase.Execute(context.Background(), application.CalculateSafeAvailableInput{OwnerID: "owner-1", Period: period})
			if !errors.Is(err, expected) {
				t.Fatalf("Execute() error = %v, want %v", err, expected)
			}
		})
	}
}

func TestNewCalculateSafeAvailableRequiresReader(t *testing.T) {
	if _, err := application.NewCalculateSafeAvailable(nil); !errors.Is(err, application.ErrMissingSafeAvailableSnapshotReader) {
		t.Fatalf("NewCalculateSafeAvailable(nil) error = %v", err)
	}
}

func appMustCalculateSafeAvailable(t *testing.T, reader application.SafeAvailableSnapshotReader) *application.CalculateSafeAvailable {
	t.Helper()
	useCase, err := application.NewCalculateSafeAvailable(reader)
	if err != nil {
		t.Fatal(err)
	}
	return useCase
}

func appSafeAvailableSnapshot(t *testing.T, params domain.SafeAvailableSnapshotParams) domain.SafeAvailableSnapshot {
	t.Helper()
	snapshot, err := domain.NewSafeAvailableSnapshot(params)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func appSafeAvailableEntry(t *testing.T, kind domain.SafeAvailableEntryKind, sourceID string, sequence int, date domain.CivilDate, amountMinor int64) domain.SafeAvailableEntry {
	t.Helper()
	entry, err := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{Kind: kind, OwnerID: "owner-1", SourceID: sourceID, Sequence: sequence, DueOn: date, Amount: appSafeAvailableMoney(t, amountMinor)})
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func appSafeAvailableMoney(t *testing.T, minor int64) domain.Money {
	t.Helper()
	money, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func appSafeAvailablePeriod(t *testing.T, startYear int, startMonth time.Month, startDay int, endYear int, endMonth time.Month, endDay int) domain.SafeAvailablePeriod {
	t.Helper()
	start := appSafeAvailableDate(t, startYear, startMonth, startDay)
	end := appSafeAvailableDate(t, endYear, endMonth, endDay)
	period, err := domain.NewSafeAvailablePeriod(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return period
}

func appSafeAvailableDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
