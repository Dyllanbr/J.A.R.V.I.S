package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

const fullCreditCardBody = `{"name":"  Cartão principal  ","lastFour":"1234","brand":"VISA","closingDay":31,"dueDay":10,"creditLimit":{"minor":850000,"currency":"BRL"}}`

func TestCreditCardPreviewIsCanonicalAndWriteFree(t *testing.T) {
	store := newCreditCardHTTPStore()
	handler, _, _ := newCreditCardTestHandler(t, testOwner, store)

	response := serve(handler, http.MethodPost, "/v1/cards/preview", fullCreditCardBody,
		map[string]string{"Content-Type": "application/json"})

	if response.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	assertFinancialHeaders(t, response)
	if body := response.Body.String(); body != "{\"name\":\"Cartão principal\",\"lastFour\":\"1234\",\"brand\":\"VISA\",\"closingDay\":31,\"dueDay\":10,\"creditLimit\":{\"minor\":850000,\"currency\":\"BRL\"}}\n" {
		t.Fatalf("preview body = %s", body)
	}
	if store.cardCount() != 0 || store.auditCount != 0 || len(store.operations) != 0 {
		t.Fatal("preview reached persistence")
	}

	minimal := serve(handler, http.MethodPost, "/v1/cards/preview",
		`{"name":"Reserva","closingDay":1,"dueDay":31}`,
		map[string]string{"Content-Type": "application/json"})
	if minimal.Code != http.StatusOK || minimal.Body.String() != "{\"name\":\"Reserva\",\"closingDay\":1,\"dueDay\":31}\n" {
		t.Fatalf("minimal preview = %d %s", minimal.Code, minimal.Body.String())
	}
}

func TestCreditCardQueryPolicyRejectsPreviewAndCreateBeforeBodyOrApplication(t *testing.T) {
	for _, route := range []struct {
		name    string
		target  string
		headers map[string]string
	}{
		{
			name: "preview foo", target: "/v1/cards/preview?foo=bar",
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name: "preview owner", target: "/v1/cards/preview?owner=spoofed",
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name: "preview userId", target: "/v1/cards/preview?userId=spoofed",
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name: "preview cardId", target: "/v1/cards/preview?cardId=x",
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name: "preview empty value", target: "/v1/cards/preview?foo=",
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name: "create foo", target: "/v1/cards?foo=bar",
			headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "query-rejected"},
		},
		{
			name: "create owner", target: "/v1/cards?owner=spoofed",
			headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "query-rejected"},
		},
		{
			name: "create userId", target: "/v1/cards?userId=spoofed",
			headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "query-rejected"},
		},
		{
			name: "create limit", target: "/v1/cards?limit=10",
			headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "query-rejected"},
		},
		{
			name: "create multiple", target: "/v1/cards?foo=a&bar=b",
			headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "query-rejected"},
		},
	} {
		t.Run(route.name, func(t *testing.T) {
			store := newCreditCardHTTPStore()
			handler, ids, clock := newCreditCardTestHandler(t, testOwner, store)
			response := serve(handler, http.MethodPost, route.target, fullCreditCardBody, route.headers)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
				t.Fatalf("query response = %d %s", response.Code, response.Body.String())
			}
			assertFinancialHeaders(t, response)
			if ids.calls != 0 || clock.calls != 0 || store.replayCalls != 0 || store.recordCalls != 0 ||
				store.cardCount() != 0 || store.auditCount != 0 || len(store.operations) != 0 {
				t.Fatalf(
					"query reached application effects: ids=%d clock=%d replay=%d record=%d cards=%d audit=%d operations=%d",
					ids.calls, clock.calls, store.replayCalls, store.recordCalls, store.cardCount(), store.auditCount, len(store.operations),
				)
			}
		})
	}

	for _, route := range []struct {
		name    string
		target  string
		headers map[string]string
	}{
		{
			name: "preview", target: "/v1/cards/preview?foo;bar=baz",
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name: "create", target: "/v1/cards?foo;bar=baz",
			headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "unread-malformed-query"},
		},
	} {
		t.Run("unread body "+route.name, func(t *testing.T) {
			store := newCreditCardHTTPStore()
			handler, ids, clock := newCreditCardTestHandler(t, testOwner, store)
			unread := &creditCardUnreadBody{}
			request := httptest.NewRequest(http.MethodPost, route.target, unread)
			for name, value := range route.headers {
				request.Header.Set(name, value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || unread.reads != 0 {
				t.Fatalf("malformed query plus unreadable body = status %d reads %d, want 400/0", response.Code, unread.reads)
			}
			if ids.calls != 0 || clock.calls != 0 || store.replayCalls != 0 || store.recordCalls != 0 ||
				store.cardCount() != 0 || store.auditCount != 0 || len(store.operations) != 0 {
				t.Fatalf("malformed query reached application effects: ids=%d clock=%d replay=%d record=%d cards=%d audit=%d operations=%d",
					ids.calls, clock.calls, store.replayCalls, store.recordCalls, store.cardCount(), store.auditCount, len(store.operations))
			}
		})
	}

	store := newCreditCardHTTPStore()
	handler, ids, clock := newCreditCardTestHandler(t, testOwner, store)
	const sameKey = "query-rejected-then-create"
	rejected := serve(handler, http.MethodPost, "/v1/cards?foo;bar=baz", fullCreditCardBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": sameKey,
	})
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("same-key rejected query = %d %s", rejected.Code, rejected.Body.String())
	}
	created := serve(handler, http.MethodPost, "/v1/cards", fullCreditCardBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": sameKey,
	})
	if created.Code != http.StatusCreated || created.Header().Get("Idempotency-Replayed") != "" {
		t.Fatalf("same key after rejected query = %d replay=%q body=%s", created.Code, created.Header().Get("Idempotency-Replayed"), created.Body.String())
	}
	if ids.calls != 1 || clock.calls != 1 || store.replayCalls != 1 || store.recordCalls != 1 ||
		store.cardCount() != 1 || store.auditCount != 1 || len(store.operations) != 1 {
		t.Fatalf(
			"same-key effects ids=%d clock=%d replay=%d record=%d cards=%d audit=%d operations=%d",
			ids.calls, clock.calls, store.replayCalls, store.recordCalls, store.cardCount(), store.auditCount, len(store.operations),
		)
	}
}

