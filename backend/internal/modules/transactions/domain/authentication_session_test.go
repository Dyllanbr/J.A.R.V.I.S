package domain_test

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestAuthenticatedSessionIsImmutableAndNormalizesClaims(t *testing.T) {
	issuedAt := time.Date(2026, time.September, 10, 12, 0, 0, 123, time.FixedZone("test", -3*60*60))
	expiresAt := issuedAt.Add(24 * time.Hour)
	revokedAt := issuedAt.Add(time.Hour)
	hash := sha256.Sum256([]byte("synthetic-session-token"))
	session, err := domain.NewAuthenticatedSession(domain.AuthenticatedSessionParams{
		ID: "ses-immutable-001", OwnerID: "usr-auth-001", TokenHash: hash,
		IssuedAt: issuedAt, ExpiresAt: expiresAt, RevokedAt: &revokedAt,
	})
	if err != nil {
		t.Fatalf("NewAuthenticatedSession() error = %v", err)
	}
	if session.ID() != "ses-immutable-001" || session.OwnerID() != "usr-auth-001" {
		t.Fatalf("claims = %q/%q", session.ID(), session.OwnerID())
	}
	if !session.IssuedAt().Equal(issuedAt.UTC().Round(0)) || !session.ExpiresAt().Equal(expiresAt.UTC().Round(0)) {
		t.Fatal("instants were not normalized")
	}
	gotHash := session.TokenHash()
	gotHash[0] ^= 0xff
	if session.TokenHash() != hash {
		t.Fatal("token hash was mutable through an accessor")
	}
	gotRevokedAt := session.RevokedAt()
	*gotRevokedAt = gotRevokedAt.Add(time.Hour)
	if !session.RevokedAt().Equal(revokedAt.UTC().Round(0)) {
		t.Fatal("revoked timestamp was mutable through an accessor")
	}
	if !session.IsRevoked() || !session.StructurallyValid() {
		t.Fatal("session should be revoked and structurally valid")
	}
}

func TestAuthenticatedSessionRejectsInvalidClaims(t *testing.T) {
	issuedAt := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	valid := domain.AuthenticatedSessionParams{
		ID: "ses-valid-001", OwnerID: "usr-auth-001", TokenHash: sha256.Sum256([]byte("synthetic-token")), IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
	}
	tests := []struct {
		name   string
		mutate func(*domain.AuthenticatedSessionParams)
		want   error
	}{
		{name: "id", mutate: func(value *domain.AuthenticatedSessionParams) { value.ID = "" }, want: domain.ErrInvalidSessionID},
		{name: "owner", mutate: func(value *domain.AuthenticatedSessionParams) { value.OwnerID = "" }, want: domain.ErrInvalidSessionOwnerID},
		{name: "token hash", mutate: func(value *domain.AuthenticatedSessionParams) { value.TokenHash = [32]byte{} }, want: domain.ErrInvalidSessionTokenHash},
		{name: "issued at", mutate: func(value *domain.AuthenticatedSessionParams) { value.IssuedAt = time.Time{} }, want: domain.ErrInvalidSessionIssuedAt},
		{name: "expiry missing", mutate: func(value *domain.AuthenticatedSessionParams) { value.ExpiresAt = time.Time{} }, want: domain.ErrInvalidSessionExpiresAt},
		{name: "expiry not after issue", mutate: func(value *domain.AuthenticatedSessionParams) { value.ExpiresAt = value.IssuedAt }, want: domain.ErrInvalidSessionExpiresAt},
		{name: "revoked before issue", mutate: func(value *domain.AuthenticatedSessionParams) {
			revokedAt := value.IssuedAt.Add(-time.Nanosecond)
			value.RevokedAt = &revokedAt
		}, want: domain.ErrInvalidSessionRevokedAt},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			params := valid
			testCase.mutate(&params)
			if _, err := domain.NewAuthenticatedSession(params); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}
