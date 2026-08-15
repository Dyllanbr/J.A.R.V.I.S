package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestNewIncomeCreatesRecordedIncome(t *testing.T) {
	params := validIncomeParams(t)
	params.Details.Description = "  Synthetic salary  "
	params.Details.OccurredAt = time.Date(2026, time.August, 14, 8, 30, 0, 123_456_789, time.FixedZone("synthetic", -3*60*60))
	params.CreatedAt = time.Date(2026, time.August, 14, 9, 45, 0, 987_654_321, time.FixedZone("synthetic", 2*60*60))

	income, err := domain.NewIncome(params)
	if err != nil {
		t.Fatalf("NewIncome() error = %v", err)
	}

	if income.ID() != params.ID || income.UserID() != params.Details.UserID {
		t.Fatal("NewIncome() did not preserve identifiers")
	}
	if income.Type() != domain.TransactionTypeIncome {
		t.Fatalf("Type() = %q, want %q", income.Type(), domain.TransactionTypeIncome)
	}
	if income.Description() != "Synthetic salary" {
		t.Fatalf("Description() = %q, want trimmed description", income.Description())
	}
	if !income.Amount().Equal(params.Details.Amount) || income.Amount().MinorUnits() <= 0 {
		t.Fatal("Amount() did not preserve a positive Money value")
	}
	if income.Origin() != params.Details.Origin || income.FinancialTimezone() != params.Details.FinancialTimezone {
		t.Fatal("NewIncome() did not preserve origin or financial timezone")
	}
	if !income.OccurredAt().Equal(params.Details.OccurredAt) || income.OccurredAt().Location() != time.UTC {
		t.Fatal("OccurredAt() did not preserve the instant normalized to UTC")
	}
	if income.Status() != domain.IncomeStatusRecorded || income.Version() != 1 {
		t.Fatal("NewIncome() did not apply RECORDED and version 1")
	}
	if !income.CreatedAt().Equal(params.CreatedAt) || !income.UpdatedAt().Equal(params.CreatedAt) {
		t.Fatal("NewIncome() did not initialize timestamps from CreatedAt")
	}
	if income.CreatedAt().Location() != time.UTC || income.UpdatedAt().Location() != time.UTC {
		t.Fatal("NewIncome() timestamps are not normalized to UTC")
	}
}

func TestNormalizeIncomeDetailsReturnsCanonicalPreviewData(t *testing.T) {
	details := validIncomeParams(t).Details
	details.Description = "\u00a0 Receita sintética \u00a0"
	details.OccurredAt = time.Date(2026, time.August, 14, 12, 0, 0, 123_456_789, time.FixedZone("synthetic", -3*60*60))

	normalized, err := domain.NormalizeIncomeDetails(details)
	if err != nil {
		t.Fatalf("NormalizeIncomeDetails() error = %v", err)
	}
	if normalized.Description != "Receita sintética" {
		t.Fatalf("description = %q", normalized.Description)
	}
	if normalized.OccurredAt.Location() != time.UTC || normalized.OccurredAt.Nanosecond() != 123_456_789 {
		t.Fatal("domain normalization must preserve the instant and nanoseconds while using UTC")
	}
	if err := domain.ValidateIncomeDetails(normalized); err != nil {
		t.Fatalf("ValidateIncomeDetails() error = %v", err)
	}
}

func TestNewIncomeAcceptsSupportedOrigins(t *testing.T) {
	for _, origin := range []domain.Origin{domain.OriginIOS, domain.OriginWhatsApp} {
		t.Run(string(origin), func(t *testing.T) {
			params := validIncomeParams(t)
			params.Details.Origin = origin
			if _, err := domain.NewIncome(params); err != nil {
				t.Fatalf("NewIncome() error = %v", err)
			}
		})
	}
}

