package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingScheduledCommitmentPool     = errors.New("scheduled commitment postgres reader: pool is required")
	ErrInvalidScheduledCommitmentTimeout  = errors.New("scheduled commitment postgres reader: operation timeout is invalid")
	ErrBeginScheduledCommitmentSnapshot   = errors.New("scheduled commitment postgres reader: read-only snapshot begin failed")
	ErrCommitScheduledCommitmentSnapshot  = errors.New("scheduled commitment postgres reader: read-only snapshot commit failed")
	ErrReadScheduledCommitmentPlans       = errors.New("scheduled commitment postgres reader: installment plan read failed")
	ErrReadScheduledCommitmentRecurrences = errors.New("scheduled commitment postgres reader: recurrence read failed")
)

// ScheduledCommitmentSources is retained as an adapter alias for callers that
// used the Stage 2 preview API. The application owns the actual port type.
type ScheduledCommitmentSources = application.ScheduledCommitmentSnapshot

// ScheduledCommitmentRepository reads the two existing commitment sources.
// Read obtains both sources inside one repeatable-read, read-only transaction
// so a single projection operation observes one PostgreSQL MVCC snapshot.
type ScheduledCommitmentRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var (
	_ application.InstallmentPlanReader             = (*ScheduledCommitmentRepository)(nil)
	_ application.RecurrenceReader                  = (*ScheduledCommitmentRepository)(nil)
	_ application.ScheduledCommitmentSnapshotReader = (*ScheduledCommitmentRepository)(nil)
)

func NewScheduledCommitmentRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*ScheduledCommitmentRepository, error) {
	if pool == nil {
		return nil, ErrMissingScheduledCommitmentPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidScheduledCommitmentTimeout
	}
	return &ScheduledCommitmentRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

// Read returns both persisted sources from one owner-scoped MVCC snapshot.
// The transaction is explicitly read-only; no query in this adapter can write.
func (repository *ScheduledCommitmentRepository) Read(ctx context.Context, ownerID string) (ScheduledCommitmentSources, error) {
	if domain.ValidateUserID(ownerID) != nil {
		return ScheduledCommitmentSources{}, ErrInvalidStoredScheduledCommitment
	}
	var sources ScheduledCommitmentSources
	err := withReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, transaction pgx.Tx) error {
		plans, err := readInstallmentPlans(operationContext, transaction, ownerID)
		if err != nil {
			return err
		}
		recurrences, err := readRecurrences(operationContext, transaction, ownerID)
		if err != nil {
			return err
		}
		sources = ScheduledCommitmentSources{InstallmentPlans: plans, Recurrences: recurrences}
		return nil
	})
	if err != nil {
		return ScheduledCommitmentSources{}, newRepositoryError(ErrReadScheduledCommitments, err)
	}
	return sources, nil
}

// FindInstallmentPlan implements the application reader on the combined
// adapter. It uses a read-only transaction just like the dedicated adapter.
func (repository *ScheduledCommitmentRepository) FindInstallmentPlan(ctx context.Context, ownerID, planID string) (application.InstallmentPlanLookup, error) {
	if domain.ValidateUserID(ownerID) != nil || domain.ValidateInstallmentPlanID(planID) != nil {
		return application.InstallmentPlanLookup{}, ErrInvalidStoredInstallmentPlan
	}
	var lookup application.InstallmentPlanLookup
	err := withReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, transaction pgx.Tx) error {
		var err error
		lookup, err = readInstallmentPlan(operationContext, transaction, ownerID, planID)
		return err
	})
	if err != nil {
		return application.InstallmentPlanLookup{}, newRepositoryError(ErrReadInstallmentPlan, err)
	}
	return lookup, nil
}

func (repository *ScheduledCommitmentRepository) ListInstallmentPlans(ctx context.Context, ownerID string) ([]domain.InstallmentPlan, error) {
	if domain.ValidateUserID(ownerID) != nil {
		return nil, ErrInvalidStoredInstallmentPlan
	}
	var plans []domain.InstallmentPlan
	err := withReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, transaction pgx.Tx) error {
		var err error
		plans, err = readInstallmentPlans(operationContext, transaction, ownerID)
		return err
	})
	if err != nil {
		return nil, newRepositoryError(ErrListInstallmentPlans, err)
	}
	return plans, nil
}

