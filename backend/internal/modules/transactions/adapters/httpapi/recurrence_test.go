package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

const validRecurrenceBody = `{"type":"EXPENSE","description":"  Academia sintética  ","expectedAmount":{"minor":11900,"currency":"BRL"},"frequency":"MONTHLY","startsOn":"2026-09-10"}`

func TestRecurrencePreviewIsStrictCanonicalAndWriteFree(t *testing.T) {
	store := newRecurrenceHTTPStore()
	handler, _, _, _ := newRecurrenceTestHandler(t, testOwner, store)
	response := serve(handler, http.MethodPost, "/v1/recurrences/preview", validRecurrenceBody,
		map[string]string{"Content-Type": "application/json"})

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	assertFinancialHeaders(t, response)
	body := response.Body.String()
	for _, expected := range []string{
		`"type":"EXPENSE"`, `"description":"Academia sintética"`,
		`"expectedAmount":{"minor":11900,"currency":"BRL"}`,
		`"frequency":"MONTHLY"`, `"startsOn":"2026-09-10"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("preview is missing %s: %s", expected, body)
		}
	}
	for _, forbidden := range []string{`"id"`, `"userId"`, `"status"`, `"createdAt"`, `"cancelledAt"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("preview exposed %s: %s", forbidden, body)
		}
	}
	if store.recordCalls != 0 || store.cancelCalls != 0 || store.auditCount != 0 {
		t.Fatalf("preview caused writes: %+v", store)
	}
}

