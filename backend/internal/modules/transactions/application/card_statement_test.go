package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type cardStatementSnapshotReaderFake struct {
	snapshot application.CardStatementSnapshot
	err      error
	calls    int
	query    application.CardStatementSnapshotQuery
}

func (reader *cardStatementSnapshotReaderFake) Read(_ context.Context, query application.CardStatementSnapshotQuery) (application.CardStatementSnapshot, error) {
	reader.calls++
	reader.query = query
	return reader.snapshot, reader.err
}

func TestGetCardStatementCombinesOneTimeAndInstallmentForRequestedCycle(t *testing.T) {
	owner := "statement-owner"
	card := statementCard(t, owner, false)
	oneTime := statementExpense(t, owner, card.ID(), "expense-one-time", "Mercado", 500, civilDate(t, 2026, time.June, 10), time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC))
	installmentExpense := statementExpense(t, owner, card.ID(), "expense-installment", "Notebook", 3000, civilDate(t, 2026, time.May, 10), time.Date(2026, time.May, 3, 10, 0, 0, 0, time.UTC))
	plan := statementPlan(t, owner, card.ID(), installmentExpense.ID(), "ipl_0123456789abcdef0123456789abcdef", 3000, 3, civilDate(t, 2026, time.May, 10))
	reader := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{installmentExpense, oneTime}, InstallmentPlans: []domain.InstallmentPlan{plan}, Found: true}}
	useCase := mustGetCardStatement(t, reader)

	result, err := useCase.Execute(context.Background(), application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.June, 10)})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if reader.calls != 1 || reader.query.OwnerID != owner || reader.query.CreditCardID != card.ID() || !reader.query.StatementDueOn.Equal(civilDate(t, 2026, time.June, 10)) {
		t.Fatalf("snapshot query = %#v, calls=%d", reader.query, reader.calls)
	}
	statement := result.Statement
	if statement.CreditCardID() != card.ID() || !statement.StatementDueOn().Equal(civilDate(t, 2026, time.June, 10)) || statement.TotalAmount().MinorUnits() != 1500 {
		t.Fatalf("statement header = %q/%s/%d", statement.CreditCardID(), statement.StatementDueOn(), statement.TotalAmount().MinorUnits())
	}
	lines := statement.Lines()
	if len(lines) != 2 || lines[0].PurchaseMode() != domain.CardStatementPurchaseModeOneTime || lines[1].PurchaseMode() != domain.CardStatementPurchaseModeInstallment {
		t.Fatalf("statement lines = %#v", lines)
	}
	if lines[0].Amount().MinorUnits() != 500 || lines[1].Amount().MinorUnits() != 1000 {
		t.Fatalf("line amounts = %d/%d", lines[0].Amount().MinorUnits(), lines[1].Amount().MinorUnits())
	}
	number, hasNumber := lines[1].InstallmentNumber()
	count, hasCount := lines[1].InstallmentCount()
	if !hasNumber || number != 2 || !hasCount || count != 3 {
		t.Fatalf("installment metadata = %d/%v/%d/%v", number, hasNumber, count, hasCount)
	}
	if _, ok := lines[0].InstallmentNumber(); ok {
		t.Fatal("one-time line unexpectedly contains installment metadata")
	}
}

func TestGetCardStatementUsesScheduleAndEffectiveSchedule(t *testing.T) {
	owner := "statement-schedule-owner"
	card := statementCard(t, owner, false)
	expense := statementExpense(t, owner, card.ID(), "expense-plan", "Plano", 3000, civilDate(t, 2026, time.May, 10), time.Date(2026, time.May, 4, 10, 0, 0, 0, time.UTC))
	plan := statementPlan(t, owner, card.ID(), expense.ID(), "ipl_fedcba9876543210fedcba9876543210", 3000, 3, civilDate(t, 2026, time.May, 10))

	activeReader := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{expense}, InstallmentPlans: []domain.InstallmentPlan{plan}, Found: true}}
	active := mustGetCardStatement(t, activeReader)
	activeResult, err := active.Execute(context.Background(), application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.June, 10)})
	if err != nil || len(activeResult.Statement.Lines()) != 1 || activeResult.Statement.Lines()[0].Amount().MinorUnits() != 1000 {
		t.Fatalf("active schedule = %#v/%v", activeResult.Statement.Lines(), err)
	}

	cancelled, err := plan.Cancel(civilDate(t, 2026, time.June, 15))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		due  domain.CivilDate
		want int
	}{
		{"cutoff equality included", civilDate(t, 2026, time.June, 10), 1},
		{"after cutoff excluded", civilDate(t, 2026, time.July, 10), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{expense}, InstallmentPlans: []domain.InstallmentPlan{cancelled}, Found: true}}
			useCase := mustGetCardStatement(t, reader)
			result, err := useCase.Execute(context.Background(), application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: test.due})
			if err != nil || len(result.Statement.Lines()) != test.want {
				t.Fatalf("cancelled schedule = %#v/%v", result.Statement.Lines(), err)
			}
		})
	}
}

