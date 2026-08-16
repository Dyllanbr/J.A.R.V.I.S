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

func TestCategorizedPreviewReturnsValidatedCategoryAndRejectsInvalidCategoryShapes(t *testing.T) {
	store := &httpTestStore{}
	handler := newTestHandler(t, store, &httpTestReader{})
	for _, test := range []struct {
		name       string
		body       string
		categoryID string
	}{
		{
			name:       "Expense",
			body:       strings.TrimSuffix(validBody, "}") + `,"categoryId":"expense.food"}`,
			categoryID: "expense.food",
		},
		{
			name:       "Income",
			body:       strings.TrimSuffix(validIncomeBody, "}") + `,"categoryId":"income.salary"}`,
			categoryID: "income.salary",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serve(handler, http.MethodPost, "/v1/transactions/preview", test.body,
				map[string]string{"Content-Type": "application/json"})
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"categoryId":"`+test.categoryID+`"`) {
				t.Fatalf("categorized preview = %d %s", response.Code, response.Body.String())
			}
		})
	}

	invalid := []string{
		strings.TrimSuffix(validBody, "}") + `,"categoryId":null}`,
		strings.TrimSuffix(validBody, "}") + `,"categoryId":""}`,
		strings.TrimSuffix(validBody, "}") + `,"categoryId":"Expense.Food"}`,
		strings.TrimSuffix(validBody, "}") + `,"categoryId":"expense.unknown"}`,
		strings.TrimSuffix(validBody, "}") + `,"categoryId":"income.salary"}`,
		strings.TrimSuffix(validIncomeBody, "}") + `,"categoryId":"expense.food"}`,
		strings.TrimSuffix(validIncomeBody, "}") + `,"categoryName":"Salário"}`,
		strings.TrimSuffix(validIncomeBody, "}") + `,"sortOrder":10}`,
	}
	for _, body := range invalid {
		response := serve(handler, http.MethodPost, "/v1/transactions/preview", body,
			map[string]string{"Content-Type": "application/json"})
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("invalid Category response = %d %s", response.Code, response.Body.String())
		}
	}
	if store.calls != 0 || store.incomeCalls != 0 {
		t.Fatal("preview Category validation reached a write store")
	}
}

func TestCategoryCatalogFailureIsInternalAndSafe(t *testing.T) {
	catalog := newHTTPTestCatalog(t)
	catalog.findErr = errors.New("PRIVATE_CATEGORY_CATALOG_MARKER")
	handler := newTestHandlerWithCatalog(t, &httpTestStore{}, &httpTestReader{}, catalog)
	body := strings.TrimSuffix(validBody, "}") + `,"categoryId":"expense.food"}`
	response := serve(handler, http.MethodPost, "/v1/transactions/preview", body,
		map[string]string{"Content-Type": "application/json"})
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"INTERNAL_ERROR"`) ||
		strings.Contains(response.Body.String(), "PRIVATE_CATEGORY_CATALOG_MARKER") || strings.Contains(response.Body.String(), "expense.food") {
		t.Fatalf("unsafe Category catalog failure = %d %s", response.Code, response.Body.String())
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

func TestCategorizedCreateAndReplayPreserveCategoryByteForByte(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "Expense", body: strings.TrimSuffix(validBody, "}") + `,"categoryId":"expense.food"}`},
		{name: "Income", body: strings.TrimSuffix(validIncomeBody, "}") + `,"categoryId":"income.salary"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &httpTestStore{}
			handler := newTestHandler(t, store, &httpTestReader{})
			headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "category-replay-" + test.name}
			created := serve(handler, http.MethodPost, "/v1/transactions", test.body, headers)
			if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"categoryId"`) {
				t.Fatalf("categorized create = %d %s", created.Code, created.Body.String())
			}
			if test.name == "Expense" {
				store.replay = true
			} else {
				store.incomeReplay = true
			}
			replayed := serve(handler, http.MethodPost, "/v1/transactions", test.body, headers)
			if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" ||
				replayed.Body.String() != created.Body.String() {
				t.Fatalf("categorized replay differs: first=%s replay=%s", created.Body.String(), replayed.Body.String())
			}
		})
	}
}

