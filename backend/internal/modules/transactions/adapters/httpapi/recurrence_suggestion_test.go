package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestRecurrenceSuggestionListUsesClosedOwnerScopedResponse(t *testing.T) {
	backend := newSuggestionHTTPBackend()
	handler := newSuggestionTestHandler(t, testOwner, backend)

	empty := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", "", nil)
	if empty.Code != http.StatusOK || empty.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("empty list = %d %q", empty.Code, empty.Body.String())
	}
	assertFinancialHeaders(t, empty)

	backend.expenses[testOwner] = append(
		suggestionHTTPExpenses(t, testOwner, " Academia ", 11900, 10),
		suggestionHTTPExpenses(t, testOwner, "Seguro", 9900, 20)...,
	)
	response := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("items = %d, want 2: %s", len(payload.Items), response.Body.String())
	}
	wantedKeys := []string{"anchorDay", "description", "evidenceCount", "expectedAmount", "id", "observedDates", "proposedStartsOn"}
	for _, item := range payload.Items {
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, wantedKeys) {
			t.Fatalf("public suggestion keys = %v, want %v", keys, wantedKeys)
		}
	}
	first := payload.Items[0]
	if first["description"] != "Academia" || first["anchorDay"] != float64(10) ||
		first["evidenceCount"] != float64(3) || first["proposedStartsOn"] != "2026-09-10" {
		t.Fatalf("first suggestion = %#v", first)
	}
	amount := first["expectedAmount"].(map[string]any)
	if amount["minor"] != float64(11900) || amount["currency"] != "BRL" {
		t.Fatalf("amount = %#v", amount)
	}
	observed := first["observedDates"].([]any)
	if !slices.Equal(observed, []any{"2026-06-10", "2026-07-10", "2026-08-10"}) {
		t.Fatalf("observedDates = %#v", observed)
	}
}

func TestRecurrenceSuggestionDismissReplayAndPreviewAreServerAuthoritative(t *testing.T) {
	backend := newSuggestionHTTPBackend()
	backend.expenses[testOwner] = suggestionHTTPExpenses(t, testOwner, "Academia", 11900, 10)
	handler := newSuggestionTestHandler(t, testOwner, backend)
	id := firstSuggestionHTTPID(t, handler)

	preview := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/preview", "", nil)
	if preview.Code != http.StatusOK || preview.Body.String() != "{\"type\":\"EXPENSE\",\"description\":\"Academia\",\"expectedAmount\":{\"minor\":11900,\"currency\":\"BRL\"},\"frequency\":\"MONTHLY\",\"startsOn\":\"2026-09-10\"}\n" {
		t.Fatalf("preview = %d %s", preview.Code, preview.Body.String())
	}
	if backend.storeCalls != 0 {
		t.Fatal("preview persisted suppression")
	}

	first := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/dismiss", "", nil)
	if first.Code != http.StatusNoContent || first.Body.Len() != 0 || first.Header().Get("Idempotency-Replayed") != "" {
		t.Fatalf("first dismiss = %d replay=%q body=%q", first.Code, first.Header().Get("Idempotency-Replayed"), first.Body.String())
	}
	if first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("dismiss headers = %v", first.Header())
	}
	replay := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/dismiss", "", nil)
	if replay.Code != http.StatusNoContent || replay.Header().Get("Idempotency-Replayed") != "true" || replay.Body.Len() != 0 {
		t.Fatalf("replay dismiss = %d replay=%q body=%q", replay.Code, replay.Header().Get("Idempotency-Replayed"), replay.Body.String())
	}
	if backend.storeCalls != 1 || backend.suppressionCount(testOwner) != 1 {
		t.Fatalf("dismiss writes = %d rows=%d, want 1/1", backend.storeCalls, backend.suppressionCount(testOwner))
	}
	hidden := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", "", nil)
	if hidden.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("dismissed suggestion remained visible: %s", hidden.Body.String())
	}
	suppressed := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/preview", "", nil)
	if suppressed.Code != http.StatusConflict || !strings.Contains(suppressed.Body.String(), `"code":"RECURRENCE_SUGGESTION_SUPPRESSED"`) {
		t.Fatalf("suppressed preview = %d %s", suppressed.Code, suppressed.Body.String())
	}
}

