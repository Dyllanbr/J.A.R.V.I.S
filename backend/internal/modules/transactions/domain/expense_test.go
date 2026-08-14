package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestNewExpenseCreatesRecordedExpense(t *testing.T) {
	params := validExpenseParams(t)
	params.Details.Description = "  Synthetic market purchase  "
	params.Details.OccurredAt = time.Date(2026, time.August, 14, 8, 30, 0, 0, time.FixedZone("synthetic", -3*60*60))
	params.CreatedAt = time.Date(2026, time.August, 14, 9, 45, 0, 0, time.FixedZone("synthetic", 2*60*60))

	expense, err := domain.NewExpense(params)
	if err != nil {
		t.Fatalf("NewExpense() returned an error: %v", err)
	}

	if expense.ID() != params.ID || expense.UserID() != params.Details.UserID {
		t.Fatal("NewExpense() did not preserve its identifiers")
	}
	if expense.Type() != domain.TransactionTypeExpense {
		t.Fatalf("Type() = %q, want %q", expense.Type(), domain.TransactionTypeExpense)
	}
	if expense.Description() != "Synthetic market purchase" {
		t.Fatalf("Description() = %q, want trimmed description", expense.Description())
	}
	if !expense.Amount().Equal(params.Details.Amount) {
		t.Fatal("Amount() did not preserve the exact Money value")
	}
	if expense.PaymentMethod() != params.Details.PaymentMethod || expense.Origin() != params.Details.Origin {
		t.Fatal("NewExpense() did not preserve payment method or origin")
	}
	if !expense.OccurredAt().Equal(params.Details.OccurredAt) || expense.OccurredAt().Location() != time.UTC {
		t.Fatal("OccurredAt() did not preserve the instant normalized to UTC")
	}
	if expense.FinancialTimezone() != "America/Sao_Paulo" {
		t.Fatalf("FinancialTimezone() = %q, want America/Sao_Paulo", expense.FinancialTimezone())
	}
	if expense.Status() != domain.ExpenseStatusRecorded || expense.Version() != 1 {
		t.Fatal("NewExpense() did not apply the initial status and version")
	}
	if !expense.CreatedAt().Equal(params.CreatedAt) || !expense.UpdatedAt().Equal(params.CreatedAt) {
		t.Fatal("NewExpense() did not initialize timestamps from CreatedAt")
	}
	if expense.CreatedAt().Location() != time.UTC || expense.UpdatedAt().Location() != time.UTC {
		t.Fatal("NewExpense() timestamps do not use the UTC location")
	}
}

func TestNewExpenseAcceptsSupportedPaymentMethods(t *testing.T) {
	methods := []domain.PaymentMethod{
		domain.PaymentMethodPIX,
		domain.PaymentMethodDebit,
		domain.PaymentMethodCredit,
		domain.PaymentMethodCash,
	}

	for _, method := range methods {
		t.Run(string(method), func(t *testing.T) {
			params := validExpenseParams(t)
			params.Details.PaymentMethod = method
			if _, err := domain.NewExpense(params); err != nil {
				t.Fatalf("NewExpense() returned an error for %s: %v", method, err)
			}
		})
	}
}

func TestNewExpenseAcceptsSupportedOrigins(t *testing.T) {
	origins := []domain.Origin{domain.OriginIOS, domain.OriginWhatsApp}

	for _, origin := range origins {
		t.Run(string(origin), func(t *testing.T) {
			params := validExpenseParams(t)
			params.Details.Origin = origin
			if _, err := domain.NewExpense(params); err != nil {
				t.Fatalf("NewExpense() returned an error for %s: %v", origin, err)
			}
		})
	}
}

