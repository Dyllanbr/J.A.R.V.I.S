package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type financialGoalsHTTPReader struct {
	snapshot application.FinancialGoalsSnapshot
	err      error
	calls    int
	owner    string
}

type financialGoalsHTTPStore struct {
	goals  map[string]domain.FinancialGoal
	values map[string]domain.ProtectedValue
	err    error
	calls  int
}

func (reader *financialGoalsHTTPReader) Read(_ context.Context, ownerID string) (application.FinancialGoalsSnapshot, error) {
	reader.calls++
	reader.owner = ownerID
	if reader.err != nil {
		return application.FinancialGoalsSnapshot{}, reader.err
	}
	return reader.snapshot, nil
}

func (store *financialGoalsHTTPStore) ReplaceFinancialGoal(_ context.Context, goal domain.FinancialGoal) error {
	store.calls++
	if store.err != nil {
		return store.err
	}
	if store.goals == nil {
		store.goals = make(map[string]domain.FinancialGoal)
	}
	store.goals[goal.ID()] = goal
	return nil
}

func (store *financialGoalsHTTPStore) ReplaceProtectedValue(_ context.Context, value domain.ProtectedValue) error {
	store.calls++
	if store.err != nil {
		return store.err
	}
	if store.values == nil {
		store.values = make(map[string]domain.ProtectedValue)
	}
	store.values[value.ID()] = value
	return nil
}

func newFinancialGoalsHTTPHandler(t *testing.T, reader *financialGoalsHTTPReader, store *financialGoalsHTTPStore) http.Handler {
	t.Helper()
	list, err := application.NewListFinancialGoals(reader)
	if err != nil {
		t.Fatal(err)
	}
	replaceGoal, err := application.NewReplaceFinancialGoal(store)
	if err != nil {
		t.Fatal(err)
	}
	replaceValue, err := application.NewReplaceProtectedValue(store)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewFinancialGoals("owner-http", list, replaceGoal, replaceValue).Register(mux)
	return mux
}

