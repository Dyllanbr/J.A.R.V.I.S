package postgres

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingSafeAvailablePool     = errors.New("safe available postgres reader: pool is required")
	ErrInvalidSafeAvailableTimeout  = errors.New("safe available postgres reader: operation timeout is invalid")
	ErrInvalidSafeAvailableQuery    = errors.New("safe available postgres reader: query is invalid")
	ErrSafeAvailableTimezone        = errors.New("safe available postgres reader: financial timezone is unavailable")
	ErrBeginSafeAvailableSnapshot   = errors.New("safe available postgres reader: read-only snapshot begin failed")
	ErrCommitSafeAvailableSnapshot  = errors.New("safe available postgres reader: read-only snapshot commit failed")
	ErrReadSafeAvailableBalance     = errors.New("safe available postgres reader: opening balance read failed")
	ErrReadSafeAvailableExpenses    = errors.New("safe available postgres reader: expense read failed")
	ErrReadSafeAvailableIncomes     = errors.New("safe available postgres reader: income read failed")
	ErrReadSafeAvailablePlans       = errors.New("safe available postgres reader: installment plan read failed")
	ErrReadSafeAvailableRecurrences = errors.New("safe available postgres reader: recurrence read failed")
	ErrReadSafeAvailableBudget      = errors.New("safe available postgres reader: budget read failed")
	ErrInvalidStoredSafeAvailable   = errors.New("safe available postgres reader: stored result is invalid")
	ErrReadSafeAvailableSnapshot    = errors.New("safe available postgres reader: snapshot read failed")
)

// SafeAvailableRepository reads all confirmed inputs for one calculation from
// one PostgreSQL MVCC snapshot. The balance is derived from the existing
// confirmed transaction ledger before the requested period; no balance or
// budget table is introduced by this stage.
type SafeAvailableRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

var _ application.SafeAvailableSnapshotReader = (*SafeAvailableRepository)(nil)

func NewSafeAvailableRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*SafeAvailableRepository, error) {
	if pool == nil {
		return nil, ErrMissingSafeAvailablePool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidSafeAvailableTimeout
	}
	return &SafeAvailableRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

// Read validates the explicit owner and civil period before opening a
// transaction. All five source reads and all rehydration happen in the same
// RepeatableRead/ReadOnly transaction. The result is assigned only after the
// complete domain snapshot has been built.
func (repository *SafeAvailableRepository) Read(
	ctx context.Context,
	query application.SafeAvailableSnapshotQuery,
) (domain.SafeAvailableSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return domain.SafeAvailableSnapshot{}, err
	}
	if repository == nil || repository.pool == nil {
		return domain.SafeAvailableSnapshot{}, ErrMissingSafeAvailablePool
	}
	if domain.ValidateUserID(query.OwnerID) != nil {
		return domain.SafeAvailableSnapshot{}, ErrInvalidSafeAvailableQuery
	}
	if _, err := domain.NewSafeAvailablePeriod(query.Period.StartOn(), query.Period.EndOn()); err != nil {
		return domain.SafeAvailableSnapshot{}, ErrInvalidSafeAvailableQuery
	}

	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		return domain.SafeAvailableSnapshot{}, ErrSafeAvailableTimezone
	}
	startUTC, endUTC, err := safeAvailableUTCWindow(query.Period, location)
	if err != nil {
		return domain.SafeAvailableSnapshot{}, ErrInvalidSafeAvailableQuery
	}

	var snapshot domain.SafeAvailableSnapshot
	err = withSafeAvailableReadOnlySnapshot(ctx, repository.pool, repository.operationTimeout, func(operationContext context.Context, transaction pgx.Tx) error {
		openingBalance, err := readSafeAvailableOpeningBalance(operationContext, transaction, query.OwnerID, startUTC)
		if err != nil {
			return err
		}
		incomes, err := readSafeAvailableIncomes(operationContext, transaction, query.OwnerID, startUTC, endUTC, location)
		if err != nil {
			return err
		}
		expenses, err := readSafeAvailableExpenses(operationContext, transaction, query.OwnerID, startUTC, endUTC, location)
		if err != nil {
			return err
		}
		plans, err := readInstallmentPlans(operationContext, transaction, query.OwnerID)
		if err != nil {
			return newRepositoryError(ErrReadSafeAvailablePlans, err)
		}
		recurrences, err := readRecurrences(operationContext, transaction, query.OwnerID)
		if err != nil {
			return newRepositoryError(ErrReadSafeAvailableRecurrences, err)
		}
		budget, budgetFound, err := readSafeAvailableBudget(operationContext, transaction, query.OwnerID, query.Period)
		if err != nil {
			return err
		}
		commitments, err := safeAvailableCommitments(query.OwnerID, query.Period, plans, recurrences)
		if err != nil {
			return err
		}

		var budgetPtr *domain.MonthlyBudget
		if budgetFound {
			budgetPtr = &budget
		}
		missingData := []domain.SafeAvailableMissingData(nil)
		if !budgetFound {
			missingData = []domain.SafeAvailableMissingData{domain.SafeAvailableMissingBudget}
		}
		candidate, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{
			OwnerID:          query.OwnerID,
			Period:           query.Period,
			AvailableBalance: openingBalance,
			Budget:           budgetPtr,
			Incomes:          incomes,
			Expenses:         expenses,
			Commitments:      commitments,
			MissingData:      missingData,
		})
		if err != nil {
			return newRepositoryError(ErrInvalidStoredSafeAvailable, err)
		}
		snapshot = candidate
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return domain.SafeAvailableSnapshot{}, err
		}
		return domain.SafeAvailableSnapshot{}, newRepositoryError(ErrReadSafeAvailableSnapshot, err)
	}
	return snapshot, nil
}

