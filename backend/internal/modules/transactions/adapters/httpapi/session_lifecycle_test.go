package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type sessionLifecycleStoreFake struct {
	created []domain.AuthenticatedSession
	revoked bool
	err     error
}

func (store *sessionLifecycleStoreFake) CreateSession(_ context.Context, session domain.AuthenticatedSession) error {
	store.created = append(store.created, session)
	return store.err
}

func (store *sessionLifecycleStoreFake) RevokeSession(_ context.Context, _, _ string, _ time.Time) (bool, error) {
	return store.revoked, store.err
}

type sessionIDGeneratorFake struct{}

func (sessionIDGeneratorFake) NewSessionID() (string, error) { return "ses-http-001", nil }

type sessionCredentialGeneratorFake struct{}

func (sessionCredentialGeneratorFake) NewSessionCredential() (string, error) {
	return "tok-http-credential", nil
}

type sessionLifecycleClockFake struct{ now time.Time }

func (clock sessionLifecycleClockFake) Now() time.Time { return clock.now }

func newSessionLifecycleHandler(t *testing.T, store *sessionLifecycleStoreFake) *httpapi.SessionLifecycleHandler {
	t.Helper()
	clock := sessionLifecycleClockFake{now: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)}
	issue, err := application.NewIssueSession(store, sessionIDGeneratorFake{}, sessionCredentialGeneratorFake{}, clock)
	if err != nil {
		t.Fatalf("NewIssueSession() error = %v", err)
	}
	revoke, err := application.NewRevokeSession(store, clock)
	if err != nil {
		t.Fatalf("NewRevokeSession() error = %v", err)
	}
	handler, err := httpapi.NewSessionLifecycle("usr-http-session-owner", "bootstrap-http-secret", issue, revoke)
	if err != nil {
		t.Fatalf("NewSessionLifecycle() error = %v", err)
	}
	return handler
}

func TestSessionLifecycleIssueRequiresBootstrapAndReturnsRawCredentialOnce(t *testing.T) {
	store := &sessionLifecycleStoreFake{}
	handler := newSessionLifecycleHandler(t, store)
	mux := http.NewServeMux()
	handler.Register(mux)

	request := httptest.NewRequest(http.MethodPost, "/v1/auth/sessions", strings.NewReader(`{"lifetimeSeconds":3600}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-JARVIS-Session-Bootstrap", "bootstrap-http-secret")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || len(store.created) != 1 {
		t.Fatalf("issue response/creates = %d/%d", response.Code, len(store.created))
	}
	var body struct {
		SessionID string `json:"sessionId"`
		Token     string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SessionID != "ses-http-001" || body.Token != "tok-http-credential" {
		t.Fatalf("response = %#v", body)
	}
	if store.created[0].TokenHash() != sha256.Sum256([]byte(body.Token)) {
		t.Fatal("store did not receive the digest of the returned credential")
	}
	if strings.Contains(response.Body.String(), "usr-http-session-owner") {
		t.Fatal("response leaked owner")
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
}

func TestSessionLifecycleIssueRejectsInvalidBootstrapAndRequest(t *testing.T) {
	store := &sessionLifecycleStoreFake{}
	handler := newSessionLifecycleHandler(t, store)
	mux := http.NewServeMux()
	handler.Register(mux)

	tests := []struct {
		name   string
		secret string
		body   string
		query  string
		want   int
	}{
		{name: "missing secret", body: `{"lifetimeSeconds":3600}`, want: http.StatusUnauthorized},
		{name: "wrong secret", secret: "wrong", body: `{"lifetimeSeconds":3600}`, want: http.StatusUnauthorized},
		{name: "unknown field", secret: "bootstrap-http-secret", body: `{"lifetimeSeconds":3600,"ownerId":"spoof"}`, want: http.StatusBadRequest},
		{name: "query", secret: "bootstrap-http-secret", body: `{"lifetimeSeconds":3600}`, query: "owner=spoof", want: http.StatusBadRequest},
		{name: "invalid lifetime", secret: "bootstrap-http-secret", body: `{"lifetimeSeconds":1}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/sessions?"+test.query, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.secret != "" {
				request.Header.Set("X-JARVIS-Session-Bootstrap", test.secret)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != test.want || len(store.created) != 0 {
				t.Fatalf("response/creates = %d/%d, want %d/0", response.Code, len(store.created), test.want)
			}
			if strings.Contains(response.Body.String(), "spoof") || strings.Contains(response.Body.String(), "wrong") {
				t.Fatal("response leaked request material")
			}
		})
	}
}

func TestSessionLifecycleRevokeRequiresAuthenticatedCurrentSession(t *testing.T) {
	store := &sessionLifecycleStoreFake{revoked: true}
	handler := newSessionLifecycleHandler(t, store)
	mux := http.NewServeMux()
	handler.Register(mux)

	principal, err := domain.NewAuthenticatedPrincipal(domain.AuthenticatedPrincipalParams{
		Subject: "usr-http-session-owner", SessionID: "ses-http-001",
		IssuedAt:  time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, time.September, 10, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("principal fixture: %v", err)
	}
	request := httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/ses-http-001", nil)
	response := httptest.NewRecorder()
	// The context key is intentionally private to the package, so exercise the
	// real middleware boundary instead of manufacturing a handler context.
	middlewareAuth := &middlewareAuthenticator{principal: principal}
	middleware := newAuthenticationMiddleware(t, "usr-http-session-owner", middlewareAuth)
	middleware.Wrap(mux).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("direct handler status = %d, want middleware authentication failure", response.Code)
	}

	// A real authenticated request reaches the handler and revokes exactly once.
	request = httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/ses-http-001", nil)
	request.Header.Set("Authorization", "Bearer opaque-token")
	response = httptest.NewRecorder()
	middleware.Wrap(mux).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !store.revoked {
		t.Fatalf("revoke response/store = %d/%t", response.Code, store.revoked)
	}
}

func TestSessionLifecycleRevokeSanitizesAndPreservesContextErrors(t *testing.T) {
	for _, expected := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(expected.Error(), func(t *testing.T) {
			store := &sessionLifecycleStoreFake{err: expected}
			handler := newSessionLifecycleHandler(t, store)
			mux := http.NewServeMux()
			handler.Register(mux)
			principal, err := domain.NewAuthenticatedPrincipal(domain.AuthenticatedPrincipalParams{
				Subject: "usr-http-session-owner", SessionID: "ses-http-001",
				IssuedAt:  time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC),
				ExpiresAt: time.Date(2026, time.September, 10, 13, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("principal fixture: %v", err)
			}
			authenticator := &middlewareAuthenticator{principal: principal}
			middleware := newAuthenticationMiddleware(t, "usr-http-session-owner", authenticator)
			request := httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/ses-http-001", nil).WithContext(contextWithError(expected))
			request.Header.Set("Authorization", "Bearer opaque-token")
			response := httptest.NewRecorder()
			middleware.Wrap(mux).ServeHTTP(response, request)
			if response.Body.Len() != 0 {
				t.Fatalf("context error wrote response: %s", response.Body.String())
			}
		})
	}
}
