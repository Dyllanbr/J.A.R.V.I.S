package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
)

func TestCardPurchaseRoutesRejectQueriesBeforeApplication(t *testing.T) {
	handler := http.NewServeMux()
	httpapi.NewCardPurchase("usr_http_stage3", nil, nil).Register(handler)
	for _, path := range []string{"/v1/card-purchases/preview?owner=spoofed", "/v1/card-purchases?userId=spoofed"} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"not":"decoded"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("%s = %d %s", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s missing security headers", path)
		}
	}
}

func TestCardPurchaseRoutesRejectImplicitHEAD(t *testing.T) {
	handler := http.NewServeMux()
	httpapi.NewCardPurchase("usr_http_stage3", nil, nil).Register(handler)
	for _, path := range []string{"/v1/card-purchases/preview", "/v1/card-purchases"} {
		request := httptest.NewRequest(http.MethodHead, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Body.String(), `"code":"METHOD_NOT_ALLOWED"`) {
			t.Fatalf("HEAD %s = %d %s", path, response.Code, response.Body.String())
		}
	}
}

func TestInstallmentPlanRoutesRejectQueriesAndHEAD(t *testing.T) {
	handler := http.NewServeMux()
	httpapi.NewInstallmentPlan("usr_http_stage3", nil, nil, nil, nil).Register(handler)
	request := httptest.NewRequest(http.MethodGet, "/v1/installment-plans?limit=10", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("query status = %d, want 400", response.Code)
	}
	request = httptest.NewRequest(http.MethodHead, "/v1/installment-plans", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD status = %d, want 405", response.Code)
	}
}

func TestFourBRoutesRejectEveryUnexpectedQueryBeforeApplication(t *testing.T) {
	type route struct {
		name, method, path string
	}
	routes := []route{
		{"card purchase preview", http.MethodPost, "/v1/card-purchases/preview"},
		{"card purchase create", http.MethodPost, "/v1/card-purchases"},
		{"plan list", http.MethodGet, "/v1/installment-plans"},
		{"plan detail", http.MethodGet, "/v1/installment-plans/ipl_00000000000000000000000000000001"},
		{"cancellation preview", http.MethodPost, "/v1/installment-plans/ipl_00000000000000000000000000000001/cancellation-preview"},
		{"cancel", http.MethodPost, "/v1/installment-plans/ipl_00000000000000000000000000000001/cancel"},
	}
	queries := []string{"foo=bar", "foo=", "foo;bar=baz", "foo=%ZZ", "owner=spoofed", "userId=spoofed"}
	for _, route := range routes {
		for _, query := range queries {
			t.Run(route.name+"/"+query, func(t *testing.T) {
				handler := newFourBEmptyHandler()
				request := httptest.NewRequest(route.method, route.path+"?"+query, strings.NewReader(`{"invalid":true}`))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", "query-must-not-reach-use-case")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				assertFourBError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
			})
		}
	}
}

