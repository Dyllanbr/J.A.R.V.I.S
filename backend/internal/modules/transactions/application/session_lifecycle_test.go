package application_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type sessionLifecycleStore struct {
	mu           sync.Mutex
	created      []domain.AuthenticatedSession
	createErr    error
	revokeCalls  int
	revokeResult bool
	revokeErr    error
}

func (store *sessionLifecycleStore) CreateSession(_ context.Context, session domain.AuthenticatedSession) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.createErr != nil {
		return store.createErr
	}
	store.created = append(store.created, session)
	return nil
}

func (store *sessionLifecycleStore) RevokeSession(_ context.Context, _, _ string, _ time.Time) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.revokeCalls++
	return store.revokeResult, store.revokeErr
}

type sessionIDGenerator struct {
	id    string
	err   error
	calls int
}

func (generator *sessionIDGenerator) NewSessionID() (string, error) {
	generator.calls++
	return generator.id, generator.err
}

type sessionCredentialGenerator struct {
	credential string
	err        error
	calls      int
}

func (generator *sessionCredentialGenerator) NewSessionCredential() (string, error) {
	generator.calls++
	return generator.credential, generator.err
}

type sessionClock struct{ now time.Time }

func (clock sessionClock) Now() time.Time { return clock.now }

func validSessionDependencies() (*sessionLifecycleStore, *sessionIDGenerator, *sessionCredentialGenerator, sessionClock) {
	return &sessionLifecycleStore{}, &sessionIDGenerator{id: "ses-issued-001"}, &sessionCredentialGenerator{credential: "opaque-token-001"}, sessionClock{now: time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)}
}

func TestIssueSessionCreatesOneOpaqueSessionAndNeverStoresRawCredential(t *testing.T) {
	store, ids, credentials, clock := validSessionDependencies()
	useCase, err := application.NewIssueSession(store, ids, credentials, clock)
	if err != nil {
		t.Fatalf("NewIssueSession() error = %v", err)
	}
	result, err := useCase.Execute(context.Background(), application.IssueSessionInput{OwnerID: "usr-owner-a", Lifetime: 24 * time.Hour})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Credential() != "opaque-token-001" || result.Session().OwnerID() != "usr-owner-a" {
		t.Fatalf("result = %q/%q", result.Credential(), result.Session().OwnerID())
	}
	if len(store.created) != 1 || ids.calls != 1 || credentials.calls != 1 {
		t.Fatalf("calls/created = %d/%d/%d", ids.calls, credentials.calls, len(store.created))
	}
	storedHash := store.created[0].TokenHash()
	if string(storedHash[:]) == result.Credential() {
		t.Fatal("raw credential was passed to persistence")
	}
	if store.created[0].TokenHash() != sha256.Sum256([]byte(result.Credential())) {
		t.Fatal("persisted digest does not match the issued credential")
	}
}

func TestIssueSessionValidatesBeforeGeneratingAndSanitizesFailures(t *testing.T) {
	store, ids, credentials, clock := validSessionDependencies()
	useCase, _ := application.NewIssueSession(store, ids, credentials, clock)
	for _, testCase := range []struct {
		name  string
		input application.IssueSessionInput
		want  error
	}{
		{name: "owner", input: application.IssueSessionInput{OwnerID: "", Lifetime: time.Hour}, want: application.ErrInvalidSessionOwner},
		{name: "lifetime", input: application.IssueSessionInput{OwnerID: "usr-owner-a", Lifetime: time.Minute}, want: application.ErrInvalidSessionLifetime},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := useCase.Execute(context.Background(), testCase.input)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
			if ids.calls != 0 || credentials.calls != 0 || len(store.created) != 0 {
				t.Fatalf("invalid input reached dependencies")
			}
		})
	}
	store.createErr = errors.New("INSERT auth_sessions DSN=private")
	_, err := useCase.Execute(context.Background(), application.IssueSessionInput{OwnerID: "usr-owner-a", Lifetime: time.Hour})
	if !errors.Is(err, application.ErrSessionPersistence) || strings.Contains(err.Error(), "DSN") {
		t.Fatalf("error = %v, want sanitized persistence error", err)
	}
}

func TestIssueSessionPreservesContextCancellation(t *testing.T) {
	store, ids, credentials, clock := validSessionDependencies()
	useCase, _ := application.NewIssueSession(store, ids, credentials, clock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(ctx, application.IssueSessionInput{OwnerID: "usr-owner-a", Lifetime: time.Hour}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestRevokeSessionCallsStoreOnceAndPreservesAbsentResult(t *testing.T) {
	store, _, _, clock := validSessionDependencies()
	store.revokeResult = false
	useCase, err := application.NewRevokeSession(store, clock)
	if err != nil {
		t.Fatalf("NewRevokeSession() error = %v", err)
	}
	result, err := useCase.Execute(context.Background(), application.RevokeSessionInput{OwnerID: "usr-owner-a", SessionID: "ses-issued-001"})
	if err != nil || result.Revoked || store.revokeCalls != 1 {
		t.Fatalf("result/error/calls = %+v/%v/%d", result, err, store.revokeCalls)
	}
}

func TestRevokeSessionValidatesBeforeStoreAndSanitizesErrors(t *testing.T) {
	store, _, _, clock := validSessionDependencies()
	useCase, _ := application.NewRevokeSession(store, clock)
	if _, err := useCase.Execute(context.Background(), application.RevokeSessionInput{OwnerID: "", SessionID: "ses-issued-001"}); !errors.Is(err, application.ErrInvalidSessionForRevocation) {
		t.Fatalf("error = %v", err)
	}
	if store.revokeCalls != 0 {
		t.Fatal("invalid owner reached store")
	}
	store.revokeErr = errors.New("UPDATE auth_sessions DSN=private")
	_, err := useCase.Execute(context.Background(), application.RevokeSessionInput{OwnerID: "usr-owner-a", SessionID: "ses-issued-001"})
	if !errors.Is(err, application.ErrSessionRevocation) || strings.Contains(err.Error(), "DSN") {
		t.Fatalf("error = %v, want sanitized revocation error", err)
	}
}

func TestRevokeSessionPreservesDeadlineExceeded(t *testing.T) {
	store, _, _, clock := validSessionDependencies()
	useCase, _ := application.NewRevokeSession(store, clock)
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	if _, err := useCase.Execute(ctx, application.RevokeSessionInput{OwnerID: "usr-owner-a", SessionID: "ses-issued-001"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}