func TestRecurrenceJSONAndCivilDateValidation(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		code        string
	}{
		{name: "missing content type", body: validRecurrenceBody, code: "INVALID_REQUEST"},
		{name: "wrong content type", body: validRecurrenceBody, contentType: "text/plain", code: "INVALID_REQUEST"},
		{name: "unknown field", body: strings.TrimSuffix(validRecurrenceBody, "}") + `,"userId":"spoofed"}`, contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "trailing document", body: validRecurrenceBody + `{}`, contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "missing type", body: strings.Replace(validRecurrenceBody, `"type":"EXPENSE",`, "", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "null amount", body: strings.Replace(validRecurrenceBody, `{"minor":11900,"currency":"BRL"}`, "null", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "income unsupported", body: strings.Replace(validRecurrenceBody, "EXPENSE", "INCOME", 1), contentType: "application/json", code: "UNSUPPORTED_RECURRENCE_TYPE"},
		{name: "invalid frequency", body: strings.Replace(validRecurrenceBody, "MONTHLY", "YEARLY", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "zero amount", body: strings.Replace(validRecurrenceBody, "11900", "0", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "invalid currency", body: strings.Replace(validRecurrenceBody, "BRL", "USD", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "decimal amount", body: strings.Replace(validRecurrenceBody, "11900", "119.00", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "timestamp date", body: strings.Replace(validRecurrenceBody, "2026-09-10", "2026-09-10T00:00:00Z", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "impossible date", body: strings.Replace(validRecurrenceBody, "2026-09-10", "2026-02-29", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "out of range year", body: strings.Replace(validRecurrenceBody, "2026-09-10", "0000-09-10", 1), contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "server id", body: strings.TrimSuffix(validRecurrenceBody, "}") + `,"id":"rec_spoofed"}`, contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "server status", body: strings.TrimSuffix(validRecurrenceBody, "}") + `,"status":"ACTIVE"}`, contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "category is independent", body: strings.TrimSuffix(validRecurrenceBody, "}") + `,"categoryId":"expense.subscriptions"}`, contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "payment method is not recurrence data", body: strings.TrimSuffix(validRecurrenceBody, "}") + `,"paymentMethod":"CREDIT"}`, contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "occurred at is not recurrence data", body: strings.TrimSuffix(validRecurrenceBody, "}") + `,"occurredAt":"2026-09-10T12:00:00Z"}`, contentType: "application/json", code: "INVALID_REQUEST"},
		{name: "oversized", body: `{"type":"EXPENSE","description":"` + strings.Repeat("x", 17*1024) + `"}`, contentType: "application/json", code: "INVALID_REQUEST"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, _, _, _ := newRecurrenceTestHandler(t, testOwner, newRecurrenceHTTPStore())
			headers := map[string]string{}
			if test.contentType != "" {
				headers["Content-Type"] = test.contentType
			}
			response := serve(handler, http.MethodPost, "/v1/recurrences/preview", test.body, headers)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "spoofed") {
				t.Fatalf("error leaked request data: %s", response.Body.String())
			}
		})
	}

	for _, date := range []string{"2024-02-29", "2026-04-30", "2026-08-31"} {
		handler, _, _, _ := newRecurrenceTestHandler(t, testOwner, newRecurrenceHTTPStore())
		body := strings.Replace(validRecurrenceBody, "2026-09-10", date, 1)
		response := serve(handler, http.MethodPost, "/v1/recurrences/preview", body,
			map[string]string{"Content-Type": "application/json"})
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"startsOn":"`+date+`"`) {
			t.Fatalf("valid CivilDate %s response = %d %s", date, response.Code, response.Body.String())
		}
	}
}

func TestCreateRecurrenceReplayConflictAndHistoricalSnapshot(t *testing.T) {
	store := newRecurrenceHTTPStore()
	handler, ids, clock, _ := newRecurrenceTestHandler(t, testOwner, store)
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "recurrence-create-key"}

	created := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody, headers)
	if created.Code != http.StatusCreated || created.Header().Get("Idempotency-Replayed") != "" {
		t.Fatalf("create = %d replay=%q body=%s", created.Code, created.Header().Get("Idempotency-Replayed"), created.Body.String())
	}
	if !strings.Contains(created.Body.String(), `"id":"rec_synthetic_http_001"`) ||
		!strings.Contains(created.Body.String(), `"status":"ACTIVE"`) ||
		strings.Contains(created.Body.String(), `"cancelledAt"`) || strings.Contains(created.Body.String(), `"userId"`) {
		t.Fatalf("unexpected create shape: %s", created.Body.String())
	}

	replayed := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody, headers)
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || replayed.Body.String() != created.Body.String() {
		t.Fatalf("replay = %d replay=%q body=%s; want byte-identical %s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String(), created.Body.String())
	}
	if ids.calls != 1 || clock.calls != 1 || store.recordCalls != 1 || store.auditCount != 1 {
		t.Fatalf("replay caused side effects: ids=%d clock=%d record=%d audit=%d", ids.calls, clock.calls, store.recordCalls, store.auditCount)
	}

	conflictingBody := strings.Replace(validRecurrenceBody, "11900", "12900", 1)
	conflict := serve(handler, http.MethodPost, "/v1/recurrences", conflictingBody, headers)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) {
		t.Fatalf("conflict = %d %s", conflict.Code, conflict.Body.String())
	}
	if ids.calls != 1 || clock.calls != 1 || store.recordCalls != 1 || store.auditCount != 1 {
		t.Fatal("persisted conflict consumed generated values or wrote state")
	}

	cancelled := serve(handler, http.MethodPost, "/v1/recurrences/rec_synthetic_http_001/cancel", "",
		map[string]string{"Idempotency-Key": "recurrence-cancel-key"})
	if cancelled.Code != http.StatusOK || !strings.Contains(cancelled.Body.String(), `"status":"CANCELLED"`) {
		t.Fatalf("cancel = %d %s", cancelled.Code, cancelled.Body.String())
	}
	historical := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody, headers)
	if historical.Body.String() != created.Body.String() || historical.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("historical create replay changed: %s", historical.Body.String())
	}
}

func TestListAndCancelRecurrencesUseStableOwnerScopedContracts(t *testing.T) {
	store := newRecurrenceHTTPStore()
	handler, _, _, _ := newRecurrenceTestHandler(t, testOwner, store)

	empty := serve(handler, http.MethodGet, "/v1/recurrences", "", nil)
	if empty.Code != http.StatusOK || empty.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("empty list = %d %q", empty.Code, empty.Body.String())
	}
	withQuery := serve(handler, http.MethodGet, "/v1/recurrences?status=ACTIVE", "", nil)
	if withQuery.Code != http.StatusBadRequest {
		t.Fatalf("list with query status = %d", withQuery.Code)
	}

	create := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody,
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "list-create-key"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", create.Code, create.Body.String())
	}
	listed := serve(handler, http.MethodGet, "/v1/recurrences", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":"rec_synthetic_http_001"`) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}

	badBody := serve(handler, http.MethodPost, "/v1/recurrences/rec_synthetic_http_001/cancel", `{}`,
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "cancel-body-key"})
	if badBody.Code != http.StatusBadRequest {
		t.Fatalf("cancel body status = %d", badBody.Code)
	}
	unknown := serve(handler, http.MethodPost, "/v1/recurrences/rec_unknown_http/cancel", "",
		map[string]string{"Idempotency-Key": "unknown-key"})
	if unknown.Code != http.StatusNotFound || !strings.Contains(unknown.Body.String(), `"code":"RECURRENCE_NOT_FOUND"`) {
		t.Fatalf("unknown = %d %s", unknown.Code, unknown.Body.String())
	}

	first := serve(handler, http.MethodPost, "/v1/recurrences/rec_synthetic_http_001/cancel", "",
		map[string]string{"Idempotency-Key": "cancel-key"})
	if first.Code != http.StatusOK || strings.Contains(first.Body.String(), `"cancelledAt":null`) || !strings.Contains(first.Body.String(), `"cancelledAt":`) {
		t.Fatalf("cancel = %d %s", first.Code, first.Body.String())
	}
	replay := serve(handler, http.MethodPost, "/v1/recurrences/rec_synthetic_http_001/cancel", "",
		map[string]string{"Idempotency-Key": "cancel-key"})
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" || replay.Body.String() != first.Body.String() {
		t.Fatalf("cancel replay = %d replay=%q body=%s", replay.Code, replay.Header().Get("Idempotency-Replayed"), replay.Body.String())
	}
	already := serve(handler, http.MethodPost, "/v1/recurrences/rec_synthetic_http_001/cancel", "",
		map[string]string{"Idempotency-Key": "new-cancel-key"})
	if already.Code != http.StatusConflict || !strings.Contains(already.Body.String(), `"code":"RECURRENCE_ALREADY_CANCELLED"`) {
		t.Fatalf("already cancelled = %d %s", already.Code, already.Body.String())
	}
	if store.cancelCalls != 3 || store.auditCount != 2 {
		t.Fatalf("cancel writes=%d audits=%d, want 3 store attempts and create+cancel audits", store.cancelCalls, store.auditCount)
	}

	otherHandler, _, _, _ := newRecurrenceTestHandler(t, "usr_synthetic_other_owner", store)
	crossOwner := serve(otherHandler, http.MethodPost, "/v1/recurrences/rec_synthetic_http_001/cancel", "",
		map[string]string{"Idempotency-Key": "cross-owner-key"})
	if crossOwner.Code != unknown.Code || crossOwner.Body.String() != unknown.Body.String() {
		t.Fatalf("cross-owner enumerates resource: unknown=%d %s cross=%d %s", unknown.Code, unknown.Body.String(), crossOwner.Code, crossOwner.Body.String())
	}
}

func TestRecurrenceRoutesMethodHeadersAndSafeErrors(t *testing.T) {
	store := newRecurrenceHTTPStore()
	handler, _, _, _ := newRecurrenceTestHandler(t, testOwner, store)
	for _, path := range []string{"/v1/recurrences/preview", "/v1/recurrences", "/v1/recurrences/rec_synthetic_http_001/cancel"} {
		response := serve(handler, http.MethodPut, path, "", nil)
		if response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Body.String(), `"code":"METHOD_NOT_ALLOWED"`) {
			t.Fatalf("PUT %s = %d %s", path, response.Code, response.Body.String())
		}
		assertFinancialHeaders(t, response)
	}

	missingKey := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody,
		map[string]string{"Content-Type": "application/json"})
	if missingKey.Code != http.StatusBadRequest || !strings.Contains(missingKey.Body.String(), `"code":"IDEMPOTENCY_KEY_REQUIRED"`) {
		t.Fatalf("missing key = %d %s", missingKey.Code, missingKey.Body.String())
	}
	invalidKey := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody,
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "invalid key"})
	if invalidKey.Code != http.StatusBadRequest || !strings.Contains(invalidKey.Body.String(), `"code":"IDEMPOTENCY_KEY_INVALID"`) {
		t.Fatalf("invalid key = %d %s", invalidKey.Code, invalidKey.Body.String())
	}
	missingCancelKey := serve(handler, http.MethodPost, "/v1/recurrences/rec_synthetic_http_001/cancel", "", nil)
	if missingCancelKey.Code != http.StatusBadRequest || !strings.Contains(missingCancelKey.Body.String(), `"code":"IDEMPOTENCY_KEY_REQUIRED"`) {
		t.Fatalf("missing cancel key = %d %s", missingCancelKey.Code, missingCancelKey.Body.String())
	}
	malformedID := serve(handler, http.MethodPost, "/v1/recurrences/"+strings.Repeat("r", 129)+"/cancel", "",
		map[string]string{"Idempotency-Key": "malformed-id-key"})
	if malformedID.Code != http.StatusBadRequest || !strings.Contains(malformedID.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("malformed ID = %d %s", malformedID.Code, malformedID.Body.String())
	}

	store.replayErr = errors.New("synthetic database detail")
	internal := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody,
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "safe-error-key"})
	if internal.Code != http.StatusInternalServerError || internal.Body.String() != "{\"error\":{\"code\":\"INTERNAL_ERROR\",\"message\":\"internal error\"}}\n" {
		t.Fatalf("unsafe error = %d %s", internal.Code, internal.Body.String())
	}
}

