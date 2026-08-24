package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestNewRecurrenceCreatesActiveExpenseCommitment(t *testing.T) {
	params := validRecurrenceParams(t)
	params.Details.Description = "  Serviço sintético mensal  "
	params.CreatedAt = time.Date(2026, time.August, 16, 10, 30, 0, 123, time.FixedZone("synthetic", -3*60*60))

	recurrence, err := domain.NewRecurrence(params)
	if err != nil {
		t.Fatalf("NewRecurrence() error = %v", err)
	}
	if recurrence.ID() != params.ID || recurrence.UserID() != params.Details.UserID {
		t.Fatal("NewRecurrence() did not preserve opaque identifiers")
	}
	if recurrence.Type() != domain.TransactionTypeExpense {
		t.Fatalf("Type() = %q, want EXPENSE", recurrence.Type())
	}
	if recurrence.Description() != "Serviço sintético mensal" {
		t.Fatalf("Description() = %q", recurrence.Description())
	}
	if !recurrence.ExpectedAmount().Equal(params.Details.ExpectedAmount) || recurrence.ExpectedAmount().Currency() != domain.CurrencyBRL {
		t.Fatal("ExpectedAmount() did not preserve exact BRL Money")
	}
	if recurrence.Frequency() != domain.RecurrenceFrequencyMonthly || !recurrence.StartsOn().Equal(params.Details.StartsOn) {
		t.Fatal("NewRecurrence() did not preserve monthly civil-date semantics")
	}
	if recurrence.Status() != domain.RecurrenceStatusActive {
		t.Fatalf("Status() = %q, want ACTIVE", recurrence.Status())
	}
	if _, exists := recurrence.CancelledAt(); exists {
		t.Fatal("new ACTIVE recurrence has a cancellation time")
	}
	expectedCreatedAt := time.Date(2026, time.August, 16, 13, 30, 0, 123, time.UTC)
	if recurrence.CreatedAt() != expectedCreatedAt || recurrence.CreatedAt().Location() != time.UTC {
		t.Fatalf("CreatedAt() = %v, want normalized UTC instant %v", recurrence.CreatedAt(), expectedCreatedAt)
	}
}

func TestCivilDateHasNoTimezoneOrTimeOfDay(t *testing.T) {
	date := mustCivilDate(t, 2026, time.August, 31)
	if date.Year() != 2026 || date.Month() != time.August || date.Day() != 31 {
		t.Fatal("CivilDate did not preserve calendar fields")
	}
	if date.String() != "2026-08-31" {
		t.Fatalf("String() = %q, want YYYY-MM-DD", date.String())
	}
	if !date.Before(mustCivilDate(t, 2026, time.September, 1)) || !date.Before(mustCivilDate(t, 2027, time.January, 1)) {
		t.Fatal("Before() did not compare civil calendar fields")
	}
	if (domain.CivilDate{}).String() != "" {
		t.Fatal("invalid zero CivilDate rendered a transport value")
	}
}

func TestNewCivilDateRejectsInvalidCalendarValues(t *testing.T) {
	tests := []struct {
		year  int
		month time.Month
		day   int
	}{
		{year: 0, month: time.January, day: 1},
		{year: 10000, month: time.January, day: 1},
		{year: 2026, month: 0, day: 1},
		{year: 2026, month: 13, day: 1},
		{year: 2026, month: time.January, day: 0},
		{year: 2026, month: time.April, day: 31},
		{year: 2023, month: time.February, day: 29},
	}
	for _, test := range tests {
		if _, err := domain.NewCivilDate(test.year, test.month, test.day); !errors.Is(err, domain.ErrInvalidCivilDate) {
			t.Fatalf("NewCivilDate(%d, %d, %d) error = %v", test.year, test.month, test.day, err)
		}
	}
	if _, err := domain.NewCivilDate(2024, time.February, 29); err != nil {
		t.Fatalf("NewCivilDate(leap day) error = %v", err)
	}
}

