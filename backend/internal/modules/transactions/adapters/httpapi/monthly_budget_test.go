package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type monthlyBudgetHTTPStore struct {
	budget domain.MonthlyBudget
	found  bool
	calls  int
	err    error
}

func (store *monthlyBudgetHTTPStore) ReplaceMonthlyBudget(_ context.Context, budget domain.MonthlyBudget) error {
	store.calls++
	if store.err != nil {
		return store.err
	}
	store.budget, store.found = budget, true
	return nil
}

func (store *monthlyBudgetHTTPStore) ReadMonthlyBudget(_ context.Context, owner string, month domain.CivilMonth) (domain.MonthlyBudget, bool, error) {
	store.calls++
	if store.err != nil {
		return domain.MonthlyBudget{}, false, store.err
	}
	if !store.found || store.budget.OwnerID() != owner || !store.budget.Month().Equal(month) {
		return domain.MonthlyBudget{}, false, nil
	}
	return store.budget, true, nil
}

func newMonthlyBudgetHTTPHandler(t *testing.T, store *monthlyBudgetHTTPStore) http.Handler {
	t.Helper()
	set, err := application.NewSetMonthlyBudget(store)
	if err != nil {
		t.Fatal(err)
	}
	get, err := application.NewGetMonthlyBudget(store)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewMonthlyBudget("owner-http", set, get).Register(mux)
	return mux
}

func TestMonthlyBudgetHTTPReplacesAndReadsOwnerScopedBudget(t *testing.T) {
	store := &monthlyBudgetHTTPStore{}
	handler := newMonthlyBudgetHTTPHandler(t, store)
	request := httptest.NewRequest(http.MethodPut, "/v1/monthly-budgets/2026-09", strings.NewReader(`{"amount":{"minor":0,"currency":"BRL"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.calls != 1 {
		t.Fatalf("PUT status/calls = %d/%d body=%s", response.Code, store.calls, response.Body.String())
	}
	var payload struct {
		Month  string `json:"month"`
		Amount struct {
			Minor    int64  `json:"minor"`
			Currency string `json:"currency"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Month != "2026-09" || payload.Amount.Minor != 0 || payload.Amount.Currency != "BRL" {
		t.Fatalf("PUT payload = %#v", payload)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/monthly-budgets/2026-09", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.calls != 2 || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("GET status/calls/headers = %d/%d/%s/%s", response.Code, store.calls, response.Header().Get("Cache-Control"), response.Header().Get("X-Content-Type-Options"))
	}
}

func TestMonthlyBudgetHTTPRejectsInvalidRequestsBeforeApplication(t *testing.T) {
	store := &monthlyBudgetHTTPStore{}
	handler := newMonthlyBudgetHTTPHandler(t, store)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		header string
		want   int
	}{
		{name: "invalid month", method: http.MethodGet, path: "/v1/monthly-budgets/2026-13", want: http.StatusBadRequest},
		{name: "unknown query", method: http.MethodGet, path: "/v1/monthly-budgets/2026-09?owner=spoofed", want: http.StatusBadRequest},
		{name: "unexpected body", method: http.MethodGet, path: "/v1/monthly-budgets/2026-09", body: `{}`, want: http.StatusBadRequest},
		{name: "missing content type", method: http.MethodPut, path: "/v1/monthly-budgets/2026-09", body: `{ "amount": {"minor": 1, "currency":"BRL"}}`, want: http.StatusBadRequest},
		{name: "wrong content type", method: http.MethodPut, path: "/v1/monthly-budgets/2026-09", body: `{ "amount": {"minor": 1, "currency":"BRL"}}`, header: "text/plain", want: http.StatusBadRequest},
		{name: "negative amount", method: http.MethodPut, path: "/v1/monthly-budgets/2026-09", body: `{ "amount": {"minor": -1, "currency":"BRL"}}`, header: "application/json", want: http.StatusBadRequest},
		{name: "idempotency header", method: http.MethodPut, path: "/v1/monthly-budgets/2026-09", body: `{ "amount": {"minor": 1, "currency":"BRL"}}`, header: "application/json", want: http.StatusBadRequest},
		{name: "method", method: http.MethodDelete, path: "/v1/monthly-budgets/2026-09", want: http.StatusMethodNotAllowed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			if testCase.header != "" {
				request.Header.Set("Content-Type", testCase.header)
			}
			if testCase.name == "idempotency header" {
				request.Header.Set("Idempotency-Key", "forbidden")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), testCase.want)
			}
		})
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want zero", store.calls)
	}
}

func TestMonthlyBudgetHTTPMapsMissingAndDependencyErrorsSafely(t *testing.T) {
	store := &monthlyBudgetHTTPStore{}
	handler := newMonthlyBudgetHTTPHandler(t, store)
	request := httptest.NewRequest(http.MethodGet, "/v1/monthly-budgets/2026-09", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "MONTHLY_BUDGET_NOT_FOUND") {
		t.Fatalf("missing budget status = %d body=%s", response.Code, response.Body.String())
	}

	store = &monthlyBudgetHTTPStore{err: errors.New("SQL monthly_budgets owner=owner-http dsn=secret")}
	handler = newMonthlyBudgetHTTPHandler(t, store)
	request = httptest.NewRequest(http.MethodGet, "/v1/monthly-budgets/2026-09", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || response.Body.String() == "" || containsAny(response.Body.String(), "SQL", "owner-http", "dsn", "secret") {
		t.Fatalf("dependency response = %d %s", response.Code, response.Body.String())
	}
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
