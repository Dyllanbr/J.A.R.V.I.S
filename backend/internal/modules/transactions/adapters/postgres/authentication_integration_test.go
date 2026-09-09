//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
)

func TestAuthenticationRepositoryReadsActiveSessionsWithoutWrites(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const (
		ownerA      = "usr_auth_owner_a"
		ownerB      = "usr_auth_owner_b"
		credentialA = "synthetic-credential-a"
		credentialB = "synthetic-credential-b"
	)
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 11)
	})
	assertTableExists(t, ctx, pool, "auth_sessions", true)

	now := time.Date(2099, time.September, 9, 18, 0, 0, 0, time.UTC)
	issuedAt := now.Add(-time.Minute)
	expiresAt := now.Add(time.Hour)
	insertAuthenticationSession(t, ctx, pool, "ses_auth_a", ownerA, credentialA, issuedAt, expiresAt, nil)
	insertAuthenticationSession(t, ctx, pool, "ses_auth_b", ownerB, credentialB, issuedAt, expiresAt, nil)

	repository, err := adapter.NewAuthenticationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewAuthenticationRepository() error = %v", err)
	}
	useCase, err := application.NewAuthenticatePrincipal(repository)
	if err != nil {
		t.Fatalf("NewAuthenticatePrincipal() error = %v", err)
	}

	before := countAuthenticationSessions(t, ctx, pool)
	resultA, err := useCase.Execute(ctx, application.AuthenticatePrincipalInput{Credential: credentialA})
	if err != nil {
		t.Fatalf("owner A authentication failed: %v", err)
	}
	if resultA.Principal().Subject() != ownerA || resultA.Principal().SessionID() != "ses_auth_a" {
		t.Fatalf("owner A principal = %#v, want owner A session", resultA.Principal())
	}
	resultB, err := useCase.Execute(ctx, application.AuthenticatePrincipalInput{Credential: credentialB})
	if err != nil {
		t.Fatalf("owner B authentication failed: %v", err)
	}
	if resultB.Principal().Subject() != ownerB || resultB.Principal().SessionID() != "ses_auth_b" {
		t.Fatalf("owner B principal = %#v, want owner B session", resultB.Principal())
	}
	if _, err := useCase.Execute(ctx, application.AuthenticatePrincipalInput{Credential: "unknown-credential"}); !errors.Is(err, application.ErrAuthenticationFailed) {
		t.Fatalf("unknown credential error = %v, want sanitized authentication failure", err)
	}
	if after := countAuthenticationSessions(t, ctx, pool); after != before {
		t.Fatalf("authentication reads changed session count from %d to %d", before, after)
	}
}

func TestAuthenticationRepositoryRejectsRevokedAndExpiredSessions(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_auth_lifecycle_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	now := time.Date(2099, time.September, 9, 18, 0, 0, 0, time.UTC)
	expiredAt := time.Date(2020, time.September, 9, 18, 0, 0, 0, time.UTC)
	insertAuthenticationSession(t, ctx, pool, "ses_auth_revoked", owner, "revoked-credential", now, now.Add(time.Hour), &now)
	insertAuthenticationSession(t, ctx, pool, "ses_auth_expired", owner, "expired-credential", expiredAt.Add(-2*time.Hour), expiredAt.Add(-time.Hour), nil)
	repository, err := adapter.NewAuthenticationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewAuthenticationRepository() error = %v", err)
	}
	for _, credential := range []string{"revoked-credential", "expired-credential"} {
		if _, err := repository.Authenticate(ctx, credential); !errors.Is(err, adapter.ErrAuthenticationSessionNotFound) {
			t.Fatalf("credential %q error = %v, want not found", credential, err)
		}
	}
}

func TestAuthenticationRepositoryPreservesContextAndSanitizesDatabaseErrors(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const owner = "usr_auth_context_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	repository, err := adapter.NewAuthenticationRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewAuthenticationRepository() error = %v", err)
	}

	cancelled, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	if _, err := repository.Authenticate(cancelled, "cancelled-credential"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled authentication error = %v, want context.Canceled", err)
	}
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if _, err := repository.Authenticate(deadline, "deadline-credential"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline authentication error = %v, want context.DeadlineExceeded", err)
	}

	payload := "postgres://jarvis:jarvis@localhost:55432/jarvis_test"
	pool.Close()
	_, err = repository.Authenticate(context.Background(), "database-error-credential")
	if err == nil || strings.Contains(err.Error(), payload) || strings.Contains(strings.ToLower(err.Error()), "select") {
		t.Fatalf("database authentication error was not sanitized: %v", err)
	}
}

func insertAuthenticationSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID, ownerID, credential string, issuedAt, expiresAt time.Time, revokedAt *time.Time) {
	t.Helper()
	// This helper is intentionally kept local to the integration fixture. The
	// production adapter never receives raw credentials and never writes rows.
	digest := sha256.Sum256([]byte(credential))
	_, err := pool.Exec(ctx, `
		INSERT INTO auth_sessions (id, user_id, token_hash, issued_at, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $4)
	`, sessionID, ownerID, digest[:], issuedAt, expiresAt, revokedAt)
	if err != nil {
		t.Fatalf("inserting authentication session fixture failed: %v", err)
	}
}

func countAuthenticationSessions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM auth_sessions").Scan(&count); err != nil {
		t.Fatalf("authentication session count failed: %v", err)
	}
	return count
}
