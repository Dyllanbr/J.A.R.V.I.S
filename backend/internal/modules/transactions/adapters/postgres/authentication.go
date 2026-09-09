package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingAuthenticationPool       = errors.New("authentication postgres repository: pool is required")
	ErrInvalidAuthenticationTimeout    = errors.New("authentication postgres repository: operation timeout is invalid")
	ErrInvalidAuthenticationCredential = errors.New("authentication postgres repository: credential is invalid")
	ErrAuthenticationSessionNotFound   = errors.New("authentication postgres repository: session not found")
	ErrAuthenticationSessionRead       = errors.New("authentication postgres repository: session read failed")
	ErrAuthenticationSessionInvalid    = errors.New("authentication postgres repository: stored session is invalid")
	ErrBeginAuthenticationTransaction  = errors.New("authentication postgres repository: transaction begin failed")
	ErrCommitAuthenticationTransaction = errors.New("authentication postgres repository: transaction commit failed")
)

// AuthenticationRepository validates opaque bearer credentials against a
// server-side session record. Raw credentials are never persisted or exposed.
// The adapter is read-only in this stage; session issuance and revocation are
// separate application operations.
type AuthenticationRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var _ application.SessionAuthenticator = (*AuthenticationRepository)(nil)

func NewAuthenticationRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*AuthenticationRepository, error) {
	if pool == nil {
		return nil, ErrMissingAuthenticationPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidAuthenticationTimeout
	}
	return &AuthenticationRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

func (repository *AuthenticationRepository) Authenticate(ctx context.Context, credential string) (domain.AuthenticatedPrincipal, error) {
	if err := ctx.Err(); err != nil {
		return domain.AuthenticatedPrincipal{}, err
	}
	if repository == nil || repository.pool == nil {
		return domain.AuthenticatedPrincipal{}, ErrMissingAuthenticationPool
	}
	if !validAuthenticationCredential(credential) {
		return domain.AuthenticatedPrincipal{}, ErrInvalidAuthenticationCredential
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	tx, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return domain.AuthenticatedPrincipal{}, contextErr
		}
		return domain.AuthenticatedPrincipal{}, newRepositoryError(ErrBeginAuthenticationTransaction, err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackContext, cancelRollback := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancelRollback()
		_ = tx.Rollback(rollbackContext)
	}()

	digest := sha256.Sum256([]byte(credential))
	var sessionID, ownerID string
	var issuedAt, expiresAt time.Time
	err = tx.QueryRow(operationContext, `
		SELECT id, user_id, issued_at, expires_at
		FROM auth_sessions
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > CURRENT_TIMESTAMP
	`, digest[:]).Scan(&sessionID, &ownerID, &issuedAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AuthenticatedPrincipal{}, ErrAuthenticationSessionNotFound
	}
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return domain.AuthenticatedPrincipal{}, contextErr
		}
		return domain.AuthenticatedPrincipal{}, newRepositoryError(ErrAuthenticationSessionRead, err)
	}
	principal, err := domain.NewAuthenticatedPrincipal(domain.AuthenticatedPrincipalParams{
		Subject: ownerID, SessionID: sessionID, IssuedAt: issuedAt, ExpiresAt: expiresAt,
	})
	if err != nil {
		return domain.AuthenticatedPrincipal{}, newRepositoryError(ErrAuthenticationSessionInvalid, err)
	}
	if err := tx.Commit(operationContext); err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return domain.AuthenticatedPrincipal{}, contextErr
		}
		return domain.AuthenticatedPrincipal{}, newRepositoryError(ErrCommitAuthenticationTransaction, err)
	}
	committed = true
	return principal, nil
}

func validAuthenticationCredential(value string) bool {
	return value != "" && len(value) <= 4096
}
