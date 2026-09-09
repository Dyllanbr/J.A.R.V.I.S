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
)

type purchaseSimulationHTTPReader struct {
	snapshot application.PurchaseSimulationSnapshot
	err      error
	calls    int
	query    application.PurchaseSimulationSnapshotQuery
}

func (reader *purchaseSimulationHTTPReader) Read(_ context.Context, query application.PurchaseSimulationSnapshotQuery) (application.PurchaseSimulationSnapshot, error) {
	reader.calls++
	reader.query = query
	return reader.snapshot, reader.err
}

func TestPurchaseSimulationHTTPReturnsReadOnlyProjectionAndServerOwner(t *testing.T) {
	const owner = "usr_purchase_simulation_http"
	period := safeHTTPPeriod(t)
	card := httpStatementCard(t, owner, false)
	reader := &purchaseSimulationHTTPReader{snapshot: application.PurchaseSimulationSnapshot{SafeAvailable: safeHTTPSnapshot(t, owner, period), CreditCard: card, Found: true}}
	useCase, err := application.NewSimulatePurchase(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewPurchaseSimulation(owner, useCase).Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/v1/purchase-simulations", strings.NewReader(`{"creditCardId":"`+card.ID()+`","amount":{"minor":25,"currency":"BRL"},"purchaseOn":"2026-09-02","purchaseMode":"ONE_TIME","periodStart":"2026-09-01","periodEnd":"2026-09-30"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	assertPurchaseSimulationHeaders(t, response)
	var payload struct {
		CreditCardID     string `json:"creditCardId"`
		PurchaseMode     string `json:"purchaseMode"`
		InstallmentCount int    `json:"installmentCount"`
		PeriodStart      string `json:"periodStart"`
		PeriodEnd        string `json:"periodEnd"`
		Baseline         struct {
			Final amountPayload `json:"finalAmount"`
		} `json:"baseline"`
		Projected struct {
			Final amountPayload `json:"finalAmount"`
		} `json:"projected"`
		Impact       amountPayload `json:"impact"`
		Hypothetical []struct {
			Kind   string        `json:"kind"`
			Amount amountPayload `json:"amount"`
		} `json:"hypotheticalCommitments"`
		Assumptions []string `json:"assumptions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CreditCardID != card.ID() || payload.PurchaseMode != "ONE_TIME" || payload.InstallmentCount != 0 || payload.PeriodStart != "2026-09-01" || payload.PeriodEnd != "2026-09-30" || payload.Baseline.Final.Minor != 100 || payload.Projected.Final.Minor != 75 || payload.Impact.Minor != -25 || len(payload.Hypothetical) != 1 || payload.Hypothetical[0].Kind != "COMMITMENT" || payload.Hypothetical[0].Amount.Minor != 25 {
		t.Fatalf("simulation payload = %#v", payload)
	}
	if len(payload.Assumptions) != 2 || payload.Assumptions[0] != "NOT_PERSISTED" || payload.Assumptions[1] != "NO_EXPENSE_CREATED" {
		t.Fatalf("assumptions = %#v", payload.Assumptions)
	}
	if reader.calls != 1 || reader.query.OwnerID != owner || reader.query.CreditCardID != card.ID() {
		t.Fatalf("reader calls/query = %d/%#v", reader.calls, reader.query)
	}
}

func TestPurchaseSimulationHTTPRejectsUnexpectedBoundaryAndMethods(t *testing.T) {
	reader := &purchaseSimulationHTTPReader{}
	useCase, err := application.NewSimulatePurchase(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewPurchaseSimulation("usr_purchase_simulation_http", useCase).Register(mux)
	validBody := `{"creditCardId":"card_0123456789abcdef0123456789abcdef","amount":{"minor":1,"currency":"BRL"},"purchaseOn":"2026-09-02","purchaseMode":"ONE_TIME","periodStart":"2026-09-01","periodEnd":"2026-09-30"}`
	for _, test := range []struct {
		name, method, path, body string
	}{
		{"query", http.MethodPost, "/v1/purchase-simulations?owner=spoofed", validBody},
		{"idempotency", http.MethodPost, "/v1/purchase-simulations", validBody},
		{"unknown field", http.MethodPost, "/v1/purchase-simulations", strings.TrimSuffix(validBody, "}") + `,"unexpected":true}`},
		{"missing body", http.MethodPost, "/v1/purchase-simulations", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.name == "idempotency" {
				request.Header.Set("Idempotency-Key", "forbidden")
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			assertPurchaseSimulationError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace} {
		request := httptest.NewRequest(method, "/v1/purchase-simulations", strings.NewReader(validBody))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		assertPurchaseSimulationError(t, response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	}
	if reader.calls != 0 {
		t.Fatalf("invalid requests reached reader: %d", reader.calls)
	}
}

func TestPurchaseSimulationHTTPSanitizesNotFoundAndDependencyErrors(t *testing.T) {
	owner := "usr_purchase_simulation_http_errors"
	card := httpStatementCard(t, owner, false)
	body := `{"creditCardId":"` + card.ID() + `","amount":{"minor":1,"currency":"BRL"},"purchaseOn":"2026-09-02","purchaseMode":"ONE_TIME","periodStart":"2026-09-01","periodEnd":"2026-09-30"}`
	for _, test := range []struct {
		name      string
		snapshot  application.PurchaseSimulationSnapshot
		readerErr error
		status    int
		code      string
	}{
		{"not found", application.PurchaseSimulationSnapshot{}, nil, http.StatusNotFound, "CREDIT_CARD_NOT_FOUND"},
		{"internal", application.PurchaseSimulationSnapshot{}, errors.New("SQL DSN owner secret"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &purchaseSimulationHTTPReader{snapshot: test.snapshot, err: test.readerErr}
			useCase, err := application.NewSimulatePurchase(reader)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			httpapi.NewPurchaseSimulation(owner, useCase).Register(mux)
			request := httptest.NewRequest(http.MethodPost, "/v1/purchase-simulations", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			assertPurchaseSimulationError(t, response, test.status, test.code)
			if strings.Contains(response.Body.String(), "SQL DSN owner secret") {
				t.Fatal("dependency detail leaked")
			}
		})
	}
}

func assertPurchaseSimulationHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers = %#v", response.Header())
	}
}

func assertPurchaseSimulationError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("error = %d %s", response.Code, response.Body.String())
	}
	assertPurchaseSimulationHeaders(t, response)
}
