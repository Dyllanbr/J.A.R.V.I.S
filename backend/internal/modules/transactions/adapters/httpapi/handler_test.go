package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

const (
	testOwner       = "usr_synthetic_http_owner"
	validBody       = `{"type":"EXPENSE","description":"  Mercado sintético  ","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"2026-08-14T15:00:00.000000123Z"}`
	validIncomeBody = `{"type":"INCOME","description":"  Salário sintético  ","amount":{"minor":725000,"currency":"BRL"},"occurredAt":"2026-08-14T15:00:00.123456789Z"}`
)

func TestPreviewReturnsCanonicalDataWithoutCallingWriteStore(t *testing.T) {
	store := &httpTestStore{}
	handler := newTestHandler(t, store, &httpTestReader{})
	response := serve(handler, http.MethodPost, "/v1/transactions/preview", validBody, map[string]string{
		"Content-Type": "application/json",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	assertFinancialHeaders(t, response)
	body := response.Body.String()
	if !strings.Contains(body, `"description":"Mercado sintético"`) ||
		!strings.Contains(body, `"occurredAt":"2026-08-14T15:00:00Z"`) ||
		!strings.Contains(body, `"financialTimezone":"America/Sao_Paulo"`) ||
		!strings.Contains(body, `"origin":"IOS"`) {
		t.Fatalf("preview body is not canonical: %s", body)
	}
	for _, forbidden := range []string{`"id"`, `"createdAt"`, `"updatedAt"`, `"userId"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("preview exposed non-preview field %s", forbidden)
		}
	}
	if store.calls != 0 {
		t.Fatalf("preview called write store %d times", store.calls)
	}
}

func TestFinancialJSONDecodingIsStrictAndBounded(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "missing content type", body: validBody},
		{name: "wrong content type", body: validBody, contentType: "text/plain"},
		{name: "empty body", contentType: "application/json"},
		{name: "malformed", body: `{`, contentType: "application/json"},
		{name: "unknown field", body: strings.TrimSuffix(validBody, "}") + `,"userId":"spoofed"}`, contentType: "application/json"},
		{name: "trailing object", body: validBody + `{}`, contentType: "application/json"},
		{name: "decimal amount", body: strings.Replace(validBody, "4250", "42.50", 1), contentType: "application/json"},
		{name: "oversized", body: `{"type":"EXPENSE","description":"` + strings.Repeat("x", 17*1024) + `"}`, contentType: "application/json"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newTestHandler(t, &httpTestStore{}, &httpTestReader{})
			headers := map[string]string{}
			if test.contentType != "" {
				headers["Content-Type"] = test.contentType
			}
			response := serve(handler, http.MethodPost, "/v1/transactions/preview", test.body, headers)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", response.Code, response.Body.String())
			}
			if body := response.Body.String(); !strings.Contains(body, `"code":"INVALID_REQUEST"`) || strings.Contains(body, "spoofed") {
				t.Fatalf("unsafe or unstable error body: %s", body)
			}
		})
	}
}

func TestPreviewRejectsUnsupportedTypeAndDomainInvalidInput(t *testing.T) {
	handler := newTestHandler(t, &httpTestStore{}, &httpTestReader{})
	unsupported := strings.Replace(validBody, `"EXPENSE"`, `"TRANSFER"`, 1)
	response := serve(handler, http.MethodPost, "/v1/transactions/preview", unsupported, map[string]string{"Content-Type": "application/json"})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"UNSUPPORTED_TRANSACTION_TYPE"`) {
		t.Fatalf("unsupported response = %d %s", response.Code, response.Body.String())
	}

	invalid := strings.Replace(validBody, "4250", "0", 1)
	response = serve(handler, http.MethodPost, "/v1/transactions/preview", invalid, map[string]string{"Content-Type": "application/json"})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("invalid response = %d %s", response.Code, response.Body.String())
	}
}

func TestIncomePreviewReturnsCanonicalShapeWithoutPaymentMethodOrWrites(t *testing.T) {
	store := &httpTestStore{}
	handler := newTestHandler(t, store, &httpTestReader{})
	response := serve(handler, http.MethodPost, "/v1/transactions/preview", validIncomeBody, map[string]string{
		"Content-Type": "application/json",
	})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	assertFinancialHeaders(t, response)
	body := response.Body.String()
	for _, expected := range []string{
		`"type":"INCOME"`,
		`"description":"Salário sintético"`,
		`"occurredAt":"2026-08-14T15:00:00.123456Z"`,
		`"financialTimezone":"America/Sao_Paulo"`,
		`"origin":"IOS"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Income preview missing %s: %s", expected, body)
		}
	}
	for _, forbidden := range []string{`"paymentMethod"`, `"id"`, `"createdAt"`, `"updatedAt"`, `"userId"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Income preview exposed forbidden field %s", forbidden)
		}
	}
	if store.calls != 0 || store.incomeCalls != 0 {
		t.Fatal("Income preview reached a write store")
	}
}