func TestCreditCardRawQueryPolicyRejectsAdversarialValues(t *testing.T) {
	for _, rawQuery := range []string{
		"foo=bar",
		"foo=",
		"foo;bar=baz",
		"foo=%ZZ",
		"%",
		"&",
		"=",
		"&&",
		"owner=spoofed",
		"userId=spoofed",
	} {
		for _, route := range []struct {
			name    string
			path    string
			headers map[string]string
		}{
			{
				name: "preview", path: "/v1/cards/preview",
				headers: map[string]string{"Content-Type": "application/json"},
			},
			{
				name: "create", path: "/v1/cards",
				headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "adversarial-query"},
			},
		} {
			t.Run(route.name+"/"+rawQuery, func(t *testing.T) {
				store := newCreditCardHTTPStore()
				handler, ids, clock := newCreditCardTestHandler(t, testOwner, store)
				request := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(fullCreditCardBody))
				request.URL.RawQuery = rawQuery
				for name, value := range route.headers {
					request.Header.Set(name, value)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
					t.Fatalf("raw query %q response = %d %s", rawQuery, response.Code, response.Body.String())
				}
				assertFinancialHeaders(t, response)
				if ids.calls != 0 || clock.calls != 0 || store.replayCalls != 0 || store.recordCalls != 0 ||
					store.cardCount() != 0 || store.auditCount != 0 || len(store.operations) != 0 {
					t.Fatalf("raw query %q reached application effects", rawQuery)
				}
			})
		}
	}
}

