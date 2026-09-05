//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestCardStatementPostgresSnapshotBuildsOneTimeAndInstallmentReadOnly(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const ownerA = "usr_statement_postgres_a"
	const ownerB = "usr_statement_postgres_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	cardRepository := newCreditCardRepository(t, pool)
	card := createCreditCardFixture(t, ctx, cardRepository, ownerA, creditCardTestID('b'), "statement-card-create")
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewCardPurchaseRepository() error = %v", err)
	}

	occurredAt := time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC)
	clockNow := time.Date(2026, time.August, 25, 15, 0, 0, 123_456_000, time.UTC)
	record := func(input application.RecordCardPurchaseInput, expenseID, planID string) application.RecordCardPurchaseResult {
		t.Helper()
		useCase, constructorErr := application.NewRecordCardPurchase(
			purchaseRepository,
			purchaseRepository,
			cardRepository,
			nil,
			fixedCardPurchaseExpenseIDGenerator{id: expenseID},
			&fixedCardPurchasePlanIDGenerator{id: planID},
			fixedCardPurchaseClock{now: clockNow},
		)
		if constructorErr != nil {
			t.Fatalf("NewRecordCardPurchase() error = %v", constructorErr)
		}
		result, executeErr := useCase.Execute(ctx, input)
		if executeErr != nil {
			t.Fatalf("RecordCardPurchase.Execute() error = %v", executeErr)
		}
		return result
	}

	oneTime := record(application.RecordCardPurchaseInput{
		Purchase: application.CardPurchaseInput{
			UserID: ownerA, Description: "Compra à vista", AmountMinor: 10000,
			Currency: domain.CurrencyBRL, OccurredAt: occurredAt, CreditCardID: card.ID(), Origin: domain.OriginIOS,
		},
		IdempotencyKey: "statement-one-time",
	}, "exp_00000000000000000000000000000081", "")
	if oneTime.InstallmentPlan != nil {
		t.Fatal("one-time purchase unexpectedly created an installment plan")
	}

	count := 3
	installment := record(application.RecordCardPurchaseInput{
		Purchase: application.CardPurchaseInput{
			UserID: ownerA, Description: "Compra parcelada", AmountMinor: 10100,
			Currency: domain.CurrencyBRL, OccurredAt: occurredAt, CreditCardID: card.ID(), InstallmentCount: &count, Origin: domain.OriginIOS,
		},
		IdempotencyKey: "statement-installment",
	}, "exp_00000000000000000000000000000082", "ipl_00000000000000000000000000000082")
	if installment.InstallmentPlan == nil {
		t.Fatal("installment purchase did not create a plan")
	}
	statementDueOn, ok := oneTime.Expense.StatementDueOn()
	if !ok {
		t.Fatal("one-time expense has no statement due date")
	}

	reader, err := adapter.NewCardStatementRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewCardStatementRepository() error = %v", err)
	}
	getStatement, err := application.NewGetCardStatement(reader)
	if err != nil {
		t.Fatalf("NewGetCardStatement() error = %v", err)
	}

	before := statementFinancialCounts(t, ctx, pool)
	result, err := getStatement.Execute(ctx, application.GetCardStatementInput{
		OwnerID: ownerA, CreditCardID: card.ID(), StatementDueOn: statementDueOn,
	})
	if err != nil {
		t.Fatalf("GetCardStatement.Execute() error = %v", err)
	}
	lines := result.Statement.Lines()
	if len(lines) != 2 || result.Statement.TotalAmount().MinorUnits() != 13366 {
		t.Fatalf("statement lines/total = %d/%d, want two lines and 13366 minor units", len(lines), result.Statement.TotalAmount().MinorUnits())
	}
	if lines[0].PurchaseMode() != domain.CardStatementPurchaseModeOneTime || lines[0].Amount().MinorUnits() != 10000 {
		t.Fatalf("one-time statement line = %#v", lines[0])
	}
	installmentNumber, hasNumber := lines[1].InstallmentNumber()
	if lines[1].PurchaseMode() != domain.CardStatementPurchaseModeInstallment || lines[1].Amount().MinorUnits() != 3366 || !hasNumber || installmentNumber != 1 {
		t.Fatalf("installment statement line = %#v/%d/%v", lines[1], installmentNumber, hasNumber)
	}
	if after := statementFinancialCounts(t, ctx, pool); after != before {
		t.Fatalf("statement read changed financial counts: before=%v after=%v", before, after)
	}

	// A card that has since been archived remains readable for its historical
	// statement. The read still uses the persisted card snapshot and never
	// requires ACTIVE status.
	archivedAt := time.Date(2026, time.August, 31, 16, 0, 0, 0, time.UTC)
	if _, err := newArchiveCreditCardUseCase(t, cardRepository, fixedCreditCardClock{now: archivedAt}).Execute(ctx, application.ArchiveCreditCardInput{
		UserID: ownerA, CreditCardID: card.ID(), IdempotencyKey: "statement-card-archive",
	}); err != nil {
		t.Fatalf("archiving statement card failed: %v", err)
	}
	archivedResult, err := getStatement.Execute(ctx, application.GetCardStatementInput{OwnerID: ownerA, CreditCardID: card.ID(), StatementDueOn: statementDueOn})
	if err != nil || len(archivedResult.Statement.Lines()) != 2 {
		t.Fatalf("archived card statement = %#v, error = %v", archivedResult.Statement.Lines(), err)
	}

	// Cancellation changes only the effective schedule. The original plan is
	// still rehydrated by the reader, while an installment after the cutoff is
	// absent from the derived statement.
	cancelOn, err := domain.NewCivilDate(2026, time.September, 15)
	if err != nil {
		t.Fatal(err)
	}
	cancelUseCase, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, fixedCardPurchaseFinancialDate{date: cancelOn})
	if err != nil {
		t.Fatalf("NewCancelInstallmentPlan() error = %v", err)
	}
	if _, err := cancelUseCase.Execute(ctx, application.CancelInstallmentPlanInput{
		OwnerID: ownerA, PlanID: installment.InstallmentPlan.ID(), ExpectedCancelledOn: cancelOn, IdempotencyKey: "statement-plan-cancel",
	}); err != nil {
		t.Fatalf("cancelling statement plan failed: %v", err)
	}
	laterDue, err := domain.NewCivilDate(2026, time.November, 10)
	if err != nil {
		t.Fatal(err)
	}
	later, err := getStatement.Execute(ctx, application.GetCardStatementInput{OwnerID: ownerA, CreditCardID: card.ID(), StatementDueOn: laterDue})
	if err != nil {
		t.Fatalf("cancelled statement read error = %v", err)
	}
	if len(later.Statement.Lines()) != 0 || later.Statement.TotalAmount().MinorUnits() != 0 {
		t.Fatalf("cancelled plan leaked a post-cutoff installment: %#v", later.Statement.Lines())
	}

	// The same card ID under another owner is indistinguishable from not found.
	other, err := getStatement.Execute(ctx, application.GetCardStatementInput{OwnerID: ownerB, CreditCardID: card.ID(), StatementDueOn: statementDueOn})
	if !errors.Is(err, application.ErrCardStatementCreditCardNotFound) || !isZeroCardStatementResult(other) {
		t.Fatalf("cross-owner statement = %#v, error = %v", other, err)
	}
}