func TestGetCardStatementAllowsArchivedCardAndNeverReadsRecurrence(t *testing.T) {
	owner := "statement-archive-owner"
	card := statementCard(t, owner, true)
	expense := statementExpense(t, owner, card.ID(), "expense-archived-card", "Compra", 900, civilDate(t, 2026, time.August, 10), time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC))
	reader := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{expense}, Found: true}}
	result, err := mustGetCardStatement(t, reader).Execute(context.Background(), application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.August, 10)})
	if err != nil {
		t.Fatalf("archived card error = %v", err)
	}
	if len(result.Statement.Lines()) != 1 || result.Statement.Lines()[0].Amount().MinorUnits() != 900 {
		t.Fatalf("archived card lines = %#v", result.Statement.Lines())
	}
	// CardStatementSnapshot deliberately has no Recurrence field or recurrence
	// reader, so recurring commitments cannot become statement lines.
}

func TestGetCardStatementReturnsEmptyCycleWithExactZero(t *testing.T) {
	owner := "statement-empty-owner"
	card := statementCard(t, owner, false)
	reader := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Found: true}}
	result, err := mustGetCardStatement(t, reader).Execute(context.Background(), application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.September, 10)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Statement.Lines() == nil || len(result.Statement.Lines()) != 0 || result.Statement.TotalAmount().MinorUnits() != 0 {
		t.Fatalf("empty statement = %#v/%d", result.Statement.Lines(), result.Statement.TotalAmount().MinorUnits())
	}
}

func TestGetCardStatementExcludesOneTimeOutsideCycleAndOtherInstallmentCycles(t *testing.T) {
	owner := "statement-boundary-owner"
	card := statementCard(t, owner, false)
	oneTime := statementExpense(t, owner, card.ID(), "expense-september", "Compra", 900, civilDate(t, 2026, time.September, 10), time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC))
	planExpense := statementExpense(t, owner, card.ID(), "expense-plan-boundary", "Plano", 3000, civilDate(t, 2026, time.May, 10), time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC))
	plan := statementPlan(t, owner, card.ID(), planExpense.ID(), "ipl_22222222222222222222222222222222", 3000, 3, civilDate(t, 2026, time.May, 10))
	reader := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{oneTime, planExpense}, InstallmentPlans: []domain.InstallmentPlan{plan}, Found: true}}
	result, err := mustGetCardStatement(t, reader).Execute(context.Background(), application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.June, 10)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Statement.Lines()) != 1 || result.Statement.Lines()[0].ExpenseID() != planExpense.ID() {
		t.Fatalf("out-of-cycle lines = %#v", result.Statement.Lines())
	}
}

func TestGetCardStatementRejectsInvalidInputBeforeSnapshot(t *testing.T) {
	card := statementCard(t, "statement-valid-owner", false)
	reader := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Found: true}}
	useCase := mustGetCardStatement(t, reader)
	cases := []application.GetCardStatementInput{
		{OwnerID: "", CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.January, 10)},
		{OwnerID: "statement-valid-owner", CreditCardID: "card_invalid", StatementDueOn: civilDate(t, 2026, time.January, 10)},
		{OwnerID: "statement-valid-owner", CreditCardID: card.ID(), StatementDueOn: domain.CivilDate{}},
	}
	for _, input := range cases {
		if _, err := useCase.Execute(context.Background(), input); err == nil {
			t.Fatalf("invalid input unexpectedly succeeded: %#v", input)
		}
	}
	if reader.calls != 0 {
		t.Fatalf("invalid input reached snapshot reader: %d", reader.calls)
	}
}

func TestGetCardStatementRejectsNotFoundOwnerMismatchDuplicatesAndMissingExpense(t *testing.T) {
	owner := "statement-validation-owner"
	card := statementCard(t, owner, false)
	input := application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.October, 10)}
	missing := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{Found: false}}
	if _, err := mustGetCardStatement(t, missing).Execute(context.Background(), input); !errors.Is(err, application.ErrCardStatementCreditCardNotFound) {
		t.Fatalf("not found error = %v", err)
	}

	otherCard := statementCard(t, "other-owner", false)
	ownerMismatch := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: otherCard, Found: true}}
	if _, err := mustGetCardStatement(t, ownerMismatch).Execute(context.Background(), input); !errors.Is(err, application.ErrCardStatementDependency) {
		t.Fatalf("owner mismatch error = %v", err)
	}

	expense := statementExpense(t, owner, card.ID(), "expense-duplicate", "Compra", 100, input.StatementDueOn, time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC))
	duplicate := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{expense, expense}, Found: true}}
	if _, err := mustGetCardStatement(t, duplicate).Execute(context.Background(), input); !errors.Is(err, application.ErrCardStatementDuplicate) {
		t.Fatalf("duplicate error = %v", err)
	}

	plan := statementPlan(t, owner, card.ID(), "expense-missing", "ipl_11111111111111111111111111111111", 200, 2, input.StatementDueOn)
	missingExpense := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, InstallmentPlans: []domain.InstallmentPlan{plan}, Found: true}}
	if _, err := mustGetCardStatement(t, missingExpense).Execute(context.Background(), input); !errors.Is(err, application.ErrCardStatementDependency) {
		t.Fatalf("missing expense error = %v", err)
	}
}

