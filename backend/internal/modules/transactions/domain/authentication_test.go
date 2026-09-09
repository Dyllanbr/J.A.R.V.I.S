package domain_test

import (
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestAuthenticatedPrincipalIsImmutableAndNormalizesInstants(t *testing.T) {
	issuedAt := time.Date(2026, time.September, 9, 12, 0, 0, 123, time.FixedZone("test", -3*60*60))
	expiresAt := issuedAt.Add(time.Hour)
	principal, err := domain.NewAuthenticatedPrincipal(domain.AuthenticatedPrincipalParams{
		Subject: "usr-auth-001", SessionID: "ses-auth-001", IssuedAt: issuedAt, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("NewAuthenticatedPrincipal() error = %v", err)
	}
	if principal.Subject() != "usr-auth-001" || principal.SessionID() != "ses-auth-001" {
		t.Fatalf("principal claims = %q/%q", principal.Subject(), principal.SessionID())
	}
	if !principal.IssuedAt().Equal(issuedAt.UTC().Round(0)) || !principal.ExpiresAt().Equal(expiresAt.UTC().Round(0)) {
		t.Fatalf("principal instants were not normalized")
	}
	if !principal.StructurallyValid() {
		t.Fatal("principal should remain structurally valid")
	}
}

func TestAuthenticatedPrincipalRejectsInvalidClaims(t *testing.T) {
	valid := domain.AuthenticatedPrincipalParams{
		Subject: "usr-auth-001", SessionID: "ses-auth-001",
		IssuedAt:  time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, time.September, 9, 13, 0, 0, 0, time.UTC),
	}
	tests := []struct {
		name   string
		params domain.AuthenticatedPrincipalParams
		want   error
	}{
		{name: "subject", params: func() domain.AuthenticatedPrincipalParams { p := valid; p.Subject = ""; return p }(), want: domain.ErrInvalidPrincipalSubject},
		{name: "session", params: func() domain.AuthenticatedPrincipalParams { p := valid; p.SessionID = ""; return p }(), want: domain.ErrInvalidPrincipalSessionID},
		{name: "issued at", params: func() domain.AuthenticatedPrincipalParams { p := valid; p.IssuedAt = time.Time{}; return p }(), want: domain.ErrInvalidPrincipalIssuedAt},
		{name: "expiry missing", params: func() domain.AuthenticatedPrincipalParams { p := valid; p.ExpiresAt = time.Time{}; return p }(), want: domain.ErrInvalidPrincipalExpiresAt},
		{name: "expiry before issue", params: func() domain.AuthenticatedPrincipalParams { p := valid; p.ExpiresAt = p.IssuedAt; return p }(), want: domain.ErrInvalidPrincipalExpiresAt},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := domain.NewAuthenticatedPrincipal(testCase.params); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}