func withSafeAvailableReadOnlySnapshot(
	ctx context.Context,
	pool *pgxpool.Pool,
	timeout time.Duration,
	operation func(context.Context, pgx.Tx) error,
) error {
	operationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transaction, err := pool.BeginTx(operationContext, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return newRepositoryError(ErrBeginSafeAvailableSnapshot, err)
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
		return newRepositoryError(ErrCommitSafeAvailableSnapshot, err)
	}
	committed = true
	return nil
}

func safeAvailableUTCWindow(period domain.SafeAvailablePeriod, location *time.Location) (time.Time, time.Time, error) {
	start := time.Date(period.StartOn().Year(), period.StartOn().Month(), period.StartOn().Day(), 0, 0, 0, 0, location)
	end := time.Date(period.EndOn().Year(), period.EndOn().Month(), period.EndOn().Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1)
	if end.Before(start) {
		return time.Time{}, time.Time{}, ErrInvalidSafeAvailableQuery
	}
	return start.UTC(), end.UTC(), nil
}

func readSafeAvailableOpeningBalance(ctx context.Context, querier postgresRowsQuerier, ownerID string, before time.Time) (domain.Money, error) {
	rows, err := querier.Query(ctx, `
		SELECT
			id, user_id, type, description, amount_minor, currency,
			payment_method, category_id, credit_card_id, statement_due_on,
			occurred_at, financial_timezone, origin, status, version,
			created_at, updated_at
		FROM transactions
		WHERE user_id = $1
		  AND financial_timezone = $2
		  AND occurred_at < $3
		ORDER BY occurred_at ASC, id ASC
	`, ownerID, application.FinancialTimezone, before)
	if err != nil {
		return domain.Money{}, newRepositoryError(ErrReadSafeAvailableBalance, err)
	}
	defer rows.Close()

	var balance int64
	for rows.Next() {
		transaction, scanErr := scanMonthlyTransaction(rows)
		if scanErr != nil {
			return domain.Money{}, newRepositoryError(ErrInvalidStoredSafeAvailable, scanErr)
		}
		if transaction.UserID != ownerID || transaction.FinancialTimezone != application.FinancialTimezone || transaction.Status != string(domain.IncomeStatusRecorded) || transaction.Version != 1 || transaction.Amount.MinorUnits() <= 0 || transaction.OccurredAt.IsZero() {
			return domain.Money{}, ErrInvalidStoredSafeAvailable
		}
		var next int64
		switch transaction.Type {
		case domain.TransactionTypeIncome:
			next, err = addSafeAvailableMinor(balance, transaction.Amount.MinorUnits())
		case domain.TransactionTypeExpense:
			next, err = subtractSafeAvailableMinor(balance, transaction.Amount.MinorUnits())
		default:
			return domain.Money{}, ErrInvalidStoredSafeAvailable
		}
		if err != nil {
			return domain.Money{}, err
		}
		balance = next
	}
	if err := rows.Err(); err != nil {
		return domain.Money{}, newRepositoryError(ErrReadSafeAvailableBalance, err)
	}
	return domain.NewMoney(balance, domain.CurrencyBRL)
}

