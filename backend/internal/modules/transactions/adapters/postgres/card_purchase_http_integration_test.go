//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// TestCardPurchaseHTTPPostgresLifecycle proves the Stage 3 boundary against
// the real application and PostgreSQL adapters: previews do not write,
// one-time and installment purchases produce their distinct snapshots, and
// cancellation is replayable without changing the original Expense.
func TestCardPurchaseHTTPPostgresLifecycle(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const owner = "usr_card_purchase_http_4b"
	insertSyntheticUser(t, ctx, pool, owner)

	cardRepository := newCreditCardRepository(t, pool)
	card, err := newRecordCreditCardUseCase(
		t,
		cardRepository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('8')},
		fixedCreditCardClock{now: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)},
	).Execute(ctx, fullCreditCardInput(owner, "card-purchase-http-4b-card"))
	if err != nil {
		t.Fatalf("creating card fixture: %v", err)
	}

	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("creating card purchase repository: %v", err)
	}
	dateProvider := fixedCardPurchaseFinancialDate{date: mustCivilDate(t, 2026, 8, 30)}
	server := newCardPurchaseHTTPIntegrationServer(t, pool, owner, cardRepository, purchaseRepository, dateProvider)
	defer server.Close()

	oneTimeBody := []byte(`{"description":"Compra HTTP 4B à vista","amount":{"minor":10000,"currency":"BRL"},"occurredAt":"2026-08-25T14:00:00Z","creditCardId":"` + card.CreditCard.ID() + `"}`)
	preview := postFinancialJSON(t, server.Client(), server.URL+"/v1/card-purchases/preview", oneTimeBody, "")
	if preview.status != http.StatusOK || !bytes.Contains(preview.body, []byte(`"purchaseMode":"ONE_TIME"`)) || !bytes.Contains(preview.body, []byte(`"statementDueOn":"2026-09-10"`)) {
		t.Fatalf("one-time preview = %d %s", preview.status, preview.body)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 0, TransactionAudits: 0, LegacyIdempotency: 0,
		InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 0,
		InstallmentPlanIdempotency: 0,
	})

	oneTime := postFinancialJSON(t, server.Client(), server.URL+"/v1/card-purchases", oneTimeBody, "http-card-purchase-one-time")
	if oneTime.status != http.StatusCreated || oneTime.header.Get("Idempotency-Replayed") != "" || !bytes.Contains(oneTime.body, []byte(`"purchaseMode":"ONE_TIME"`)) || bytes.Contains(oneTime.body, []byte(`"installmentPlan"`)) {
		t.Fatalf("one-time create = %d headers=%v body=%s", oneTime.status, oneTime.header, oneTime.body)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
		InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 1,
		InstallmentPlanIdempotency: 0,
	})
	oneTimeReplay := postFinancialJSON(t, server.Client(), server.URL+"/v1/card-purchases", oneTimeBody, "http-card-purchase-one-time")
	if oneTimeReplay.status != http.StatusCreated || oneTimeReplay.header.Get("Idempotency-Replayed") != "true" || !bytes.Equal(oneTimeReplay.body, oneTime.body) {
		t.Fatalf("one-time replay = %d headers=%v body=%s", oneTimeReplay.status, oneTimeReplay.header, oneTimeReplay.body)
	}

	installmentBody := []byte(`{"description":"Compra HTTP 4B parcelada","amount":{"minor":10100,"currency":"BRL"},"occurredAt":"2026-08-25T14:00:00Z","creditCardId":"` + card.CreditCard.ID() + `","installmentCount":3}`)
	installmentPreview := postFinancialJSON(t, server.Client(), server.URL+"/v1/card-purchases/preview", installmentBody, "")
	if installmentPreview.status != http.StatusOK || !bytes.Contains(installmentPreview.body, []byte(`"purchaseMode":"INSTALLMENT"`)) || !bytes.Contains(installmentPreview.body, []byte(`"installmentCount":3`)) {
		t.Fatalf("installment preview = %d %s", installmentPreview.status, installmentPreview.body)
	}
	installment := postFinancialJSON(t, server.Client(), server.URL+"/v1/card-purchases", installmentBody, "http-card-purchase-installment")
	if installment.status != http.StatusCreated || installment.header.Get("Idempotency-Replayed") != "" || !bytes.Contains(installment.body, []byte(`"purchaseMode":"INSTALLMENT"`)) || !bytes.Contains(installment.body, []byte(`"installmentPlan"`)) {
		t.Fatalf("installment create = %d headers=%v body=%s", installment.status, installment.header, installment.body)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 1, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 0,
	})

	plans := getFinancialJSON(t, server.Client(), server.URL+"/v1/installment-plans")
	if plans.status != http.StatusOK || !bytes.Contains(plans.body, []byte(`"items":[`)) {
		t.Fatalf("installment plan list = %d %s", plans.status, plans.body)
	}
	var planID string
	if err := pool.QueryRow(ctx, `SELECT id FROM installment_plans WHERE user_id=$1`, owner).Scan(&planID); err != nil {
		t.Fatalf("reading installment plan id: %v", err)
	}
	detail := getFinancialJSON(t, server.Client(), server.URL+"/v1/installment-plans/"+planID)
	if detail.status != http.StatusOK || !bytes.Contains(detail.body, []byte(`"id":"`+planID+`"`)) {
		t.Fatalf("installment plan detail = %d %s", detail.status, detail.body)
	}
	cancelPreview := postFinancialJSON(t, server.Client(), server.URL+"/v1/installment-plans/"+planID+"/cancellation-preview", nil, "")
	if cancelPreview.status != http.StatusOK || !bytes.Contains(cancelPreview.body, []byte(`"expectedCancelledOn":"2026-08-30"`)) {
		t.Fatalf("cancellation preview = %d %s", cancelPreview.status, cancelPreview.body)
	}
	cancelBody := []byte(`{"expectedCancelledOn":"2026-08-30"}`)
	cancelled := postFinancialJSON(t, server.Client(), server.URL+"/v1/installment-plans/"+planID+"/cancel", cancelBody, "http-card-purchase-cancel")
	if cancelled.status != http.StatusOK || cancelled.header.Get("Idempotency-Replayed") != "" || !bytes.Contains(cancelled.body, []byte(`"status":"CANCELLED"`)) || !bytes.Contains(cancelled.body, []byte(`"cancelledOn":"2026-08-30"`)) {
		t.Fatalf("cancellation = %d headers=%v body=%s", cancelled.status, cancelled.header, cancelled.body)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 1,
	})
	cancelReplay := postFinancialJSON(t, server.Client(), server.URL+"/v1/installment-plans/"+planID+"/cancel", cancelBody, "http-card-purchase-cancel")
	if cancelReplay.status != http.StatusOK || cancelReplay.header.Get("Idempotency-Replayed") != "true" || !bytes.Equal(cancelReplay.body, cancelled.body) {
		t.Fatalf("cancellation replay = %d headers=%v body=%s", cancelReplay.status, cancelReplay.header, cancelReplay.body)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 1,
	})
	assertNoCardPurchaseUnrelatedWrites(t, ctx, pool)
}

