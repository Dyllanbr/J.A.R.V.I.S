package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingRecurrenceSuggestionPool         = errors.New("recurrence suggestion postgres repository: pool is required")
	ErrInvalidRecurrenceSuggestionTimeout      = errors.New("recurrence suggestion postgres repository: operation timeout is invalid")
	ErrInvalidRecurrenceSuggestionQuery        = errors.New("recurrence suggestion postgres repository: invalid query")
	ErrInvalidRecurrenceSuggestionCommand      = errors.New("recurrence suggestion postgres repository: invalid command")
	ErrQueryRecurrenceSuggestionExpenses       = errors.New("recurrence suggestion postgres repository: expense query failed")
	ErrLoadRecurrenceSuggestionExpense         = errors.New("recurrence suggestion postgres repository: stored expense is invalid")
	ErrQueryRecurrenceSuggestionSuppressions   = errors.New("recurrence suggestion postgres repository: suppression query failed")
	ErrInvalidStoredSuggestionSuppression      = errors.New("recurrence suggestion postgres repository: stored suppression is invalid")
	ErrInsertRecurrenceSuggestionSuppression   = errors.New("recurrence suggestion postgres repository: suppression insert failed")
	ErrReadRecurrenceSuggestionSuppression     = errors.New("recurrence suggestion postgres repository: suppression lookup failed")
	ErrRecurrenceSuggestionSuppressionConflict = errors.New(
		"recurrence suggestion postgres repository: suppression conflicts with stored decision",
	)
)

// RecurrenceSuggestionRepository implements only the observation and
// suppression ports. Suggestions themselves remain derived and are never
// persisted by this adapter.
type RecurrenceSuggestionRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var (
	_ application.RecurrenceSuggestionExpenseReader     = (*RecurrenceSuggestionRepository)(nil)
	_ application.RecurrenceSuggestionSuppressionReader = (*RecurrenceSuggestionRepository)(nil)
	_ application.RecurrenceSuggestionSuppressionStore  = (*RecurrenceSuggestionRepository)(nil)
)

