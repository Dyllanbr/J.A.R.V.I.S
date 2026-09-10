//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/adapters/randomid"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestAuthenticationLifecycleApplicationToPostgreSQL(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_session_lifecycle_owner"
	insertSyntheticUser(t, ctx, pool, owner)

	repository, err := adapter.NewAuthenticationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewAuthenticationRepository() error = %v", err)
	}
	clock := fixedSessionLifecycleClock{now: time.Date(2099, time.September, 10, 12, 0, 0, 0, time.UTC)}
	issue, err := application.NewIssueSession(
		repository,
		randomid.NewSessionIDGenerator(),
		randomid.NewSessionCredentialGenerator(),
		clock,
	)
	if err != nil {
		t.Fatalf("NewIssueSession() error = %v", err)
	}
	issued, err := issue.Execute(ctx, application.IssueSessionInput{OwnerID: owner, Lifetime: 24 * time.Hour})
	if err != nil {
		t.Fatalf("IssueSession.Execute() error = %v", err)
	}
	principal, err := repository.Authenticate(ctx, issued.Credential())
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.Subject() != owner || principal.SessionID() != issued.Session().ID() {
		t.Fatalf("principal = %q/%q, want %q/%q", principal.Subject(), principal.SessionID(), owner, issued.Session().ID())
	}

	revoke, err := application.NewRevokeSession(repository, clock)
	if err != nil {
		t.Fatalf("NewRevokeSession() error = %v", err)
	}
	revoked, err := revoke.Execute(ctx, application.RevokeSessionInput{OwnerID: owner, SessionID: issued.Session().ID()})
	if err != nil || !revoked.Revoked {
		t.Fatalf("first revoke = %+v/%v, want revoked", revoked, err)
	}
	repeated, err := revoke.Execute(ctx, application.RevokeSessionInput{OwnerID: owner, SessionID: issued.Session().ID()})
	if err != nil || repeated.Revoked {
		t.Fatalf("repeated revoke = %+v/%v, want idempotent false", repeated, err)
	}
	if _, err := repository.Authenticate(ctx, issued.Credential()); !errors.Is(err, adapter.ErrAuthenticationSessionNotFound) {
		t.Fatalf("revoked authentication error = %v, want not found", err)
	}
	var storedHashLength int
	if err := pool.QueryRow(ctx, "SELECT octet_length(token_hash) FROM auth_sessions WHERE id = $1", issued.Session().ID()).Scan(&storedHashLength); err != nil {
		t.Fatalf("stored session lookup failed: %v", err)
	}
	if storedHashLength != sha256.Size {
		t.Fatalf("stored token hash length = %d, want %d", storedHashLength, sha256.Size)
	}
}

func TestAuthenticationLifecycleOwnerIsolationAndConflict(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const (
		ownerA = "usr_session_owner_a"
		ownerB = "usr_session_owner_b"
	)
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)
	repository, err := adapter.NewAuthenticationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewAuthenticationRepository() error = %v", err)
	}
	now := time.Date(2099, time.September, 10, 12, 0, 0, 0, time.UTC)
	first, err := domain.NewAuthenticatedSession(domain.AuthenticatedSessionParams{
		ID: "ses_owner_isolation_001", OwnerID: ownerA, TokenHash: sha256.Sum256([]byte("owner-a-token")),
		IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("NewAuthenticatedSession() error = %v", err)
	}
	if err := repository.CreateSession(ctx, first); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if revoked, err := repository.RevokeSession(ctx, ownerB, first.ID(), now.Add(time.Minute)); err != nil || revoked {
		t.Fatalf("cross-owner revoke = %t/%v, want false", revoked, err)
	}
	if _, err := repository.Authenticate(ctx, "owner-a-token"); err != nil {
		t.Fatalf("cross-owner revoke invalidated owner A session: %v", err)
	}
	if err := repository.CreateSession(ctx, first); !errors.Is(err, adapter.ErrAuthenticationSessionConflict) {
		t.Fatalf("duplicate CreateSession() error = %v, want conflict", err)
	}
}

func TestAuthenticationLifecyclePreservesContextAndSanitizesDatabaseFailures(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const owner = "usr_session_error_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	repository, err := adapter.NewAuthenticationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewAuthenticationRepository() error = %v", err)
	}
	now := time.Date(2099, time.September, 10, 12, 0, 0, 0, time.UTC)
	session, err := domain.NewAuthenticatedSession(domain.AuthenticatedSessionParams{
		ID: "ses_error_001", OwnerID: owner, TokenHash: sha256.Sum256([]byte("error-token")), IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("NewAuthenticatedSession() error = %v", err)
	}
	cancelled, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	if err := repository.CreateSession(cancelled, session); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled CreateSession() error = %v, want context.Canceled", err)
	}
	if _, err := repository.RevokeSession(cancelled, owner, session.ID(), now.Add(time.Minute)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled RevokeSession() error = %v, want context.Canceled", err)
	}
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if err := repository.CreateSession(deadline, session); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline CreateSession() error = %v, want context.DeadlineExceeded", err)
	}
	payload := "postgres://jarvis:jarvis@localhost:55432/jarvis_test"
	pool.Close()
	err = repository.CreateSession(context.Background(), session)
	if err == nil || strings.Contains(err.Error(), payload) || strings.Contains(strings.ToLower(err.Error()), "insert into") {
		t.Fatalf("database CreateSession() error was not sanitized: %v", err)
	}
}

type fixedSessionLifecycleClock struct{ now time.Time }

func (clock fixedSessionLifecycleClock) Now() time.Time { return clock.now }
