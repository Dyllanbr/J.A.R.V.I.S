package application

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const (
	MinSessionLifetime = 5 * time.Minute
	MaxSessionLifetime = 30 * 24 * time.Hour
)

var (
	ErrMissingSessionStore         = errors.New("session lifecycle: store is required")
	ErrMissingSessionIDGenerator   = errors.New("session lifecycle: session id generator is required")
	ErrMissingCredentialGenerator  = errors.New("session lifecycle: credential generator is required")
	ErrMissingSessionClock         = errors.New("session lifecycle: clock is required")
	ErrInvalidSessionOwner         = errors.New("session lifecycle: invalid owner")
	ErrInvalidSessionLifetime      = errors.New("session lifecycle: invalid lifetime")
	ErrSessionIDGeneration         = errors.New("session lifecycle: session id generation failed")
	ErrSessionCredentialGeneration = errors.New("session lifecycle: credential generation failed")
	ErrInvalidGeneratedSessionID   = errors.New("session lifecycle: generated session id is invalid")
	ErrInvalidGeneratedCredential  = errors.New("session lifecycle: generated credential is invalid")
	ErrSessionPersistence          = errors.New("session lifecycle: session persistence failed")
	ErrInvalidSessionForRevocation = errors.New("session lifecycle: invalid session for revocation")
	ErrSessionRevocation           = errors.New("session lifecycle: session revocation failed")
)

// SessionIDGenerator provides opaque session identifiers without choosing a
// format in the application layer.
type SessionIDGenerator interface {
	NewSessionID() (string, error)
}

// SessionCredentialGenerator creates a raw bearer credential that is returned
// once to the caller and never persisted by the application.
type SessionCredentialGenerator interface {
	NewSessionCredential() (string, error)
}

// SessionLifecycleStore is the write boundary for session metadata. It does
// not accept raw credentials; only the SHA-256 digest reaches persistence.
type SessionLifecycleStore interface {
	CreateSession(context.Context, domain.AuthenticatedSession) error
	RevokeSession(context.Context, string, string, time.Time) (bool, error)
}

type IssueSessionInput struct {
	OwnerID  string
	Lifetime time.Duration
}

type IssueSessionResult struct {
	session    domain.AuthenticatedSession
	credential string
}

func (result IssueSessionResult) Session() domain.AuthenticatedSession { return result.session }

// Credential is the only accessor for the raw bearer value. Callers should
// transmit it immediately and must not log or persist it outside secure
// platform storage.
func (result IssueSessionResult) Credential() string { return result.credential }

type IssueSession struct {
	store               SessionLifecycleStore
	idGenerator         SessionIDGenerator
	credentialGenerator SessionCredentialGenerator
	clock               Clock
}

func NewIssueSession(store SessionLifecycleStore, idGenerator SessionIDGenerator, credentialGenerator SessionCredentialGenerator, clock Clock) (*IssueSession, error) {
	if store == nil {
		return nil, ErrMissingSessionStore
	}
	if idGenerator == nil {
		return nil, ErrMissingSessionIDGenerator
	}
	if credentialGenerator == nil {
		return nil, ErrMissingCredentialGenerator
	}
	if clock == nil {
		return nil, ErrMissingSessionClock
	}
	return &IssueSession{store: store, idGenerator: idGenerator, credentialGenerator: credentialGenerator, clock: clock}, nil
}

func (useCase *IssueSession) Execute(ctx context.Context, input IssueSessionInput) (IssueSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return IssueSessionResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return IssueSessionResult{}, ErrInvalidSessionOwner
	}
	if input.Lifetime < MinSessionLifetime || input.Lifetime > MaxSessionLifetime {
		return IssueSessionResult{}, ErrInvalidSessionLifetime
	}
	issuedAt := useCase.clock.Now()
	if issuedAt.IsZero() {
		return IssueSessionResult{}, ErrInvalidSessionLifetime
	}
	id, err := useCase.idGenerator.NewSessionID()
	if err != nil {
		return IssueSessionResult{}, newSafeOperationError(ErrSessionIDGeneration, err)
	}
	if !validGeneratedSessionID(id) {
		return IssueSessionResult{}, ErrInvalidGeneratedSessionID
	}
	credential, err := useCase.credentialGenerator.NewSessionCredential()
	if err != nil {
		return IssueSessionResult{}, newSafeOperationError(ErrSessionCredentialGeneration, err)
	}
	if !validGeneratedCredential(credential) {
		return IssueSessionResult{}, ErrInvalidGeneratedCredential
	}
	session, err := domain.NewAuthenticatedSession(domain.AuthenticatedSessionParams{
		ID: id, OwnerID: input.OwnerID, TokenHash: sha256.Sum256([]byte(credential)),
		IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(input.Lifetime),
	})
	if err != nil {
		return IssueSessionResult{}, newSafeOperationError(ErrInvalidGeneratedSessionID, err)
	}
	if err := useCase.store.CreateSession(ctx, session); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return IssueSessionResult{}, err
		}
		return IssueSessionResult{}, newSafeOperationError(ErrSessionPersistence, err)
	}
	if err := ctx.Err(); err != nil {
		return IssueSessionResult{}, err
	}
	return IssueSessionResult{session: session, credential: credential}, nil
}

type RevokeSessionInput struct {
	OwnerID   string
	SessionID string
}

type RevokeSessionResult struct{ Revoked bool }

type RevokeSession struct {
	store SessionLifecycleStore
	clock Clock
}

func NewRevokeSession(store SessionLifecycleStore, clock Clock) (*RevokeSession, error) {
	if store == nil {
		return nil, ErrMissingSessionStore
	}
	if clock == nil {
		return nil, ErrMissingSessionClock
	}
	return &RevokeSession{store: store, clock: clock}, nil
}

func (useCase *RevokeSession) Execute(ctx context.Context, input RevokeSessionInput) (RevokeSessionResult, error) {
	if err := ctx.Err(); err != nil {
		return RevokeSessionResult{}, err
	}
	if domain.ValidateUserID(input.OwnerID) != nil || !validGeneratedSessionID(input.SessionID) {
		return RevokeSessionResult{}, ErrInvalidSessionForRevocation
	}
	revokedAt := useCase.clock.Now()
	if revokedAt.IsZero() {
		return RevokeSessionResult{}, ErrInvalidSessionForRevocation
	}
	revoked, err := useCase.store.RevokeSession(ctx, input.OwnerID, input.SessionID, revokedAt)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return RevokeSessionResult{}, err
		}
		return RevokeSessionResult{}, newSafeOperationError(ErrSessionRevocation, err)
	}
	if err := ctx.Err(); err != nil {
		return RevokeSessionResult{}, err
	}
	return RevokeSessionResult{Revoked: revoked}, nil
}

func validGeneratedSessionID(value string) bool {
	return value != "" && len(value) <= domain.MaxIdentifierBytes && value == strings.TrimSpace(value)
}

func validGeneratedCredential(value string) bool {
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