func TestCardPurchaseHTTPPostgresReplayAfterArchiveAndPlanCancellation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_card_purchase_http_replay"
	insertSyntheticUser(t, ctx, pool, owner)

	cardRepository := newCreditCardRepository(t, pool)
	card, err := newRecordCreditCardUseCase(
		t,
		cardRepository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('9')},
		fixedCreditCardClock{now: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)},
	).Execute(ctx, fullCreditCardInput(owner, "card-purchase-http-replay-card"))
	if err != nil {
		t.Fatalf("creating card fixture: %v", err)
	}

	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("creating card purchase repository: %v", err)
	}
	server := newCardPurchaseHTTPIntegrationServer(t, pool, owner, cardRepository, purchaseRepository, fixedCardPurchaseFinancialDate{date: mustCivilDate(t, 2026, 8, 30)})
	defer server.Close()

	body := []byte(`{"description":"Replay HTTP 4B","amount":{"minor":10100,"currency":"BRL"},"occurredAt":"2026-08-25T14:00:00Z","creditCardId":"` + card.CreditCard.ID() + `","installmentCount":3}`)
	created := postFinancialJSON(t, server.Client(), server.URL+"/v1/card-purchases", body, "http-replay-after-mutation")
	if created.status != http.StatusCreated || created.header.Get("Idempotency-Replayed") != "" {
		t.Fatalf("create = %d headers=%v body=%s", created.status, created.header, created.body)
	}
	var planID string
	if err := pool.QueryRow(ctx, `SELECT id FROM installment_plans WHERE user_id=$1`, owner).Scan(&planID); err != nil {
		t.Fatalf("reading plan id: %v", err)
	}

	if _, err := newArchiveCreditCardUseCase(t, cardRepository, fixedCreditCardClock{now: time.Date(2026, time.August, 31, 15, 0, 0, 0, time.UTC)}).Execute(ctx, application.ArchiveCreditCardInput{
		UserID: owner, CreditCardID: card.CreditCard.ID(), IdempotencyKey: "archive-http-replay-card",
	}); err != nil {
		t.Fatalf("archiving card: %v", err)
	}
	cancelPlan, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, fixedCardPurchaseFinancialDate{date: mustCivilDate(t, 2026, 8, 30)})
	if err != nil {
		t.Fatalf("creating cancellation use case: %v", err)
	}
	if _, err := cancelPlan.Execute(ctx, application.CancelInstallmentPlanInput{
		OwnerID: owner, PlanID: planID, ExpectedCancelledOn: mustCivilDate(t, 2026, 8, 30), IdempotencyKey: "cancel-http-replay-plan",
	}); err != nil {
		t.Fatalf("cancelling plan: %v", err)
	}

	replayed := postFinancialJSON(t, server.Client(), server.URL+"/v1/card-purchases", body, "http-replay-after-mutation")
	if replayed.status != http.StatusCreated || replayed.header.Get("Idempotency-Replayed") != "true" || !bytes.Equal(replayed.body, created.body) {
		t.Fatalf("replay after card archive/plan cancellation = %d headers=%v body=%s, want original snapshot", replayed.status, replayed.header, replayed.body)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 2, CreditCardIdempotency: 2,
		Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 1,
		InstallmentPlanIdempotency: 1,
	})
}

