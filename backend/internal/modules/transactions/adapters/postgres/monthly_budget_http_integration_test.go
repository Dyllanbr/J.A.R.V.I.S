//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
)

func TestMonthlyBudgetHTTPAndSafeAvailableUseRealOwnerScopedPersistence(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_monthly_budget_http_owner_a"
	const ownerB = "usr_monthly_budget_http_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	repository := newMonthlyBudgetRepository(t, pool)
	set, err := application.NewSetMonthlyBudget(repository)
	if err != nil {
		t.Fatal(err)
	}
	get, err := application.NewGetMonthlyBudget(repository)
	if err != nil {
		t.Fatal(err)
	}
	monthlyMux := http.NewServeMux()
	httpapi.NewMonthlyBudget(ownerA, set, get).Register(monthlyMux)
	request := httptest.NewRequest(http.MethodPut, "/v1/monthly-budgets/2026-09", strings.NewReader(`{"amount":{"minor":7000,"currency":"BRL"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	monthlyMux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/monthly-budgets/2026-09", nil)
	response = httptest.NewRecorder()
	monthlyMux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"minor":7000`) {
		t.Fatalf("GET status=%d body=%s", response.Code, response.Body.String())
	}

	cardID := "card_00000000000000000000000000000d01"
	insertSafeAvailableCard(t, ctx, pool, cardID, ownerA)
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{id: "inc_monthly_budget_http_opening", ownerID: ownerA, transactionType: "INCOME", description: "saldo", amountMinor: 10000, occurredAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{id: "inc_monthly_budget_http_period", ownerID: ownerA, transactionType: "INCOME", description: "receita", amountMinor: 5000, occurredAt: time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{id: "exp_monthly_budget_http_period", ownerID: ownerA, transactionType: "EXPENSE", description: "despesa", amountMinor: 2000, paymentMethod: "PIX", occurredAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{id: "exp_monthly_budget_http_plan", ownerID: ownerA, transactionType: "EXPENSE", description: "plano", amountMinor: 4000, paymentMethod: "CREDIT", cardID: &cardID, statementDueOn: "2026-09-10", occurredAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)})
	insertSafeAvailablePlan(t, ctx, pool, ownerA, cardID, "exp_monthly_budget_http_plan", "ipl_00000000000000000000000000000d01", 4000, 2, "2026-09-10", 10, "ACTIVE", nil)
	safeRepository, err := adapter.NewSafeAvailableRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	calculate, err := application.NewCalculateSafeAvailable(safeRepository)
	if err != nil {
		t.Fatal(err)
	}
	safeMux := http.NewServeMux()
	httpapi.NewSafeAvailable(ownerA, calculate).Register(safeMux)
	before := readSafeAvailableCounts(t, ctx, pool)
	request = httptest.NewRequest(http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30", nil)
	response = httptest.NewRecorder()
	safeMux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("safe available status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Balance      integrationAmount `json:"availableBalance"`
		Income       integrationAmount `json:"totalConfirmedIncome"`
		Expense      integrationAmount `json:"totalConfirmedExpense"`
		Commitments  integrationAmount `json:"totalConfirmedCommitments"`
		Final        integrationAmount `json:"finalAmount"`
		Budget       integrationAmount `json:"budget"`
		BudgetRemain integrationAmount `json:"budgetRemaining"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Balance.Minor != 6000 || payload.Income.Minor != 5000 || payload.Expense.Minor != 2000 || payload.Commitments.Minor != 2000 || payload.Final.Minor != 3000 || payload.Budget.Minor != 7000 || payload.BudgetRemain.Minor != 3000 {
		t.Fatalf("safe available payload=%+v, want balance=6000 income=5000 expense=2000 commitments=2000 final=3000 budget=7000 remaining=3000", payload)
	}
	if after := readSafeAvailableCounts(t, ctx, pool); before != after {
		t.Fatalf("safe available GET changed financial counts: before=%+v after=%+v", before, after)
	}

	ownerBMux := http.NewServeMux()
	httpapi.NewMonthlyBudget(ownerB, set, get).Register(ownerBMux)
	request = httptest.NewRequest(http.MethodGet, "/v1/monthly-budgets/2026-09", nil)
	response = httptest.NewRecorder()
	ownerBMux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "MONTHLY_BUDGET_NOT_FOUND") {
		t.Fatalf("owner B status=%d body=%s", response.Code, response.Body.String())
	}
}
