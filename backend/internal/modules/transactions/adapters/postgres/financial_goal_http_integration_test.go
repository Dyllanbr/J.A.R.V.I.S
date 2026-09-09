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
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestFinancialGoalsHTTPApplicationPostgresComposition(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_financial_goal_http_a"
	const ownerB = "usr_financial_goal_http_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)
	repository, err := adapter.NewFinancialGoalsRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	list, err := application.NewListFinancialGoals(repository)
	if err != nil {
		t.Fatal(err)
	}
	replaceGoal, err := application.NewReplaceFinancialGoal(repository)
	if err != nil {
		t.Fatal(err)
	}
	replaceValue, err := application.NewReplaceProtectedValue(repository)
	if err != nil {
		t.Fatal(err)
	}
	goalAmount, err := domain.NewMoney(250000, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	valueAmount, err := domain.NewMoney(15000, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replaceGoal.Execute(ctx, application.ReplaceFinancialGoalInput{OwnerID: ownerA, ID: "goal_http_a", Title: "Viagem", TargetAmount: goalAmount}); err != nil {
		t.Fatal(err)
	}
	if _, err := replaceValue.Execute(ctx, application.ReplaceProtectedValueInput{OwnerID: ownerA, ID: "value_http_a", Label: "Reserva", Amount: valueAmount}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewFinancialGoals(ownerA, list, replaceGoal, replaceValue).Register(mux)
	response := serveFinancialGoalsHTTPIntegration(t, mux, "/v1/financial-goals")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "goal_http_a") || !strings.Contains(response.Body.String(), "value_http_a") {
		t.Fatalf("owner A response = %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Goals []struct {
			ID string `json:"id"`
		} `json:"goals"`
		ProtectedValues []struct {
			ID string `json:"id"`
		} `json:"protectedValues"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Goals) != 1 || len(payload.ProtectedValues) != 1 {
		t.Fatalf("owner A payload = %#v", payload)
	}
	ownerBMux := http.NewServeMux()
	httpapi.NewFinancialGoals(ownerB, list, replaceGoal, replaceValue).Register(ownerBMux)
	response = serveFinancialGoalsHTTPIntegration(t, ownerBMux, "/v1/financial-goals")
	if response.Code != http.StatusOK || response.Body.String() != "{\"goals\":[],\"protectedValues\":[]}\n" {
		t.Fatalf("owner B response = %d %s", response.Code, response.Body.String())
	}
	beforeGoals := countRows(t, ctx, pool, "financial_goals")
	beforeValues := countRows(t, ctx, pool, "protected_values")
	response = serveFinancialGoalsHTTPIntegration(t, mux, "/v1/financial-goals")
	if response.Code != http.StatusOK || countRows(t, ctx, pool, "financial_goals") != beforeGoals || countRows(t, ctx, pool, "protected_values") != beforeValues {
		t.Fatalf("read changed declarations: status=%d goals=%d/%d values=%d/%d", response.Code, countRows(t, ctx, pool, "financial_goals"), beforeGoals, countRows(t, ctx, pool, "protected_values"), beforeValues)
	}
}

func serveFinancialGoalsHTTPIntegration(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