func TestGetCardStatementSanitizesDependencyFailuresAndPreservesContext(t *testing.T) {
	marker := "SQL_SECRET_STATEMENT"
	reader := &cardStatementSnapshotReaderFake{err: errors.New(marker)}
	card := statementCard(t, "statement-error-owner", false)
	input := application.GetCardStatementInput{OwnerID: card.UserID(), CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.November, 10)}
	_, err := mustGetCardStatement(t, reader).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCardStatementSnapshotQuery) || strings.Contains(err.Error(), marker) {
		t.Fatalf("sanitized error = %v", err)
	}

	for _, expected := range []error{context.Canceled, context.DeadlineExceeded} {
		reader := &cardStatementSnapshotReaderFake{err: expected}
		if _, err := mustGetCardStatement(t, reader).Execute(context.Background(), input); !errors.Is(err, expected) {
			t.Fatalf("context error = %v, want %v", err, expected)
		}
	}
}

func TestGetCardStatementRejectsCorruptPlanAndExpenseSnapshots(t *testing.T) {
	owner := "statement-corruption-owner"
	card := statementCard(t, owner, false)
	input := application.GetCardStatementInput{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: civilDate(t, 2026, time.December, 10)}
	corruptPlan := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, InstallmentPlans: []domain.InstallmentPlan{{}}, Found: true}}
	if _, err := mustGetCardStatement(t, corruptPlan).Execute(context.Background(), input); !errors.Is(err, application.ErrCardStatementDependency) {
		t.Fatalf("corrupt plan error = %v", err)
	}
	corruptExpense := &cardStatementSnapshotReaderFake{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{{}}, Found: true}}
	if _, err := mustGetCardStatement(t, corruptExpense).Execute(context.Background(), input); !errors.Is(err, application.ErrCardStatementDependency) {
		t.Fatalf("corrupt expense error = %v", err)
	}
}

func mustGetCardStatement(t *testing.T, reader application.CardStatementSnapshotReader) *application.GetCardStatement {
	t.Helper()
	useCase, err := application.NewGetCardStatement(reader)
	if err != nil {
		t.Fatal(err)
	}
	return useCase
}

func statementCard(t *testing.T, owner string, archived bool) domain.CreditCard {
	t.Helper()
	name, err := domain.NewCreditCardName("Cartão principal")
	if err != nil {
		t.Fatal(err)
	}
	closing, err := domain.NewDayOfMonthAnchor(5)
	if err != nil {
		t.Fatal(err)
	}
	due, err := domain.NewDayOfMonthAnchor(10)
	if err != nil {
		t.Fatal(err)
	}
	card, err := domain.NewCreditCard(domain.CreditCardParams{ID: "card_0123456789abcdef0123456789abcdef", Details: domain.CreditCardDetails{UserID: owner, Name: name, ClosingDayAnchor: closing, DueDayAnchor: due}, CreatedAt: time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if archived {
		card, err = card.Archive(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
	}
	return card
}

func statementExpense(t *testing.T, owner, cardID, id, description string, amountMinor int64, dueOn domain.CivilDate, occurredAt time.Time) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(amountMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{ID: id, Details: domain.ExpenseDetails{UserID: owner, Description: description, Amount: amount, PaymentMethod: domain.PaymentMethodCredit, CreditCardID: &cardID, StatementDueOn: &dueOn, OccurredAt: occurredAt, FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS}, CreatedAt: time.Date(2025, time.January, 2, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return expense
}

func statementPlan(t *testing.T, owner, cardID, expenseID, planID string, totalMinor int64, count int, firstDue domain.CivilDate) domain.InstallmentPlan {
	t.Helper()
	amount, err := domain.NewMoney(totalMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := domain.NewDayOfMonthAnchor(firstDue.Day())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewInstallmentPlan(domain.InstallmentPlanParams{ID: planID, OwnerID: owner, CreditCardID: cardID, ExpenseID: expenseID, TotalAmount: amount, InstallmentCount: count, FirstDueDate: firstDue, DueDayAnchor: anchor, CreatedAt: time.Date(2025, time.January, 3, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func civilDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