func TestCreditCardQueryPolicyRejectsEveryRoute(t *testing.T) {
	store := newCreditCardHTTPStore()
	handler, ids, clock := newCreditCardTestHandler(t, testOwner, store)
	created := serve(handler, http.MethodPost, "/v1/cards", fullCreditCardBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "query-matrix-create",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("fixture create = %d %s", created.Code, created.Body.String())
	}
	cardID := "card_00000000000000000000000000000001"
	baselineCards, baselineAudits, baselineOperations := store.cardCount(), store.auditCount, len(store.operations)
	baselineReplayCalls, baselineRecordCalls := store.replayCalls, store.recordCalls
	for _, route := range []struct {
		name    string
		method  string
		target  string
		body    string
		headers map[string]string
	}{
		{name: "preview", method: http.MethodPost, target: "/v1/cards/preview?foo;bar=baz", body: fullCreditCardBody, headers: map[string]string{"Content-Type": "application/json"}},
		{name: "create", method: http.MethodPost, target: "/v1/cards?foo;bar=baz", body: fullCreditCardBody, headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "query-matrix-rejected"}},
		{name: "list", method: http.MethodGet, target: "/v1/cards?foo;bar=baz"},
		{name: "detail", method: http.MethodGet, target: "/v1/cards/" + cardID + "?foo;bar=baz"},
		{name: "archive", method: http.MethodPost, target: "/v1/cards/" + cardID + "/archive?foo;bar=baz", headers: map[string]string{"Idempotency-Key": "query-matrix-archive"}},
		{name: "archive before idempotency key", method: http.MethodPost, target: "/v1/cards/" + cardID + "/archive?foo;bar=baz"},
	} {
		t.Run(route.name, func(t *testing.T) {
			response := serve(handler, route.method, route.target, route.body, route.headers)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
				t.Fatalf("query matrix response = %d %s", response.Code, response.Body.String())
			}
		})
	}
	card := store.card(cardID)
	if store.cardCount() != baselineCards || store.auditCount != baselineAudits || len(store.operations) != baselineOperations ||
		store.replayCalls != baselineReplayCalls || store.recordCalls != baselineRecordCalls ||
		store.listCalls != 0 || store.lookupCalls != 0 || store.archiveCalls != 0 ||
		ids.calls != 1 || clock.calls != 1 || card.Status() != domain.CreditCardStatusActive {
		t.Fatalf("query matrix reached application or changed persistence")
	}
	if _, archived := card.ArchivedAt(); archived {
		t.Fatal("malformed archive query populated archivedAt")
	}

	archived := serve(handler, http.MethodPost, "/v1/cards/"+cardID+"/archive", "", map[string]string{
		"Idempotency-Key": "query-matrix-archive",
	})
	if archived.Code != http.StatusOK || archived.Header().Get("Idempotency-Replayed") != "" ||
		!strings.Contains(archived.Body.String(), `"status":"ARCHIVED"`) {
		t.Fatalf("same key after malformed archive = %d replay=%q body=%s",
			archived.Code, archived.Header().Get("Idempotency-Replayed"), archived.Body.String())
	}
	if store.auditCount != baselineAudits+1 || len(store.operations) != baselineOperations+1 ||
		store.replayCalls != baselineReplayCalls+1 || store.archiveCalls != 1 ||
		store.lookupCalls != 1 || clock.calls != 2 {
		t.Fatalf("same-key archive effects audit=%d operations=%d replay=%d archive=%d lookup=%d clock=%d",
			store.auditCount, len(store.operations), store.replayCalls, store.archiveCalls, store.lookupCalls, clock.calls)
	}
}

type creditCardUnreadBody struct {
	reads int
}

func (body *creditCardUnreadBody) Read([]byte) (int, error) {
	body.reads++
	return 0, errors.New("body must not be read")
}

func TestCreditCardJSONIsStrictBoundedAndOwnerIsServerSide(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
	}{
		{name: "missing content type", body: fullCreditCardBody},
		{name: "wrong content type", body: fullCreditCardBody, contentType: "text/plain"},
		{name: "empty", contentType: "application/json"},
		{name: "malformed", body: `{`, contentType: "application/json"},
		{name: "trailing document", body: fullCreditCardBody + `{}`, contentType: "application/json"},
		{name: "unknown owner", body: strings.TrimSuffix(fullCreditCardBody, "}") + `,"userId":"other"}`, contentType: "application/json"},
		{name: "unknown card number", body: strings.TrimSuffix(fullCreditCardBody, "}") + `,"number":"4111111111111111"}`, contentType: "application/json"},
		{name: "missing name", body: `{"closingDay":10,"dueDay":20}`, contentType: "application/json"},
		{name: "null name", body: `{"name":null,"closingDay":10,"dueDay":20}`, contentType: "application/json"},
		{name: "null optional", body: `{"name":"Reserva","lastFour":null,"closingDay":10,"dueDay":20}`, contentType: "application/json"},
		{name: "nested unknown", body: `{"name":"Reserva","closingDay":10,"dueDay":20,"creditLimit":{"minor":1,"currency":"BRL","available":1}}`, contentType: "application/json"},
		{name: "wrong day type", body: `{"name":"Reserva","closingDay":"10","dueDay":20}`, contentType: "application/json"},
		{name: "oversized", body: `{"name":"` + strings.Repeat("x", 17*1024) + `","closingDay":10,"dueDay":20}`, contentType: "application/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, _, _ := newCreditCardTestHandler(t, testOwner, newCreditCardHTTPStore())
			headers := map[string]string{}
			if test.contentType != "" {
				headers["Content-Type"] = test.contentType
			}
			response := serve(handler, http.MethodPost, "/v1/cards/preview", test.body, headers)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "411111") || strings.Contains(response.Body.String(), "other") {
				t.Fatalf("response leaked input: %s", response.Body.String())
			}
		})
	}
}

