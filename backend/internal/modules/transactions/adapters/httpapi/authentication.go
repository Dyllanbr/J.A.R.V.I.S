package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingAuthenticationOwner   = errors.New("authentication middleware: owner is required")
	ErrInvalidAuthenticationOwner   = errors.New("authentication middleware: owner is invalid")
	ErrMissingAuthenticationUseCase = errors.New("authentication middleware: authenticate use case is required")
)

const sessionBootstrapPath = "/v1/auth/sessions"

type principalContextKey struct{}

// AuthenticationMiddleware is the opt-in HTTP boundary for the authenticated
// single-owner beta. It verifies one opaque bearer credential and then checks
// that its server-verified subject matches the owner composed into the app.
// The request never supplies or overrides that owner.
type AuthenticationMiddleware struct {
	ownerID      string
	authenticate *application.AuthenticatePrincipal
}

// NewAuthenticationMiddleware validates the server-owned identity boundary.
// Credential issuance, revocation, and multi-owner authorization are outside
// this boundary and remain separate application work.
func NewAuthenticationMiddleware(ownerID string, authenticate *application.AuthenticatePrincipal) (*AuthenticationMiddleware, error) {
	if ownerID == "" {
		return nil, ErrMissingAuthenticationOwner
	}
	if err := domain.ValidateUserID(ownerID); err != nil {
		return nil, ErrInvalidAuthenticationOwner
	}
	if authenticate == nil {
		return nil, ErrMissingAuthenticationUseCase
	}
	return &AuthenticationMiddleware{ownerID: ownerID, authenticate: authenticate}, nil
}

// Wrap protects every financial route while preserving the unauthenticated
// health probe used by process orchestration.
func (middleware *AuthenticationMiddleware) Wrap(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" {
			next.ServeHTTP(response, request)
			return
		}
		// Session bootstrap is the only unauthenticated financial route. It is
		// protected by the server-configured bootstrap secret in the
		// session lifecycle handler; all other routes require a bearer session.
		if request.Method == http.MethodPost && request.URL.Path == sessionBootstrapPath {
			next.ServeHTTP(response, request)
			return
		}

		credential, ok := bearerCredential(request)
		if !ok {
			writeError(response, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		result, err := middleware.authenticate.Execute(request.Context(), application.AuthenticatePrincipalInput{Credential: credential})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			writeError(response, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		if result.Principal().Subject() != middleware.ownerID {
			writeError(response, http.StatusForbidden, "FORBIDDEN", "forbidden")
			return
		}
		request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, result.Principal()))
		next.ServeHTTP(response, request)
	})
}

func authenticatedPrincipal(request *http.Request) (domain.AuthenticatedPrincipal, bool) {
	principal, ok := request.Context().Value(principalContextKey{}).(domain.AuthenticatedPrincipal)
	return principal, ok
}

func bearerCredential(request *http.Request) (string, bool) {
	values := request.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	if strings.IndexFunc(parts[1], func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return "", false
	}
	return parts[1], true
}
