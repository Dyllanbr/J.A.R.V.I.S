package domain

import (
	"errors"
	"testing"
	"time"
)

func TestCreditCardArchiveRejectsStructurallyInvalidReceivers(t *testing.T) {
	active := validInternalCreditCard(t)
	tests := []struct {
		name   string
		mutate func(*CreditCard)
	}{
		{name: "invalid owner", mutate: func(card *CreditCard) { card.userID = " owner " }},
		{name: "unknown status", mutate: func(card *CreditCard) { card.status = "UNKNOWN" }},
		{name: "active with archived time", mutate: func(card *CreditCard) { card.archivedAt = card.createdAt.Add(time.Hour) }},
		{name: "archived without time", mutate: func(card *CreditCard) { card.status = CreditCardStatusArchived }},
		{name: "invalid name", mutate: func(card *CreditCard) { card.name = CreditCardName{} }},
		{name: "invalid last four", mutate: func(card *CreditCard) { card.hasLastFour = true; card.lastFour = CreditCardLastFour{} }},
		{name: "invalid brand", mutate: func(card *CreditCard) { card.hasBrand = true; card.brand = "UNKNOWN" }},
		{name: "invalid closing", mutate: func(card *CreditCard) { card.closingDayAnchor = DayOfMonthAnchor{} }},
		{name: "invalid due", mutate: func(card *CreditCard) { card.dueDayAnchor = DayOfMonthAnchor{} }},
		{name: "invalid limit", mutate: func(card *CreditCard) { card.hasCreditLimit = true; card.creditLimit = Money{} }},
		{name: "noncanonical created time", mutate: func(card *CreditCard) { card.createdAt = card.createdAt.In(time.FixedZone("synthetic", -3*60*60)) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := active
			test.mutate(&invalid)
			if _, err := invalid.Archive(active.createdAt.Add(time.Hour)); !errors.Is(err, ErrInvalidCreditCardState) {
				t.Fatalf("Archive() error = %v, want ErrInvalidCreditCardState", err)
			}
		})
	}
}

func validInternalCreditCard(t testing.TB) CreditCard {
	t.Helper()
	name, _ := NewCreditCardName("Cartão sintético")
	closing, _ := NewDayOfMonthAnchor(10)
	due, _ := NewDayOfMonthAnchor(17)
	card, err := NewCreditCard(CreditCardParams{
		ID: "card_abcdef0123456789abcdef0123456789",
		Details: CreditCardDetails{
			UserID: "user-credit-card-internal",
			Name:   name, ClosingDayAnchor: closing, DueDayAnchor: due,
		},
		CreatedAt: time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewCreditCard() error = %v", err)
	}
	return card
}