func TestCreateRejectsInvalidUnknownAndInapplicableCategoriesBeforeStore(t *testing.T) {
	for _, body := range []string{
		strings.TrimSuffix(validBody, "}") + `,"categoryId":null}`,
		strings.TrimSuffix(validBody, "}") + `,"categoryId":"expense.unknown"}`,
		strings.TrimSuffix(validBody, "}") + `,"categoryId":"income.salary"}`,
		strings.TrimSuffix(validIncomeBody, "}") + `,"categoryId":"expense.food"}`,
	} {
		store := &httpTestStore{}
		response := serve(newTestHandler(t, store, &httpTestReader{}), http.MethodPost, "/v1/transactions", body,
			map[string]string{"Content-Type": "application/json", "Idempotency-Key": "invalid-category-create"})
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("invalid categorized create = %d %s", response.Code, response.Body.String())
		}
		if store.calls != 0 || store.incomeCalls != 0 {
			t.Fatal("invalid Category reached a write store")
		}
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

func TestMonthlyListOmitsMissingCategoryAndPreservesCategorizedItems(t *testing.T) {
	expenseCategory := domain.CategoryIDExpenseFood
	incomeCategory := domain.CategoryIDIncomeSalary
	expenseWithCategory := application.NewMonthlyTransactionFromExpense(mustHTTPExpense(t, "exp_http_category"))
	expenseWithCategory.CategoryID = &expenseCategory
	incomeWithCategory := application.NewMonthlyTransactionFromIncome(mustHTTPIncome(t, "inc_http_category"))
	incomeWithCategory.CategoryID = &incomeCategory
	items := []application.MonthlyTransaction{
		incomeWithCategory,
		application.NewMonthlyTransactionFromIncome(mustHTTPIncome(t, "inc_http_uncategorized")),
		expenseWithCategory,
		application.NewMonthlyTransactionFromExpense(mustHTTPExpense(t, "exp_http_uncategorized")),
	}
	response := serve(newTestHandler(t, &httpTestStore{}, &httpTestReader{items: items}), http.MethodGet,
		"/v1/transactions?month=2026-08", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(body.Items) != 4 || body.Items[0]["categoryId"] != "income.salary" || body.Items[2]["categoryId"] != "expense.food" {
		t.Fatalf("categorized history = %s", response.Body.String())
	}
	if _, ok := body.Items[1]["categoryId"]; ok {
		t.Fatal("uncategorized Income exposed categoryId")
	}
	if _, ok := body.Items[3]["categoryId"]; ok {
		t.Fatal("uncategorized Expense exposed categoryId")
	}
}

func TestListCategoriesReturnsCompleteOrderedCatalogWithoutInternalFields(t *testing.T) {
	handler := newTestHandler(t, &httpTestStore{}, &httpTestReader{})
	response := serve(handler, http.MethodGet, "/v1/categories", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	assertFinancialHeaders(t, response)
	var items []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode categories: %v", err)
	}
	if len(items) != 17 || items[0]["id"] != "expense.food" || items[9]["id"] != "expense.other" ||
		items[10]["id"] != "income.salary" || items[16]["id"] != "income.other" {
		t.Fatalf("unexpected catalog ordering: %s", response.Body.String())
	}
	for _, item := range items {
		if len(item) != 3 || item["id"] == "uncategorized" {
			t.Fatalf("catalog item exposed unexpected shape: %#v", item)
		}
		if _, ok := item["sortOrder"]; ok {
			t.Fatalf("catalog exposed sortOrder: %#v", item)
		}
	}
}

func TestListCategoriesDoesNotReturnPartialCatalogOnFailure(t *testing.T) {
	catalog := newHTTPTestCatalog(t)
	catalog.listErrType = domain.TransactionTypeIncome
	catalog.listErr = errors.New("PRIVATE_LIST_CATEGORY_MARKER")
	response := serve(newTestHandlerWithCatalog(t, &httpTestStore{}, &httpTestReader{}, catalog),
		http.MethodGet, "/v1/categories", "", nil)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"INTERNAL_ERROR"`) ||
		strings.Contains(response.Body.String(), "PRIVATE_LIST_CATEGORY_MARKER") || strings.Contains(response.Body.String(), "expense.food") {
		t.Fatalf("partial or unsafe catalog failure = %d %s", response.Code, response.Body.String())
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
		{method: http.MethodPost, target: "/v1/categories"},
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
	return newTestHandlerWithCatalog(t, store, reader, newHTTPTestCatalog(t))
}

func newTestHandlerWithCatalog(
	t *testing.T,
	store *httpTestStore,
	reader application.MonthlyTransactionReader,
	catalog application.CategoryCatalog,
) http.Handler {
	t.Helper()
	previewExpense, err := application.NewPreviewExpenseWithCategoryCatalog(catalog)
	if err != nil {
		t.Fatalf("NewPreviewExpenseWithCategoryCatalog() error = %v", err)
	}
	previewIncome, err := application.NewPreviewIncomeWithCategoryCatalog(catalog)
	if err != nil {
		t.Fatalf("NewPreviewIncomeWithCategoryCatalog() error = %v", err)
	}
	recordExpense, err := application.NewRecordExpenseWithCategoryCatalog(store, httpTestIDGenerator{}, httpTestClock{}, catalog)
	if err != nil {
		t.Fatalf("NewRecordExpenseWithCategoryCatalog() error = %v", err)
	}
	recordIncome, err := application.NewRecordIncomeWithCategoryCatalog(store, httpTestIncomeIDGenerator{}, httpTestClock{}, catalog)
	if err != nil {
		t.Fatalf("NewRecordIncomeWithCategoryCatalog() error = %v", err)
	}
	list, err := application.NewListTransactionsByMonth(reader)
	if err != nil {
		t.Fatalf("NewListTransactionsByMonth() error = %v", err)
	}
	listCategories, err := application.NewListCategories(catalog)
	if err != nil {
		t.Fatalf("NewListCategories() error = %v", err)
	}
	financial := httpapi.New(
		testOwner,
		previewExpense,
		previewIncome,
		recordExpense,
		recordIncome,
		list,
		listCategories,
	)
	mux := http.NewServeMux()
	financial.Register(mux)
	return mux
}

type httpTestCatalog struct {
	definitions map[domain.CategoryID]application.CategoryDefinition
	byType      map[domain.TransactionType][]application.CategoryDefinition
	findErr     error
	listErrType domain.TransactionType
	listErr     error
}

func (catalog *httpTestCatalog) FindCategory(
	_ context.Context,
	categoryID domain.CategoryID,
) (application.CategoryDefinition, error) {
	if catalog.findErr != nil {
		return application.CategoryDefinition{}, catalog.findErr
	}
	definition, ok := catalog.definitions[categoryID]
	if !ok {
		return application.CategoryDefinition{}, application.ErrCategoryNotFound
	}
	return definition, nil
}

func (catalog *httpTestCatalog) ListCategories(
	_ context.Context,
	transactionType domain.TransactionType,
) ([]application.CategoryDefinition, error) {
	if catalog.listErr != nil && catalog.listErrType == transactionType {
		return nil, catalog.listErr
	}
	return catalog.byType[transactionType], nil
}

func newHTTPTestCatalog(t *testing.T) *httpTestCatalog {
	t.Helper()
	entries := []struct {
		id          string
		typeValue   domain.TransactionType
		displayName string
		sortOrder   uint16
	}{
		{"expense.food", domain.TransactionTypeExpense, "Alimentação", 10},
		{"expense.transport", domain.TransactionTypeExpense, "Transporte", 20},
		{"expense.housing", domain.TransactionTypeExpense, "Moradia", 30},
		{"expense.health", domain.TransactionTypeExpense, "Saúde", 40},
		{"expense.leisure", domain.TransactionTypeExpense, "Lazer", 50},
		{"expense.education", domain.TransactionTypeExpense, "Educação", 60},
		{"expense.subscriptions", domain.TransactionTypeExpense, "Assinaturas", 70},
		{"expense.shopping", domain.TransactionTypeExpense, "Compras", 80},
		{"expense.taxes_fees", domain.TransactionTypeExpense, "Impostos e taxas", 90},
		{"expense.other", domain.TransactionTypeExpense, "Outros", 100},
		{"income.salary", domain.TransactionTypeIncome, "Salário", 10},
		{"income.freelance", domain.TransactionTypeIncome, "Freelance", 20},
		{"income.refund", domain.TransactionTypeIncome, "Reembolso", 30},
		{"income.sale", domain.TransactionTypeIncome, "Venda", 40},
		{"income.investment_return", domain.TransactionTypeIncome, "Rendimentos", 50},
		{"income.benefits", domain.TransactionTypeIncome, "Benefícios", 60},
		{"income.other", domain.TransactionTypeIncome, "Outros", 70},
	}
	catalog := &httpTestCatalog{
		definitions: make(map[domain.CategoryID]application.CategoryDefinition, len(entries)),
		byType:      make(map[domain.TransactionType][]application.CategoryDefinition, 2),
	}
	for _, entry := range entries {
		categoryID, err := domain.NewCategoryID(entry.id)
		if err != nil {
			t.Fatalf("NewCategoryID(%q) error = %v", entry.id, err)
		}
		definition, err := application.NewCategoryDefinition(categoryID, entry.typeValue, entry.displayName, entry.sortOrder)
		if err != nil {
			t.Fatalf("NewCategoryDefinition(%q) error = %v", entry.id, err)
		}
		catalog.definitions[categoryID] = definition
		catalog.byType[entry.typeValue] = append(catalog.byType[entry.typeValue], definition)
	}
	return catalog
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