func TestDiscriminatedRequestRejectsInvalidTypesShapesAndServerOwnedFields(t *testing.T) {
	serverOwned := []string{
		"userId", "ownerId", "origin", "financialTimezone", "status", "version",
		"createdAt", "updatedAt", "id", "employer", "source", "category",
		"recurring", "account", "notes",
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "type absent", body: `{"description":"Receita","amount":{"minor":1,"currency":"BRL"},"occurredAt":"2026-08-14T15:00:00Z"}`},
		{name: "type null", body: strings.Replace(validIncomeBody, `"INCOME"`, "null", 1)},
		{name: "type empty", body: strings.Replace(validIncomeBody, "INCOME", "", 1)},
		{name: "type lowercase", body: strings.Replace(validIncomeBody, "INCOME", "income", 1)},
		{name: "type unknown", body: strings.Replace(validIncomeBody, "INCOME", "TRANSFER", 1)},
		{name: "Income payment null", body: strings.TrimSuffix(validIncomeBody, "}") + `,"paymentMethod":null}`},
		{name: "Income payment value", body: strings.TrimSuffix(validIncomeBody, "}") + `,"paymentMethod":"PIX"}`},
		{name: "Expense missing payment", body: strings.Replace(validIncomeBody, "INCOME", "EXPENSE", 1)},
		{name: "Income zero", body: strings.Replace(validIncomeBody, "725000", "0", 1)},
		{name: "Income negative", body: strings.Replace(validIncomeBody, "725000", "-1", 1)},
		{name: "Income float", body: strings.Replace(validIncomeBody, "725000", "7250.00", 1)},
		{name: "Income string amount", body: strings.Replace(validIncomeBody, "725000", `"725000"`, 1)},
		{name: "Income overflow", body: strings.Replace(validIncomeBody, "725000", "9223372036854775808", 1)},
	}
	for _, field := range serverOwned {
		tests = append(tests, struct {
			name string
			body string
		}{name: "forbidden " + field, body: strings.TrimSuffix(validIncomeBody, "}") + `,"` + field + `":"synthetic"}`})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &httpTestStore{}
			response := serve(newTestHandler(t, store, &httpTestReader{}), http.MethodPost,
				"/v1/transactions/preview", test.body, map[string]string{"Content-Type": "application/json"})
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", response.Code, response.Body.String())
			}
			if store.calls != 0 || store.incomeCalls != 0 {
				t.Fatal("invalid request reached a write store")
			}
		})
	}
}

func TestCreateRequiresValidIdempotencyKey(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		wantedCode string
	}{
		{name: "missing", headers: map[string]string{"Content-Type": "application/json"}, wantedCode: "IDEMPOTENCY_KEY_REQUIRED"},
		{name: "invalid", headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "invalid key"}, wantedCode: "IDEMPOTENCY_KEY_INVALID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &httpTestStore{}
			response := serve(newTestHandler(t, store, &httpTestReader{}), http.MethodPost, "/v1/transactions", validBody, test.headers)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"`+test.wantedCode+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if store.calls != 0 {
				t.Fatal("invalid idempotency key reached the store")
			}
		})
	}
}