func TestNewExpenseValidatesInvariants(t *testing.T) {
	negativeAmount := mustMoney(t, -1)
	validAmount := mustMoney(t, 4250)

	tests := []struct {
		name    string
		mutate  func(*domain.ExpenseParams)
		wantErr error
	}{
		{name: "empty id", mutate: func(params *domain.ExpenseParams) { params.ID = "" }, wantErr: domain.ErrInvalidExpenseID},
		{name: "id with surrounding spaces", mutate: func(params *domain.ExpenseParams) { params.ID = " expense-id " }, wantErr: domain.ErrInvalidExpenseID},
		{name: "id above byte limit", mutate: func(params *domain.ExpenseParams) { params.ID = strings.Repeat("a", domain.MaxIdentifierBytes+1) }, wantErr: domain.ErrInvalidExpenseID},
		{name: "id with newline", mutate: func(params *domain.ExpenseParams) { params.ID = "expense\nid" }, wantErr: domain.ErrInvalidExpenseID},
		{name: "id with tab", mutate: func(params *domain.ExpenseParams) { params.ID = "expense\tid" }, wantErr: domain.ErrInvalidExpenseID},
		{name: "id with null", mutate: func(params *domain.ExpenseParams) { params.ID = "expense\x00id" }, wantErr: domain.ErrInvalidExpenseID},
		{name: "id with delete control", mutate: func(params *domain.ExpenseParams) { params.ID = "expense\x7fid" }, wantErr: domain.ErrInvalidExpenseID},
		{name: "id with invalid UTF-8", mutate: func(params *domain.ExpenseParams) { params.ID = string([]byte{0xff}) }, wantErr: domain.ErrInvalidExpenseID},
		{name: "empty user id", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = "" }, wantErr: domain.ErrInvalidUserID},
		{name: "user id with surrounding spaces", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = " user-id " }, wantErr: domain.ErrInvalidUserID},
		{name: "user id above byte limit", mutate: func(params *domain.ExpenseParams) {
			params.Details.UserID = strings.Repeat("b", domain.MaxIdentifierBytes+1)
		}, wantErr: domain.ErrInvalidUserID},
		{name: "user id with newline", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = "user\nid" }, wantErr: domain.ErrInvalidUserID},
		{name: "user id with tab", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = "user\tid" }, wantErr: domain.ErrInvalidUserID},
		{name: "user id with null", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = "user\x00id" }, wantErr: domain.ErrInvalidUserID},
		{name: "user id with delete control", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = "user\x7fid" }, wantErr: domain.ErrInvalidUserID},
		{name: "user id with invalid UTF-8", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = string([]byte{0xff}) }, wantErr: domain.ErrInvalidUserID},
		{name: "empty description", mutate: func(params *domain.ExpenseParams) { params.Details.Description = "" }, wantErr: domain.ErrInvalidDescription},
		{name: "blank description", mutate: func(params *domain.ExpenseParams) { params.Details.Description = " \t\n " }, wantErr: domain.ErrInvalidDescription},
		{name: "description above limit", mutate: func(params *domain.ExpenseParams) {
			params.Details.Description = strings.Repeat("á", domain.MaxExpenseDescriptionRunes+1)
		}, wantErr: domain.ErrInvalidDescription},
		{name: "invalid UTF-8 description", mutate: func(params *domain.ExpenseParams) { params.Details.Description = string([]byte{0xff}) }, wantErr: domain.ErrInvalidDescription},
		{name: "zero value money", mutate: func(params *domain.ExpenseParams) { params.Details.Amount = domain.Money{} }, wantErr: domain.ErrInvalidExpenseAmount},
		{name: "negative amount", mutate: func(params *domain.ExpenseParams) { params.Details.Amount = negativeAmount }, wantErr: domain.ErrInvalidExpenseAmount},
		{name: "invalid payment method", mutate: func(params *domain.ExpenseParams) { params.Details.PaymentMethod = domain.PaymentMethod("SYNTHETIC") }, wantErr: domain.ErrInvalidPaymentMethod},
		{name: "zero occurrence time", mutate: func(params *domain.ExpenseParams) { params.Details.OccurredAt = time.Time{} }, wantErr: domain.ErrInvalidOccurredAt},
		{name: "empty timezone", mutate: func(params *domain.ExpenseParams) { params.Details.FinancialTimezone = "" }, wantErr: domain.ErrInvalidFinancialTimezone},
		{name: "invalid timezone", mutate: func(params *domain.ExpenseParams) { params.Details.FinancialTimezone = "Synthetic/Invalid" }, wantErr: domain.ErrInvalidFinancialTimezone},
		{name: "process local timezone", mutate: func(params *domain.ExpenseParams) { params.Details.FinancialTimezone = "Local" }, wantErr: domain.ErrInvalidFinancialTimezone},
		{name: "invalid origin", mutate: func(params *domain.ExpenseParams) { params.Details.Origin = domain.Origin("SYNTHETIC") }, wantErr: domain.ErrInvalidOrigin},
		{name: "zero creation time", mutate: func(params *domain.ExpenseParams) { params.CreatedAt = time.Time{} }, wantErr: domain.ErrInvalidCreatedAt},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validExpenseParamsWithAmount(validAmount)
			test.mutate(&params)

			_, err := domain.NewExpense(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewExpense() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestNewExpenseAcceptsDescriptionAtLimit(t *testing.T) {
	params := validExpenseParams(t)
	params.Details.Description = strings.Repeat("á", domain.MaxExpenseDescriptionRunes)

	expense, err := domain.NewExpense(params)
	if err != nil {
		t.Fatalf("NewExpense() returned an error: %v", err)
	}
	if utf8.RuneCountInString(expense.Description()) != domain.MaxExpenseDescriptionRunes {
		t.Fatal("Description() did not preserve all Unicode characters at the limit")
	}
}

func TestNewExpensePreservesUnicodeDescriptionContent(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{name: "multibyte", description: "Compra sintética com café"},
		{name: "emoji", description: "Compra sintética 🛒"},
		{name: "combining mark", description: "Cafe\u0301 sintético"},
		{name: "emoji ZWJ", description: "Ferramenta sintética 👩‍💻"},
		{name: "internal whitespace", description: "Compra\t  sintética"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validExpenseParams(t)
			params.Details.Description = "  " + test.description + "  "

			expense, err := domain.NewExpense(params)
			if err != nil {
				t.Fatalf("NewExpense() returned an error: %v", err)
			}
			if expense.Description() != test.description {
				t.Fatalf("Description() = %q, want exact internal content %q", expense.Description(), test.description)
			}
		})
	}
}

