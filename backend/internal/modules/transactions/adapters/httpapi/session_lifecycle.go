package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingSessionLifecycleOwner  = errors.New("session lifecycle http: owner is required")
	ErrInvalidSessionLifecycleOwner  = errors.New("session lifecycle http: owner is invalid")
	ErrMissingSessionBootstrapSecret = errors.New("session lifecycle http: bootstrap secret is required")
	ErrInvalidSessionBootstrapSecret = errors.New("session lifecycle http: bootstrap secret is invalid")
	ErrMissingSessionIssueUseCase    = errors.New("session lifecycle http: issue use case is required")
	ErrMissingSessionRevokeUseCase   = errors.New("session lifecycle http: revoke use case is required")
)

// SessionLifecycleHandler exposes the deliberately narrow local bootstrap and
// self-revocation boundary. Issuance requires a server-configured bootstrap
// secret; revocation requires the bearer principal established by the auth
// middleware. Neither operation accepts an owner from the request.
type SessionLifecycleHandler struct {
	ownerID   string
	bootstrap string
	issue     *application.IssueSession
	revoke    *application.RevokeSession
}

func NewSessionLifecycle(ownerID, bootstrap string, issue *application.IssueSession, revoke *application.RevokeSession) (*SessionLifecycleHandler, error) {
	if ownerID == "" {
		return nil, ErrMissingSessionLifecycleOwner
	}
	if domain.ValidateUserID(ownerID) != nil {
		return nil, ErrInvalidSessionLifecycleOwner
	}
	if !validBootstrapSecret(bootstrap) {
		if bootstrap == "" {
			return nil, ErrMissingSessionBootstrapSecret
		}
		return nil, ErrInvalidSessionBootstrapSecret
	}
	if issue == nil {
		return nil, ErrMissingSessionIssueUseCase
	}
	if revoke == nil {
		return nil, ErrMissingSessionRevokeUseCase
	}
	return &SessionLifecycleHandler{ownerID: ownerID, bootstrap: bootstrap, issue: issue, revoke: revoke}, nil
}

func (handler *SessionLifecycleHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST "+sessionBootstrapPath, handler.issueSession)
	mux.HandleFunc("DELETE /v1/auth/sessions/{sessionID}", handler.revokeSession)
	mux.HandleFunc(sessionBootstrapPath, methodNotAllowed)
	mux.HandleFunc("/v1/auth/sessions/{sessionID}", methodNotAllowed)
	mux.HandleFunc("/v1/auth/sessions/", handler.malformedPath)
}

type sessionIssueRequest struct {
	LifetimeSeconds *int64 `json:"lifetimeSeconds"`
}

type sessionIssueResponse struct {
	SessionID string    `json:"sessionId"`
	Token     string    `json:"token"`
	IssuedAt  time.Time `json:"issuedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (handler *SessionLifecycleHandler) issueSession(response http.ResponseWriter, request *http.Request) {
	if !handler.validBootstrapRequest(request) {
		writeError(response, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) {
		writeInvalidRequest(response)
		return
	}
	body, ok := decodeSessionIssueBody(response, request)
	if !ok {
		return
	}
	result, err := handler.issue.Execute(request.Context(), application.IssueSessionInput{
		OwnerID:  handler.ownerID,
		Lifetime: time.Duration(*body.LifetimeSeconds) * time.Second,
	})
	if err != nil {
		if isContextTermination(err) {
			return
		}
		handler.writeIssueError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, sessionIssueResponse{
		SessionID: result.Session().ID(), Token: result.Credential(),
		IssuedAt: result.Session().IssuedAt(), ExpiresAt: result.Session().ExpiresAt(),
	})
}

func (handler *SessionLifecycleHandler) revokeSession(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) || !decodeEmptyBody(response, request) {
		return
	}
	principal, ok := authenticatedPrincipal(request)
	if !ok || principal.Subject() != handler.ownerID {
		writeError(response, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	sessionID := request.PathValue("sessionID")
	if !validSessionPathID(sessionID) || sessionID != principal.SessionID() {
		writeError(response, http.StatusNotFound, "SESSION_NOT_FOUND", "session was not found")
		return
	}
	result, err := handler.revoke.Execute(request.Context(), application.RevokeSessionInput{OwnerID: handler.ownerID, SessionID: sessionID})
	if err != nil {
		if isContextTermination(err) {
			return
		}
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
		return
	}
	if !result.Revoked {
		writeError(response, http.StatusNotFound, "SESSION_NOT_FOUND", "session was not found")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusNoContent)
}

func (handler *SessionLifecycleHandler) malformedPath(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost && request.Method != http.MethodDelete {
		methodNotAllowed(response, request)
		return
	}
	writeInvalidRequest(response)
}

func (handler *SessionLifecycleHandler) validBootstrapRequest(request *http.Request) bool {
	if len(request.Header.Values("X-JARVIS-Session-Bootstrap")) != 1 {
		return false
	}
	if len(request.Header.Values("Authorization")) != 0 {
		return false
	}
	provided := request.Header.Get("X-JARVIS-Session-Bootstrap")
	return subtle.ConstantTimeCompare([]byte(provided), []byte(handler.bootstrap)) == 1
}

func decodeSessionIssueBody(response http.ResponseWriter, request *http.Request) (sessionIssueRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return sessionIssueRequest{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(raw) {
		writeInvalidRequest(response)
		return sessionIssueRequest{}, false
	}
	var body sessionIssueRequest
	if !decodeStrict(raw, &body) || body.LifetimeSeconds == nil || *body.LifetimeSeconds <= 0 {
		writeInvalidRequest(response)
		return sessionIssueRequest{}, false
	}
	if *body.LifetimeSeconds > int64(application.MaxSessionLifetime/time.Second) {
		writeInvalidRequest(response)
		return sessionIssueRequest{}, false
	}
	return body, true
}

func (handler *SessionLifecycleHandler) writeIssueError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidSessionOwner), errors.Is(err, application.ErrInvalidSessionLifetime),
		errors.Is(err, application.ErrInvalidGeneratedSessionID), errors.Is(err, application.ErrInvalidGeneratedCredential):
		writeInvalidRequest(response)
	case errors.Is(err, application.ErrSessionPersistence):
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func validBootstrapSecret(value string) bool {
	if value == "" || len(value) > 4096 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '!' || value[index] > '~' {
			return false
		}
	}
	return true
}

func validSessionPathID(value string) bool {
	if value == "" || len(value) > domain.MaxIdentifierBytes || value != strings.TrimSpace(value) {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '!' || value[index] > '~' {
			return false
		}
	}
	return true
}

func isContextTermination(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
