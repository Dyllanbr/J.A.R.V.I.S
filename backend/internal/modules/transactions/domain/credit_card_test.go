package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const validCreditCardID = "card_0123456789abcdef0123456789abcdef"

func TestCreditCardIDValidationIsOpaqueAndStrict(t *testing.T) {
	if err := domain.ValidateCreditCardID(validCreditCardID); err != nil {
		t.Fatalf("ValidateCreditCardID(valid) error = %v", err)
	}
	invalid := []string{
		"",
		"card_0123456789abcdef0123456789abcde",
		"card_0123456789abcdef0123456789abcdef0",
		"card_0123456789ABCDEF0123456789ABCDEF",
		"card_0123456789abcdef0123456789abcdeg",
		"card-0123456789abcdef0123456789abcdef",
		" card_0123456789abcdef0123456789abcdef",
	}
	for _, value := range invalid {
		if err := domain.ValidateCreditCardID(value); !errors.Is(err, domain.ErrInvalidCreditCardID) {
			t.Fatalf("ValidateCreditCardID(%q) error = %v", value, err)
		}
	}
}

func TestCreditCardNameCanonicalizesHumanWhitespaceAndUnicode(t *testing.T) {
	name, err := domain.NewCreditCardName(" \u00a0Nubank\u2003Ultravioleta  ")
	if err != nil {
		t.Fatalf("NewCreditCardName() error = %v", err)
	}
	if name.String() != "Nubank Ultravioleta" {
		t.Fatalf("String() = %q", name.String())
	}

	decomposed, err := domain.NewCreditCardName("Cafe\u0301 Premium")
	if err != nil {
		t.Fatalf("NewCreditCardName(decomposed) error = %v", err)
	}
	if decomposed.String() != "Café Premium" {
		t.Fatalf("NFC name = %q", decomposed.String())
	}
	if preserved, _ := domain.NewCreditCardName("NUBANK Ágil!"); preserved.String() != "NUBANK Ágil!" {
		t.Fatalf("human form was changed: %q", preserved.String())
	}
}

func TestCreditCardNameRejectsInvalidInputAndHonorsBound(t *testing.T) {
	invalid := []string{
		"",
		" \u00a0 ",
		"Card\nName",
		"Card\tName",
		"Card\x00Name",
		string([]byte{0xff, 0xfe}),
		strings.Repeat("á", domain.MaxCreditCardNameRunes+1),
	}
	for _, value := range invalid {
		if _, err := domain.NewCreditCardName(value); !errors.Is(err, domain.ErrInvalidCreditCardName) {
			t.Fatalf("NewCreditCardName(%q) error = %v", value, err)
		}
	}
	if _, err := domain.NewCreditCardName(strings.Repeat("á", domain.MaxCreditCardNameRunes)); err != nil {
		t.Fatalf("NewCreditCardName(max) error = %v", err)
	}
}

func TestCreditCardLastFourAcceptsOnlyFourASCIIDigits(t *testing.T) {
	lastFour, err := domain.NewCreditCardLastFour("4821")
	if err != nil || lastFour.String() != "4821" {
		t.Fatalf("NewCreditCardLastFour(valid) = %q, %v", lastFour.String(), err)
	}
	for _, value := range []string{"821", "04821", "12 34", "１２３４", "abcd", "48\n1"} {
		if _, err := domain.NewCreditCardLastFour(value); !errors.Is(err, domain.ErrInvalidCreditCardLastFour) {
			t.Fatalf("NewCreditCardLastFour(%q) error = %v", value, err)
		}
	}
}

func TestCreditCardBrandEnum(t *testing.T) {
	valid := []domain.CreditCardBrand{
		domain.CreditCardBrandVisa,
		domain.CreditCardBrandMastercard,
		domain.CreditCardBrandElo,
		domain.CreditCardBrandAmericanExpress,
		domain.CreditCardBrandHipercard,
		domain.CreditCardBrandOther,
	}
	for _, brand := range valid {
		if err := domain.ValidateCreditCardBrand(brand); err != nil {
			t.Fatalf("ValidateCreditCardBrand(%q) error = %v", brand, err)
		}
	}
	for _, brand := range []domain.CreditCardBrand{"", "VISA_FREE_TEXT", "visa"} {
		if err := domain.ValidateCreditCardBrand(brand); !errors.Is(err, domain.ErrInvalidCreditCardBrand) {
			t.Fatalf("ValidateCreditCardBrand(%q) error = %v", brand, err)
		}
	}
}

