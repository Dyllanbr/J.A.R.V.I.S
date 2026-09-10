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

type middlewareAuthenticator struct {
	principal  domain.AuthenticatedPrincipal
	err        error
	calls      int
	credential string
}

func (authenticator *middlewareAuthenticator) Authenticate(_ context.Context, credential string) (domain.AuthenticatedPrincipal, error) {
	authenticator.calls++
	authenticator.credential = credential
	return authenticator.principal, authenticator.err
}

func TestAuthenticationMiddlewareAllowsHealthWithoutCredentials(t *testing.T) {
	authenticator := &middlewareAuthenticator{}
	middleware := newAuthenticationMiddleware(t, "usr-middleware-owner", authenticator)
	nextCalls := 0
	handler := middleware.Wrap(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		nextCalls++
		response.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || nextCalls != 1 || authenticator.calls != 0 {
		t.Fatalf("health response/calls = %d/%d/%d", response.Code, nextCalls, authenticator.calls)
	}
}

func TestAuthenticationMiddlewareRejectsMissingAndMalformedCredentialsBeforeReading(t *testing.T) {
	for _, header := range []string{"", "Basic credential", "Bearer", "Bearer one two", "Bearer tok\ten"} {
		t.Run(strings.ReplaceAll(header, "\t", "-tab-"), func(t *testing.T) {
			authenticator := &middlewareAuthenticator{principal: validMiddlewarePrincipal(t)}
			middleware := newAuthenticationMiddleware(t, "usr-middleware-owner", authenticator)
			nextCalls := 0
			handler := middleware.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalls++ }))
			request := httptest.NewRequest(http.MethodGet, "/v1/cards", nil)
			if header != "" {
				request.Header.Set("Authorization", header)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			assertAuthenticationError(t, response, http.StatusUnauthorized, "UNAUTHENTICATED")
			if authenticator.calls != 0 || nextCalls != 0 {
				t.Fatalf("malformed credential reached authenticator/handler: %d/%d", authenticator.calls, nextCalls)
			}
		})
	}

	authenticator := &middlewareAuthenticator{principal: validMiddlewarePrincipal(t)}
	middleware := newAuthenticationMiddleware(t, "usr-middleware-owner", authenticator)
	request := httptest.NewRequest(http.MethodGet, "/v1/cards", nil)
	request.Header.Add("Authorization", "Bearer token-a")
	request.Header.Add("Authorization", "Bearer token-b")
	response := httptest.NewRecorder()
	middleware.Wrap(http.NotFoundHandler()).ServeHTTP(response, request)
	assertAuthenticationError(t, response, http.StatusUnauthorized, "UNAUTHENTICATED")
	if authenticator.calls != 0 {
		t.Fatalf("multiple Authorization headers reached authenticator: %d", authenticator.calls)
	}
}

func TestAuthenticationMiddlewareAuthenticatesOnceAndKeepsOwnerServerSide(t *testing.T) {
	authenticator := &middlewareAuthenticator{principal: validMiddlewarePrincipal(t)}
	middleware := newAuthenticationMiddleware(t, "usr-authenticated-owner", authenticator)
	nextCalls := 0
	handler := middleware.Wrap(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		nextCalls++
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/v1/cards?owner=spoof", nil)
	request.Header.Set("Authorization", "bEaReR opaque-token")
	request.Header.Set("X-Owner-ID", "spoof")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || authenticator.calls != 1 || authenticator.credential != "opaque-token" || nextCalls != 1 {
		t.Fatalf("authenticated response/calls/token = %d/%d/%q/%d", response.Code, authenticator.calls, authenticator.credential, nextCalls)
	}
}

func TestAuthenticationMiddlewareAllowsOnlySessionBootstrapPostToReachHandler(t *testing.T) {
	authenticator := &middlewareAuthenticator{principal: validMiddlewarePrincipal(t)}
	middleware := newAuthenticationMiddleware(t, "usr-authenticated-owner", authenticator)
	nextCalls := 0
	handler := middleware.Wrap(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		nextCalls++
		if request.Method != http.MethodPost || request.URL.Path != "/v1/auth/sessions" {
			t.Fatalf("unexpected bootstrap request: %s %s", request.Method, request.URL.Path)
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/sessions", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || nextCalls != 1 || authenticator.calls != 0 {
		t.Fatalf("bootstrap response/calls = %d/%d/%d", response.Code, nextCalls, authenticator.calls)
	}
}

func TestAuthenticationMiddlewareRejectsSessionBootstrapOnOtherMethods(t *testing.T) {
	authenticator := &middlewareAuthenticator{principal: validMiddlewarePrincipal(t)}
	middleware := newAuthenticationMiddleware(t, "usr-authenticated-owner", authenticator)
	request := httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil)
	response := httptest.NewRecorder()
	middleware.Wrap(http.NotFoundHandler()).ServeHTTP(response, request)
	assertAuthenticationError(t, response, http.StatusUnauthorized, "UNAUTHENTICATED")
}

func TestAuthenticationMiddlewareRejectsDifferentSubjectWithoutLeakingOwner(t *testing.T) {
	authenticator := &middlewareAuthenticator{principal: validMiddlewarePrincipal(t)}
	middleware := newAuthenticationMiddleware(t, "usr-other-owner", authenticator)
	nextCalls := 0
	handler := middleware.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalls++ }))
	request := httptest.NewRequest(http.MethodGet, "/v1/cards", nil)
	request.Header.Set("Authorization", "Bearer opaque-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assertAuthenticationError(t, response, http.StatusForbidden, "FORBIDDEN")
	if nextCalls != 0 || authenticator.calls != 1 || strings.Contains(response.Body.String(), "usr-authenticated-owner") {
		t.Fatalf("cross-owner response/calls = %d/%d/%s", response.Code, authenticator.calls, response.Body.String())
	}
}

