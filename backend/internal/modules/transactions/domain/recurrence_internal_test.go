package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRecurrenceCancelRejectsStructurallyInvalidReceivers(t *testing.T) {
	active := validInternalRecurrence(t)
	tests := []struct {
		name   string
		mutate func(*Recurrence)
	}{
		{name: "unknown status", mutate: func(recurrence *Recurrence) {
			recurrence.status = RecurrenceStatus("UNKNOWN")
		}},
		{name: "active with cancellation time", mutate: func(recurrence *Recurrence) {
			recurrence.cancelledAt = recurrence.createdAt.Add(time.Hour)
		}},
		{name: "cancelled without cancellation time", mutate: func(recurrence *Recurrence) {
			recurrence.status = RecurrenceStatusCancelled
		}},
		{name: "cancelled before creation", mutate: func(recurrence *Recurrence) {
			recurrence.status = RecurrenceStatusCancelled
			recurrence.cancelledAt = recurrence.createdAt.Add(-time.Nanosecond)
		}},
		{name: "noncanonical creation time", mutate: func(recurrence *Recurrence) {
			recurrence.createdAt = recurrence.createdAt.In(time.FixedZone("synthetic", -3*60*60))
		}},
		{name: "noncanonical cancellation time", mutate: func(recurrence *Recurrence) {
			recurrence.status = RecurrenceStatusCancelled
			recurrence.cancelledAt = recurrence.createdAt.Add(time.Hour).In(time.FixedZone("synthetic", -3*60*60))
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := active
			test.mutate(&invalid)
			if _, err := invalid.Cancel(active.createdAt.Add(2 * time.Hour)); !errors.Is(err, ErrInvalidRecurrenceState) {
				t.Fatalf("Cancel() error = %v, want ErrInvalidRecurrenceState", err)
			}
		})
	}
}

func validInternalRecurrence(t testing.TB) Recurrence {
	t.Helper()
	amount, err := NewMoney(11900, CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	startsOn, err := NewCivilDate(2026, time.August, 10)
	if err != nil {
		t.Fatalf("NewCivilDate() error = %v", err)
	}
	recurrence, err := NewRecurrence(RecurrenceParams{
		ID: "recurrence-internal-valid",
		Details: RecurrenceDetails{
			UserID:          "user-internal-valid",
			TransactionType: TransactionTypeExpense,
			Description:     "Serviço sintético mensal",
			ExpectedAmount:  amount,
			Frequency:       RecurrenceFrequencyMonthly,
			StartsOn:        startsOn,
		},
		CreatedAt: time.Date(2026, time.August, 16, 13, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewRecurrence() error = %v", err)
	}
	return recurrence
}