func TestCreateReturnsResourceReplayAndConflictSafely(t *testing.T) {
	store := &httpTestStore{}
	handler := newTestHandler(t, store, &httpTestReader{})
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "synthetic-key-001"}
	created := serve(handler, http.MethodPost, "/v1/transactions", validBody, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("created status = %d; body=%s", created.Code, created.Body.String())
	}
	assertFinancialHeaders(t, created)
	if strings.Contains(created.Body.String(), "userId") || !strings.Contains(created.Body.String(), `"status":"RECORDED"`) {
		t.Fatalf("unsafe or incomplete create body: %s", created.Body.String())
	}

	store.replay = true
	replayed := serve(handler, http.MethodPost, "/v1/transactions", validBody, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay response = %d headers=%v", replayed.Code, replayed.Header())
	}
	if replayed.Body.String() != created.Body.String() {
		t.Fatal("replay did not return the original resource representation")
	}

	store.err = errors.New("SUPER_SECRET_MARKER_HTTP_98431")
	store.conflict = true
	conflict := serve(handler, http.MethodPost, "/v1/transactions", validBody, headers)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) {
		t.Fatalf("conflict response = %d %s", conflict.Code, conflict.Body.String())
	}
	if strings.Contains(conflict.Body.String(), "SUPER_SECRET_MARKER_HTTP_98431") {
		t.Fatal("conflict exposed a wrapped technical marker")
	}

	store.conflict = false
	internal := serve(handler, http.MethodPost, "/v1/transactions", validBody, headers)
	if internal.Code != http.StatusInternalServerError || !strings.Contains(internal.Body.String(), `"code":"INTERNAL_ERROR"`) {
		t.Fatalf("internal response = %d %s", internal.Code, internal.Body.String())
	}
	if strings.Contains(internal.Body.String(), "SUPER_SECRET_MARKER_HTTP_98431") {
		t.Fatal("internal error exposed a wrapped technical marker")
	}
}

func TestCreateIncomeReturnsPersistedResourceReplayConflictAndSafeFailure(t *testing.T) {
	store := &httpTestStore{}
	handler := newTestHandler(t, store, &httpTestReader{})
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "synthetic-income-key-001"}
	created := serve(handler, http.MethodPost, "/v1/transactions", validIncomeBody, headers)
	if created.Code != http.StatusCreated {
		t.Fatalf("created status = %d; body=%s", created.Code, created.Body.String())
	}
	assertFinancialHeaders(t, created)
	if strings.Contains(created.Body.String(), "paymentMethod") || strings.Contains(created.Body.String(), "userId") ||
		!strings.Contains(created.Body.String(), `"type":"INCOME"`) ||
		!strings.Contains(created.Body.String(), `"status":"RECORDED"`) {
		t.Fatalf("unsafe or incomplete Income body: %s", created.Body.String())
	}

	store.incomeReplay = true
	replayed := serve(handler, http.MethodPost, "/v1/transactions", validIncomeBody, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay response = %d headers=%v", replayed.Code, replayed.Header())
	}
	if replayed.Body.String() != created.Body.String() {
		t.Fatal("Income replay did not serialize the original persisted resource")
	}

	store.incomeConflict = true
	store.incomeErr = errors.New("SUPER_SECRET_INCOME_HTTP_98431")
	conflict := serve(handler, http.MethodPost, "/v1/transactions", validIncomeBody, headers)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) {
		t.Fatalf("conflict response = %d %s", conflict.Code, conflict.Body.String())
	}
	if strings.Contains(conflict.Body.String(), "SUPER_SECRET_INCOME_HTTP_98431") {
		t.Fatal("Income conflict exposed a technical marker")
	}

	store.incomeConflict = false
	internal := serve(handler, http.MethodPost, "/v1/transactions", validIncomeBody, headers)
	if internal.Code != http.StatusInternalServerError || strings.Contains(internal.Body.String(), "SUPER_SECRET_INCOME_HTTP_98431") {
		t.Fatalf("unsafe Income internal response = %d %s", internal.Code, internal.Body.String())
	}
}

