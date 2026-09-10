package domain

import (
	"crypto/sha256"
	"errors"
	"time"
)

var (
	ErrInvalidSessionID        = errors.New("authentication session: invalid id")
	ErrInvalidSessionOwnerID   = errors.New("authentication session: invalid owner id")
	ErrInvalidSessionTokenHash = errors.New("authentication session: invalid token hash")
	ErrInvalidSessionIssuedAt  = errors.New("authentication session: invalid issued at")
	ErrInvalidSessionExpiresAt = errors.New("authentication session: invalid expires at")
	ErrInvalidSessionRevokedAt = errors.New("authentication session: invalid revoked at")
)

// AuthenticatedSession is the immutable persistence boundary for an opaque
// bearer session. Raw credentials never enter the domain and are returned
// only once by the issuing application service.
type AuthenticatedSession struct {
	id        string
	ownerID   string
	tokenHash [sha256.Size]byte
	issuedAt  time.Time
	expiresAt time.Time
	revokedAt *time.Time
}

type AuthenticatedSessionParams struct {
	ID        string
	OwnerID   string
	TokenHash [sha256.Size]byte
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// NewAuthenticatedSession validates the durable claims used by the session
// store. It performs no I/O, clock access or credential generation.
func NewAuthenticatedSession(params AuthenticatedSessionParams) (AuthenticatedSession, error) {
	if !isValidIdentifier(params.ID) {
		return AuthenticatedSession{}, ErrInvalidSessionID
	}
	if err := ValidateUserID(params.OwnerID); err != nil {
		return AuthenticatedSession{}, ErrInvalidSessionOwnerID
	}
	if isZeroSessionTokenHash(params.TokenHash) {
		return AuthenticatedSession{}, ErrInvalidSessionTokenHash
	}
	if params.IssuedAt.IsZero() {
		return AuthenticatedSession{}, ErrInvalidSessionIssuedAt
	}
	if params.ExpiresAt.IsZero() || !params.ExpiresAt.After(params.IssuedAt) {
		return AuthenticatedSession{}, ErrInvalidSessionExpiresAt
	}
	if params.RevokedAt != nil {
		revokedAt := normalizeInstant(*params.RevokedAt)
		if revokedAt.Before(normalizeInstant(params.IssuedAt)) {
			return AuthenticatedSession{}, ErrInvalidSessionRevokedAt
		}
		params.RevokedAt = &revokedAt
	}
	return AuthenticatedSession{
		id:        params.ID,
		ownerID:   params.OwnerID,
		tokenHash: params.TokenHash,
		issuedAt:  normalizeInstant(params.IssuedAt),
		expiresAt: normalizeInstant(params.ExpiresAt),
		revokedAt: copyTimePointer(params.RevokedAt),
	}, nil
}

func (session AuthenticatedSession) ID() string      { return session.id }
func (session AuthenticatedSession) OwnerID() string { return session.ownerID }

// TokenHash returns the digest only; callers cannot mutate the session value.
func (session AuthenticatedSession) TokenHash() [sha256.Size]byte { return session.tokenHash }

func (session AuthenticatedSession) IssuedAt() time.Time  { return session.issuedAt }
func (session AuthenticatedSession) ExpiresAt() time.Time { return session.expiresAt }

func (session AuthenticatedSession) RevokedAt() *time.Time {
	return copyTimePointer(session.revokedAt)
}

func (session AuthenticatedSession) IsRevoked() bool { return session.revokedAt != nil }

func (session AuthenticatedSession) StructurallyValid() bool {
	_, err := NewAuthenticatedSession(AuthenticatedSessionParams{
		ID: session.id, OwnerID: session.ownerID, TokenHash: session.tokenHash,
		IssuedAt: session.issuedAt, ExpiresAt: session.expiresAt, RevokedAt: session.revokedAt,
	})
	return err == nil
}

func copyTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyOfValue := normalizeInstant(*value)
	return &copyOfValue
}

func isZeroSessionTokenHash(value [sha256.Size]byte) bool {
	for _, byteValue := range value {
		if byteValue != 0 {
			return false
		}
	}
	return true
}