func readSafeAvailableExpenses(ctx context.Context, querier postgresRowsQuerier, ownerID string, start, end time.Time, location *time.Location) ([]domain.SafeAvailableEntry, error) {
	rows, err := querier.Query(ctx, `
		SELECT id, user_id, type, description, amount_minor, currency,
		       payment_method, category_id, credit_card_id, statement_due_on,
		       occurred_at, financial_timezone, origin, status, version,
		       created_at, updated_at
		FROM transactions
		WHERE user_id = $1
		  AND type = 'EXPENSE'
		  AND occurred_at >= $2
		  AND occurred_at < $3
		  AND financial_timezone = $4
		ORDER BY occurred_at ASC, id ASC
	`, ownerID, start, end, application.FinancialTimezone)
	if err != nil {
		return nil, newRepositoryError(ErrReadSafeAvailableExpenses, err)
	}
	defer rows.Close()

	entries := make([]domain.SafeAvailableEntry, 0)
	for rows.Next() {
		expense, scanErr := scanExpense(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, scanErr)
		}
		if expense.UserID() != ownerID || expense.Status() != domain.ExpenseStatusRecorded {
			return nil, ErrInvalidStoredSafeAvailable
		}
		dueOn, dateErr := safeAvailableCivilDate(expense.OccurredAt(), location)
		if dateErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, dateErr)
		}
		entry, entryErr := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{
			Kind: domain.SafeAvailableEntryKindExpense, OwnerID: ownerID, SourceID: expense.ID(), DueOn: dueOn, Amount: expense.Amount(),
		})
		if entryErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, entryErr)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrReadSafeAvailableExpenses, err)
	}
	return entries, nil
}

func readSafeAvailableIncomes(ctx context.Context, querier postgresRowsQuerier, ownerID string, start, end time.Time, location *time.Location) ([]domain.SafeAvailableEntry, error) {
	rows, err := querier.Query(ctx, `
		SELECT id, user_id, type, description, amount_minor, currency,
		       payment_method, category_id, occurred_at, financial_timezone,
		       origin, status, version, created_at, updated_at
		FROM transactions
		WHERE user_id = $1
		  AND type = 'INCOME'
		  AND occurred_at >= $2
		  AND occurred_at < $3
		  AND financial_timezone = $4
		ORDER BY occurred_at ASC, id ASC
	`, ownerID, start, end, application.FinancialTimezone)
	if err != nil {
		return nil, newRepositoryError(ErrReadSafeAvailableIncomes, err)
	}
	defer rows.Close()

	entries := make([]domain.SafeAvailableEntry, 0)
	for rows.Next() {
		income, scanErr := scanIncome(rows)
		if scanErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, scanErr)
		}
		if income.UserID() != ownerID || income.Status() != domain.IncomeStatusRecorded {
			return nil, ErrInvalidStoredSafeAvailable
		}
		dueOn, dateErr := safeAvailableCivilDate(income.OccurredAt(), location)
		if dateErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, dateErr)
		}
		entry, entryErr := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{
			Kind: domain.SafeAvailableEntryKindIncome, OwnerID: ownerID, SourceID: income.ID(), DueOn: dueOn, Amount: income.Amount(),
		})
		if entryErr != nil {
			return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, entryErr)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, newRepositoryError(ErrReadSafeAvailableIncomes, err)
	}
	return entries, nil
}

func safeAvailableCivilDate(value time.Time, location *time.Location) (domain.CivilDate, error) {
	local := value.In(location)
	return domain.NewCivilDate(local.Year(), local.Month(), local.Day())
}

// readSafeAvailableBudget reads the optional owner/month budget inside the
// caller's existing repeatable-read transaction. A period spanning more than
// one civil month remains explicitly unbudgeted because this slice does not
// define an aggregation rule across multiple monthly budgets.
func readSafeAvailableBudget(ctx context.Context, querier postgresRowQuerier, ownerID string, period domain.SafeAvailablePeriod) (domain.MonthlyBudget, bool, error) {
	if period.StartOn().Year() != period.EndOn().Year() || period.StartOn().Month() != period.EndOn().Month() {
		return domain.MonthlyBudget{}, false, nil
	}
	var (
		storedMonth pgtype.Date
		minor       int64
		currency    string
	)
	month, err := domain.NewCivilMonth(period.StartOn().Year(), period.StartOn().Month())
	if err != nil {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredSafeAvailable
	}
	err = querier.QueryRow(ctx, `
		SELECT month, amount_minor, currency
		FROM monthly_budgets
		WHERE user_id = $1 AND month = $2
	`, ownerID, postgresDate(month.StartOn())).Scan(&storedMonth, &minor, &currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MonthlyBudget{}, false, nil
	}
	if err != nil {
		return domain.MonthlyBudget{}, false, newRepositoryError(ErrReadSafeAvailableBudget, err)
	}
	if !storedMonth.Valid || storedMonth.InfinityModifier != pgtype.Finite {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredSafeAvailable
	}
	storedDate, err := civilDateFromPostgres(storedMonth)
	if err != nil || storedDate.Day() != 1 {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredSafeAvailable
	}
	storedCivilMonth, err := domain.NewCivilMonth(storedDate.Year(), storedDate.Month())
	if err != nil {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredSafeAvailable
	}
	amount, err := domain.NewMoney(minor, domain.Currency(currency))
	if err != nil {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredSafeAvailable
	}
	budget, err := domain.NewMonthlyBudget(domain.MonthlyBudgetParams{OwnerID: ownerID, Month: storedCivilMonth, Amount: amount})
	if err != nil || !storedCivilMonth.Equal(month) {
		return domain.MonthlyBudget{}, false, ErrInvalidStoredSafeAvailable
	}
	return budget, true, nil
}