func TestAuthenticationMiddlewareSanitizesFailuresAndPreservesContextCancellation(t *testing.T) {
	authenticator := &middlewareAuthenticator{err: errors.New("SELECT auth_sessions DSN=private-marker")}
	middleware := newAuthenticationMiddleware(t, "usr-middleware-owner", authenticator)
	request := httptest.NewRequest(http.MethodGet, "/v1/cards", nil)
	request.Header.Set("Authorization", "Bearer opaque-token")
	response := httptest.NewRecorder()
	middleware.Wrap(http.NotFoundHandler()).ServeHTTP(response, request)
	assertAuthenticationError(t, response, http.StatusUnauthorized, "UNAUTHENTICATED")
	if strings.Contains(response.Body.String(), "private-marker") || strings.Contains(response.Body.String(), "SELECT") {
		t.Fatalf("authentication failure leaked internal details: %s", response.Body.String())
	}

	for _, expected := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(expected.Error(), func(t *testing.T) {
			authenticator := &middlewareAuthenticator{err: expected}
			middleware := newAuthenticationMiddleware(t, "usr-middleware-owner", authenticator)
			request := httptest.NewRequest(http.MethodGet, "/v1/cards", nil)
			request.Header.Set("Authorization", "Bearer opaque-token")
			request = request.WithContext(contextWithError(expected))
			response := httptest.NewRecorder()
			middleware.Wrap(http.NotFoundHandler()).ServeHTTP(response, request)
			if response.Body.Len() != 0 || authenticator.calls != 0 {
				t.Fatalf("context error wrote response or called authenticator: %d/%d", response.Body.Len(), authenticator.calls)
			}
		})
	}
}

func TestAuthenticationMiddlewareConstructorValidatesDependencies(t *testing.T) {
	useCase, err := application.NewAuthenticatePrincipal(&middlewareAuthenticator{})
	if err != nil {
		t.Fatalf("NewAuthenticatePrincipal() error = %v", err)
	}
	for _, testCase := range []struct {
		name  string
		owner string
		auth  *application.AuthenticatePrincipal
		want  error
	}{
		{name: "missing owner", want: httpapi.ErrMissingAuthenticationOwner},
		{name: "invalid owner", owner: " owner ", auth: useCase, want: httpapi.ErrInvalidAuthenticationOwner},
		{name: "missing use case", owner: "usr-middleware-owner", want: httpapi.ErrMissingAuthenticationUseCase},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := httpapi.NewAuthenticationMiddleware(testCase.owner, testCase.auth)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("constructor error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func newAuthenticationMiddleware(t *testing.T, owner string, authenticator *middlewareAuthenticator) *httpapi.AuthenticationMiddleware {
	t.Helper()
	useCase, err := application.NewAuthenticatePrincipal(authenticator)
	if err != nil {
		t.Fatalf("NewAuthenticatePrincipal() error = %v", err)
	}
	middleware, err := httpapi.NewAuthenticationMiddleware(owner, useCase)
	if err != nil {
		t.Fatalf("NewAuthenticationMiddleware() error = %v", err)
	}
	return middleware
}

func validMiddlewarePrincipal(t *testing.T) domain.AuthenticatedPrincipal {
	t.Helper()
	principal, err := domain.NewAuthenticatedPrincipal(domain.AuthenticatedPrincipalParams{
		Subject: "usr-authenticated-owner", SessionID: "ses-middleware-001",
		IssuedAt:  time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, time.September, 9, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("principal fixture: %v", err)
	}
	return principal
}

func assertAuthenticationError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("authentication response = %d %s, want %d/%s", response.Code, response.Body.String(), status, code)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers = %q/%q", response.Header().Get("Cache-Control"), response.Header().Get("X-Content-Type-Options"))
	}
}

type contextError struct{ err error }

func (ctx contextError) Deadline() (time.Time, bool) { return time.Time{}, false }
func (ctx contextError) Done() <-chan struct{}       { return nil }
func (ctx contextError) Err() error                  { return ctx.err }
func (ctx contextError) Value(any) any               { return nil }

func contextWithError(err error) context.Context { return contextError{err: err} }