func TestCreditCardValidationMatrix(t *testing.T) {
	tests := []string{
		`{"name":"","closingDay":10,"dueDay":20}`,
		`{"name":"` + strings.Repeat("x", 201) + `","closingDay":10,"dueDay":20}`,
		`{"name":"Card","lastFour":"123","closingDay":10,"dueDay":20}`,
		`{"name":"Card","lastFour":"12x4","closingDay":10,"dueDay":20}`,
		`{"name":"Card","brand":"visa","closingDay":10,"dueDay":20}`,
		`{"name":"Card","closingDay":0,"dueDay":20}`,
		`{"name":"Card","closingDay":32,"dueDay":20}`,
		`{"name":"Card","closingDay":10,"dueDay":0}`,
		`{"name":"Card","closingDay":10,"dueDay":32}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"creditLimit":{"minor":0,"currency":"BRL"}}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"creditLimit":{"minor":-1,"currency":"BRL"}}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"creditLimit":{"minor":1,"currency":"USD"}}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"id":"card_00000000000000000000000000000000"}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"owner":"other"}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"status":"ACTIVE"}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"createdAt":"2026-08-25T00:00:00Z"}`,
		`{"name":"Card","closingDay":10,"dueDay":20,"archivedAt":"2026-08-25T00:00:00Z"}`,
		string([]byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 0xff, '"', ',', '"', 'c', 'l', 'o', 's', 'i', 'n', 'g', 'D', 'a', 'y', '"', ':', '1', ',', '"', 'd', 'u', 'e', 'D', 'a', 'y', '"', ':', '2', '}'}),
	}
	for _, body := range tests {
		handler, _, _ := newCreditCardTestHandler(t, testOwner, newCreditCardHTTPStore())
		response := serve(handler, http.MethodPost, "/v1/cards/preview", body,
			map[string]string{"Content-Type": "application/json"})
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("invalid body %s returned %d %s", body, response.Code, response.Body.String())
		}
	}
}

func TestCreditCardOptionalFieldMatrixRoundTripsThroughHTTP(t *testing.T) {
	store := newCreditCardHTTPStore()
	handler, _, _ := newCreditCardTestHandler(t, testOwner, store)
	tests := []struct {
		name      string
		body      string
		present   []string
		forbidden []string
	}{
		{name: "none", body: `{"name":"Sem opcionais","closingDay":1,"dueDay":31}`, forbidden: []string{"lastFour", "brand", "creditLimit"}},
		{name: "last four", body: `{"name":"Com final","lastFour":"1111","closingDay":2,"dueDay":3}`, present: []string{"lastFour"}, forbidden: []string{"brand", "creditLimit"}},
		{name: "brand", body: `{"name":"Com bandeira","brand":"ELO","closingDay":4,"dueDay":5}`, present: []string{"brand"}, forbidden: []string{"lastFour", "creditLimit"}},
		{name: "limit", body: `{"name":"Com limite","closingDay":6,"dueDay":7,"creditLimit":{"minor":1,"currency":"BRL"}}`, present: []string{"creditLimit"}, forbidden: []string{"lastFour", "brand"}},
		{name: "complete", body: fullCreditCardBody, present: []string{"lastFour", "brand", "creditLimit"}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preview := serve(handler, http.MethodPost, "/v1/cards/preview", test.body, map[string]string{"Content-Type": "application/json"})
			if preview.Code != http.StatusOK {
				t.Fatalf("preview = %d %s", preview.Code, preview.Body.String())
			}
			created := serve(handler, http.MethodPost, "/v1/cards", test.body, map[string]string{
				"Content-Type": "application/json", "Idempotency-Key": fmt.Sprintf("optional-%d", index),
			})
			if created.Code != http.StatusCreated {
				t.Fatalf("create = %d %s", created.Code, created.Body.String())
			}
			var response map[string]any
			if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			for _, field := range test.present {
				if _, exists := response[field]; !exists {
					t.Fatalf("create omitted %s: %s", field, created.Body.String())
				}
			}
			for _, field := range test.forbidden {
				if _, exists := response[field]; exists {
					t.Fatalf("create unexpectedly returned %s: %s", field, created.Body.String())
				}
			}
			id, _ := response["id"].(string)
			detail := serve(handler, http.MethodGet, "/v1/cards/"+id, "", nil)
			if detail.Code != http.StatusOK || detail.Body.String() != created.Body.String() {
				t.Fatalf("detail = %d %s; create=%s", detail.Code, detail.Body.String(), created.Body.String())
			}
		})
	}
	listed := serve(handler, http.MethodGet, "/v1/cards", "", nil)
	var listBody struct {
		Items []map[string]any `json:"items"`
	}
	if listed.Code != http.StatusOK || json.Unmarshal(listed.Body.Bytes(), &listBody) != nil || len(listBody.Items) != len(tests) {
		t.Fatalf("list matrix = %d %s", listed.Code, listed.Body.String())
	}
}