func TestCardStatementPostgresReaderPreservesContextCancellationAndRejectsInvalidQueries(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	owner := "usr_statement_context_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	card := createCreditCardFixture(t, ctx, newCreditCardRepository(t, pool), owner, creditCardTestID('c'), "statement-context-card")
	reader, err := adapter.NewCardStatementRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	validDate, err := domain.NewCivilDate(2026, time.September, 10)
	if err != nil {
		t.Fatal(err)
	}
	query := application.CardStatementSnapshotQuery{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: validDate}
	emptySnapshot, err := reader.Read(ctx, query)
	if err != nil || !emptySnapshot.Found || emptySnapshot.Expenses == nil || len(emptySnapshot.Expenses) != 0 || emptySnapshot.InstallmentPlans == nil || len(emptySnapshot.InstallmentPlans) != 0 {
		t.Fatalf("empty card snapshot = %+v, error = %v", emptySnapshot, err)
	}
	for _, expected := range []error{context.Canceled, context.DeadlineExceeded} {
		cancelled := context.Background()
		if expected == context.Canceled {
			var cancelContext context.CancelFunc
			cancelled, cancelContext = context.WithCancel(cancelled)
			cancelContext()
		} else {
			var deadlineCancel context.CancelFunc
			cancelled, deadlineCancel = context.WithDeadline(cancelled, time.Now().Add(-time.Second))
			deadlineCancel()
		}
		if _, readErr := reader.Read(cancelled, query); !errors.Is(readErr, expected) {
			t.Fatalf("Read() context error = %v, want %v", readErr, expected)
		}
	}
	for _, invalid := range []application.CardStatementSnapshotQuery{
		{OwnerID: "", CreditCardID: card.ID(), StatementDueOn: validDate},
		{OwnerID: owner, CreditCardID: "card_invalid", StatementDueOn: validDate},
		{OwnerID: owner, CreditCardID: card.ID(), StatementDueOn: domain.CivilDate{}},
	} {
		if _, readErr := reader.Read(context.Background(), invalid); !errors.Is(readErr, adapter.ErrInvalidCardStatementQuery) {
			t.Fatalf("invalid query error = %v", readErr)
		}
	}
}

type statementCounts struct {
	transactions, audits, legacyIdempotency         int
	plans, planAudits, cardPurchases, cancellations int
}

func statementFinancialCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) statementCounts {
	t.Helper()
	// This helper is intentionally kept local to the integration test. The
	// production reader cannot write, so every count must remain unchanged.
	queries := []string{
		"SELECT count(*) FROM transactions",
		"SELECT count(*) FROM audit_events",
		"SELECT count(*) FROM idempotency_records",
		"SELECT count(*) FROM installment_plans",
		"SELECT count(*) FROM installment_plan_audit_events",
		"SELECT count(*) FROM card_purchase_idempotency_records",
		"SELECT count(*) FROM installment_plan_idempotency_records",
	}
	values := make([]int, len(queries))
	for index, query := range queries {
		if err := pool.QueryRow(ctx, query).Scan(&values[index]); err != nil {
			t.Fatalf("statement count query failed: %v", err)
		}
	}
	return statementCounts{
		transactions: values[0], audits: values[1], legacyIdempotency: values[2],
		plans: values[3], planAudits: values[4], cardPurchases: values[5], cancellations: values[6],
	}
}

func isZeroCardStatementResult(result application.GetCardStatementResult) bool {
	return result.Statement.CreditCardID() == "" && len(result.Statement.Lines()) == 0
}