func TestDayOfMonthAnchorBoundaries(t *testing.T) {
	for _, day := range []int{1, 31} {
		anchor, err := domain.NewDayOfMonthAnchor(day)
		if err != nil || anchor.Day() != day {
			t.Fatalf("NewDayOfMonthAnchor(%d) = %d, %v", day, anchor.Day(), err)
		}
	}
	for _, day := range []int{-1, 0, 32, 256} {
		if _, err := domain.NewDayOfMonthAnchor(day); !errors.Is(err, domain.ErrInvalidDayOfMonthAnchor) {
			t.Fatalf("NewDayOfMonthAnchor(%d) error = %v", day, err)
		}
	}
}

func TestCreditCardCreditLimitUsesPositiveBRLMoney(t *testing.T) {
	limit, err := domain.NewCreditCardCreditLimit(250_000, domain.CurrencyBRL)
	if err != nil || limit.MinorUnits() != 250_000 || limit.Currency() != domain.CurrencyBRL {
		t.Fatalf("NewCreditCardCreditLimit(valid) = %d/%q, %v", limit.MinorUnits(), limit.Currency(), err)
	}
	for _, amount := range []int64{0, -1} {
		if _, err := domain.NewCreditCardCreditLimit(amount, domain.CurrencyBRL); !errors.Is(err, domain.ErrInvalidCreditCardCreditLimit) {
			t.Fatalf("NewCreditCardCreditLimit(%d) error = %v", amount, err)
		}
	}
	if _, err := domain.NewCreditCardCreditLimit(1, "USD"); !errors.Is(err, domain.ErrUnsupportedCurrency) {
		t.Fatalf("NewCreditCardCreditLimit(USD) error = %v", err)
	}
}

func TestNewCreditCardCreatesActiveReferenceWithOptionalMetadata(t *testing.T) {
	params := validCreditCardParams(t)
	params.CreatedAt = time.Date(2026, time.August, 25, 10, 30, 0, 123, time.FixedZone("synthetic", -3*60*60))
	card, err := domain.NewCreditCard(params)
	if err != nil {
		t.Fatalf("NewCreditCard() error = %v", err)
	}
	if card.ID() != params.ID || card.UserID() != params.Details.UserID || card.Name().String() != "Nubank Ultravioleta" {
		t.Fatal("NewCreditCard() lost identity or canonical name")
	}
	if card.Status() != domain.CreditCardStatusActive {
		t.Fatalf("Status() = %q", card.Status())
	}
	if _, exists := card.ArchivedAt(); exists {
		t.Fatal("ACTIVE card has ArchivedAt")
	}
	if lastFour, exists := card.LastFour(); !exists || lastFour.String() != "4821" {
		t.Fatalf("LastFour() = %q/%v", lastFour.String(), exists)
	}
	if brand, exists := card.Brand(); !exists || brand != domain.CreditCardBrandMastercard {
		t.Fatalf("Brand() = %q/%v", brand, exists)
	}
	if limit, exists := card.CreditLimit(); !exists || limit.MinorUnits() != 250_000 {
		t.Fatalf("CreditLimit() = %d/%v", limit.MinorUnits(), exists)
	}
	if card.ClosingDayAnchor().Day() != 25 || card.DueDayAnchor().Day() != 2 {
		t.Fatal("anchors were not preserved")
	}
	wantCreatedAt := time.Date(2026, time.August, 25, 13, 30, 0, 123, time.UTC)
	if card.CreatedAt() != wantCreatedAt || card.CreatedAt().Location() != time.UTC {
		t.Fatalf("CreatedAt() = %v, want %v", card.CreatedAt(), wantCreatedAt)
	}
}