func TestCreditCardCreateListGetArchiveAndHistoricalReplay(t *testing.T) {
	store := newCreditCardHTTPStore()
	handler, ids, clock := newCreditCardTestHandler(t, testOwner, store)

	created := serve(handler, http.MethodPost, "/v1/cards", fullCreditCardBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "card-create-key",
	})
	if created.Code != http.StatusCreated || created.Header().Get("Idempotency-Replayed") != "" {
		t.Fatalf("create = %d replay=%q body=%s", created.Code, created.Header().Get("Idempotency-Replayed"), created.Body.String())
	}
	assertFinancialHeaders(t, created)
	for _, expected := range []string{`"id":"card_00000000000000000000000000000001"`, `"status":"ACTIVE"`, `"createdAt":"2026-08-25T15:00:00.000001Z"`} {
		if !strings.Contains(created.Body.String(), expected) {
			t.Fatalf("create missing %s: %s", expected, created.Body.String())
		}
	}

	replayed := serve(handler, http.MethodPost, "/v1/cards", fullCreditCardBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "card-create-key",
	})
	if replayed.Code != http.StatusCreated || replayed.Header().Get("Idempotency-Replayed") != "true" || replayed.Body.String() != created.Body.String() {
		t.Fatalf("create replay = %d replay=%q body=%s", replayed.Code, replayed.Header().Get("Idempotency-Replayed"), replayed.Body.String())
	}
	if ids.calls != 1 || clock.calls != 1 || store.auditCount != 1 || len(store.operations) != 1 {
		t.Fatalf("create effects ids=%d clock=%d audit=%d operations=%d", ids.calls, clock.calls, store.auditCount, len(store.operations))
	}

	conflictBody := strings.Replace(fullCreditCardBody, "Cartão principal", "Cartão alterado", 1)
	conflict := serve(handler, http.MethodPost, "/v1/cards", conflictBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "card-create-key",
	})
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_KEY_REUSED"`) {
		t.Fatalf("create conflict = %d %s", conflict.Code, conflict.Body.String())
	}

	list := serve(handler, http.MethodGet, "/v1/cards", "", nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"items":[{`) || strings.Contains(list.Body.String(), `"userId"`) {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	detail := serve(handler, http.MethodGet, "/v1/cards/card_00000000000000000000000000000001", "", nil)
	if detail.Code != http.StatusOK || detail.Body.String() != created.Body.String() {
		t.Fatalf("detail = %d %s", detail.Code, detail.Body.String())
	}
	missingArchiveKey := serve(handler, http.MethodPost, "/v1/cards/card_00000000000000000000000000000001/archive", "", nil)
	if missingArchiveKey.Code != http.StatusBadRequest || !strings.Contains(missingArchiveKey.Body.String(), `"code":"IDEMPOTENCY_KEY_REQUIRED"`) {
		t.Fatalf("missing archive key = %d %s", missingArchiveKey.Code, missingArchiveKey.Body.String())
	}
	invalidArchiveKey := serve(handler, http.MethodPost, "/v1/cards/card_00000000000000000000000000000001/archive", "", map[string]string{"Idempotency-Key": "   "})
	if invalidArchiveKey.Code != http.StatusBadRequest || !strings.Contains(invalidArchiveKey.Body.String(), `"code":"IDEMPOTENCY_KEY_INVALID"`) {
		t.Fatalf("invalid archive key = %d %s", invalidArchiveKey.Code, invalidArchiveKey.Body.String())
	}

	archived := serve(handler, http.MethodPost, "/v1/cards/card_00000000000000000000000000000001/archive", "", map[string]string{
		"Idempotency-Key": "card-archive-key",
	})
	if archived.Code != http.StatusOK || !strings.Contains(archived.Body.String(), `"status":"ARCHIVED"`) ||
		!strings.Contains(archived.Body.String(), `"archivedAt":"2026-08-25T15:00:00.000002Z"`) {
		t.Fatalf("archive = %d %s", archived.Code, archived.Body.String())
	}
	archiveReplay := serve(handler, http.MethodPost, "/v1/cards/card_00000000000000000000000000000001/archive", "", map[string]string{
		"Idempotency-Key": "card-archive-key",
	})
	if archiveReplay.Code != http.StatusOK || archiveReplay.Header().Get("Idempotency-Replayed") != "true" || archiveReplay.Body.String() != archived.Body.String() {
		t.Fatalf("archive replay = %d replay=%q body=%s", archiveReplay.Code, archiveReplay.Header().Get("Idempotency-Replayed"), archiveReplay.Body.String())
	}
	newKey := serve(handler, http.MethodPost, "/v1/cards/card_00000000000000000000000000000001/archive", "", map[string]string{
		"Idempotency-Key": "card-archive-new-key",
	})
	if newKey.Code != http.StatusConflict || !strings.Contains(newKey.Body.String(), `"code":"CREDIT_CARD_ALREADY_ARCHIVED"`) {
		t.Fatalf("already archived = %d %s", newKey.Code, newKey.Body.String())
	}

	historical := serve(handler, http.MethodPost, "/v1/cards", fullCreditCardBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "card-create-key",
	})
	if historical.Body.String() != created.Body.String() || historical.Header().Get("Idempotency-Replayed") != "true" || strings.Contains(historical.Body.String(), "ARCHIVED") {
		t.Fatalf("historical replay = %d replay=%q body=%s", historical.Code, historical.Header().Get("Idempotency-Replayed"), historical.Body.String())
	}
	if store.cardCount() != 1 || store.auditCount != 2 || len(store.operations) != 2 || clock.calls != 2 {
		t.Fatalf("final effects cards=%d audit=%d operations=%d clock=%d", store.cardCount(), store.auditCount, len(store.operations), clock.calls)
	}
}

func TestCreditCardIdempotencyHeaderValidation(t *testing.T) {
	handler, _, _ := newCreditCardTestHandler(t, testOwner, newCreditCardHTTPStore())
	for _, test := range []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "missing", headers: map[string]string{"Content-Type": "application/json"}, want: "IDEMPOTENCY_KEY_REQUIRED"},
		{name: "empty", headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": ""}, want: "IDEMPOTENCY_KEY_REQUIRED"},
		{name: "whitespace", headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "   "}, want: "IDEMPOTENCY_KEY_INVALID"},
		{name: "invalid characters", headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "key with spaces"}, want: "IDEMPOTENCY_KEY_INVALID"},
		{name: "too long", headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": strings.Repeat("k", 129)}, want: "IDEMPOTENCY_KEY_INVALID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := serve(handler, http.MethodPost, "/v1/cards", fullCreditCardBody, test.headers)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"`+test.want+`"`) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/cards", strings.NewReader(fullCreditCardBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Add("Idempotency-Key", "one")
	request.Header.Add("Idempotency-Key", "two")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"IDEMPOTENCY_KEY_INVALID"`) {
		t.Fatalf("multiple keys = %d %s", response.Code, response.Body.String())
	}
	boundary := serve(handler, http.MethodPost, "/v1/cards", fullCreditCardBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": strings.Repeat("k", 128),
	})
	if boundary.Code != http.StatusCreated {
		t.Fatalf("128-byte key = %d %s", boundary.Code, boundary.Body.String())
	}
}

func TestCreditCardBodylessRoutesOwnerIsolationAndMethods(t *testing.T) {
	store := newCreditCardHTTPStore()
	handlerA, _, _ := newCreditCardTestHandler(t, testOwner, store)
	created := serve(handlerA, http.MethodPost, "/v1/cards", `{"name":"Shared","closingDay":10,"dueDay":20}`, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "owner-create",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	handlerB, _, _ := newCreditCardTestHandler(t, "usr_synthetic_http_other", store)
	unknown := serve(handlerB, http.MethodGet, "/v1/cards/card_ffffffffffffffffffffffffffffffff", "", nil)
	crossOwner := serve(handlerB, http.MethodGet, "/v1/cards/card_00000000000000000000000000000001", "", nil)
	if unknown.Code != http.StatusNotFound || crossOwner.Code != http.StatusNotFound || unknown.Body.String() != crossOwner.Body.String() {
		t.Fatalf("unknown/cross-owner differ: unknown=%d %s cross=%d %s", unknown.Code, unknown.Body.String(), crossOwner.Code, crossOwner.Body.String())
	}
	if list := serve(handlerB, http.MethodGet, "/v1/cards", "", nil); list.Code != http.StatusOK || list.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("owner B list = %d %s", list.Code, list.Body.String())
	}
	invalidID := serve(handlerA, http.MethodGet, "/v1/cards/not-a-card", "", nil)
	if invalidID.Code != http.StatusBadRequest || !strings.Contains(invalidID.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("invalid ID = %d %s", invalidID.Code, invalidID.Body.String())
	}

	for _, route := range []struct {
		method  string
		target  string
		headers map[string]string
	}{
		{method: http.MethodGet, target: "/v1/cards"},
		{method: http.MethodGet, target: "/v1/cards/card_00000000000000000000000000000001"},
		{method: http.MethodPost, target: "/v1/cards/card_00000000000000000000000000000001/archive", headers: map[string]string{"Idempotency-Key": "body"}},
	} {
		if response := serve(handlerA, route.method, route.target, `{}`, route.headers); response.Code != http.StatusBadRequest {
			t.Fatalf("body on %s = %d %s", route.target, response.Code, response.Body.String())
		}
		if response := serve(handlerA, route.method, route.target+"?owner=other", "", route.headers); response.Code != http.StatusBadRequest {
			t.Fatalf("query on %s = %d %s", route.target, response.Code, response.Body.String())
		}
	}

	baselineListCalls := store.listCalls
	for _, method := range []string{http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, target := range []string{"/v1/cards", "/v1/cards/preview", "/v1/cards/card_00000000000000000000000000000001", "/v1/cards/card_00000000000000000000000000000001/archive"} {
			response := serve(handlerA, method, target, "", nil)
			if response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Body.String(), `"code":"METHOD_NOT_ALLOWED"`) {
				t.Fatalf("%s %s = %d %s", method, target, response.Code, response.Body.String())
			}
			assertFinancialHeaders(t, response)
		}
	}
	previewGet := serve(handlerA, http.MethodGet, "/v1/cards/preview", "", nil)
	if previewGet.Code != http.StatusMethodNotAllowed || !strings.Contains(previewGet.Body.String(), `"code":"METHOD_NOT_ALLOWED"`) {
		t.Fatalf("GET preview = %d %s", previewGet.Code, previewGet.Body.String())
	}
	if store.listCalls != baselineListCalls {
		t.Fatalf("rejected methods called list: before=%d after=%d", baselineListCalls, store.listCalls)
	}
}

func TestCreditCardDependencyFailuresAreSafe(t *testing.T) {
	for _, test := range []struct {
		name    string
		setup   func(*creditCardHTTPStore)
		method  string
		target  string
		body    string
		headers map[string]string
	}{
		{name: "create replay lookup", setup: func(store *creditCardHTTPStore) { store.replayErr = errors.New("PRIVATE_DATABASE_MARKER") }, method: http.MethodPost, target: "/v1/cards", body: fullCreditCardBody, headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "failure"}},
		{name: "create persistence", setup: func(store *creditCardHTTPStore) { store.recordErr = errors.New("PRIVATE_DATABASE_MARKER") }, method: http.MethodPost, target: "/v1/cards", body: fullCreditCardBody, headers: map[string]string{"Content-Type": "application/json", "Idempotency-Key": "failure"}},
		{name: "list", setup: func(store *creditCardHTTPStore) { store.listErr = errors.New("PRIVATE_DATABASE_MARKER") }, method: http.MethodGet, target: "/v1/cards"},
		{name: "get", setup: func(store *creditCardHTTPStore) { store.lookupErr = errors.New("PRIVATE_DATABASE_MARKER") }, method: http.MethodGet, target: "/v1/cards/card_00000000000000000000000000000001"},
		{name: "archive replay lookup", setup: func(store *creditCardHTTPStore) { store.replayErr = errors.New("PRIVATE_DATABASE_MARKER") }, method: http.MethodPost, target: "/v1/cards/card_00000000000000000000000000000001/archive", headers: map[string]string{"Idempotency-Key": "failure"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newCreditCardHTTPStore()
			test.setup(store)
			handler, _, _ := newCreditCardTestHandler(t, testOwner, store)
			response := serve(handler, test.method, test.target, test.body, test.headers)
			if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"INTERNAL_ERROR"`) || strings.Contains(response.Body.String(), "PRIVATE") {
				t.Fatalf("unsafe dependency error = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

type creditCardHTTPOperation struct {
	fingerprint application.RequestFingerprint
	card        domain.CreditCard
}

type creditCardHTTPStore struct {
	mu           sync.Mutex
	cards        map[string]domain.CreditCard
	operations   map[string]creditCardHTTPOperation
	auditCount   int
	listCalls    int
	listErr      error
	replayErr    error
	recordErr    error
	lookupErr    error
	replayCalls  int
	recordCalls  int
	lookupCalls  int
	archiveCalls int
}

func newCreditCardHTTPStore() *creditCardHTTPStore {
	return &creditCardHTTPStore{cards: make(map[string]domain.CreditCard), operations: make(map[string]creditCardHTTPOperation)}
}

func (store *creditCardHTTPStore) operationKey(owner, operation, key string) string {
	return owner + "\x00" + operation + "\x00" + key
}

func (store *creditCardHTTPStore) FindCreditCardReplay(_ context.Context, query application.CreditCardReplayQuery) (application.CreditCardReplayLookup, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.replayCalls++
	if store.replayErr != nil {
		return application.CreditCardReplayLookup{}, store.replayErr
	}
	operation, found := store.operations[store.operationKey(query.UserID, query.Operation, query.IdempotencyKey)]
	if !found {
		return application.CreditCardReplayLookup{}, nil
	}
	if operation.fingerprint != query.Fingerprint {
		return application.CreditCardReplayLookup{}, application.ErrCreditCardIdempotencyConflict
	}
	return application.CreditCardReplayLookup{CreditCard: operation.card, Found: true}, nil
}

func (store *creditCardHTTPStore) RecordCreditCard(_ context.Context, command application.IdempotentCreditCardCommand) (application.IdempotentCreditCardResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.recordCalls++
	if store.recordErr != nil {
		return application.IdempotentCreditCardResult{}, store.recordErr
	}
	key := store.operationKey(command.CreditCard.UserID(), command.Operation, command.IdempotencyKey)
	if operation, found := store.operations[key]; found {
		if operation.fingerprint != command.Fingerprint {
			return application.IdempotentCreditCardResult{}, application.ErrCreditCardIdempotencyConflict
		}
		return application.IdempotentCreditCardResult{CreditCard: operation.card, Replayed: true}, nil
	}
	store.cards[command.CreditCard.ID()] = command.CreditCard
	store.operations[key] = creditCardHTTPOperation{fingerprint: command.Fingerprint, card: command.CreditCard}
	store.auditCount++
	return application.IdempotentCreditCardResult{CreditCard: command.CreditCard}, nil
}

func (store *creditCardHTTPStore) ArchiveCreditCard(_ context.Context, command application.IdempotentArchiveCreditCardCommand) (application.IdempotentArchiveCreditCardResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.archiveCalls++
	key := store.operationKey(command.UserID, command.Operation, command.IdempotencyKey)
	if operation, found := store.operations[key]; found {
		if operation.fingerprint != command.Fingerprint {
			return application.IdempotentArchiveCreditCardResult{}, application.ErrCreditCardIdempotencyConflict
		}
		return application.IdempotentArchiveCreditCardResult{CreditCard: operation.card, Replayed: true}, nil
	}
	current, found := store.cards[command.CreditCardID]
	if !found || current.UserID() != command.UserID {
		return application.IdempotentArchiveCreditCardResult{}, application.ErrCreditCardNotFound
	}
	if current.Status() == domain.CreditCardStatusArchived {
		return application.IdempotentArchiveCreditCardResult{}, domain.ErrCreditCardAlreadyArchived
	}
	store.cards[command.CreditCardID] = command.CreditCard
	store.operations[key] = creditCardHTTPOperation{fingerprint: command.Fingerprint, card: command.CreditCard}
	store.auditCount++
	return application.IdempotentArchiveCreditCardResult{CreditCard: command.CreditCard}, nil
}

func (store *creditCardHTTPStore) FindCreditCard(_ context.Context, owner, id string) (application.CreditCardLookup, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.lookupCalls++
	if store.lookupErr != nil {
		return application.CreditCardLookup{}, store.lookupErr
	}
	card, found := store.cards[id]
	if !found || card.UserID() != owner {
		return application.CreditCardLookup{}, nil
	}
	return application.CreditCardLookup{CreditCard: card, Found: true}, nil
}

func (store *creditCardHTTPStore) ListCreditCards(_ context.Context, owner string) ([]domain.CreditCard, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.listCalls++
	if store.listErr != nil {
		return nil, store.listErr
	}
	result := make([]domain.CreditCard, 0)
	for _, card := range store.cards {
		if card.UserID() == owner {
			result = append(result, card)
		}
	}
	return result, nil
}

func (store *creditCardHTTPStore) cardCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.cards)
}

func (store *creditCardHTTPStore) card(id string) domain.CreditCard {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.cards[id]
}

type creditCardHTTPIDGenerator struct {
	calls int
}

func (generator *creditCardHTTPIDGenerator) NewCreditCardID() (string, error) {
	generator.calls++
	return fmt.Sprintf("card_%032x", generator.calls), nil
}

type creditCardHTTPClock struct {
	calls int
}

func (clock *creditCardHTTPClock) Now() time.Time {
	clock.calls++
	return time.Date(2026, time.August, 25, 15, 0, 0, clock.calls*1000, time.UTC)
}

func newCreditCardTestHandler(t *testing.T, owner string, store *creditCardHTTPStore) (http.Handler, *creditCardHTTPIDGenerator, *creditCardHTTPClock) {
	t.Helper()
	ids := &creditCardHTTPIDGenerator{}
	clock := &creditCardHTTPClock{}
	record, err := application.NewRecordCreditCard(store, store, ids, clock)
	if err != nil {
		t.Fatalf("NewRecordCreditCard() error = %v", err)
	}
	list, err := application.NewListCreditCards(store)
	if err != nil {
		t.Fatalf("NewListCreditCards() error = %v", err)
	}
	get, err := application.NewGetCreditCard(store)
	if err != nil {
		t.Fatalf("NewGetCreditCard() error = %v", err)
	}
	archive, err := application.NewArchiveCreditCard(store, store, store, clock)
	if err != nil {
		t.Fatalf("NewArchiveCreditCard() error = %v", err)
	}
	routes := httpapi.NewCreditCard(owner, application.PreviewCreditCard{}, record, list, get, archive)
	mux := http.NewServeMux()
	routes.Register(mux)
	return mux, ids, clock
}