func TestLegacyCreditGuardHTTPPostgresAndPaymentMethodRegression(t *testing.T) {
	t.Run("new credit is rejected without writes", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		const owner = "usr_legacy_credit_http_new"
		insertSyntheticUser(t, ctx, pool, owner)
		server := newLegacyCreditHTTPIntegrationServer(t, pool, owner)
		defer server.Close()

		body := []byte(`{"type":"EXPENSE","description":"Compra CREDIT nova","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"CREDIT","occurredAt":"2026-08-14T15:00:00Z"}`)
		response := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", body, "legacy-credit-new")
		if response.status != http.StatusBadRequest || !bytes.Contains(response.body, []byte(`"code":"CREDIT_CARD_REQUIRED"`)) {
			t.Fatalf("new CREDIT = %d %s", response.status, response.body)
		}
		assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)
		assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
			CreditCards: 0, CreditCardAudits: 0, CreditCardIdempotency: 0,
			Transactions: 0, TransactionAudits: 0, LegacyIdempotency: 0,
			InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 0,
			InstallmentPlanIdempotency: 0,
		})
	})

	t.Run("historical credit replay remains allowed", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		const owner = "usr_legacy_credit_http_replay"
		insertSyntheticUser(t, ctx, pool, owner)
		repository := newRepository(t, pool)
		input := application.RecordExpenseInput{IdempotencyKey: "legacy-credit-history", Expense: application.CreateExpenseInput{
			UserID: owner, Description: "CREDIT histórico", AmountMinor: 4250, Currency: domain.CurrencyBRL,
			PaymentMethod: domain.PaymentMethodCredit, OccurredAt: time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS,
		}}
		historical, err := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: "exp_legacy_credit_history"}).Execute(ctx, input)
		if err != nil || historical.Replayed {
			t.Fatalf("creating historical CREDIT fixture=%+v error=%v", historical, err)
		}
		server := newLegacyCreditHTTPIntegrationServer(t, pool, owner)
		defer server.Close()
		body := []byte(`{"type":"EXPENSE","description":"CREDIT histórico","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"CREDIT","occurredAt":"2026-08-14T15:00:00Z"}`)
		replay := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", body, input.IdempotencyKey)
		if replay.status != http.StatusCreated || replay.header.Get("Idempotency-Replayed") != "true" || jsonStringField(t, replay.body, "id") != historical.Expense.ID() || jsonStringField(t, replay.body, "paymentMethod") != "CREDIT" {
			t.Fatalf("historical CREDIT replay = %d headers=%v body=%s", replay.status, replay.header, replay.body)
		}
		assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("pix debit and cash remain unchanged", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		const owner = "usr_legacy_payment_methods_http"
		insertSyntheticUser(t, ctx, pool, owner)
		server := newLegacyCreditHTTPIntegrationServer(t, pool, owner)
		defer server.Close()
		for index, method := range []string{"PIX", "DEBIT", "CASH"} {
			key := fmt.Sprintf("legacy-method-%d", index)
			body := []byte(`{"type":"EXPENSE","description":"Método ` + method + `","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"` + method + `","occurredAt":"2026-08-14T15:00:00Z"}`)
			created := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", body, key)
			if created.status != http.StatusCreated || created.header.Get("Idempotency-Replayed") != "" {
				t.Fatalf("%s create = %d headers=%v body=%s", method, created.status, created.header, created.body)
			}
			replay := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", body, key)
			if replay.status != http.StatusCreated || replay.header.Get("Idempotency-Replayed") != "true" || !bytes.Equal(replay.body, created.body) {
				t.Fatalf("%s replay = %d headers=%v body=%s", method, replay.status, replay.header, replay.body)
			}
		}
		assertFinancialRowCounts(t, ctx, pool, 3, 3, 3)
	})
}

