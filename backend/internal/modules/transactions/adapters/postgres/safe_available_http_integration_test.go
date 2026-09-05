//go:build integration

package postgres_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
)

func TestSafeAvailableHTTPPostgresReturnsOwnerScopedReadOnlyProjection(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_safe_available_http_owner_a"
	const ownerB = "usr_safe_available_http_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	cardID := "card_00000000000000000000000000000b01"
	insertSafeAvailableCard(t, ctx, pool, cardID, ownerA)
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "inc_safe_available_http_opening", ownerID: ownerA, transactionType: "INCOME", description: "Saldo HTTP", amountMinor: 10_000,
		occurredAt: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "inc_safe_available_http_period", ownerID: ownerA, transactionType: "INCOME", description: "Receita HTTP", amountMinor: 5_000,
		occurredAt: time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_http_period", ownerID: ownerA, transactionType: "EXPENSE", description: "Despesa HTTP", amountMinor: 2_000,
		paymentMethod: "PIX", occurredAt: time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailableTransaction(t, ctx, pool, safeAvailableTransaction{
		id: "exp_safe_available_http_plan", ownerID: ownerA, transactionType: "EXPENSE", description: "Plano HTTP", amountMinor: 8_000,
		paymentMethod: "CREDIT", cardID: &cardID, statementDueOn: "2026-09-10", occurredAt: time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
	})
	insertSafeAvailablePlan(t, ctx, pool, ownerA, cardID, "exp_safe_available_http_plan", "ipl_00000000000000000000000000000b01", 8_000, 2, "2026-09-10", 10, "ACTIVE", nil)
	insertSafeAvailableRecurrence(t, ctx, pool, ownerA, "rec_safe_available_http", 3_000, "2026-09-15", "ACTIVE", nil)

	repository, err := adapter.NewSafeAvailableRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewSafeAvailableRepository() error = %v", err)
	}
	useCase, err := application.NewCalculateSafeAvailable(repository)
	if err != nil {
		t.Fatalf("NewCalculateSafeAvailable() error = %v", err)
	}

	before := readSafeAvailableCounts(t, ctx, pool)
	ownerAMux := http.NewServeMux()
	httpapi.NewSafeAvailable(ownerA, useCase).Register(ownerAMux)
	response := serveSafeAvailableIntegration(t, ownerAMux, http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30")
	if response.Code != http.StatusOK {
		t.Fatalf("owner A status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		PeriodStart      string                 `json:"periodStart"`
		PeriodEnd        string                 `json:"periodEnd"`
		AvailableBalance integrationAmount      `json:"availableBalance"`
		Income           integrationAmount      `json:"totalConfirmedIncome"`
		Expense          integrationAmount      `json:"totalConfirmedExpense"`
		Commitments      integrationAmount      `json:"totalConfirmedCommitments"`
		Final            integrationAmount      `json:"finalAmount"`
		Breakdown        []integrationBreakdown `json:"breakdown"`
		MissingData      []string               `json:"missingData"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PeriodStart != "2026-09-01" || payload.PeriodEnd != "2026-09-30" || payload.AvailableBalance.Minor != 2_000 || payload.Income.Minor != 5_000 || payload.Expense.Minor != 2_000 || payload.Commitments.Minor != 7_000 || payload.Final.Minor != -2_000 || len(payload.Breakdown) != 5 {
		t.Fatalf("owner A payload = %#v", payload)
	}
	if payload.Breakdown[0].Kind != "AVAILABLE_BALANCE" || payload.Breakdown[1].Kind != "INCOME" || payload.Breakdown[2].Kind != "EXPENSE" || payload.Breakdown[3].Kind != "COMMITMENT" || payload.Breakdown[4].Kind != "COMMITMENT" {
		t.Fatalf("owner A breakdown = %#v", payload.Breakdown)
	}
	if len(payload.MissingData) != 1 || payload.MissingData[0] != "BUDGET" {
		t.Fatalf("owner A missing data = %#v", payload.MissingData)
	}

	afterA := readSafeAvailableCounts(t, ctx, pool)
	if before != afterA {
		t.Fatalf("owner A read changed counts: before=%+v after=%+v", before, afterA)
	}

	ownerBMux := http.NewServeMux()
	httpapi.NewSafeAvailable(ownerB, useCase).Register(ownerBMux)
	response = serveSafeAvailableIntegration(t, ownerBMux, http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30")
	if response.Code != http.StatusOK {
		t.Fatalf("owner B status = %d body=%s", response.Code, response.Body.String())
	}
	var ownerBPayload struct {
		Final     integrationAmount      `json:"finalAmount"`
		Breakdown []integrationBreakdown `json:"breakdown"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &ownerBPayload); err != nil {
		t.Fatal(err)
	}
	if ownerBPayload.Final.Minor != 0 || len(ownerBPayload.Breakdown) != 1 {
		t.Fatalf("owner B leaked owner A data: %#v", ownerBPayload)
	}
	afterB := readSafeAvailableCounts(t, ctx, pool)
	if before != afterB {
		t.Fatalf("owner B read changed counts: before=%+v after=%+v", before, afterB)
	}
}

type integrationAmount struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type integrationBreakdown struct {
	Kind     string `json:"kind"`
	SourceID string `json:"sourceId"`
	Sequence int    `json:"sequence"`
	DueOn    string `json:"dueOn"`
}

func serveSafeAvailableIntegration(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