func TestMonthlyListIsStrictOwnerFreeAndUsesEmptyArray(t *testing.T) {
	reader := &httpTestReader{}
	handler := newTestHandler(t, &httpTestStore{}, reader)
	response := serve(handler, http.MethodGet, "/v1/transactions?month=2026-08", "", nil)
	if response.Code != http.StatusOK || response.Body.String() != "{\"month\":\"2026-08\",\"items\":[]}\n" {
		t.Fatalf("monthly response = %d %s", response.Code, response.Body.String())
	}
	if reader.query.UserID != testOwner {
		t.Fatal("monthly query did not use the server owner")
	}
	if strings.Contains(response.Body.String(), "userId") {
		t.Fatal("monthly response exposed owner identity")
	}

	for _, target := range []string{
		"/v1/transactions",
		"/v1/transactions?month=2026-8",
		"/v1/transactions?month=2026-08&userId=spoofed",
		"/v1/transactions?month=2026-08&month=2026-09",
	} {
		invalid := serve(handler, http.MethodGet, target, "", nil)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d, want 400", target, invalid.Code)
		}
	}
}

func TestMonthlyListSerializesDiscriminatedExpenseAndIncome(t *testing.T) {
	expense := mustHTTPExpense(t, "exp_http_month")
	income := mustHTTPIncome(t, "inc_http_month")
	reader := &httpTestReader{items: []application.MonthlyTransaction{
		application.NewMonthlyTransactionFromIncome(income),
		application.NewMonthlyTransactionFromExpense(expense),
	}}
	response := serve(newTestHandler(t, &httpTestStore{}, reader), http.MethodGet,
		"/v1/transactions?month=2026-08", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Month string                   `json:"month"`
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal("mixed response is not valid JSON")
	}
	if body.Month != "2026-08" || len(body.Items) != 2 || body.Items[0]["type"] != "INCOME" ||
		body.Items[1]["type"] != "EXPENSE" {
		t.Fatalf("unexpected mixed response: %s", response.Body.String())
	}
	if _, exists := body.Items[0]["paymentMethod"]; exists {
		t.Fatal("Income history item exposed paymentMethod")
	}
	if body.Items[1]["paymentMethod"] != "PIX" {
		t.Fatal("Expense history item lost paymentMethod")
	}
	if strings.Contains(response.Body.String(), "userId") {
		t.Fatal("mixed history exposed owner")
	}
}

func TestFinancialRoutesReturnJSONMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(t, &httpTestStore{}, &httpTestReader{})
	for _, test := range []struct {
		method string
		target string
	}{
		{method: http.MethodDelete, target: "/v1/transactions"},
		{method: http.MethodHead, target: "/v1/transactions"},
		{method: http.MethodDelete, target: "/v1/transactions/preview"},
	} {
		response := serve(handler, test.method, test.target, "", nil)
		if response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Body.String(), `"code":"METHOD_NOT_ALLOWED"`) {
			t.Fatalf("%s %s response = %d %s", test.method, test.target, response.Code, response.Body.String())
		}
		assertFinancialHeaders(t, response)
	}
}

type httpTestStore struct {
	calls          int
	original       domain.Expense
	replay         bool
	conflict       bool
	err            error
	incomeCalls    int
	originalIncome domain.Income
	incomeReplay   bool
	incomeConflict bool
	incomeErr      error
}

func (store *httpTestStore) RecordIncome(
	_ context.Context,
	command application.IdempotentIncomeCommand,
) (application.IdempotentIncomeResult, error) {
	store.incomeCalls++
	if store.incomeConflict {
		return application.IdempotentIncomeResult{}, safeIncomeConflict{cause: store.incomeErr}
	}
	if store.originalIncome.ID() == "" {
		store.originalIncome = command.Income
	}
	return application.IdempotentIncomeResult{Income: store.originalIncome, Replayed: store.incomeReplay}, store.incomeErr
}