func TestRecurrenceListOrderingIsolationErrorsAndCancelKeyConflict(t *testing.T) {
	store := newRecurrenceHTTPStore()
	handler, _, _, _ := newRecurrenceTestHandler(t, testOwner, store)
	first := serve(handler, http.MethodPost, "/v1/recurrences", validRecurrenceBody,
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "ordering-create-a"})
	secondBody := strings.Replace(validRecurrenceBody, "Academia sintética", "Internet sintética", 1)
	second := serve(handler, http.MethodPost, "/v1/recurrences", secondBody,
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "ordering-create-b"})
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("setup creates = %d/%d", first.Code, second.Code)
	}
	firstID := "rec_synthetic_http_001"
	secondID := "rec_synthetic_http_002"
	cancelled := serve(handler, http.MethodPost, "/v1/recurrences/"+firstID+"/cancel", "",
		map[string]string{"Idempotency-Key": "shared-cancel-key"})
	if cancelled.Code != http.StatusOK {
		t.Fatalf("setup cancel = %d %s", cancelled.Code, cancelled.Body.String())
	}
	keyConflict := serve(handler, http.MethodPost, "/v1/recurrences/"+secondID+"/cancel", "",
		map[string]string{"Idempotency-Key": "shared-cancel-key"})
	if keyConflict.Code != http.StatusConflict || !strings.Contains(keyConflict.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) {
		t.Fatalf("same cancel key/different resource = %d %s", keyConflict.Code, keyConflict.Body.String())
	}

	listed := serve(handler, http.MethodGet, "/v1/recurrences", "", nil)
	activeIndex := strings.Index(listed.Body.String(), `"id":"`+secondID+`"`)
	cancelledIndex := strings.Index(listed.Body.String(), `"id":"`+firstID+`"`)
	if activeIndex < 0 || cancelledIndex < 0 || activeIndex > cancelledIndex {
		t.Fatalf("ACTIVE is not before CANCELLED: %s", listed.Body.String())
	}

	otherHandler, _, _, _ := newRecurrenceTestHandler(t, "usr_synthetic_list_other", store)
	otherBody := strings.Replace(validRecurrenceBody, "Academia sintética", "Outro owner sintético", 1)
	otherCreated := serve(otherHandler, http.MethodPost, "/v1/recurrences", otherBody,
		map[string]string{"Content-Type": "application/json", "Idempotency-Key": "other-owner-create"})
	if otherCreated.Code != http.StatusCreated {
		t.Fatalf("other owner create = %d %s", otherCreated.Code, otherCreated.Body.String())
	}
	listed = serve(handler, http.MethodGet, "/v1/recurrences", "", nil)
	if strings.Contains(listed.Body.String(), "Outro owner sintético") {
		t.Fatalf("list leaked another owner: %s", listed.Body.String())
	}

	store.listErr = errors.New("synthetic SQL details")
	safeError := serve(handler, http.MethodGet, "/v1/recurrences", "", nil)
	if safeError.Code != http.StatusInternalServerError || strings.Contains(safeError.Body.String(), "SQL") {
		t.Fatalf("unsafe list error = %d %s", safeError.Code, safeError.Body.String())
	}
}