func TestNewExpenseAcceptsIdentifiersAtByteLimit(t *testing.T) {
	params := validExpenseParams(t)
	params.ID = strings.Repeat("a", domain.MaxIdentifierBytes)
	params.Details.UserID = strings.Repeat("á", domain.MaxIdentifierBytes/len("á"))
	if len(params.Details.UserID) != domain.MaxIdentifierBytes {
		t.Fatal("test setup did not create a multibyte identifier at the byte limit")
	}

	expense, err := domain.NewExpense(params)
	if err != nil {
		t.Fatalf("NewExpense() returned an error: %v", err)
	}
	if expense.ID() != params.ID || expense.UserID() != params.Details.UserID {
		t.Fatal("NewExpense() did not preserve valid identifiers at the byte limit")
	}
}

func TestNewExpenseValidationErrorsDoNotExposeRawInput(t *testing.T) {
	const marker = "SUPER_SECRET_MARKER_98431"

	tests := []struct {
		name    string
		mutate  func(*domain.ExpenseParams)
		wantErr error
	}{
		{name: "description", mutate: func(params *domain.ExpenseParams) {
			params.Details.Description = strings.Repeat(marker, domain.MaxExpenseDescriptionRunes+1)
		}, wantErr: domain.ErrInvalidDescription},
		{name: "payment method", mutate: func(params *domain.ExpenseParams) { params.Details.PaymentMethod = domain.PaymentMethod(marker) }, wantErr: domain.ErrInvalidPaymentMethod},
		{name: "timezone", mutate: func(params *domain.ExpenseParams) { params.Details.FinancialTimezone = marker }, wantErr: domain.ErrInvalidFinancialTimezone},
		{name: "origin", mutate: func(params *domain.ExpenseParams) { params.Details.Origin = domain.Origin(marker) }, wantErr: domain.ErrInvalidOrigin},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validExpenseParams(t)
			test.mutate(&params)

			_, err := domain.NewExpense(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewExpense() error = %v, want %v", err, test.wantErr)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatal("NewExpense() exposed raw invalid input")
			}
		})
	}
}

func TestNewExpenseIdentifierErrorsDoNotExposeRawValues(t *testing.T) {
	const marker = "SUPER_SECRET_MARKER_98431"

	tests := []struct {
		name    string
		mutate  func(*domain.ExpenseParams)
		wantErr error
	}{
		{name: "expense id", mutate: func(params *domain.ExpenseParams) { params.ID = marker + "\n" }, wantErr: domain.ErrInvalidExpenseID},
		{name: "user id", mutate: func(params *domain.ExpenseParams) { params.Details.UserID = marker + "\n" }, wantErr: domain.ErrInvalidUserID},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := validExpenseParams(t)
			test.mutate(&params)

			_, err := domain.NewExpense(params)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("NewExpense() error = %v, want %v", err, test.wantErr)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatal("NewExpense() exposed a raw invalid identifier")
			}
		})
	}
}

func FuzzExpenseDescription(f *testing.F) {
	f.Add("Synthetic expense")
	f.Add("  trimmed description  ")
	f.Add("")
	f.Add(strings.Repeat("á", domain.MaxExpenseDescriptionRunes))
	f.Add(strings.Repeat("a", domain.MaxExpenseDescriptionRunes+1))

	f.Fuzz(func(t *testing.T, description string) {
		params := validExpenseParams(t)
		params.Details.Description = description

		expense, err := domain.NewExpense(params)
		trimmed := strings.TrimSpace(description)
		valid := utf8.ValidString(trimmed) && trimmed != "" && utf8.RuneCountInString(trimmed) <= domain.MaxExpenseDescriptionRunes

		if !valid {
			if !errors.Is(err, domain.ErrInvalidDescription) {
				t.Fatalf("NewExpense() error = %v, want ErrInvalidDescription", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("NewExpense() rejected a valid description: %v", err)
		}
		if expense.Description() != trimmed {
			t.Fatal("NewExpense() changed description content beyond trimming external spaces")
		}
	})
}

func validExpenseParams(t testing.TB) domain.ExpenseParams {
	t.Helper()
	return validExpenseParamsWithAmount(mustMoney(t, 4250))
}

func validExpenseParamsWithAmount(amount domain.Money) domain.ExpenseParams {
	return domain.ExpenseParams{
		ID: "expense-synthetic-001",
		Details: domain.ExpenseDetails{
			UserID:            "user-synthetic-001",
			Description:       "Synthetic expense",
			Amount:            amount,
			PaymentMethod:     domain.PaymentMethodPIX,
			OccurredAt:        time.Date(2026, time.August, 13, 20, 0, 0, 0, time.UTC),
			FinancialTimezone: "America/Sao_Paulo",
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC),
	}
}

func mustMoney(t testing.TB, minorUnits int64) domain.Money {
	t.Helper()
	money, err := domain.NewMoney(minorUnits, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() returned an error: %v", err)
	}
	return money
}
