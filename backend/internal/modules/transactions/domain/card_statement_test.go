package domain_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestNewCardStatementAcceptsEmptyAndOneTimeLines(t *testing.T) {
	cardID := "card_0123456789abcdef0123456789abcdef"
	dueOn := civilStatementDate(t, 2026, time.March, 10)
	zero, err := domain.NewMoney(0, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := domain.NewCardStatement(domain.CardStatementParams{CreditCardID: cardID, StatementDueOn: dueOn, TotalAmount: zero})
	if err != nil {
		t.Fatalf("empty statement error = %v", err)
	}
	if empty.Lines() == nil || len(empty.Lines()) != 0 || !empty.TotalAmount().Equal(zero) {
		t.Fatalf("empty statement = %#v", empty)
	}

	amount, err := domain.NewMoney(1250, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	line, err := domain.NewCardStatementLine(domain.CardStatementLineParams{
		ExpenseID: "expense-one-time", Description: "  Mercado  ", Amount: amount,
		OccurredAt: time.Date(2026, time.February, 2, 15, 4, 5, 0, time.UTC), PurchaseMode: domain.CardStatementPurchaseModeOneTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	statement, err := domain.NewCardStatement(domain.CardStatementParams{CreditCardID: cardID, StatementDueOn: dueOn, TotalAmount: amount, Lines: []domain.CardStatementLine{line}})
	if err != nil {
		t.Fatal(err)
	}
	if statement.Lines()[0].Description() != "Mercado" {
		t.Fatalf("description = %q", statement.Lines()[0].Description())
	}
	if _, ok := statement.Lines()[0].InstallmentNumber(); ok {
		t.Fatal("one-time line unexpectedly has installment number")
	}

	lines := statement.Lines()
	lines[0] = domain.CardStatementLine{}
	if statement.Lines()[0].ExpenseID() != "expense-one-time" {
		t.Fatal("Lines() exposed mutable statement state")
	}
}

func TestCardStatementInstallmentMetadataAndValidation(t *testing.T) {
	amount, err := domain.NewMoney(1000, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	number, count := 2, 3
	valid := domain.CardStatementLineParams{
		ExpenseID: "expense-installment", Description: "Plano", Amount: amount,
		OccurredAt: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC), PurchaseMode: domain.CardStatementPurchaseModeInstallment,
		InstallmentNumber: &number, InstallmentCount: &count,
	}
	line, err := domain.NewCardStatementLine(valid)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := line.InstallmentNumber(); !ok || got != 2 {
		t.Fatalf("installment number = %d/%v", got, ok)
	}
	if got, ok := line.InstallmentCount(); !ok || got != 3 {
		t.Fatalf("installment count = %d/%v", got, ok)
	}

	cases := []struct {
		name   string
		mutate func(*domain.CardStatementLineParams)
		want   error
	}{
		{"one-time with number", func(params *domain.CardStatementLineParams) {
			params.PurchaseMode = domain.CardStatementPurchaseModeOneTime
		}, domain.ErrInvalidCardStatementInstallment},
		{"missing number", func(params *domain.CardStatementLineParams) { params.InstallmentNumber = nil }, domain.ErrInvalidCardStatementInstallment},
		{"missing count", func(params *domain.CardStatementLineParams) { params.InstallmentCount = nil }, domain.ErrInvalidCardStatementInstallment},
		{"count one", func(params *domain.CardStatementLineParams) { value := 1; params.InstallmentCount = &value }, domain.ErrInvalidCardStatementInstallment},
		{"count too large", func(params *domain.CardStatementLineParams) {
			value := domain.MaxInstallmentCount + 1
			params.InstallmentCount = &value
		}, domain.ErrInvalidCardStatementInstallment},
		{"number zero", func(params *domain.CardStatementLineParams) { value := 0; params.InstallmentNumber = &value }, domain.ErrInvalidCardStatementInstallment},
		{"unknown mode", func(params *domain.CardStatementLineParams) { params.PurchaseMode = "UNKNOWN" }, domain.ErrInvalidCardStatementPurchaseMode},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			params := valid
			test.mutate(&params)
			if _, err := domain.NewCardStatementLine(params); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCardStatementSortsDeterministicallyAndRejectsDuplicates(t *testing.T) {
	firstAmount, _ := domain.NewMoney(100, domain.CurrencyBRL)
	secondAmount, _ := domain.NewMoney(200, domain.CurrencyBRL)
	first, err := domain.NewCardStatementLine(domain.CardStatementLineParams{
		ExpenseID: "expense-b", Description: "B", Amount: secondAmount,
		OccurredAt: time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC), PurchaseMode: domain.CardStatementPurchaseModeOneTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.NewCardStatementLine(domain.CardStatementLineParams{
		ExpenseID: "expense-a", Description: "A", Amount: firstAmount,
		OccurredAt: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC), PurchaseMode: domain.CardStatementPurchaseModeOneTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	total, _ := domain.NewMoney(300, domain.CurrencyBRL)
	statement, err := domain.NewCardStatement(domain.CardStatementParams{
		CreditCardID: "card_0123456789abcdef0123456789abcdef", StatementDueOn: civilStatementDate(t, 2026, time.April, 10), TotalAmount: total,
		Lines: []domain.CardStatementLine{first, second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := statement.Lines()[0].ExpenseID(); got != "expense-a" {
		t.Fatalf("first sorted expense = %q", got)
	}

	_, err = domain.NewCardStatement(domain.CardStatementParams{
		CreditCardID: "card_0123456789abcdef0123456789abcdef", StatementDueOn: civilStatementDate(t, 2026, time.April, 10), TotalAmount: total,
		Lines: []domain.CardStatementLine{first, first},
	})
	if !errors.Is(err, domain.ErrCardStatementDuplicateLine) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestCardStatementRejectsInvalidTotalsAndOverflow(t *testing.T) {
	if _, err := domain.NewMoney(1, domain.Currency("USD")); !errors.Is(err, domain.ErrUnsupportedCurrency) {
		t.Fatalf("unsupported currency error = %v", err)
	}
	max, err := domain.NewMoney(math.MaxInt64, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	one, err := domain.NewCardStatementLine(domain.CardStatementLineParams{
		ExpenseID: "expense-max-a", Description: "A", Amount: max,
		OccurredAt: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), PurchaseMode: domain.CardStatementPurchaseModeOneTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	two, err := domain.NewCardStatementLine(domain.CardStatementLineParams{
		ExpenseID: "expense-max-b", Description: "B", Amount: max,
		OccurredAt: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC), PurchaseMode: domain.CardStatementPurchaseModeOneTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = domain.NewCardStatement(domain.CardStatementParams{
		CreditCardID: "card_0123456789abcdef0123456789abcdef", StatementDueOn: civilStatementDate(t, 2026, time.April, 10), TotalAmount: max,
		Lines: []domain.CardStatementLine{one, two},
	})
	if !errors.Is(err, domain.ErrCardStatementAmountOverflow) {
		t.Fatalf("overflow error = %v", err)
	}

	zero, _ := domain.NewMoney(0, domain.CurrencyBRL)
	if _, err := domain.NewCardStatement(domain.CardStatementParams{CreditCardID: "card_invalid", StatementDueOn: civilStatementDate(t, 2026, time.April, 10), TotalAmount: zero}); !errors.Is(err, domain.ErrInvalidCardStatementCreditCardID) {
		t.Fatalf("invalid card error = %v", err)
	}
	if _, err := domain.NewCardStatement(domain.CardStatementParams{CreditCardID: "card_0123456789abcdef0123456789abcdef", StatementDueOn: domain.CivilDate{}, TotalAmount: zero}); !errors.Is(err, domain.ErrInvalidCardStatementDueDate) {
		t.Fatalf("invalid due date error = %v", err)
	}
	wrongTotal, _ := domain.NewMoney(1, domain.CurrencyBRL)
	if _, err := domain.NewCardStatement(domain.CardStatementParams{CreditCardID: "card_0123456789abcdef0123456789abcdef", StatementDueOn: civilStatementDate(t, 2026, time.April, 10), TotalAmount: wrongTotal, Lines: []domain.CardStatementLine{one}}); !errors.Is(err, domain.ErrCardStatementTotalMismatch) {
		t.Fatalf("total mismatch error = %v", err)
	}
}

func civilStatementDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