func (store *httpTestStore) Record(
	_ context.Context,
	command application.IdempotentExpenseCommand,
) (application.IdempotentExpenseResult, error) {
	store.calls++
	if store.conflict {
		return application.IdempotentExpenseResult{}, safeConflict{cause: store.err}
	}
	if store.original.ID() == "" {
		store.original = command.Expense
	}
	return application.IdempotentExpenseResult{Expense: store.original, Replayed: store.replay}, store.err
}

type safeConflict struct{ cause error }

func (err safeConflict) Error() string { return application.ErrIdempotencyConflict.Error() }
func (err safeConflict) Unwrap() []error {
	return []error{application.ErrIdempotencyConflict, err.cause}
}

type safeIncomeConflict struct{ cause error }

func (err safeIncomeConflict) Error() string { return application.ErrIncomeIdempotencyConflict.Error() }
func (err safeIncomeConflict) Unwrap() []error {
	return []error{application.ErrIncomeIdempotencyConflict, err.cause}
}

type httpTestReader struct {
	query application.ExpenseMonthQuery
	items []application.MonthlyTransaction
}

func (reader *httpTestReader) ListMonthlyTransactions(
	_ context.Context,
	query application.ExpenseMonthQuery,
) ([]application.MonthlyTransaction, error) {
	reader.query = query
	return reader.items, nil
}

type httpTestIDGenerator struct{}

func (httpTestIDGenerator) NewExpenseID() (string, error) { return "exp_synthetic_http_001", nil }

type httpTestIncomeIDGenerator struct{}

func (httpTestIncomeIDGenerator) NewIncomeID() (string, error) { return "inc_synthetic_http_001", nil }

type httpTestClock struct{}

func (httpTestClock) Now() time.Time {
	return time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)
}

func newTestHandler(t *testing.T, store *httpTestStore, reader application.MonthlyTransactionReader) http.Handler {
	t.Helper()
	recordExpense, err := application.NewRecordExpense(store, httpTestIDGenerator{}, httpTestClock{})
	if err != nil {
		t.Fatalf("NewRecordExpense() error = %v", err)
	}
	recordIncome, err := application.NewRecordIncome(store, httpTestIncomeIDGenerator{}, httpTestClock{})
	if err != nil {
		t.Fatalf("NewRecordIncome() error = %v", err)
	}
	list, err := application.NewListTransactionsByMonth(reader)
	if err != nil {
		t.Fatalf("NewListTransactionsByMonth() error = %v", err)
	}
	financial := httpapi.New(
		testOwner,
		application.PreviewExpense{},
		application.PreviewIncome{},
		recordExpense,
		recordIncome,
		list,
	)
	mux := http.NewServeMux()
	financial.Register(mux)
	return mux
}

func mustHTTPExpense(t *testing.T, id string) domain.Expense {
	t.Helper()
	amount, _ := domain.NewMoney(4250, domain.CurrencyBRL)
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID: testOwner, Description: "Mercado sintético", Amount: amount,
			PaymentMethod:     domain.PaymentMethodPIX,
			OccurredAt:        time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS,
		},
		CreatedAt: httpTestClock{}.Now(),
	})
	if err != nil {
		t.Fatalf("NewExpense() error = %v", err)
	}
	return expense
}

func mustHTTPIncome(t *testing.T, id string) domain.Income {
	t.Helper()
	amount, _ := domain.NewMoney(725000, domain.CurrencyBRL)
	income, err := domain.NewIncome(domain.IncomeParams{
		ID: id,
		Details: domain.IncomeDetails{
			UserID: testOwner, Description: "Salário sintético", Amount: amount,
			OccurredAt:        time.Date(2026, time.August, 14, 16, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS,
		},
		CreatedAt: httpTestClock{}.Now(),
	})
	if err != nil {
		t.Fatalf("NewIncome() error = %v", err)
	}
	return income
}

func serve(handler http.Handler, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertFinancialHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("financial response headers = %v", response.Header())
	}
}