func TestFinancialGoalsHTTPListsCombinedOwnerScopedDeclarations(t *testing.T) {
	goal := newFinancialGoal(t, "goal_a", "owner-http", "Viagem", 125000)
	value := newProtectedValue(t, "protected_a", "owner-http", "Reserva", 0)
	reader := &financialGoalsHTTPReader{snapshot: application.FinancialGoalsSnapshot{Goals: []domain.FinancialGoal{goal}, ProtectedValues: []domain.ProtectedValue{value}}}
	handler := newFinancialGoalsHTTPHandler(t, reader, &financialGoalsHTTPStore{})
	request := httptest.NewRequest(http.MethodGet, "/v1/financial-goals", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.calls != 1 || reader.owner != "owner-http" {
		t.Fatalf("status/calls/owner = %d/%d/%s body=%s", response.Code, reader.calls, reader.owner, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{`"goals"`, `"goal_a"`, `"targetAmount":{"minor":125000,"currency":"BRL"}`, `"protectedValues"`, `"protected_a"`, `"minor":0`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("response missing %q: %s", expected, body)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers = %q/%q", response.Header().Get("Cache-Control"), response.Header().Get("X-Content-Type-Options"))
	}
}

func TestFinancialGoalsHTTPReplacesDeclarationsWithoutClientOwnerOrIdempotency(t *testing.T) {
	reader := &financialGoalsHTTPReader{}
	store := &financialGoalsHTTPStore{}
	handler := newFinancialGoalsHTTPHandler(t, reader, store)
	request := httptest.NewRequest(http.MethodPut, "/v1/financial-goals/goal_1", strings.NewReader(`{"title":"Reserva","targetAmount":{"minor":5000,"currency":"BRL"}}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.calls != 1 || !strings.Contains(response.Body.String(), `"id":"goal_1"`) {
		t.Fatalf("goal replacement = %d/%d/%s", response.Code, store.calls, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPut, "/v1/protected-values/value_1", strings.NewReader(`{"label":"Reserva mínima","amount":{"minor":0,"currency":"BRL"}}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.calls != 2 || !strings.Contains(response.Body.String(), `"id":"value_1"`) {
		t.Fatalf("protected replacement = %d/%d/%s", response.Code, store.calls, response.Body.String())
	}
}

func TestFinancialGoalsHTTPRejectsInvalidRequestsBeforeApplication(t *testing.T) {
	reader := &financialGoalsHTTPReader{}
	store := &financialGoalsHTTPStore{}
	handler := newFinancialGoalsHTTPHandler(t, reader, store)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		header string
		want   int
	}{
		{name: "unknown query", method: http.MethodGet, path: "/v1/financial-goals?owner=spoof", want: http.StatusBadRequest},
		{name: "unexpected get body", method: http.MethodGet, path: "/v1/financial-goals", body: `{}`, want: http.StatusBadRequest},
		{name: "missing content type", method: http.MethodPut, path: "/v1/financial-goals/goal_1", body: `{}`, want: http.StatusBadRequest},
		{name: "unknown field", method: http.MethodPut, path: "/v1/financial-goals/goal_1", body: `{"title":"x","targetAmount":{"minor":1,"currency":"BRL"},"owner":"spoof"}`, header: "application/json", want: http.StatusBadRequest},
		{name: "zero goal", method: http.MethodPut, path: "/v1/financial-goals/goal_1", body: `{"title":"x","targetAmount":{"minor":0,"currency":"BRL"}}`, header: "application/json", want: http.StatusBadRequest},
		{name: "negative protected", method: http.MethodPut, path: "/v1/protected-values/value_1", body: `{"label":"x","amount":{"minor":-1,"currency":"BRL"}}`, header: "application/json", want: http.StatusBadRequest},
		{name: "idempotency", method: http.MethodPut, path: "/v1/financial-goals/goal_1", body: `{"title":"x","targetAmount":{"minor":1,"currency":"BRL"}}`, header: "application/json", want: http.StatusBadRequest},
		{name: "method", method: http.MethodDelete, path: "/v1/financial-goals", want: http.StatusMethodNotAllowed},
		{name: "malformed path", method: http.MethodGet, path: "/v1/financial-goals/", want: http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			if testCase.header != "" {
				request.Header.Set("Content-Type", testCase.header)
			}
			if testCase.name == "idempotency" {
				request.Header.Set("Idempotency-Key", "forbidden")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), testCase.want)
			}
		})
	}
	if reader.calls != 0 || store.calls != 0 {
		t.Fatalf("application calls = reader %d/store %d, want zero", reader.calls, store.calls)
	}
}

func TestFinancialGoalsHTTPSanitizesDependencyErrors(t *testing.T) {
	reader := &financialGoalsHTTPReader{err: errors.New("SELECT financial_goals owner=owner-http DSN=secret")}
	handler := newFinancialGoalsHTTPHandler(t, reader, &financialGoalsHTTPStore{})
	request := httptest.NewRequest(http.MethodGet, "/v1/financial-goals", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || response.Body.String() == "" || strings.Contains(response.Body.String(), "SELECT") || strings.Contains(response.Body.String(), "owner-http") || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("dependency response = %d %s", response.Code, response.Body.String())
	}
}

func newFinancialGoal(t *testing.T, id, owner, title string, amount int64) domain.FinancialGoal {
	t.Helper()
	money, err := domain.NewMoney(amount, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	goal, err := domain.NewFinancialGoal(domain.FinancialGoalParams{ID: id, OwnerID: owner, Title: title, TargetAmount: money})
	if err != nil {
		t.Fatal(err)
	}
	return goal
}

func newProtectedValue(t *testing.T, id, owner, label string, amount int64) domain.ProtectedValue {
	t.Helper()
	money, err := domain.NewMoney(amount, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	value, err := domain.NewProtectedValue(domain.ProtectedValueParams{ID: id, OwnerID: owner, Label: label, Amount: money})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