func safeAvailableCommitments(ownerID string, period domain.SafeAvailablePeriod, plans []domain.InstallmentPlan, recurrences []domain.Recurrence) ([]domain.SafeAvailableEntry, error) {
	entries := make([]domain.SafeAvailableEntry, 0)
	seenPlans := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if plan.OwnerID() != ownerID || domain.ValidateInstallmentPlanID(plan.ID()) != nil {
			return nil, ErrInvalidStoredSafeAvailable
		}
		if _, exists := seenPlans[plan.ID()]; exists {
			return nil, ErrInvalidStoredSafeAvailable
		}
		seenPlans[plan.ID()] = struct{}{}
		schedule, err := plan.EffectiveSchedule()
		if err != nil {
			return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, err)
		}
		for _, installment := range schedule {
			if !safeAvailablePeriodContains(period, installment.DueDate()) {
				continue
			}
			entry, entryErr := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{
				Kind: domain.SafeAvailableEntryKindCommitment, OwnerID: ownerID, SourceID: plan.ID(), Sequence: installment.Number(), DueOn: installment.DueDate(), Amount: installment.Amount(),
			})
			if entryErr != nil {
				return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, entryErr)
			}
			entries = append(entries, entry)
		}
	}

	seenRecurrences := make(map[string]struct{}, len(recurrences))
	for _, recurrence := range recurrences {
		if recurrence.UserID() != ownerID || domain.ValidateRecurrenceUserID(recurrence.UserID()) != nil || domain.ValidateRecurrenceID(recurrence.ID()) != nil {
			return nil, ErrInvalidStoredSafeAvailable
		}
		if _, exists := seenRecurrences[recurrence.ID()]; exists {
			return nil, ErrInvalidStoredSafeAvailable
		}
		seenRecurrences[recurrence.ID()] = struct{}{}
		if recurrence.Status() != domain.RecurrenceStatusActive {
			continue
		}
		if recurrence.Type() != domain.TransactionTypeExpense || recurrence.Frequency() != domain.RecurrenceFrequencyMonthly || recurrence.ExpectedAmount().Currency() != domain.CurrencyBRL || recurrence.ExpectedAmount().MinorUnits() <= 0 {
			return nil, ErrInvalidStoredSafeAvailable
		}
		startOffset := safeAvailableMonthOffset(recurrence.StartsOn(), period.StartOn())
		endOffset := safeAvailableMonthOffset(recurrence.StartsOn(), period.EndOn())
		for offset := startOffset; offset <= endOffset; offset++ {
			occurrence, occurrenceErr := recurrence.OccurrenceDate(offset)
			if occurrenceErr != nil {
				return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, occurrenceErr)
			}
			if !safeAvailablePeriodContains(period, occurrence) {
				continue
			}
			entry, entryErr := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{
				Kind: domain.SafeAvailableEntryKindCommitment, OwnerID: ownerID, SourceID: recurrence.ID(), Sequence: offset + 1, DueOn: occurrence, Amount: recurrence.ExpectedAmount(),
			})
			if entryErr != nil {
				return nil, newRepositoryError(ErrInvalidStoredSafeAvailable, entryErr)
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func safeAvailablePeriodContains(period domain.SafeAvailablePeriod, value domain.CivilDate) bool {
	return !value.Before(period.StartOn()) && !period.EndOn().Before(value)
}

func safeAvailableMonthOffset(startsOn, target domain.CivilDate) int {
	difference := (target.Year()-startsOn.Year())*12 + int(target.Month()) - int(startsOn.Month())
	if difference < 0 {
		return 0
	}
	return difference
}

func addSafeAvailableMinor(left, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, domain.ErrSafeAvailableOverflow
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, domain.ErrSafeAvailableOverflow
	}
	return left + right, nil
}

func subtractSafeAvailableMinor(left, right int64) (int64, error) {
	if right > 0 && left < math.MinInt64+right {
		return 0, domain.ErrSafeAvailableOverflow
	}
	return left - right, nil
}