func TestNewCreditCardAllowsAbsentOptionalMetadata(t *testing.T) {
	params := validCreditCardParams(t)
	params.Details.LastFour = nil
	params.Details.Brand = nil
	params.Details.CreditLimit = nil
	card, err := domain.NewCreditCard(params)
	if err != nil {
		t.Fatalf("NewCreditCard() error = %v", err)
	}
	if _, exists := card.LastFour(); exists {
		t.Fatal("LastFour unexpectedly present")
	}
	if _, exists := card.Brand(); exists {
		t.Fatal("Brand unexpectedly present")
	}
	if _, exists := card.CreditLimit(); exists {
		t.Fatal("CreditLimit unexpectedly present")
	}
}

func TestNewCreditCardRejectsInvalidDetails(t *testing.T) {
	zeroMoney := domain.Money{}
	invalidBrand := domain.CreditCardBrand("UNKNOWN")
	tests := []struct {
		name    string
		mutate  func(*domain.CreditCardParams)
		wantErr error
	}{
		{name: "id", mutate: func(params *domain.CreditCardParams) { params.ID = "card_bad" }, wantErr: domain.ErrInvalidCreditCardID},
		{name: "owner", mutate: func(params *domain.CreditCardParams) { params.Details.UserID = " owner " }, wantErr: domain.ErrInvalidCreditCardUserID},
		{name: "name", mutate: func(params *domain.CreditCardParams) { params.Details.Name = domain.CreditCardName{} }, wantErr: domain.ErrInvalidCreditCardName},
		{name: "last four", mutate: func(params *domain.CreditCardParams) {
			zero := domain.CreditCardLastFour{}
			params.Details.LastFour = &zero
		}, wantErr: domain.ErrInvalidCreditCardLastFour},
		{name: "brand", mutate: func(params *domain.CreditCardParams) { params.Details.Brand = &invalidBrand }, wantErr: domain.ErrInvalidCreditCardBrand},
		{name: "closing", mutate: func(params *domain.CreditCardParams) { params.Details.ClosingDayAnchor = domain.DayOfMonthAnchor{} }, wantErr: domain.ErrInvalidDayOfMonthAnchor},
		{name: "due", mutate: func(params *domain.CreditCardParams) { params.Details.DueDayAnchor = domain.DayOfMonthAnchor{} }, wantErr: domain.ErrInvalidDayOfMonthAnchor},
		{name: "limit", mutate: func(params *domain.CreditCardParams) { params.Details.CreditLimit = &zeroMoney }, wantErr: domain.ErrInvalidCreditCardCreditLimit},
		{name: "created at", mutate: func(params *domain.CreditCardParams) { params.CreatedAt = time.Time{} }, wantErr: domain.ErrInvalidCreditCardCreatedAt},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validCreditCardParams(t)
			test.mutate(&params)
			if _, err := domain.NewCreditCard(params); !errors.Is(err, test.wantErr) {
				t.Fatalf("NewCreditCard() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestCreditCardArchiveIsTerminalAndImmutable(t *testing.T) {
	active := mustCreditCard(t)
	archivedAt := active.CreatedAt().Add(2*time.Hour + 321*time.Nanosecond)
	archived, err := active.Archive(archivedAt)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if active.Status() != domain.CreditCardStatusActive {
		t.Fatal("Archive() mutated receiver")
	}
	if archived.Status() != domain.CreditCardStatusArchived {
		t.Fatalf("Status() = %q", archived.Status())
	}
	storedAt, exists := archived.ArchivedAt()
	if !exists || storedAt != archivedAt.UTC() {
		t.Fatalf("ArchivedAt() = %v/%v", storedAt, exists)
	}
	if _, err := archived.Archive(archivedAt.Add(time.Hour)); !errors.Is(err, domain.ErrCreditCardAlreadyArchived) {
		t.Fatalf("second Archive() error = %v", err)
	}
}

func TestCreditCardArchiveRejectsInvalidTimeAndZeroValue(t *testing.T) {
	active := mustCreditCard(t)
	for _, at := range []time.Time{{}, active.CreatedAt().Add(-time.Nanosecond)} {
		if _, err := active.Archive(at); !errors.Is(err, domain.ErrInvalidCreditCardArchivedAt) {
			t.Fatalf("Archive(%v) error = %v", at, err)
		}
	}
	if _, err := active.Archive(active.CreatedAt()); err != nil {
		t.Fatalf("Archive(at creation) error = %v", err)
	}
	if _, err := (domain.CreditCard{}).Archive(time.Now()); !errors.Is(err, domain.ErrInvalidCreditCardState) {
		t.Fatalf("zero Archive() error = %v", err)
	}
}

func TestRehydrateCreditCardValidatesLifecycleState(t *testing.T) {
	params := validCreditCardParams(t)
	archivedAt := params.CreatedAt.Add(time.Hour)
	archived, err := domain.RehydrateCreditCard(domain.CreditCardRehydrationParams{
		ID:         params.ID,
		Details:    params.Details,
		Status:     domain.CreditCardStatusArchived,
		CreatedAt:  params.CreatedAt,
		ArchivedAt: &archivedAt,
	})
	if err != nil || archived.Status() != domain.CreditCardStatusArchived {
		t.Fatalf("RehydrateCreditCard(valid archived) = %q, %v", archived.Status(), err)
	}

	tests := []struct {
		name       string
		status     domain.CreditCardStatus
		archivedAt *time.Time
		wantErr    error
	}{
		{name: "active with archived at", status: domain.CreditCardStatusActive, archivedAt: &archivedAt, wantErr: domain.ErrInvalidCreditCardState},
		{name: "archived without time", status: domain.CreditCardStatusArchived, wantErr: domain.ErrInvalidCreditCardState},
		{name: "unknown status", status: "UNKNOWN", wantErr: domain.ErrInvalidCreditCardStatus},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := domain.RehydrateCreditCard(domain.CreditCardRehydrationParams{
				ID:         params.ID,
				Details:    params.Details,
				Status:     test.status,
				CreatedAt:  params.CreatedAt,
				ArchivedAt: test.archivedAt,
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("RehydrateCreditCard() error = %v, want %v", err, test.wantErr)
			}
		})
	}

	before := params.CreatedAt.Add(-time.Nanosecond)
	_, err = domain.RehydrateCreditCard(domain.CreditCardRehydrationParams{
		ID: params.ID, Details: params.Details, Status: domain.CreditCardStatusArchived,
		CreatedAt: params.CreatedAt, ArchivedAt: &before,
	})
	if !errors.Is(err, domain.ErrInvalidCreditCardArchivedAt) {
		t.Fatalf("RehydrateCreditCard(before creation) error = %v", err)
	}
}

func validCreditCardParams(t testing.TB) domain.CreditCardParams {
	t.Helper()
	name, err := domain.NewCreditCardName("Nubank Ultravioleta")
	if err != nil {
		t.Fatalf("NewCreditCardName() error = %v", err)
	}
	lastFour, err := domain.NewCreditCardLastFour("4821")
	if err != nil {
		t.Fatalf("NewCreditCardLastFour() error = %v", err)
	}
	closing, err := domain.NewDayOfMonthAnchor(25)
	if err != nil {
		t.Fatalf("NewDayOfMonthAnchor(closing) error = %v", err)
	}
	due, err := domain.NewDayOfMonthAnchor(2)
	if err != nil {
		t.Fatalf("NewDayOfMonthAnchor(due) error = %v", err)
	}
	limit, err := domain.NewCreditCardCreditLimit(250_000, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewCreditCardCreditLimit() error = %v", err)
	}
	brand := domain.CreditCardBrandMastercard
	return domain.CreditCardParams{
		ID: validCreditCardID,
		Details: domain.CreditCardDetails{
			UserID:           "user-credit-card-synthetic",
			Name:             name,
			LastFour:         &lastFour,
			Brand:            &brand,
			ClosingDayAnchor: closing,
			DueDayAnchor:     due,
			CreditLimit:      &limit,
		},
		CreatedAt: time.Date(2026, time.August, 25, 13, 30, 0, 123_456_000, time.UTC),
	}
}

func mustCreditCard(t testing.TB) domain.CreditCard {
	t.Helper()
	card, err := domain.NewCreditCard(validCreditCardParams(t))
	if err != nil {
		t.Fatalf("NewCreditCard() error = %v", err)
	}
	return card
}