func TestNewIncomeValidatesInvariants(t *testing.T) {
	negativeAmount := mustMoney(t, -1)
	tests := []struct {
		name    string
		mutate  func(*domain.IncomeParams)
		wantErr error
	}{
		{name: "empty id", mutate: func(params *domain.IncomeParams) { params.ID = "" }, wantErr: domain.ErrInvalidIncomeID},
		{name: "id with surrounding spaces", mutate: func(params *domain.IncomeParams) { params.ID = " income-id " }, wantErr: domain.ErrInvalidIncomeID},
		{name: "id above byte limit", mutate: func(params *domain.IncomeParams) { params.ID = strings.Repeat("a", domain.MaxIdentifierBytes+1) }, wantErr: domain.ErrInvalidIncomeID},
		{name: "id with control", mutate: func(params *domain.IncomeParams) { params.ID = "income\nid" }, wantErr: domain.ErrInvalidIncomeID},
		{name: "id with invalid UTF-8", mutate: func(params *domain.IncomeParams) { params.ID = string([]byte{0xff}) }, wantErr: domain.ErrInvalidIncomeID},
		{name: "empty user id", mutate: func(params *domain.IncomeParams) { params.Details.UserID = "" }, wantErr: domain.ErrInvalidIncomeUserID},
		{name: "user id with surrounding spaces", mutate: func(params *domain.IncomeParams) { params.Details.UserID = " user-id " }, wantErr: domain.ErrInvalidIncomeUserID},
		{name: "user id above byte limit", mutate: func(params *domain.IncomeParams) {
			params.Details.UserID = strings.Repeat("b", domain.MaxIdentifierBytes+1)
		}, wantErr: domain.ErrInvalidIncomeUserID},
		{name: "user id with control", mutate: func(params *domain.IncomeParams) { params.Details.UserID = "user\tid" }, wantErr: domain.ErrInvalidIncomeUserID},
		{name: "empty description", mutate: func(params *domain.IncomeParams) { params.Details.Description = "" }, wantErr: domain.ErrInvalidIncomeDescription},
		{name: "blank description", mutate: func(params *domain.IncomeParams) { params.Details.Description = " \t\n " }, wantErr: domain.ErrInvalidIncomeDescription},
		{name: "description above limit", mutate: func(params *domain.IncomeParams) {
			params.Details.Description = strings.Repeat("á", domain.MaxIncomeDescriptionRunes+1)
		}, wantErr: domain.ErrInvalidIncomeDescription},
		{name: "invalid UTF-8 description", mutate: func(params *domain.IncomeParams) { params.Details.Description = string([]byte{0xff}) }, wantErr: domain.ErrInvalidIncomeDescription},
		{name: "zero amount", mutate: func(params *domain.IncomeParams) { params.Details.Amount = domain.Money{} }, wantErr: domain.ErrInvalidIncomeAmount},
		{name: "negative amount", mutate: func(params *domain.IncomeParams) { params.Details.Amount = negativeAmount }, wantErr: domain.ErrInvalidIncomeAmount},
		{name: "zero occurred at", mutate: func(params *domain.IncomeParams) { params.Details.OccurredAt = time.Time{} }, wantErr: domain.ErrInvalidIncomeOccurredAt},
		{name: "empty timezone", mutate: func(params *domain.IncomeParams) { params.Details.FinancialTimezone = "" }, wantErr: domain.ErrInvalidIncomeFinancialTimezone},
		{name: "invalid timezone", mutate: func(params *domain.IncomeParams) { params.Details.FinancialTimezone = "Synthetic/Invalid" }, wantErr: domain.ErrInvalidIncomeFinancialTimezone},
		{name: "process local timezone", mutate: func(params *domain.IncomeParams) { params.Details.FinancialTimezone = "Local" }, wantErr: domain.ErrInvalidIncomeFinancialTimezone},
		{name: "invalid origin", mutate: func(params *domain.IncomeParams) { params.Details.Origin = domain.Origin("SYNTHETIC") }, wantErr: domain.ErrInvalidIncomeOrigin},
		{name: "zero created at", mutate: func(params *domain.IncomeParams) { params.CreatedAt = time.Time{} }, wantErr: domain.ErrInvalidIncomeCreatedAt},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validIncomeParams(t)
			test.mutate(&params)
			_, err := domain.NewIncome(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewIncome() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestNewIncomePreservesValidUnicodeAndInternalWhitespace(t *testing.T) {
	tests := []string{
		"Receita sintética com café",
		"Receita sintética 💰",
		"Cafe\u0301 sintético",
		"Receita\t  sintética",
	}
	for _, description := range tests {
		t.Run(description, func(t *testing.T) {
			params := validIncomeParams(t)
			params.Details.Description = "  " + description + "  "
			income, err := domain.NewIncome(params)
			if err != nil {
				t.Fatalf("NewIncome() error = %v", err)
			}
			if income.Description() != description {
				t.Fatalf("Description() = %q, want %q", income.Description(), description)
			}
		})
	}
}

func TestNewIncomeAcceptsDescriptionAndIdentifiersAtLimits(t *testing.T) {
	params := validIncomeParams(t)
	params.ID = strings.Repeat("a", domain.MaxIdentifierBytes)
	params.Details.UserID = strings.Repeat("á", domain.MaxIdentifierBytes/len("á"))
	params.Details.Description = strings.Repeat("á", domain.MaxIncomeDescriptionRunes)

	income, err := domain.NewIncome(params)
	if err != nil {
		t.Fatalf("NewIncome() error = %v", err)
	}
	if len(income.ID()) != domain.MaxIdentifierBytes || len(income.UserID()) != domain.MaxIdentifierBytes {
		t.Fatal("NewIncome() did not preserve identifiers at the byte limit")
	}
	if utf8.RuneCountInString(income.Description()) != domain.MaxIncomeDescriptionRunes {
		t.Fatal("NewIncome() did not preserve the description at the character limit")
	}
}

func TestIncomeUsesMoneyCurrencyInvariant(t *testing.T) {
	_, err := domain.NewMoney(4250, domain.Currency("USD"))
	if !errors.Is(err, domain.ErrUnsupportedCurrency) {
		t.Fatalf("NewMoney() error = %v, want ErrUnsupportedCurrency", err)
	}
}

func TestNewIncomeErrorsDoNotExposeRawInput(t *testing.T) {
	const marker = "SUPER_SECRET_INCOME_MARKER_98431"
	tests := []struct {
		name    string
		mutate  func(*domain.IncomeParams)
		wantErr error
	}{
		{name: "id", mutate: func(params *domain.IncomeParams) { params.ID = marker + "\n" }, wantErr: domain.ErrInvalidIncomeID},
		{name: "user", mutate: func(params *domain.IncomeParams) { params.Details.UserID = marker + "\n" }, wantErr: domain.ErrInvalidIncomeUserID},
		{name: "description", mutate: func(params *domain.IncomeParams) { params.Details.Description = strings.Repeat(marker, 30) }, wantErr: domain.ErrInvalidIncomeDescription},
		{name: "timezone", mutate: func(params *domain.IncomeParams) { params.Details.FinancialTimezone = marker }, wantErr: domain.ErrInvalidIncomeFinancialTimezone},
		{name: "origin", mutate: func(params *domain.IncomeParams) { params.Details.Origin = domain.Origin(marker) }, wantErr: domain.ErrInvalidIncomeOrigin},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validIncomeParams(t)
			test.mutate(&params)
			_, err := domain.NewIncome(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewIncome() error = %v, want %v", err, test.wantErr)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatal("NewIncome() exposed raw invalid input")
			}
		})
	}
}

func validIncomeParams(t testing.TB) domain.IncomeParams {
	t.Helper()
	return domain.IncomeParams{
		ID: "income-synthetic-001",
		Details: domain.IncomeDetails{
			UserID:            "user-synthetic-001",
			Description:       "Synthetic income",
			Amount:            mustMoney(t, 725000),
			OccurredAt:        time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC),
			FinancialTimezone: "America/Sao_Paulo",
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC),
	}
}