func TestRecurrenceSuggestionStrictBoundaryAndSafeErrors(t *testing.T) {
	backend := newSuggestionHTTPBackend()
	backend.expenses[testOwner] = suggestionHTTPExpenses(t, testOwner, "Academia", 11900, 10)
	handler := newSuggestionTestHandler(t, testOwner, backend)
	id := firstSuggestionHTTPID(t, handler)
	invalidIDs := []string{"bad", "rec_" + strings.Repeat("a", 64), "rsg_" + strings.Repeat("A", 64), "rsg_" + strings.Repeat("a", 63), "rsg_" + strings.Repeat("g", 64)}
	for _, invalid := range invalidIDs {
		response := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+invalid+"/preview", "", nil)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("invalid id %q = %d %s", invalid, response.Code, response.Body.String())
		}
	}

	for _, target := range []string{
		"/v1/recurrence-suggestions?userId=spoofed",
		"/v1/recurrence-suggestions/" + id + "/dismiss?ownerId=spoofed",
		"/v1/recurrence-suggestions/" + id + "/preview?amount=1",
	} {
		method := http.MethodPost
		if strings.HasPrefix(target, "/v1/recurrence-suggestions?") {
			method = http.MethodGet
		}
		response := serve(handler, method, target, "", nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("query injection %s = %d %s", target, response.Code, response.Body.String())
		}
	}
	getBody := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", `{}`, map[string]string{"Content-Type": "application/json"})
	if getBody.Code != http.StatusBadRequest {
		t.Fatalf("GET body = %d %s", getBody.Code, getBody.Body.String())
	}
	for _, suffix := range []string{"dismiss", "preview"} {
		response := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/"+suffix, `{"userId":"spoofed","amount":1}`, map[string]string{"Content-Type": "application/json"})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unexpected %s body = %d %s", suffix, response.Code, response.Body.String())
		}
	}
	oversized := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/preview", strings.Repeat(" ", 17*1024)+"{}", nil)
	if oversized.Code != http.StatusBadRequest {
		t.Fatalf("oversized body = %d %s", oversized.Code, oversized.Body.String())
	}
	unknown := "rsg_" + strings.Repeat("f", 64)
	response := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+unknown+"/dismiss", "", nil)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"RECURRENCE_SUGGESTION_NOT_FOUND"`) {
		t.Fatalf("stale dismiss = %d %s", response.Code, response.Body.String())
	}
	method := serve(handler, http.MethodPut, "/v1/recurrence-suggestions/"+id+"/preview", "", nil)
	if method.Code != http.StatusMethodNotAllowed || !strings.Contains(method.Body.String(), `"code":"METHOD_NOT_ALLOWED"`) {
		t.Fatalf("method response = %d %s", method.Code, method.Body.String())
	}

	backend.expenseErr = errors.New("postgres private table recurrence_suggestion_suppressions")
	safe := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", "", nil)
	if safe.Code != http.StatusInternalServerError || safe.Body.String() != "{\"error\":{\"code\":\"INTERNAL_ERROR\",\"message\":\"internal error\"}}\n" {
		t.Fatalf("unsafe dependency error = %d %s", safe.Code, safe.Body.String())
	}
	backend.expenseErr = nil
	backend.corruptSuppression = true
	corrupt := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", "", nil)
	if corrupt.Code != http.StatusInternalServerError || strings.Contains(corrupt.Body.String(), "rsg_") {
		t.Fatalf("corruption error = %d %s", corrupt.Code, corrupt.Body.String())
	}
}

func TestRecurrenceSuggestionHEADIsRejectedBeforeApplication(t *testing.T) {
	backend := newSuggestionHTTPBackend()
	handler := newSuggestionTestHandler(t, testOwner, backend)

	for _, target := range []string{
		"/v1/recurrence-suggestions",
		"/v1/recurrence-suggestions/rsg_" + strings.Repeat("a", 64) + "/dismiss",
		"/v1/recurrence-suggestions/rsg_" + strings.Repeat("a", 64) + "/preview",
	} {
		response := serve(handler, http.MethodHead, target, "", nil)
		if response.Code != http.StatusMethodNotAllowed ||
			response.Body.String() != "{\"error\":{\"code\":\"METHOD_NOT_ALLOWED\",\"message\":\"method is not allowed\"}}\n" {
			t.Fatalf("HEAD %s = %d %q", target, response.Code, response.Body.String())
		}
		assertFinancialHeaders(t, response)
		if strings.Contains(response.Body.String(), "items") || backend.expenseListCallCount() != 0 {
			t.Fatalf("HEAD %s reached list application: calls=%d body=%q", target, backend.expenseListCallCount(), response.Body.String())
		}
	}

	response := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", "", nil)
	if response.Code != http.StatusOK || response.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("GET after HEAD = %d %q", response.Code, response.Body.String())
	}
	if backend.expenseListCallCount() != 1 {
		t.Fatalf("GET list calls = %d, want 1", backend.expenseListCallCount())
	}
}

func TestRecurrenceSuggestionStaleAndOwnerIsolation(t *testing.T) {
	backend := newSuggestionHTTPBackend()
	backend.expenses[testOwner] = suggestionHTTPExpenses(t, testOwner, "Academia", 11900, 10)
	handler := newSuggestionTestHandler(t, testOwner, backend)
	id := firstSuggestionHTTPID(t, handler)

	backend.expenses[testOwner] = append(backend.expenses[testOwner], suggestionHTTPExpense(t, testOwner, "Academia", 11900, 2026, time.August, 12, "new-material-evidence"))
	stale := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/preview", "", nil)
	if stale.Code != http.StatusNotFound {
		t.Fatalf("old evidence preview = %d %s", stale.Code, stale.Body.String())
	}

	backend.expenses[testOwner] = suggestionHTTPExpenses(t, testOwner, "Academia", 11900, 10)
	id = firstSuggestionHTTPID(t, handler)
	backend.recurrences[testOwner] = []domain.Recurrence{suggestionHTTPRecurrence(t, testOwner, "Academia", 11900)}
	blocked := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+id+"/preview", "", nil)
	if blocked.Code != http.StatusNotFound {
		t.Fatalf("ACTIVE recurrence did not stale suggestion: %d %s", blocked.Code, blocked.Body.String())
	}

	backend.recurrences[testOwner] = nil
	other := "usr_synthetic_other_owner"
	backend.expenses[other] = suggestionHTTPExpenses(t, other, "Academia", 11900, 10)
	otherHandler := newSuggestionTestHandler(t, other, backend)
	otherID := firstSuggestionHTTPID(t, otherHandler)
	crossOwner := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/"+otherID+"/preview", "", nil)
	unknown := serve(handler, http.MethodPost, "/v1/recurrence-suggestions/rsg_"+strings.Repeat("f", 64)+"/preview", "", nil)
	if crossOwner.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound || crossOwner.Body.String() != unknown.Body.String() {
		t.Fatalf("cross-owner enumerates suggestion: cross=%d %s unknown=%d %s", crossOwner.Code, crossOwner.Body.String(), unknown.Code, unknown.Body.String())
	}
}

type suggestionHTTPBackend struct {
	mu                 sync.Mutex
	expenses           map[string][]domain.Expense
	recurrences        map[string][]domain.Recurrence
	suppressions       map[string]map[application.RecurrenceSuggestionID]application.DismissRecurrenceSuggestionCommand
	expenseErr         error
	corruptSuppression bool
	expenseListCalls   int
	storeCalls         int
}

func newSuggestionHTTPBackend() *suggestionHTTPBackend {
	return &suggestionHTTPBackend{
		expenses: make(map[string][]domain.Expense), recurrences: make(map[string][]domain.Recurrence),
		suppressions: make(map[string]map[application.RecurrenceSuggestionID]application.DismissRecurrenceSuggestionCommand),
	}
}

func (backend *suggestionHTTPBackend) ListRecurrenceSuggestionExpenses(_ context.Context, query application.RecurrenceSuggestionExpenseQuery) ([]domain.Expense, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.expenseListCalls++
	return slices.Clone(backend.expenses[query.UserID]), backend.expenseErr
}

func (backend *suggestionHTTPBackend) ListRecurrences(_ context.Context, owner string) ([]domain.Recurrence, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return slices.Clone(backend.recurrences[owner]), nil
}

func (backend *suggestionHTTPBackend) ListDismissedRecurrenceSuggestionIDs(_ context.Context, owner string, ids []application.RecurrenceSuggestionID) ([]application.RecurrenceSuggestionID, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.corruptSuppression {
		return []application.RecurrenceSuggestionID{"rsg_" + application.RecurrenceSuggestionID(strings.Repeat("a", 64))}, nil
	}
	result := make([]application.RecurrenceSuggestionID, 0)
	for _, id := range ids {
		if _, ok := backend.suppressions[owner][id]; ok {
			result = append(result, id)
		}
	}
	return result, nil
}

func (backend *suggestionHTTPBackend) DismissRecurrenceSuggestion(_ context.Context, command application.DismissRecurrenceSuggestionCommand) (application.DismissRecurrenceSuggestionStoreResult, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.storeCalls++
	if backend.suppressions[command.UserID] == nil {
		backend.suppressions[command.UserID] = make(map[application.RecurrenceSuggestionID]application.DismissRecurrenceSuggestionCommand)
	}
	if _, ok := backend.suppressions[command.UserID][command.SuggestionID]; ok {
		return application.DismissRecurrenceSuggestionStoreResult{Replayed: true}, nil
	}
	backend.suppressions[command.UserID][command.SuggestionID] = command
	return application.DismissRecurrenceSuggestionStoreResult{}, nil
}

func (backend *suggestionHTTPBackend) suppressionCount(owner string) int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return len(backend.suppressions[owner])
}

func (backend *suggestionHTTPBackend) expenseListCallCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.expenseListCalls
}

type suggestionHTTPClock struct{}

func (suggestionHTTPClock) Now() time.Time {
	return time.Date(2026, time.August, 24, 15, 0, 0, 123456000, time.UTC)
}

func newSuggestionTestHandler(t *testing.T, owner string, backend *suggestionHTTPBackend) http.Handler {
	t.Helper()
	list, err := application.NewListRecurrenceSuggestions(backend, backend, backend, suggestionHTTPClock{})
	if err != nil {
		t.Fatal(err)
	}
	dismiss, err := application.NewDismissRecurrenceSuggestion(list, backend, backend, suggestionHTTPClock{})
	if err != nil {
		t.Fatal(err)
	}
	prepare, err := application.NewPrepareSuggestedRecurrence(list)
	if err != nil {
		t.Fatal(err)
	}
	routes := httpapi.NewRecurrenceSuggestion(owner, list, dismiss, prepare)
	mux := http.NewServeMux()
	routes.Register(mux)
	return mux
}

func firstSuggestionHTTPID(t *testing.T, handler http.Handler) string {
	t.Helper()
	response := serve(handler, http.MethodGet, "/v1/recurrence-suggestions", "", nil)
	var payload struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Items) == 0 {
		t.Fatalf("could not obtain suggestion id: %d %s", response.Code, response.Body.String())
	}
	return payload.Items[0].ID
}

func suggestionHTTPExpenses(t *testing.T, owner, description string, amount int64, day int) []domain.Expense {
	t.Helper()
	return []domain.Expense{
		suggestionHTTPExpense(t, owner, description, amount, 2026, time.June, day, "jun"),
		suggestionHTTPExpense(t, owner, description, amount, 2026, time.July, day, "jul"),
		suggestionHTTPExpense(t, owner, description, amount, 2026, time.August, day, "aug"),
	}
}

func suggestionHTTPExpense(t *testing.T, owner, description string, amountMinor int64, year int, month time.Month, day int, suffix string) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(amountMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		t.Fatal(err)
	}
	occurredAt := time.Date(year, month, day, 12, 0, 0, 0, location)
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: "exp-http-suggestion-" + owner + "-" + suffix,
		Details: domain.ExpenseDetails{UserID: owner, Description: description, Amount: amount,
			PaymentMethod: domain.PaymentMethodPIX, OccurredAt: occurredAt,
			FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS},
		CreatedAt: occurredAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return expense
}

func suggestionHTTPRecurrence(t *testing.T, owner, description string, amountMinor int64) domain.Recurrence {
	t.Helper()
	amount, err := domain.NewMoney(amountMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	startsOn, err := domain.NewCivilDate(2026, time.September, 10)
	if err != nil {
		t.Fatal(err)
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID: "rec-http-suggestion-active", Details: domain.RecurrenceDetails{UserID: owner,
			TransactionType: domain.TransactionTypeExpense, Description: description, ExpectedAmount: amount,
			Frequency: domain.RecurrenceFrequencyMonthly, StartsOn: startsOn},
		CreatedAt: time.Date(2026, time.August, 24, 15, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return recurrence
}