type recurrenceHTTPStore struct {
	mu          sync.Mutex
	recurrences map[string]domain.Recurrence
	operations  map[recurrenceHTTPOperationKey]recurrenceHTTPOperation
	replayErr   error
	listErr     error
	recordCalls int
	cancelCalls int
	auditCount  int
}

type recurrenceHTTPOperationKey struct {
	owner     string
	operation string
	key       string
}

type recurrenceHTTPOperation struct {
	fingerprint application.RequestFingerprint
	recurrence  domain.Recurrence
}

func newRecurrenceHTTPStore() *recurrenceHTTPStore {
	return &recurrenceHTTPStore{
		recurrences: make(map[string]domain.Recurrence),
		operations:  make(map[recurrenceHTTPOperationKey]recurrenceHTTPOperation),
	}
}

func (store *recurrenceHTTPStore) FindRecurrenceReplay(
	_ context.Context,
	query application.RecurrenceReplayQuery,
) (application.RecurrenceReplayLookup, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.replayErr != nil {
		return application.RecurrenceReplayLookup{}, store.replayErr
	}
	operation, ok := store.operations[recurrenceHTTPOperationKey{query.UserID, query.Operation, query.IdempotencyKey}]
	if !ok {
		return application.RecurrenceReplayLookup{}, nil
	}
	if operation.fingerprint != query.Fingerprint {
		return application.RecurrenceReplayLookup{}, application.ErrRecurrenceIdempotencyConflict
	}
	return application.RecurrenceReplayLookup{Recurrence: operation.recurrence, Found: true}, nil
}

