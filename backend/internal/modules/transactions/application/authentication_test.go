package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type authenticatorFake struct {
	principal  domain.AuthenticatedPrincipal
	err        error
	calls      int
	credential string
}

func (fake *authenticatorFake) Authenticate(_ context.Context, credential string) (domain.AuthenticatedPrincipal, error) {
	fake.calls++
	fake.credential = credential
	return fake.principal, fake.err
}

func validPrincipal(t *testing.T) domain.AuthenticatedPrincipal {
	t.Helper()
	principal, err := domain.NewAuthenticatedPrincipal(domain.AuthenticatedPrincipalParams{
		Subject: "usr-auth-001", SessionID: "ses-auth-001",
		IssuedAt:  time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, time.September, 9, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("principal fixture: %v", err)
	}
	return principal
}

func TestAuthenticatePrincipalUsesOneAuthenticatorAndReturnsPrincipal(t *testing.T) {
	fake := &authenticatorFake{principal: validPrincipal(t)}
	useCase, err := application.NewAuthenticatePrincipal(fake)
	if err != nil {
		t.Fatalf("constructor error = %v", err)
	}
	result, err := useCase.Execute(context.Background(), application.AuthenticatePrincipalInput{Credential: "opaque-token"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if fake.calls != 1 || fake.credential != "opaque-token" {
		t.Fatalf("authenticator calls/credential = %d/%q", fake.calls, fake.credential)
	}
	if result.Principal().Subject() != "usr-auth-001" {
		t.Fatalf("subject = %q", result.Principal().Subject())
	}
}

func TestAuthenticatePrincipalValidatesBeforeReadingAndSanitizesFailures(t *testing.T) {
	fake := &authenticatorFake{principal: validPrincipal(t), err: errors.New("sql: secret DSN")}
	useCase, err := application.NewAuthenticatePrincipal(fake)
	if err != nil {
		t.Fatalf("constructor error = %v", err)
	}
	if _, err := useCase.Execute(context.Background(), application.AuthenticatePrincipalInput{}); !errors.Is(err, application.ErrInvalidCredential) || fake.calls != 0 {
		t.Fatalf("invalid credential error/calls = %v/%d", err, fake.calls)
	}
	if _, err := useCase.Execute(context.Background(), application.AuthenticatePrincipalInput{Credential: "token"}); !errors.Is(err, application.ErrAuthenticationFailed) || fake.calls != 1 || errors.Is(err, fake.err) {
		t.Fatalf("sanitized failure/calls = %v/%d", err, fake.calls)
	}
}

func TestAuthenticatePrincipalPreservesContextErrors(t *testing.T) {
	for _, expected := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(expected.Error(), func(t *testing.T) {
			fake := &authenticatorFake{principal: validPrincipal(t), err: expected}
			useCase, err := application.NewAuthenticatePrincipal(fake)
			if err != nil {
				t.Fatalf("constructor error = %v", err)
			}
			if _, err := useCase.Execute(context.Background(), application.AuthenticatePrincipalInput{Credential: "token"}); !errors.Is(err, expected) {
				t.Fatalf("error = %v, want %v", err, expected)
			}
		})
	}
}

func TestAuthenticatePrincipalRejectsMalformedAdapterPrincipal(t *testing.T) {
	malformed := domain.AuthenticatedPrincipal{}
	fake := &authenticatorFake{principal: malformed}
	useCase, err := application.NewAuthenticatePrincipal(fake)
	if err != nil {
		t.Fatalf("constructor error = %v", err)
	}
	if _, err := useCase.Execute(context.Background(), application.AuthenticatePrincipalInput{Credential: "token"}); !errors.Is(err, application.ErrInvalidAuthenticatedPrincipal) {
		t.Fatalf("error = %v, want invalid principal", err)
	}
}
