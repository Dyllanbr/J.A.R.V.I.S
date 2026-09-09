package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidPrincipalSubject   = errors.New("authentication: invalid principal subject")
	ErrInvalidPrincipalSessionID = errors.New("authentication: invalid principal session id")
	ErrInvalidPrincipalIssuedAt  = errors.New("authentication: invalid principal issued at")
	ErrInvalidPrincipalExpiresAt = errors.New("authentication: invalid principal expires at")
)

// AuthenticatedPrincipal is the immutable identity boundary used by
// application services. It contains no credential material and is not a
// persisted aggregate.
type AuthenticatedPrincipal struct {
	subject   string
	sessionID string
	issuedAt  time.Time
	expiresAt time.Time
}

// AuthenticatedPrincipalParams contains the server-verified claims needed to
// construct a principal. Credentials and transport details are deliberately
// absent from this value.
type AuthenticatedPrincipalParams struct {
	Subject   string
	SessionID string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// NewAuthenticatedPrincipal validates the minimum claims shared by any
// future authentication mechanism. It does not choose a protocol, persist a
// session, or inspect a clock.
func NewAuthenticatedPrincipal(params AuthenticatedPrincipalParams) (AuthenticatedPrincipal, error) {
	if err := ValidateUserID(params.Subject); err != nil {
		return AuthenticatedPrincipal{}, ErrInvalidPrincipalSubject
	}
	if !isValidIdentifier(params.SessionID) {
		return AuthenticatedPrincipal{}, ErrInvalidPrincipalSessionID
	}
	if params.IssuedAt.IsZero() {
		return AuthenticatedPrincipal{}, ErrInvalidPrincipalIssuedAt
	}
	if params.ExpiresAt.IsZero() || !params.ExpiresAt.After(params.IssuedAt) {
		return AuthenticatedPrincipal{}, ErrInvalidPrincipalExpiresAt
	}
	return AuthenticatedPrincipal{
		subject:   params.Subject,
		sessionID: params.SessionID,
		issuedAt:  normalizeInstant(params.IssuedAt),
		expiresAt: normalizeInstant(params.ExpiresAt),
	}, nil
}

func (principal AuthenticatedPrincipal) Subject() string   { return principal.subject }
func (principal AuthenticatedPrincipal) SessionID() string { return principal.sessionID }
func (principal AuthenticatedPrincipal) IssuedAt() time.Time {
	return principal.issuedAt
}
func (principal AuthenticatedPrincipal) ExpiresAt() time.Time {
	return principal.expiresAt
}

// StructurallyValid rechecks the immutable value before an application
// service accepts a principal produced by an adapter.
func (principal AuthenticatedPrincipal) StructurallyValid() bool {
	_, err := NewAuthenticatedPrincipal(AuthenticatedPrincipalParams{
		Subject: principal.subject, SessionID: principal.sessionID,
		IssuedAt: principal.issuedAt, ExpiresAt: principal.expiresAt,
	})
	return err == nil
}
