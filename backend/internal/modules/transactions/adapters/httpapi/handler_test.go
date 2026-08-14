package httpapi_test

import (
	"context"
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
	testOwner = "usr_synthetic_http_owner"
	validBody = `{"type":"EXPENSE","description":"  Mercado sintético  ","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"2026-08-14T15:00:00.000000123Z"}`
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
	unsupported := strings.Replace(validBody, `"EXPENSE"`, `"INCOME"`, 1)
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
	calls    int
	original domain.Expense
	replay   bool
	conflict bool
	err      error
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

type httpTestReader struct {
	query application.ExpenseMonthQuery
	items []domain.Expense
}

func (reader *httpTestReader) ListByFinancialMonth(
	_ context.Context,
	query application.ExpenseMonthQuery,
) ([]domain.Expense, error) {
	reader.query = query
	return reader.items, nil
}

type httpTestIDGenerator struct{}

func (httpTestIDGenerator) NewExpenseID() (string, error) { return "exp_synthetic_http_001", nil }

type httpTestClock struct{}

func (httpTestClock) Now() time.Time {
	return time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)
}

func newTestHandler(t *testing.T, store application.ExpenseCommandStore, reader application.ExpenseReader) http.Handler {
	t.Helper()
	record, err := application.NewRecordExpense(store, httpTestIDGenerator{}, httpTestClock{})
	if err != nil {
		t.Fatalf("NewRecordExpense() error = %v", err)
	}
	list, err := application.NewListExpensesByMonth(reader)
	if err != nil {
		t.Fatalf("NewListExpensesByMonth() error = %v", err)
	}
	financial := httpapi.New(testOwner, application.PreviewExpense{}, record, list)
	mux := http.NewServeMux()
	financial.Register(mux)
	return mux
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
