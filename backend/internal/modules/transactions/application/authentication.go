package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingSessionAuthenticator   = errors.New("authentication: authenticator is required")
	ErrInvalidCredential             = errors.New("authentication: invalid credential")
	ErrAuthenticationFailed          = errors.New("authentication: authentication failed")
	ErrInvalidAuthenticatedPrincipal = errors.New("authentication: invalid authenticated principal")
)

// SessionAuthenticator is the single application boundary for a future
// transport-specific authentication adapter. It returns only a verified,
// immutable principal and never exposes credential material to callers.
type SessionAuthenticator interface {
	Authenticate(context.Context, string) (domain.AuthenticatedPrincipal, error)
}

// AuthenticatePrincipal validates the credential envelope, invokes exactly
// one authenticator read, and sanitizes adapter failures. It has no Clock,
// persistence, ID generation, or financial write dependency.
type AuthenticatePrincipal struct {
	authenticator SessionAuthenticator
}

func NewAuthenticatePrincipal(authenticator SessionAuthenticator) (*AuthenticatePrincipal, error) {
	if authenticator == nil {
		return nil, ErrMissingSessionAuthenticator
	}
	return &AuthenticatePrincipal{authenticator: authenticator}, nil
}

type AuthenticatePrincipalInput struct {
	Credential string
}

type AuthenticatePrincipalResult struct {
	principal domain.AuthenticatedPrincipal
}

func (result AuthenticatePrincipalResult) Principal() domain.AuthenticatedPrincipal {
	return result.principal
}

func (useCase *AuthenticatePrincipal) Execute(ctx context.Context, input AuthenticatePrincipalInput) (AuthenticatePrincipalResult, error) {
	if err := ctx.Err(); err != nil {
		return AuthenticatePrincipalResult{}, err
	}
	if input.Credential == "" {
		return AuthenticatePrincipalResult{}, ErrInvalidCredential
	}
	principal, err := useCase.authenticator.Authenticate(ctx, input.Credential)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return AuthenticatePrincipalResult{}, err
		}
		return AuthenticatePrincipalResult{}, ErrAuthenticationFailed
	}
	if err := ctx.Err(); err != nil {
		return AuthenticatePrincipalResult{}, err
	}
	if !principal.StructurallyValid() {
		return AuthenticatePrincipalResult{}, ErrInvalidAuthenticatedPrincipal
	}
	return AuthenticatePrincipalResult{principal: principal}, nil
}