func NewRecurrenceSuggestionRepository(
	pool *pgxpool.Pool,
	operationTimeout time.Duration,
) (*RecurrenceSuggestionRepository, error) {
	if pool == nil {
		return nil, ErrMissingRecurrenceSuggestionPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidRecurrenceSuggestionTimeout
	}
	return &RecurrenceSuggestionRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

func (repository *RecurrenceSuggestionRepository) ListRecurrenceSuggestionExpenses(
	ctx context.Context,
	query application.RecurrenceSuggestionExpenseQuery,
) ([]domain.Expense, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if domain.ValidateUserID(query.UserID) != nil ||
		query.Start.IsZero() || query.End.IsZero() || !query.Start.Before(query.End) ||
		query.FinancialTimezone != application.FinancialTimezone {
		return nil, ErrInvalidRecurrenceSuggestionQuery
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	rows, err := repository.pool.Query(operationContext, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, category_id, occurred_at, financial_timezone, origin,
			status, version, created_at, updated_at
		FROM transactions
		WHERE user_id = $1
		  AND type = 'EXPENSE'
		  AND occurred_at >= $2
		  AND occurred_at < $3
		  AND financial_timezone = $4
		ORDER BY occurred_at ASC, id ASC
	`, query.UserID, query.Start, query.End, query.FinancialTimezone)
	if err != nil {
		return nil, newRepositoryError(ErrQueryRecurrenceSuggestionExpenses, err)
	}
	defer rows.Close()

	expenses := make([]domain.Expense, 0)
	for rows.Next() {
		expense, scanErr := scanExpense(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrLoadRecurrenceSuggestionExpense, scanErr)
		}
		expenses = append(expenses, expense)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrQueryRecurrenceSuggestionExpenses, err)
	}
	return expenses, nil
}

func (repository *RecurrenceSuggestionRepository) ListDismissedRecurrenceSuggestionIDs(
	ctx context.Context,
	userID string,
	ids []application.RecurrenceSuggestionID,
) ([]application.RecurrenceSuggestionID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if domain.ValidateUserID(userID) != nil {
		return nil, ErrInvalidRecurrenceSuggestionQuery
	}
	unique := make([]string, 0, len(ids))
	seen := make(map[application.RecurrenceSuggestionID]struct{}, len(ids))
	for _, id := range ids {
		if application.ValidateRecurrenceSuggestionID(id) != nil {
			return nil, ErrInvalidRecurrenceSuggestionQuery
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, string(id))
	}
	if len(unique) == 0 {
		return []application.RecurrenceSuggestionID{}, nil
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	rows, err := repository.pool.Query(operationContext, `
		SELECT suggestion_id, operation, evidence_fingerprint, dismissed_at
		FROM recurrence_suggestion_suppressions
		WHERE user_id = $1
		  AND suggestion_id = ANY($2::text[])
		ORDER BY suggestion_id ASC
	`, userID, unique)
	if err != nil {
		return nil, newRepositoryError(ErrQueryRecurrenceSuggestionSuppressions, err)
	}
	defer rows.Close()

	dismissed := make([]application.RecurrenceSuggestionID, 0)
	for rows.Next() {
		var (
			id          application.RecurrenceSuggestionID
			operation   string
			fingerprint []byte
			dismissedAt time.Time
		)
		if err := rows.Scan(&id, &operation, &fingerprint, &dismissedAt); err != nil {
			return nil, newRepositoryError(ErrQueryRecurrenceSuggestionSuppressions, err)
		}
		var canonicalFingerprint application.RecurrenceSuggestionFingerprint
		copy(canonicalFingerprint[:], fingerprint)
		expectedID, identityErr := application.DeriveRecurrenceSuggestionID(userID, canonicalFingerprint)
		if application.ValidateRecurrenceSuggestionID(id) != nil ||
			operation != application.DismissRecurrenceSuggestionOperation ||
			len(fingerprint) != len(application.RecurrenceSuggestionFingerprint{}) ||
			identityErr != nil || expectedID != id ||
			!isValidStoredSuggestionInstant(dismissedAt) {
			return nil, ErrInvalidStoredSuggestionSuppression
		}
		dismissed = append(dismissed, id)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrQueryRecurrenceSuggestionSuppressions, err)
	}
	return dismissed, nil
}

func (repository *RecurrenceSuggestionRepository) DismissRecurrenceSuggestion(
	ctx context.Context,
	command application.DismissRecurrenceSuggestionCommand,
) (application.DismissRecurrenceSuggestionStoreResult, error) {
	if err := ctx.Err(); err != nil {
		return application.DismissRecurrenceSuggestionStoreResult{}, err
	}
	if command.Operation != application.DismissRecurrenceSuggestionOperation ||
		domain.ValidateUserID(command.UserID) != nil ||
		application.ValidateRecurrenceSuggestionID(command.SuggestionID) != nil ||
		!isCanonicalRecurrenceInstant(command.DismissedAt) {
		return application.DismissRecurrenceSuggestionStoreResult{}, ErrInvalidRecurrenceSuggestionCommand
	}
	expectedID, err := application.DeriveRecurrenceSuggestionID(command.UserID, command.Fingerprint)
	if err != nil || expectedID != command.SuggestionID {
		return application.DismissRecurrenceSuggestionStoreResult{}, ErrRecurrenceSuggestionSuppressionConflict
	}

	operationContext, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	var inserted bool
	err = repository.pool.QueryRow(operationContext, `
		INSERT INTO recurrence_suggestion_suppressions (
			user_id, suggestion_id, operation, evidence_fingerprint, dismissed_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, suggestion_id) DO NOTHING
		RETURNING TRUE
	`,
		command.UserID,
		command.SuggestionID,
		command.Operation,
		command.Fingerprint[:],
		command.DismissedAt,
	).Scan(&inserted)
	if err == nil && inserted {
		return application.DismissRecurrenceSuggestionStoreResult{}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return application.DismissRecurrenceSuggestionStoreResult{}, newRepositoryError(
			ErrInsertRecurrenceSuggestionSuppression,
			err,
		)
	}

	var (
		storedOperation   string
		storedFingerprint []byte
		storedDismissedAt time.Time
	)
	err = repository.pool.QueryRow(operationContext, `
		SELECT operation, evidence_fingerprint, dismissed_at
		FROM recurrence_suggestion_suppressions
		WHERE user_id = $1 AND suggestion_id = $2
	`, command.UserID, command.SuggestionID).Scan(
		&storedOperation,
		&storedFingerprint,
		&storedDismissedAt,
	)
	if err != nil {
		return application.DismissRecurrenceSuggestionStoreResult{}, newRepositoryError(
			ErrReadRecurrenceSuggestionSuppression,
			err,
		)
	}
	if storedOperation != command.Operation ||
		subtle.ConstantTimeCompare(storedFingerprint, command.Fingerprint[:]) != 1 {
		return application.DismissRecurrenceSuggestionStoreResult{}, ErrRecurrenceSuggestionSuppressionConflict
	}
	if !isValidStoredSuggestionInstant(storedDismissedAt) {
		return application.DismissRecurrenceSuggestionStoreResult{}, ErrInvalidStoredSuggestionSuppression
	}
	return application.DismissRecurrenceSuggestionStoreResult{Replayed: true}, nil
}

func isValidStoredSuggestionInstant(value time.Time) bool {
	return !value.IsZero() && value.Equal(value.UTC().Truncate(time.Microsecond))
}