func (store *recurrenceHTTPStore) RecordRecurrence(
	_ context.Context,
	command application.IdempotentRecurrenceCommand,
) (application.IdempotentRecurrenceResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recordCalls++
	key := recurrenceHTTPOperationKey{command.Recurrence.UserID(), command.Operation, command.IdempotencyKey}
	if operation, ok := store.operations[key]; ok {
		if operation.fingerprint != command.Fingerprint {
			return application.IdempotentRecurrenceResult{}, application.ErrRecurrenceIdempotencyConflict
		}
		return application.IdempotentRecurrenceResult{Recurrence: operation.recurrence, Replayed: true}, nil
	}
	store.recurrences[command.Recurrence.ID()] = command.Recurrence
	store.operations[key] = recurrenceHTTPOperation{command.Fingerprint, command.Recurrence}
	store.auditCount++
	return application.IdempotentRecurrenceResult{Recurrence: command.Recurrence}, nil
}

func (store *recurrenceHTTPStore) CancelRecurrence(
	_ context.Context,
	command application.IdempotentCancelRecurrenceCommand,
) (application.IdempotentCancelRecurrenceResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cancelCalls++
	key := recurrenceHTTPOperationKey{command.UserID, command.Operation, command.IdempotencyKey}
	if operation, ok := store.operations[key]; ok {
		if operation.fingerprint != command.Fingerprint {
			return application.IdempotentCancelRecurrenceResult{}, application.ErrRecurrenceIdempotencyConflict
		}
		return application.IdempotentCancelRecurrenceResult{Recurrence: operation.recurrence, Replayed: true}, nil
	}
	recurrence, ok := store.recurrences[command.RecurrenceID]
	if !ok || recurrence.UserID() != command.UserID {
		return application.IdempotentCancelRecurrenceResult{}, application.ErrRecurrenceNotFound
	}
	cancelled, err := recurrence.Cancel(command.CancelledAt)
	if err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}
	store.recurrences[command.RecurrenceID] = cancelled
	store.operations[key] = recurrenceHTTPOperation{command.Fingerprint, cancelled}
	store.auditCount++
	return application.IdempotentCancelRecurrenceResult{Recurrence: cancelled}, nil
}

func (store *recurrenceHTTPStore) ListRecurrences(_ context.Context, owner string) ([]domain.Recurrence, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.listErr != nil {
		return nil, store.listErr
	}
	result := make([]domain.Recurrence, 0)
	for _, recurrence := range store.recurrences {
		if recurrence.UserID() == owner {
			result = append(result, recurrence)
		}
	}
	return result, nil
}

type recurrenceHTTPIDGenerator struct {
	prefix string
	calls  int
}

func (generator *recurrenceHTTPIDGenerator) NewRecurrenceID() (string, error) {
	generator.calls++
	return fmt.Sprintf("%s_%03d", generator.prefix, generator.calls), nil
}

type recurrenceHTTPClock struct {
	calls int
}

func (clock *recurrenceHTTPClock) Now() time.Time {
	clock.calls++
	return time.Date(2026, time.August, 16, 15, 0, 0, clock.calls*1000, time.UTC)
}

func newRecurrenceTestHandler(
	t *testing.T,
	owner string,
	store *recurrenceHTTPStore,
) (http.Handler, *recurrenceHTTPIDGenerator, *recurrenceHTTPClock, *httpapi.RecurrenceHandler) {
	t.Helper()
	prefix := "rec_synthetic_http"
	if owner != testOwner {
		prefix = "rec_synthetic_other"
	}
	ids := &recurrenceHTTPIDGenerator{prefix: prefix}
	clock := &recurrenceHTTPClock{}
	record, err := application.NewRecordRecurrence(store, store, ids, clock)
	if err != nil {
		t.Fatalf("NewRecordRecurrence() error = %v", err)
	}
	list, err := application.NewListRecurrences(store)
	if err != nil {
		t.Fatalf("NewListRecurrences() error = %v", err)
	}
	cancel, err := application.NewCancelRecurrence(store, store, clock)
	if err != nil {
		t.Fatalf("NewCancelRecurrence() error = %v", err)
	}
	routes := httpapi.NewRecurrence(owner, application.PreviewRecurrence{}, record, list, cancel)
	mux := http.NewServeMux()
	routes.Register(mux)
	return mux, ids, clock, routes
}