func (repository *ScheduledCommitmentRepository) ListRecurrences(ctx context.Context, ownerID string) ([]domain.Recurrence, error) {
	if domain.ValidateRecurrenceUserID(ownerID) != nil {
		return nil, ErrInvalidStoredRecurrence
	}
	var recurrences []domain.Recurrence
	err := withReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, transaction pgx.Tx) error {
		var err error
		recurrences, err = readRecurrences(operationContext, transaction, ownerID)
		return err
	})
	if err != nil {
		return nil, newRepositoryError(ErrListRecurrences, err)
	}
	return recurrences, nil
}

var ErrInvalidStoredScheduledCommitment = errors.New("scheduled commitment postgres reader: stored result is invalid")
var ErrReadScheduledCommitments = errors.New("scheduled commitment postgres reader: combined read failed")

type postgresRowsQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type postgresRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func withReadOnlySnapshot(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration, operation func(context.Context, pgx.Tx) error) error {
	operationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transaction, err := pool.BeginTx(operationContext, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return newRepositoryError(ErrBeginScheduledCommitmentSnapshot, err)
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

	if err := operation(operationContext, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(operationContext); err != nil {
		return newRepositoryError(ErrCommitScheduledCommitmentSnapshot, err)
	}
	committed = true
	return nil
}

func readInstallmentPlan(ctx context.Context, querier postgresRowQuerier, ownerID, planID string) (application.InstallmentPlanLookup, error) {
	snapshot, err := queryInstallmentPlan(ctx, querier, ownerID, planID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.InstallmentPlanLookup{}, nil
	}
	if err != nil {
		return application.InstallmentPlanLookup{}, err
	}
	plan, err := snapshot.rehydrate(ownerID)
	if err != nil || plan.OwnerID() != ownerID || plan.ID() != planID {
		if err == nil {
			err = ErrInvalidStoredInstallmentPlan
		}
		return application.InstallmentPlanLookup{}, newRepositoryError(ErrInvalidStoredInstallmentPlan, err)
	}
	return application.InstallmentPlanLookup{InstallmentPlan: plan, Found: true}, nil
}

func readInstallmentPlans(ctx context.Context, querier postgresRowsQuerier, ownerID string) ([]domain.InstallmentPlan, error) {
	rows, err := querier.Query(ctx, `
		SELECT id, user_id, credit_card_id, expense_id, total_minor, total_currency,
		       installment_count, first_due_on, due_day, status, created_at, cancelled_on
		FROM installment_plans
		WHERE user_id = $1
		ORDER BY first_due_on ASC, created_at ASC, id ASC
	`, ownerID)
	if err != nil {
		return nil, newRepositoryError(ErrListInstallmentPlans, err)
	}
	defer rows.Close()
	plans := make([]domain.InstallmentPlan, 0)
	for rows.Next() {
		snapshot, scanErr := scanInstallmentPlanSnapshot(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredInstallmentPlan, scanErr)
		}
		plan, hydrateErr := snapshot.rehydrate(ownerID)
		if hydrateErr != nil || plan.OwnerID() != ownerID {
			if hydrateErr == nil {
				hydrateErr = ErrInvalidStoredInstallmentPlan
			}
			return nil, newRepositoryError(ErrInvalidStoredInstallmentPlan, hydrateErr)
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrListInstallmentPlans, err)
	}
	return plans, nil
}

func readRecurrences(ctx context.Context, querier postgresRowsQuerier, ownerID string) ([]domain.Recurrence, error) {
	rows, err := querier.Query(ctx, `
		SELECT id, user_id, transaction_type, description, expected_amount_minor,
		       currency, frequency, starts_on, status, created_at, cancelled_at
		FROM recurrences
		WHERE user_id = $1
		ORDER BY starts_on ASC, created_at ASC, id ASC
	`, ownerID)
	if err != nil {
		return nil, newRepositoryError(ErrListRecurrences, err)
	}
	defer rows.Close()
	recurrences := make([]domain.Recurrence, 0)
	for rows.Next() {
		snapshot, scanErr := scanRecurrenceSnapshot(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredRecurrence, scanErr)
		}
		recurrence, hydrateErr := snapshot.rehydrate()
		if hydrateErr != nil || recurrence.UserID() != ownerID {
			if hydrateErr == nil {
				hydrateErr = ErrInvalidStoredRecurrence
			}
			return nil, newRepositoryError(ErrInvalidStoredRecurrence, hydrateErr)
		}
		recurrences = append(recurrences, recurrence)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrListRecurrences, err)
	}
	return recurrences, nil
}
