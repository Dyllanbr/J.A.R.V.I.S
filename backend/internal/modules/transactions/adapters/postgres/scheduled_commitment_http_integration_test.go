//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestScheduledCommitmentsHTTPPostgresUsesCombinedSnapshotAndOwnerScope(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_scheduled_http_a"
	const ownerB = "usr_scheduled_http_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	cardRepository := newCreditCardRepository(t, pool)
	cardA := createCreditCardFixture(t, ctx, cardRepository, ownerA, creditCardTestID('c'), "scheduled-http-card")
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	count := 3
	record, err := application.NewRecordCardPurchase(
		purchaseRepository, purchaseRepository, cardRepository, nil,
		fixedCardPurchaseExpenseIDGenerator{id: "exp_00000000000000000000000000000090"},
		&fixedCardPurchasePlanIDGenerator{id: "ipl_00000000000000000000000000000090"},
		fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := record.Execute(ctx, application.RecordCardPurchaseInput{
		Purchase: application.CardPurchaseInput{
			UserID: ownerA, Description: "Compromisso HTTP", AmountMinor: 10100, Currency: domain.CurrencyBRL,
			OccurredAt: time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC), CreditCardID: cardA.ID(),
			InstallmentCount: &count, Origin: domain.OriginIOS,
		},
		IdempotencyKey: "scheduled-http-purchase",
	})
	if err != nil || purchase.InstallmentPlan == nil {
		t.Fatalf("card purchase = %+v error=%v", purchase, err)
	}
	recurrenceRepository := newRecurrenceRepository(t, pool)
	recurrence := persistDirectRecurrence(t, ctx, recurrenceRepository, "rec-scheduled-http", ownerA, "scheduled-http-recurrence")

	// Cancel only after creation so the HTTP projection must use the persisted
	// EffectiveSchedule rather than the original full schedule.
	cancelDate := mustCivilDate(t, 2026, 9, 30)
	cancelPlanUseCase, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, fixedCardPurchaseFinancialDate{date: cancelDate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cancelPlanUseCase.Execute(ctx, application.CancelInstallmentPlanInput{
		OwnerID: ownerA, PlanID: purchase.InstallmentPlan.ID(), ExpectedCancelledOn: cancelDate, IdempotencyKey: "scheduled-http-cancel",
	}); err != nil {
		t.Fatal(err)
	}

	repository, err := adapter.NewScheduledCommitmentRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	before := countScheduledCommitmentWrites(t, ctx, pool)
	serverA := newScheduledCommitmentHTTPServer(t, ownerA, repository)
	responseA := getScheduledHTTP(t, serverA, "/v1/scheduled-commitments?evaluationDate=2026-08-25")
	serverA.Close()
	if responseA.status != http.StatusOK || !bytes.Contains(responseA.body, []byte(`"source":"INSTALLMENT_PLAN"`)) || !bytes.Contains(responseA.body, []byte(`"source":"RECURRENCE"`)) {
		t.Fatalf("owner A projection = %d %s", responseA.status, responseA.body)
	}
	if bytes.Contains(responseA.body, []byte(`"dueOn":"2026-10-10"`)) {
		t.Fatalf("cancelled plan emitted an installment after cutoff: %s", responseA.body)
	}
	if got := countScheduledCommitmentWrites(t, ctx, pool); got != before {
		t.Fatalf("owner A read changed persisted counts from %d to %d", before, got)
	}

	serverB := newScheduledCommitmentHTTPServer(t, ownerB, repository)
	responseB := getScheduledHTTP(t, serverB, "/v1/scheduled-commitments?evaluationDate=2026-08-25")
	serverB.Close()
	if responseB.status != http.StatusOK || string(responseB.body) != "{\"items\":[]}\n" {
		t.Fatalf("owner B projection = %d %s, want empty list", responseB.status, responseB.body)
	}
	if got := countScheduledCommitmentWrites(t, ctx, pool); got != before {
		t.Fatalf("owner B read changed persisted counts from %d to %d", before, got)
	}

	serverA = newScheduledCommitmentHTTPServer(t, ownerA, repository)
	responseAAgain := getScheduledHTTP(t, serverA, "/v1/scheduled-commitments?evaluationDate=2026-08-25")
	serverA.Close()
	if responseAAgain.status != http.StatusOK || !bytes.Contains(responseAAgain.body, []byte(recurrence.ID())) || !bytes.Contains(responseAAgain.body, []byte(purchase.InstallmentPlan.ID())) {
		t.Fatalf("owner A projection after owner B = %d %s", responseAAgain.status, responseAAgain.body)
	}
}

type scheduledHTTPResponse struct {
	status int
	body   []byte
}

func newScheduledCommitmentHTTPServer(t testing.TB, owner string, repository *adapter.ScheduledCommitmentRepository) *httptest.Server {
	t.Helper()
	useCase, err := application.NewListScheduledCommitments(repository)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewScheduledCommitments(owner, useCase).Register(mux)
	return httptest.NewServer(mux)
}

func getScheduledHTTP(t testing.TB, server *httptest.Server, path string) scheduledHTTPResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return scheduledHTTPResponse{status: response.StatusCode, body: body}
}
