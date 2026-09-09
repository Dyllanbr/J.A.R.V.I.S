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
	ErrMissingPurchaseSimulationPool    = errors.New("purchase simulation postgres reader: pool is required")
	ErrInvalidPurchaseSimulationTimeout = errors.New("purchase simulation postgres reader: operation timeout is invalid")
	ErrInvalidPurchaseSimulationQuery   = errors.New("purchase simulation postgres reader: query is invalid")
	ErrBeginPurchaseSimulationSnapshot  = errors.New("purchase simulation postgres reader: read-only snapshot begin failed")
	ErrReadPurchaseSimulationCard       = errors.New("purchase simulation postgres reader: card read failed")
	ErrReadPurchaseSimulationSnapshot   = errors.New("purchase simulation postgres reader: snapshot read failed")
	ErrInvalidStoredPurchaseSimulation  = errors.New("purchase simulation postgres reader: stored snapshot is invalid")
	ErrCommitPurchaseSimulationSnapshot = errors.New("purchase simulation postgres reader: read-only snapshot commit failed")
)

// PurchaseSimulationRepository reads the card and all confirmed Safe
// Available inputs in one owner-scoped MVCC snapshot. It never writes a
// transaction, future Expense, plan, audit or idempotency record.
type PurchaseSimulationRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var _ application.PurchaseSimulationSnapshotReader = (*PurchaseSimulationRepository)(nil)

func NewPurchaseSimulationRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*PurchaseSimulationRepository, error) {
	if pool == nil {
		return nil, ErrMissingPurchaseSimulationPool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidPurchaseSimulationTimeout
	}
	return &PurchaseSimulationRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

func (repository *PurchaseSimulationRepository) Read(
	ctx context.Context,
	query application.PurchaseSimulationSnapshotQuery,
) (application.PurchaseSimulationSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return application.PurchaseSimulationSnapshot{}, err
	}
	if repository == nil || repository.pool == nil || domain.ValidateUserID(query.OwnerID) != nil || domain.ValidateCreditCardID(query.CreditCardID) != nil {
		return application.PurchaseSimulationSnapshot{}, ErrInvalidPurchaseSimulationQuery
	}
	if _, err := domain.NewSafeAvailablePeriod(query.Period.StartOn(), query.Period.EndOn()); err != nil {
		return application.PurchaseSimulationSnapshot{}, ErrInvalidPurchaseSimulationQuery
	}
	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		return application.PurchaseSimulationSnapshot{}, ErrInvalidPurchaseSimulationQuery
	}
	startUTC, endUTC, err := safeAvailableUTCWindow(query.Period, location)
	if err != nil {
		return application.PurchaseSimulationSnapshot{}, ErrInvalidPurchaseSimulationQuery
	}

	var result application.PurchaseSimulationSnapshot
	err = withPurchaseSimulationReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, transaction pgx.Tx) error {
		cardRow, cardErr := queryCardStatementCard(operationContext, transaction, query.OwnerID, query.CreditCardID)
		if errors.Is(cardErr, pgx.ErrNoRows) {
			result = application.PurchaseSimulationSnapshot{Found: false}
			return nil
		}
		if cardErr != nil {
			return newRepositoryError(ErrReadPurchaseSimulationCard, cardErr)
		}
		card, hydrateErr := cardRow.rehydrate()
		if hydrateErr != nil || card.UserID() != query.OwnerID || card.ID() != query.CreditCardID {
			if hydrateErr == nil {
				hydrateErr = ErrInvalidStoredCreditCard
			}
			return newRepositoryError(ErrInvalidStoredPurchaseSimulation, hydrateErr)
		}

		openingBalance, readErr := readSafeAvailableOpeningBalance(operationContext, transaction, query.OwnerID, startUTC)
		if readErr != nil {
			return readErr
		}
		incomes, readErr := readSafeAvailableIncomes(operationContext, transaction, query.OwnerID, startUTC, endUTC, location)
		if readErr != nil {
			return readErr
		}
		expenses, readErr := readSafeAvailableExpenses(operationContext, transaction, query.OwnerID, startUTC, endUTC, location)
		if readErr != nil {
			return readErr
		}
		plans, readErr := readInstallmentPlans(operationContext, transaction, query.OwnerID)
		if readErr != nil {
			return readErr
		}
		recurrences, readErr := readRecurrences(operationContext, transaction, query.OwnerID)
		if readErr != nil {
			return readErr
		}
		budget, budgetFound, readErr := readSafeAvailableBudget(operationContext, transaction, query.OwnerID, query.Period)
		if readErr != nil {
			return readErr
		}
		commitments, readErr := safeAvailableCommitments(query.OwnerID, query.Period, plans, recurrences)
		if readErr != nil {
			return readErr
		}

		var budgetPtr *domain.MonthlyBudget
		if budgetFound {
			budgetCopy := budget
			budgetPtr = &budgetCopy
		}
		missing := []domain.SafeAvailableMissingData(nil)
		if !budgetFound {
			missing = []domain.SafeAvailableMissingData{domain.SafeAvailableMissingBudget}
		}
		safeSnapshot, snapshotErr := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{
			OwnerID: query.OwnerID, Period: query.Period, AvailableBalance: openingBalance, Budget: budgetPtr,
			Incomes: incomes, Expenses: expenses, Commitments: commitments, MissingData: missing,
		})
		if snapshotErr != nil {
			return newRepositoryError(ErrInvalidStoredPurchaseSimulation, snapshotErr)
		}
		result = application.PurchaseSimulationSnapshot{SafeAvailable: safeSnapshot, CreditCard: card, Found: true}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.PurchaseSimulationSnapshot{}, err
		}
		return application.PurchaseSimulationSnapshot{}, newRepositoryError(ErrReadPurchaseSimulationSnapshot, err)
	}
	return result, nil
}

func withPurchaseSimulationReadOnlySnapshot(
	ctx context.Context,
	pool *pgxpool.Pool,
	timeout time.Duration,
	operation func(context.Context, pgx.Tx) error,
) error {
	operationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	transaction, err := pool.BeginTx(operationContext, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return newRepositoryError(ErrBeginPurchaseSimulationSnapshot, err)
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
		return newRepositoryError(ErrCommitPurchaseSimulationSnapshot, err)
	}
	committed = true
	return nil
}
