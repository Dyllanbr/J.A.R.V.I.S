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
)

func TestPurchaseSimulationHTTPPostgresUsesRealReadOnlyComposition(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_purchase_simulation_http_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	cardID := "card_00000000000000000000000000000c01"
	insertSafeAvailableCard(t, ctx, pool, cardID, owner)
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "income_purchase_simulation_http_opening", ownerID: owner, transactionType: "INCOME", description: "Abertura", amountMinor: 80_000,
		occurredAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "income_purchase_simulation_http_period", ownerID: owner, transactionType: "INCOME", description: "Receita", amountMinor: 10_000,
		occurredAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "expense_purchase_simulation_http_period", ownerID: owner, transactionType: "EXPENSE", description: "Despesa", amountMinor: 5_000,
		paymentMethod: "PIX", occurredAt: time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "expense_purchase_simulation_http_plan", ownerID: owner, transactionType: "EXPENSE", description: "Compra", amountMinor: 30_000,
		paymentMethod: "CREDIT", cardID: &cardID, statementDueOn: "2026-09-15", occurredAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailablePlan(t, ctx, pool, owner, cardID, "expense_purchase_simulation_http_plan", "ipl_00000000000000000000000000000c01", 30_000, 3, "2026-09-15", 15, "ACTIVE", nil)
	insertSafeAvailableRecurrence(t, ctx, pool, owner, "rec_purchase_simulation_http", 2_000, "2026-09-01", "ACTIVE", nil)

	repository, err := adapter.NewPurchaseSimulationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	useCase, err := application.NewSimulatePurchase(repository)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewPurchaseSimulation(owner, useCase).Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	before := readSafeAvailableCounts(t, ctx, pool)
	response := getPurchaseSimulationJSON(t, server.Client(), server.URL, cardID)
	if response.status != http.StatusOK || !bytes.Contains(response.body, []byte(`"creditCardId":"`+cardID+`"`)) || !bytes.Contains(response.body, []byte(`"purchaseMode":"ONE_TIME"`)) || !bytes.Contains(response.body, []byte(`"minor":31000`)) || !bytes.Contains(response.body, []byte(`"minor":-12000`)) || !bytes.Contains(response.body, []byte(`"NOT_PERSISTED"`)) || !bytes.Contains(response.body, []byte(`"NO_EXPENSE_CREATED"`)) {
		t.Fatalf("purchase simulation response = %d %s", response.status, response.body)
	}
	if after := readSafeAvailableCounts(t, ctx, pool); after != before {
		t.Fatalf("simulation HTTP read changed persistence: before=%+v after=%+v", before, after)
	}
}

type purchaseSimulationHTTPResponse struct {
	status int
	body   []byte
}

func getPurchaseSimulationJSON(t *testing.T, client *http.Client, baseURL, cardID string) purchaseSimulationHTTPResponse {
	t.Helper()
	body := []byte(`{"creditCardId":"` + cardID + `","amount":{"minor":12000,"currency":"BRL"},"purchaseOn":"2026-09-05","purchaseMode":"ONE_TIME","periodStart":"2026-09-01","periodEnd":"2026-09-30"}`)
	request, err := http.NewRequest(http.MethodPost, baseURL+"/v1/purchase-simulations", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return purchaseSimulationHTTPResponse{status: response.StatusCode, body: responseBody}
}
