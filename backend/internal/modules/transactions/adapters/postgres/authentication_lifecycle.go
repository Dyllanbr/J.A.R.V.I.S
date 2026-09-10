package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrInvalidAuthenticationSession     = errors.New("authentication postgres repository: invalid session")
	ErrCreateAuthenticationSession      = errors.New("authentication postgres repository: session creation failed")
	ErrAuthenticationSessionConflict    = errors.New("authentication postgres repository: session already exists")
	ErrRevokeAuthenticationSession      = errors.New("authentication postgres repository: session revocation failed")
	ErrCommitAuthenticationSessionWrite = errors.New("authentication postgres repository: session transaction commit failed")
)

var _ application.SessionLifecycleStore = (*AuthenticationRepository)(nil)

// CreateSession persists only validated session metadata and the credential
// digest. It never receives or stores the raw bearer credential.
func (repository *AuthenticationRepository) CreateSession(ctx context.Context, session domain.AuthenticatedSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if repository == nil || repository.pool == nil {
		return ErrMissingAuthenticationPool
	}
	if !session.StructurallyValid() {
		return ErrInvalidAuthenticationSession
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return contextErr
		}
		return newRepositoryError(ErrCreateAuthenticationSession, err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackContext, cancelRollback := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancelRollback()
		_ = transaction.Rollback(rollbackContext)
	}()
	tokenHash := session.TokenHash()
	_, err = transaction.Exec(operationContext, `
		INSERT INTO auth_sessions (
			id, user_id, token_hash, issued_at, expires_at, revoked_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $4)
	`, session.ID(), session.OwnerID(), tokenHash[:], session.IssuedAt(), session.ExpiresAt(), session.RevokedAt())
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return contextErr
		}
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return ErrAuthenticationSessionConflict
		}
		return newRepositoryError(ErrCreateAuthenticationSession, err)
	}
	if err := transaction.Commit(operationContext); err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return contextErr
		}
		return newRepositoryError(ErrCommitAuthenticationSessionWrite, err)
	}
	committed = true
	return nil
}

// RevokeSession atomically revokes one session belonging to the supplied
// owner. Repeating the operation is safe and returns false after the first
// successful revocation or for an unknown/cross-owner session.
func (repository *AuthenticationRepository) RevokeSession(ctx context.Context, ownerID, sessionID string, revokedAt time.Time) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if repository == nil || repository.pool == nil {
		return false, ErrMissingAuthenticationPool
	}
	if domain.ValidateUserID(ownerID) != nil || !validAuthenticationSessionID(sessionID) || revokedAt.IsZero() {
		return false, ErrInvalidAuthenticationSession
	}
	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	transaction, err := repository.pool.BeginTx(operationContext, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, newRepositoryError(ErrRevokeAuthenticationSession, err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackContext, cancelRollback := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancelRollback()
		_ = transaction.Rollback(rollbackContext)
	}()
	var revoked bool
	err = transaction.QueryRow(operationContext, `
		UPDATE auth_sessions
		SET revoked_at = $3
		WHERE user_id = $1
		  AND id = $2
		  AND revoked_at IS NULL
		RETURNING TRUE
	`, ownerID, sessionID, revokedAt).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, newRepositoryError(ErrRevokeAuthenticationSession, err)
	}
	if err := transaction.Commit(operationContext); err != nil {
		if contextErr := operationContext.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, newRepositoryError(ErrCommitAuthenticationSessionWrite, err)
	}
	committed = true
	return revoked, nil
}

func validAuthenticationSessionID(value string) bool {
	return value != "" && len(value) <= domain.MaxIdentifierBytes && value == strings.TrimSpace(value)
}