func TestNewRecurrenceValidatesInvariants(t *testing.T) {
	negativeAmount := mustRecurrenceMoney(t, -1)
	tests := []struct {
		name    string
		mutate  func(*domain.RecurrenceParams)
		wantErr error
	}{
		{name: "empty id", mutate: func(params *domain.RecurrenceParams) { params.ID = "" }, wantErr: domain.ErrInvalidRecurrenceID},
		{name: "id with spaces", mutate: func(params *domain.RecurrenceParams) { params.ID = " recurrence " }, wantErr: domain.ErrInvalidRecurrenceID},
		{name: "id with control", mutate: func(params *domain.RecurrenceParams) { params.ID = "recurrence\nid" }, wantErr: domain.ErrInvalidRecurrenceID},
		{name: "id above limit", mutate: func(params *domain.RecurrenceParams) { params.ID = strings.Repeat("r", domain.MaxIdentifierBytes+1) }, wantErr: domain.ErrInvalidRecurrenceID},
		{name: "empty owner", mutate: func(params *domain.RecurrenceParams) { params.Details.UserID = "" }, wantErr: domain.ErrInvalidRecurrenceUserID},
		{name: "owner with spaces", mutate: func(params *domain.RecurrenceParams) { params.Details.UserID = " owner " }, wantErr: domain.ErrInvalidRecurrenceUserID},
		{name: "owner with control", mutate: func(params *domain.RecurrenceParams) { params.Details.UserID = "owner\tid" }, wantErr: domain.ErrInvalidRecurrenceUserID},
		{name: "income type", mutate: func(params *domain.RecurrenceParams) { params.Details.TransactionType = domain.TransactionTypeIncome }, wantErr: domain.ErrInvalidRecurrenceType},
		{name: "empty type", mutate: func(params *domain.RecurrenceParams) { params.Details.TransactionType = "" }, wantErr: domain.ErrInvalidRecurrenceType},
		{name: "empty description", mutate: func(params *domain.RecurrenceParams) { params.Details.Description = "" }, wantErr: domain.ErrInvalidRecurrenceDescription},
		{name: "blank description", mutate: func(params *domain.RecurrenceParams) { params.Details.Description = " \t\n " }, wantErr: domain.ErrInvalidRecurrenceDescription},
		{name: "description above limit", mutate: func(params *domain.RecurrenceParams) {
			params.Details.Description = strings.Repeat("á", domain.MaxRecurrenceDescriptionRunes+1)
		}, wantErr: domain.ErrInvalidRecurrenceDescription},
		{name: "zero amount", mutate: func(params *domain.RecurrenceParams) { params.Details.ExpectedAmount = domain.Money{} }, wantErr: domain.ErrInvalidRecurrenceExpectedAmount},
		{name: "negative amount", mutate: func(params *domain.RecurrenceParams) { params.Details.ExpectedAmount = negativeAmount }, wantErr: domain.ErrInvalidRecurrenceExpectedAmount},
		{name: "empty frequency", mutate: func(params *domain.RecurrenceParams) { params.Details.Frequency = "" }, wantErr: domain.ErrInvalidRecurrenceFrequency},
		{name: "annual frequency", mutate: func(params *domain.RecurrenceParams) { params.Details.Frequency = "ANNUAL" }, wantErr: domain.ErrInvalidRecurrenceFrequency},
		{name: "zero start date", mutate: func(params *domain.RecurrenceParams) { params.Details.StartsOn = domain.CivilDate{} }, wantErr: domain.ErrInvalidRecurrenceStartsOn},
		{name: "zero created at", mutate: func(params *domain.RecurrenceParams) { params.CreatedAt = time.Time{} }, wantErr: domain.ErrInvalidRecurrenceCreatedAt},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validRecurrenceParams(t)
			test.mutate(&params)
			_, err := domain.NewRecurrence(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewRecurrence() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateRecurrenceIdentifiers(t *testing.T) {
	if err := domain.ValidateRecurrenceID("recurrence-synthetic-001"); err != nil {
		t.Fatalf("ValidateRecurrenceID(valid) error = %v", err)
	}
	if err := domain.ValidateRecurrenceUserID("user-synthetic-001"); err != nil {
		t.Fatalf("ValidateRecurrenceUserID(valid) error = %v", err)
	}
	if err := domain.ValidateRecurrenceID(" recurrence "); !errors.Is(err, domain.ErrInvalidRecurrenceID) {
		t.Fatalf("ValidateRecurrenceID(invalid) error = %v", err)
	}
	if err := domain.ValidateRecurrenceUserID(" user "); !errors.Is(err, domain.ErrInvalidRecurrenceUserID) {
		t.Fatalf("ValidateRecurrenceUserID(invalid) error = %v", err)
	}
}

func TestRecurrenceCancellationIsTerminalAndImmutable(t *testing.T) {
	active := mustRecurrence(t, "recurrence-synthetic-cancel")
	cancelledAt := active.CreatedAt().Add(2 * time.Hour)
	cancelled, err := active.Cancel(cancelledAt)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if active.Status() != domain.RecurrenceStatusActive {
		t.Fatal("Cancel() mutated the original recurrence")
	}
	if cancelled.Status() != domain.RecurrenceStatusCancelled {
		t.Fatalf("cancelled Status() = %q", cancelled.Status())
	}
	storedCancelledAt, exists := cancelled.CancelledAt()
	if !exists || storedCancelledAt != cancelledAt.UTC() {
		t.Fatalf("CancelledAt() = %v/%v, want %v/true", storedCancelledAt, exists, cancelledAt.UTC())
	}
	if _, err := cancelled.Cancel(cancelledAt.Add(time.Hour)); !errors.Is(err, domain.ErrRecurrenceAlreadyCancelled) {
		t.Fatalf("second Cancel() error = %v, want ErrRecurrenceAlreadyCancelled", err)
	}
}

func TestZeroValueRecurrenceCannotBeCancelled(t *testing.T) {
	var recurrence domain.Recurrence
	if _, err := recurrence.Cancel(time.Now()); !errors.Is(err, domain.ErrInvalidRecurrenceState) {
		t.Fatalf("zero-value Cancel() error = %v, want ErrInvalidRecurrenceState", err)
	}
}

func TestRecurrenceRejectsInvalidCancellationTime(t *testing.T) {
	active := mustRecurrence(t, "recurrence-synthetic-invalid-cancel")
	for _, cancelledAt := range []time.Time{{}, active.CreatedAt().Add(-time.Nanosecond)} {
		if _, err := active.Cancel(cancelledAt); !errors.Is(err, domain.ErrInvalidRecurrenceCancelledAt) {
			t.Fatalf("Cancel(%v) error = %v", cancelledAt, err)
		}
	}
	if _, err := active.Cancel(active.CreatedAt()); err != nil {
		t.Fatalf("Cancel(at creation) error = %v", err)
	}
}

func TestMonthlyOccurrenceClampsAnchorDay(t *testing.T) {
	tests := []struct {
		name     string
		start    domain.CivilDate
		offset   int
		expected string
	}{
		{name: "31 January", start: mustCivilDate(t, 2023, time.January, 31), offset: 0, expected: "2023-01-31"},
		{name: "31 non-leap February", start: mustCivilDate(t, 2023, time.January, 31), offset: 1, expected: "2023-02-28"},
		{name: "31 March", start: mustCivilDate(t, 2023, time.January, 31), offset: 2, expected: "2023-03-31"},
		{name: "31 April", start: mustCivilDate(t, 2023, time.January, 31), offset: 3, expected: "2023-04-30"},
		{name: "31 leap February", start: mustCivilDate(t, 2024, time.January, 31), offset: 1, expected: "2024-02-29"},
		{name: "30 leap February", start: mustCivilDate(t, 2024, time.January, 30), offset: 1, expected: "2024-02-29"},
		{name: "30 March", start: mustCivilDate(t, 2024, time.January, 30), offset: 2, expected: "2024-03-30"},
		{name: "December to January", start: mustCivilDate(t, 2026, time.December, 15), offset: 1, expected: "2027-01-15"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validRecurrenceParams(t)
			params.Details.StartsOn = test.start
			recurrence, err := domain.NewRecurrence(params)
			if err != nil {
				t.Fatalf("NewRecurrence() error = %v", err)
			}
			occurrence, err := recurrence.OccurrenceDate(test.offset)
			if err != nil {
				t.Fatalf("OccurrenceDate() error = %v", err)
			}
			if occurrence.String() != test.expected {
				t.Fatalf("OccurrenceDate() = %s, want %s", occurrence.String(), test.expected)
			}
		})
	}
}

func TestMonthlyOccurrenceRejectsInvalidOffset(t *testing.T) {
	recurrence := mustRecurrence(t, "recurrence-synthetic-offset")
	for _, offset := range []int{-1, 12 * 10000} {
		if _, err := recurrence.OccurrenceDate(offset); !errors.Is(err, domain.ErrInvalidRecurrenceMonthOffset) {
			t.Fatalf("OccurrenceDate(%d) error = %v", offset, err)
		}
	}
}

func validRecurrenceParams(t testing.TB) domain.RecurrenceParams {
	t.Helper()
	return domain.RecurrenceParams{
		ID: "recurrence-synthetic-001",
		Details: domain.RecurrenceDetails{
			UserID:          "user-synthetic-001",
			TransactionType: domain.TransactionTypeExpense,
			Description:     "Serviço sintético mensal",
			ExpectedAmount:  mustRecurrenceMoney(t, 11900),
			Frequency:       domain.RecurrenceFrequencyMonthly,
			StartsOn:        mustCivilDate(t, 2026, time.August, 10),
		},
		CreatedAt: time.Date(2026, time.August, 16, 13, 30, 0, 0, time.UTC),
	}
}

func mustRecurrence(t testing.TB, id string) domain.Recurrence {
	t.Helper()
	params := validRecurrenceParams(t)
	params.ID = id
	recurrence, err := domain.NewRecurrence(params)
	if err != nil {
		t.Fatalf("NewRecurrence() error = %v", err)
	}
	return recurrence
}

func mustRecurrenceMoney(t testing.TB, minor int64) domain.Money {
	t.Helper()
	amount, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	return amount
}

func mustCivilDate(t testing.TB, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatalf("NewCivilDate() error = %v", err)
	}
	return date
}