func newLegacyCreditHTTPIntegrationServer(t *testing.T, pool *pgxpool.Pool, owner string) *httptest.Server {
	t.Helper()
	repository := newRepository(t, pool)
	previewExpense, err := application.NewPreviewExpenseWithCategoryCatalog(repository)
	if err != nil {
		t.Fatal(err)
	}
	recordExpense, err := application.NewRecordExpenseWithCategoryCatalog(repository, &sequenceIDGenerator{prefix: "exp_legacy_http"}, fixedIntegrationClock{}, repository)
	if err != nil {
		t.Fatal(err)
	}
	previewIncome, err := application.NewPreviewIncomeWithCategoryCatalog(repository)
	if err != nil {
		t.Fatal(err)
	}
	recordIncome, err := application.NewRecordIncomeWithCategoryCatalog(repository, &sequenceIncomeIDGenerator{prefix: "inc_legacy_http"}, fixedIntegrationClock{}, repository)
	if err != nil {
		t.Fatal(err)
	}
	list, err := application.NewListTransactionsByMonth(repository)
	if err != nil {
		t.Fatal(err)
	}
	categories, err := application.NewListCategories(repository)
	if err != nil {
		t.Fatal(err)
	}
	guard, err := application.NewLegacyExpenseCreditGuard(repository)
	if err != nil {
		t.Fatal(err)
	}
	routes := httpapi.NewWithLegacyCreditGuard(owner, previewExpense, previewIncome, recordExpense, recordIncome, list, categories, guard)
	mux := http.NewServeMux()
	routes.Register(mux)
	return httptest.NewServer(mux)
}

func assertNoCardPurchaseUnrelatedWrites(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{
		"idempotency_records",
		"recurrences",
		"recurrence_audit_events",
		"recurrence_idempotency_records",
		"recurrence_suggestion_suppressions",
	} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("unrelated table %s count = %d error = %v, want zero", table, count, err)
		}
	}
}

func newCardPurchaseHTTPIntegrationServer(
	t *testing.T,
	pool *pgxpool.Pool,
	owner string,
	cardReader application.CreditCardLookupReader,
	purchaseRepository *adapter.CardPurchaseRepository,
	dateProvider application.FinancialDateProvider,
) *httptest.Server {
	t.Helper()
	repository := newRepository(t, pool)
	preview, err := application.NewPreviewCardPurchaseWithCategoryCatalog(cardReader, repository)
	if err != nil {
		t.Fatalf("creating card purchase preview: %v", err)
	}
	record, err := application.NewRecordCardPurchase(
		purchaseRepository, purchaseRepository, cardReader, repository,
		&sequentialCardPurchaseExpenseIDGenerator{},
		&sequentialCardPurchasePlanIDGenerator{},
		fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatalf("creating card purchase record: %v", err)
	}
	list, err := application.NewListInstallmentPlans(purchaseRepository)
	if err != nil {
		t.Fatalf("creating plan list: %v", err)
	}
	get, err := application.NewGetInstallmentPlan(purchaseRepository)
	if err != nil {
		t.Fatalf("creating plan get: %v", err)
	}
	previewCancel, err := application.NewPreviewInstallmentPlanCancellation(purchaseRepository, dateProvider)
	if err != nil {
		t.Fatalf("creating plan cancellation preview: %v", err)
	}
	cancel, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, dateProvider)
	if err != nil {
		t.Fatalf("creating plan cancellation: %v", err)
	}
	mux := http.NewServeMux()
	httpapi.NewCardPurchase(owner, preview, record).Register(mux)
	httpapi.NewInstallmentPlan(owner, list, get, previewCancel, cancel).Register(mux)
	return httptest.NewServer(mux)
}

type sequentialCardPurchaseExpenseIDGenerator struct{ next atomic.Uint64 }

func (generator *sequentialCardPurchaseExpenseIDGenerator) NewExpenseID() (string, error) {
	return fmt.Sprintf("exp_%032x", generator.next.Add(1)), nil
}

type sequentialCardPurchasePlanIDGenerator struct{ next atomic.Uint64 }

func (generator *sequentialCardPurchasePlanIDGenerator) NewInstallmentPlanID() (string, error) {
	return fmt.Sprintf("ipl_%032x", generator.next.Add(1)), nil
}
