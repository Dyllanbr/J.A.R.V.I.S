//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestCardStatementHTTPPostgresUsesRealApplicationSnapshotAndDoesNotWrite(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const ownerA = "usr_card_statement_http_a"
	const ownerB = "usr_card_statement_http_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	cardRepository := newCreditCardRepository(t, pool)
	card := createCreditCardFixture(t, ctx, cardRepository, ownerA, creditCardTestID('b'), "card-statement-http-card")
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	record, err := application.NewRecordCardPurchase(
		purchaseRepository, purchaseRepository, cardRepository, nil,
		&fixedCardPurchaseExpenseIDGenerator{id: "exp_000000000000000000000000000000b1"},
		&fixedCardPurchasePlanIDGenerator{id: "ipl_000000000000000000000000000000b1"},
		fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	const occurredAt = "2026-08-25T14:00:00Z"
	oneTime, err := record.Execute(ctx, application.RecordCardPurchaseInput{
		Purchase: application.CardPurchaseInput{
			UserID: ownerA, Description: "Compra HTTP à vista", AmountMinor: 10000, Currency: domain.CurrencyBRL,
			OccurredAt: mustRFC3339(t, occurredAt), CreditCardID: card.ID(), Origin: domain.OriginIOS,
		},
		IdempotencyKey: "card-statement-http-one-time",
	})
	if err != nil {
		t.Fatalf("one-time purchase = %+v error=%v", oneTime, err)
	}
	if oneTime.InstallmentPlan != nil {
		t.Fatal("one-time purchase unexpectedly created an installment plan")
	}
	count := 3
	recordInstallment, err := application.NewRecordCardPurchase(
		purchaseRepository, purchaseRepository, cardRepository, nil,
		&fixedCardPurchaseExpenseIDGenerator{id: "exp_000000000000000000000000000000b2"},
		&fixedCardPurchasePlanIDGenerator{id: "ipl_000000000000000000000000000000b2"},
		fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	installment, err := recordInstallment.Execute(ctx, application.RecordCardPurchaseInput{
		Purchase: application.CardPurchaseInput{
			UserID: ownerA, Description: "Compra HTTP parcelada", AmountMinor: 10100, Currency: domain.CurrencyBRL,
			OccurredAt: mustRFC3339(t, occurredAt), CreditCardID: card.ID(), InstallmentCount: &count, Origin: domain.OriginIOS,
		},
		IdempotencyKey: "card-statement-http-installment",
	})
	if err != nil || installment.InstallmentPlan == nil {
		t.Fatalf("installment purchase = %+v error=%v", installment, err)
	}

	statementRepository, err := adapter.NewCardStatementRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	getStatement, err := application.NewGetCardStatement(statementRepository)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewCardStatement(ownerA, getStatement).Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	before := cardStatementPersistenceCounts(t, ctx, pool)
	response := getFinancialJSON(t, server.Client(), server.URL+"/v1/credit-cards/"+card.ID()+"/statements/2026-09-10")
	if response.status != http.StatusOK || !bytes.Contains(response.body, []byte(`"creditCardId":"`+card.ID()+`"`)) || !bytes.Contains(response.body, []byte(`"statementDueOn":"2026-09-10"`)) || !bytes.Contains(response.body, []byte(`"minor":13366`)) || !bytes.Contains(response.body, []byte(`"purchaseMode":"ONE_TIME"`)) || !bytes.Contains(response.body, []byte(`"purchaseMode":"INSTALLMENT"`)) {
		t.Fatalf("statement response = %d %s", response.status, response.body)
	}
	if got := cardStatementPersistenceCounts(t, ctx, pool); got != before {
		t.Fatalf("statement read changed persistence from %#v to %#v", before, got)
	}

	archive := newArchiveCreditCardUseCase(t, cardRepository, fixedCreditCardClock{now: time.Date(2026, time.August, 31, 15, 0, 0, 0, time.UTC)})
	if _, err := archive.Execute(ctx, application.ArchiveCreditCardInput{UserID: ownerA, CreditCardID: card.ID(), IdempotencyKey: "card-statement-http-archive"}); err != nil {
		t.Fatal(err)
	}
	archivedResponse := getFinancialJSON(t, server.Client(), server.URL+"/v1/credit-cards/"+card.ID()+"/statements/2026-09-10")
	if archivedResponse.status != http.StatusOK || !bytes.Equal(archivedResponse.body, response.body) {
		t.Fatalf("archived card statement = %d %s, want original response", archivedResponse.status, archivedResponse.body)
	}

	ownerBUseCase, err := application.NewGetCardStatement(statementRepository)
	if err != nil {
		t.Fatal(err)
	}
	ownerBMux := http.NewServeMux()
	httpapi.NewCardStatement(ownerB, ownerBUseCase).Register(ownerBMux)
	ownerBServer := httptest.NewServer(ownerBMux)
	defer ownerBServer.Close()
	afterArchive := cardStatementPersistenceCounts(t, ctx, pool)
	ownerBResponse := getFinancialJSON(t, ownerBServer.Client(), ownerBServer.URL+"/v1/credit-cards/"+card.ID()+"/statements/2026-09-10")
	if ownerBResponse.status != http.StatusNotFound || !bytes.Contains(ownerBResponse.body, []byte(`"code":"CREDIT_CARD_NOT_FOUND"`)) {
		t.Fatalf("cross-owner statement = %d %s", ownerBResponse.status, ownerBResponse.body)
	}
	if got := cardStatementPersistenceCounts(t, ctx, pool); got != afterArchive {
		t.Fatal("owner B read changed persistence")
	}
}

type cardStatementPersistenceCount struct {
	transactions, transactionAudits, legacyIdempotency            int
	plans, planAudits, cardPurchaseIdempotency, cancelIdempotency int
}

func cardStatementPersistenceCounts(t testing.TB, ctx context.Context, pool *pgxpool.Pool) cardStatementPersistenceCount {
	t.Helper()
	var counts cardStatementPersistenceCount
	err := pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM transactions),
			(SELECT count(*) FROM audit_events),
			(SELECT count(*) FROM idempotency_records),
			(SELECT count(*) FROM installment_plans),
			(SELECT count(*) FROM installment_plan_audit_events),
			(SELECT count(*) FROM card_purchase_idempotency_records),
			(SELECT count(*) FROM installment_plan_idempotency_records)
	`).Scan(&counts.transactions, &counts.transactionAudits, &counts.legacyIdempotency, &counts.plans, &counts.planAudits, &counts.cardPurchaseIdempotency, &counts.cancelIdempotency)
	if err != nil {
		t.Fatalf("count card statement persistence: %v", err)
	}
	return counts
}

func mustRFC3339(t testing.TB, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse fixture time: %v", err)
	}
	return parsed
}