func TestFourBRoutesRejectAllUnsupportedMethodsBeforeApplication(t *testing.T) {
	paths := []string{
		"/v1/card-purchases/preview",
		"/v1/card-purchases",
		"/v1/installment-plans",
		"/v1/installment-plans/ipl_00000000000000000000000000000001",
		"/v1/installment-plans/ipl_00000000000000000000000000000001/cancellation-preview",
		"/v1/installment-plans/ipl_00000000000000000000000000000001/cancel",
	}
	methods := []string{http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace, http.MethodConnect}
	for _, path := range paths {
		for _, method := range methods {
			t.Run(method+" "+path, func(t *testing.T) {
				handler := newFourBEmptyHandler()
				request := httptest.NewRequest(method, path, strings.NewReader(`{"unexpected":true}`))
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				assertFourBError(t, response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
			})
		}
	}
}

func TestFourBRoutesRejectInvalidBodiesBeforeApplication(t *testing.T) {
	validPurchase := `{"description":"Compra","amount":{"minor":100,"currency":"BRL"},"occurredAt":"2026-08-25T14:00:00Z","creditCardId":"card_00000000000000000000000000000001"}`
	validCancel := `{"expectedCancelledOn":"2026-08-30"}`
	cardRoutes := []struct {
		name, path string
		header     map[string]string
	}{
		{"preview", "/v1/card-purchases/preview", map[string]string{}},
		{"create", "/v1/card-purchases", map[string]string{"Idempotency-Key": "invalid-body"}},
	}
	invalidPurchaseBodies := []struct {
		name string
		body string
		head map[string]string
	}{
		{"missing content type", validPurchase, nil},
		{"wrong content type", validPurchase, map[string]string{"Content-Type": "text/plain"}},
		{"malformed json", "{", map[string]string{"Content-Type": "application/json"}},
		{"invalid utf8", string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), map[string]string{"Content-Type": "application/json"}},
		{"trailing json", validPurchase + "{}", map[string]string{"Content-Type": "application/json"}},
		{"unknown top level", `{"owner":"spoofed"}`, map[string]string{"Content-Type": "application/json"}},
		{"nested unknown", `{"description":"Compra","amount":{"minor":100,"currency":"BRL","extra":true},"occurredAt":"2026-08-25T14:00:00Z","creditCardId":"card_00000000000000000000000000000001"}`, map[string]string{"Content-Type": "application/json"}},
		{"user injection", `{"userId":"spoofed"}`, map[string]string{"Content-Type": "application/json"}},
		{"server field injection", `{"status":"ACTIVE"}`, map[string]string{"Content-Type": "application/json"}},
		{"too large", strings.Repeat("x", 17*1024), map[string]string{"Content-Type": "application/json"}},
	}
	for _, route := range cardRoutes {
		for _, invalid := range invalidPurchaseBodies {
			t.Run(route.name+"/"+invalid.name, func(t *testing.T) {
				handler := newFourBEmptyHandler()
				headers := map[string]string{}
				for name, value := range route.header {
					headers[name] = value
				}
				for name, value := range invalid.head {
					headers[name] = value
				}
				response := serve(handler, http.MethodPost, route.path, invalid.body, headers)
				assertFourBError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
			})
		}
	}

	bodyless := []struct {
		name, method, path string
	}{
		{"list body", http.MethodGet, "/v1/installment-plans"},
		{"detail body", http.MethodGet, "/v1/installment-plans/ipl_00000000000000000000000000000001"},
		{"cancellation preview body", http.MethodPost, "/v1/installment-plans/ipl_00000000000000000000000000000001/cancellation-preview"},
	}
	for _, route := range bodyless {
		t.Run(route.name, func(t *testing.T) {
			handler := newFourBEmptyHandler()
			response := serve(handler, route.method, route.path, `{"unexpected":true}`, nil)
			assertFourBError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
		})
	}

	invalidCancelBodies := []struct {
		name string
		body string
		head map[string]string
	}{
		{"missing content type", validCancel, nil},
		{"wrong content type", validCancel, map[string]string{"Content-Type": "text/plain"}},
		{"malformed json", "{", map[string]string{"Content-Type": "application/json"}},
		{"invalid utf8", string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), map[string]string{"Content-Type": "application/json"}},
		{"trailing json", validCancel + "{}", map[string]string{"Content-Type": "application/json"}},
		{"unknown owner", `{"owner":"spoofed"}`, map[string]string{"Content-Type": "application/json"}},
		{"too large", strings.Repeat("x", 17*1024), map[string]string{"Content-Type": "application/json"}},
	}
	for _, invalid := range invalidCancelBodies {
		t.Run("cancel/"+invalid.name, func(t *testing.T) {
			handler := newFourBEmptyHandler()
			headers := map[string]string{"Idempotency-Key": "invalid-cancel-body"}
			for name, value := range invalid.head {
				headers[name] = value
			}
			response := serve(handler, http.MethodPost, "/v1/installment-plans/ipl_00000000000000000000000000000001/cancel", invalid.body, headers)
			assertFourBError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
		})
	}
}

func newFourBEmptyHandler() http.Handler {
	mux := http.NewServeMux()
	httpapi.NewCardPurchase("usr_http_stage3", nil, nil).Register(mux)
	httpapi.NewInstallmentPlan("usr_http_stage3", nil, nil, nil, nil).Register(mux)
	return mux
}

func assertFourBError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("response = %d %s, want %d/%s", response.Code, response.Body.String(), status, code)
	}
	assertFinancialHeaders(t, response)
}
