package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingCardPurchasePool             = errors.New("card purchase postgres repository: pool is required")
	ErrInvalidCardPurchaseTimeout          = errors.New("card purchase postgres repository: operation timeout is invalid")
	ErrInvalidCardPurchaseOperation        = errors.New("card purchase postgres repository: invalid operation")
	ErrInvalidCardPurchaseCommand          = errors.New("card purchase postgres repository: invalid command")
	ErrBeginCardPurchaseTransaction        = errors.New("card purchase postgres repository: begin transaction failed")
	ErrReserveCardPurchaseIdempotency      = errors.New("card purchase postgres repository: idempotency reservation failed")
	ErrReadCardPurchaseIdempotency         = errors.New("card purchase postgres repository: idempotency lookup failed")
	ErrCompleteCardPurchaseIdempotency     = errors.New("card purchase postgres repository: idempotency completion failed")
	ErrInvalidCardPurchaseIdempotencyState = errors.New("card purchase postgres repository: idempotency state is invalid")
	ErrInsertCardPurchasePlan              = errors.New("card purchase postgres repository: installment plan insert failed")
	ErrInsertCardPurchaseAudit             = errors.New("card purchase postgres repository: installment plan audit insert failed")
	ErrCommitCardPurchaseTransaction       = errors.New("card purchase postgres repository: commit failed")
	ErrReadInstallmentPlan                 = errors.New("installment plan postgres repository: lookup failed")
	ErrListInstallmentPlans                = errors.New("installment plan postgres repository: list failed")
	ErrInvalidStoredInstallmentPlan        = errors.New("installment plan postgres repository: stored plan is invalid")
	ErrReadInstallmentPlanIdempotency      = errors.New("installment plan postgres repository: idempotency lookup failed")
	ErrReserveInstallmentPlanIdempotency   = errors.New("installment plan postgres repository: idempotency reservation failed")
	ErrCompleteInstallmentPlanIdempotency  = errors.New("installment plan postgres repository: idempotency completion failed")
	ErrInvalidInstallmentPlanIdemState     = errors.New("installment plan postgres repository: idempotency state is invalid")
	ErrUpdateInstallmentPlan               = errors.New("installment plan postgres repository: update failed")
	ErrInsertInstallmentPlanAudit          = errors.New("installment plan postgres repository: audit insert failed")
	ErrCommitInstallmentPlanTransaction    = errors.New("installment plan postgres repository: commit failed")
	ErrBeginInstallmentPlanTransaction     = errors.New("installment plan postgres repository: begin transaction failed")
)

type CardPurchaseRepository struct {
	pool             *pgxpool.Pool
	operationTimeout time.Duration
}

// InstallmentPlanRepository is an alias kept for callers that compose plan
// reads/cancellation separately from card-purchase commands. Both capabilities
// intentionally share one pool-backed adapter and transaction boundary.
type InstallmentPlanRepository = CardPurchaseRepository

var (
	_ application.CardPurchaseCommandStore                = (*CardPurchaseRepository)(nil)
	_ application.CardPurchaseReplayReader                = (*CardPurchaseRepository)(nil)
	_ application.InstallmentPlanReader                   = (*CardPurchaseRepository)(nil)
	_ application.InstallmentPlanCancellationReplayReader = (*CardPurchaseRepository)(nil)
	_ application.InstallmentPlanCancellationStore        = (*CardPurchaseRepository)(nil)
	_ application.ExpenseReplayReader                     = (*ExpenseRepository)(nil)
)

func NewCardPurchaseRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*CardPurchaseRepository, error) {
	if pool == nil {
		return nil, ErrMissingCardPurchasePool
	}
	if operationTimeout <= 0 || operationTimeout > maximumOperationTimeout {
		return nil, ErrInvalidCardPurchaseTimeout
	}
	return &CardPurchaseRepository{pool: pool, operationTimeout: operationTimeout}, nil
}

func NewInstallmentPlanRepository(pool *pgxpool.Pool, operationTimeout time.Duration) (*InstallmentPlanRepository, error) {
	return NewCardPurchaseRepository(pool, operationTimeout)
}

func (repository *CardPurchaseRepository) RecordCardPurchase(ctx context.Context, command application.CardPurchaseCommand) (application.CardPurchaseCommandResult, error) {
	if err := validateCardPurchaseCommand(command); err != nil {
		return application.CardPurchaseCommandResult{}, err
	}
	opCtx, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	tx, err := repository.pool.BeginTx(opCtx, pgx.TxOptions{})
	if err != nil {
		return application.CardPurchaseCommandResult{}, newRepositoryError(ErrBeginCardPurchaseTransaction, err)
	}
	committed := false
	defer rollbackCardPurchaseTransaction(tx, &committed)()

	reserved, err := reserveCardPurchaseIdempotency(opCtx, tx, command)
	if err != nil {
		return application.CardPurchaseCommandResult{}, err
	}
	if !reserved {
		result, replayErr := replayCardPurchase(opCtx, tx, command.Expense.UserID(), command.Operation, command.IdempotencyKey, command.Fingerprint)
		if replayErr != nil {
			return application.CardPurchaseCommandResult{}, replayErr
		}
		if err := tx.Commit(opCtx); err != nil {
			return application.CardPurchaseCommandResult{}, newRepositoryError(ErrCommitCardPurchaseTransaction, err)
		}
		committed = true
		return application.CardPurchaseCommandResult{Expense: result.Expense, InstallmentPlan: result.InstallmentPlan, Replayed: true}, nil
	}

	cardID, linked := command.Expense.CreditCardID()
	if !linked {
		return application.CardPurchaseCommandResult{}, ErrInvalidCardPurchaseCommand
	}
	card, err := loadCreditCardForUpdate(opCtx, tx, cardID, command.Expense.UserID())
	if err != nil {
		return application.CardPurchaseCommandResult{}, err
	}
	if card.Status() != domain.CreditCardStatusActive {
		return application.CardPurchaseCommandResult{}, application.ErrCardPurchaseCreditCardArchived
	}
	if err := insertExpense(opCtx, tx, command.Expense); err != nil {
		return application.CardPurchaseCommandResult{}, err
	}
	if err := insertAuditEvent(opCtx, tx, command.Expense); err != nil {
		return application.CardPurchaseCommandResult{}, err
	}
	if command.InstallmentPlan != nil {
		if err := insertInstallmentPlan(opCtx, tx, *command.InstallmentPlan); err != nil {
			return application.CardPurchaseCommandResult{}, err
		}
		if err := insertInstallmentPlanAudit(opCtx, tx, *command.InstallmentPlan, "INSTALLMENT_PLAN_CREATED", command.InstallmentPlan.CreatedAt()); err != nil {
			return application.CardPurchaseCommandResult{}, err
		}
	}
	if err := completeCardPurchaseIdempotency(opCtx, tx, command); err != nil {
		return application.CardPurchaseCommandResult{}, err
	}
	if err := tx.Commit(opCtx); err != nil {
		return application.CardPurchaseCommandResult{}, newRepositoryError(ErrCommitCardPurchaseTransaction, err)
	}
	committed = true
	return application.CardPurchaseCommandResult{Expense: command.Expense, InstallmentPlan: cloneInstallmentPlan(command.InstallmentPlan)}, nil
}

func (repository *CardPurchaseRepository) FindCardPurchaseReplay(ctx context.Context, query application.CardPurchaseReplayQuery) (application.CardPurchaseReplayLookup, error) {
	if domain.ValidateUserID(query.UserID) != nil || query.Operation != application.CardPurchaseOperationCreate || !validIdempotencyKey(query.IdempotencyKey) {
		return application.CardPurchaseReplayLookup{}, ErrInvalidCardPurchaseCommand
	}
	opCtx, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	return readCardPurchaseReplay(opCtx, repository.pool, query)
}

func (repository *CardPurchaseRepository) FindInstallmentPlan(ctx context.Context, ownerID, planID string) (application.InstallmentPlanLookup, error) {
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

func (repository *CardPurchaseRepository) ListInstallmentPlans(ctx context.Context, ownerID string) ([]domain.InstallmentPlan, error) {
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

func (repository *CardPurchaseRepository) FindInstallmentPlanCancellationReplay(ctx context.Context, query application.InstallmentPlanCancellationReplayQuery) (application.InstallmentPlanCancellationReplayLookup, error) {
	if domain.ValidateUserID(query.OwnerID) != nil || query.Operation != application.CancelInstallmentPlanOperation || !validIdempotencyKey(query.IdempotencyKey) {
		return application.InstallmentPlanCancellationReplayLookup{}, ErrInvalidInstallmentPlanIdemState
	}
	opCtx, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	var storedFingerprint []byte
	var state string
	snapshot := installmentPlanSnapshot{}
	err := repository.pool.QueryRow(opCtx, `
		SELECT request_fingerprint, state, plan_id, plan_user_id, plan_credit_card_id, plan_expense_id,
		       plan_total_minor, plan_total_currency, plan_installment_count,
		       plan_first_due_on, plan_due_day, plan_status, plan_created_at, plan_cancelled_on
		FROM installment_plan_idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, query.OwnerID, query.Operation, query.IdempotencyKey).Scan(
		&storedFingerprint, &state, &snapshot.id, &snapshot.userID, &snapshot.creditCardID, &snapshot.expenseID,
		&snapshot.totalMinor, &snapshot.currency, &snapshot.installmentCount,
		&snapshot.firstDueOn, &snapshot.dueDay, &snapshot.status, &snapshot.createdAt, &snapshot.cancelledOn,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.InstallmentPlanCancellationReplayLookup{}, nil
	}
	if err != nil {
		return application.InstallmentPlanCancellationReplayLookup{}, newRepositoryError(ErrReadInstallmentPlanIdempotency, err)
	}
	if subtle.ConstantTimeCompare(storedFingerprint, query.Fingerprint[:]) != 1 {
		return application.InstallmentPlanCancellationReplayLookup{}, application.ErrInstallmentCancelConflict
	}
	if state != "COMPLETED" {
		return application.InstallmentPlanCancellationReplayLookup{}, ErrInvalidInstallmentPlanIdemState
	}
	plan, err := snapshot.rehydrate(query.OwnerID)
	if err != nil || plan.Status() != domain.InstallmentPlanStatusCancelled {
		if err == nil {
			err = ErrInvalidStoredInstallmentPlan
		}
		return application.InstallmentPlanCancellationReplayLookup{}, newRepositoryError(ErrInvalidStoredInstallmentPlan, err)
	}
	if plan.OwnerID() != query.OwnerID {
		return application.InstallmentPlanCancellationReplayLookup{}, ErrInvalidStoredInstallmentPlan
	}
	return application.InstallmentPlanCancellationReplayLookup{InstallmentPlan: plan, Found: true}, nil
}

func (repository *CardPurchaseRepository) CancelInstallmentPlan(ctx context.Context, command application.IdempotentInstallmentPlanCancellationCommand) (application.IdempotentInstallmentPlanCancellationResult, error) {
	if err := validateCancellationCommand(command); err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, err
	}
	opCtx, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	tx, err := repository.pool.BeginTx(opCtx, pgx.TxOptions{})
	if err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, newRepositoryError(ErrBeginInstallmentPlanTransaction, err)
	}
	committed := false
	defer rollbackCardPurchaseTransaction(tx, &committed)()
	reserved, err := reserveInstallmentPlanCancellation(opCtx, tx, command)
	if err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, err
	}
	if !reserved {
		lookup, replayErr := readInstallmentPlanCancellationReplay(opCtx, tx, command)
		if replayErr != nil {
			return application.IdempotentInstallmentPlanCancellationResult{}, replayErr
		}
		if err := tx.Commit(opCtx); err != nil {
			return application.IdempotentInstallmentPlanCancellationResult{}, newRepositoryError(ErrCommitInstallmentPlanTransaction, err)
		}
		committed = true
		return application.IdempotentInstallmentPlanCancellationResult{InstallmentPlan: lookup, Replayed: true}, nil
	}
	snapshot, err := queryInstallmentPlan(opCtx, tx, command.OwnerID, command.PlanID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.IdempotentInstallmentPlanCancellationResult{}, application.ErrInstallmentPlanNotFound
	}
	if err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, newRepositoryError(ErrReadInstallmentPlan, err)
	}
	current, err := snapshot.rehydrate(command.OwnerID)
	if err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, newRepositoryError(ErrInvalidStoredInstallmentPlan, err)
	}
	if current.Status() == domain.InstallmentPlanStatusCancelled {
		return application.IdempotentInstallmentPlanCancellationResult{}, domain.ErrInstallmentPlanAlreadyCancelled
	}
	cancelled, err := current.Cancel(command.ExpectedCancelledOn)
	if err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, err
	}
	if err := updateInstallmentPlanCancelled(opCtx, tx, cancelled); err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, err
	}
	if err := insertInstallmentPlanAudit(opCtx, tx, cancelled, "INSTALLMENT_PLAN_CANCELLED", civilDateTimestamp(command.ExpectedCancelledOn)); err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, err
	}
	if err := completeInstallmentPlanCancellation(opCtx, tx, command, cancelled); err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, err
	}
	if err := tx.Commit(opCtx); err != nil {
		return application.IdempotentInstallmentPlanCancellationResult{}, newRepositoryError(ErrCommitInstallmentPlanTransaction, err)
	}
	committed = true
	return application.IdempotentInstallmentPlanCancellationResult{InstallmentPlan: cancelled}, nil
}

func (repository *ExpenseRepository) FindExpenseReplay(ctx context.Context, query application.ExpenseReplayQuery) (application.ExpenseReplayLookup, error) {
	if domain.ValidateUserID(query.UserID) != nil || query.Operation != application.IdempotencyOperationCreateExpense || !validIdempotencyKey(query.IdempotencyKey) {
		return application.ExpenseReplayLookup{}, ErrInvalidIdempotencyState
	}
	opCtx, cancel := context.WithTimeout(ctx, repository.operationTimeout)
	defer cancel()
	var storedFingerprint []byte
	var state string
	var transactionID *string
	err := repository.pool.QueryRow(opCtx, `
		SELECT request_fingerprint, state, transaction_id
		FROM idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, query.UserID, query.Operation, query.IdempotencyKey).Scan(&storedFingerprint, &state, &transactionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ExpenseReplayLookup{}, nil
	}
	if err != nil {
		return application.ExpenseReplayLookup{}, newRepositoryError(ErrReadIdempotency, err)
	}
	if subtle.ConstantTimeCompare(storedFingerprint, query.Fingerprint[:]) != 1 {
		return application.ExpenseReplayLookup{}, application.ErrIdempotencyConflict
	}
	if state != "COMPLETED" || transactionID == nil {
		return application.ExpenseReplayLookup{}, ErrInvalidIdempotencyState
	}
	expense, err := loadExpense(opCtx, repository.pool, *transactionID, query.UserID)
	if err != nil {
		return application.ExpenseReplayLookup{}, err
	}
	return application.ExpenseReplayLookup{Expense: expense, Found: true}, nil
}

func validIdempotencyKey(value string) bool {
	return len(value) >= 1 && len(value) <= 128 && isVisibleASCII(value)
}

func isVisibleASCII(value string) bool {
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validateCardPurchaseCommand(command application.CardPurchaseCommand) error {
	if command.Operation != application.CardPurchaseOperationCreate || !validIdempotencyKey(command.IdempotencyKey) {
		return ErrInvalidCardPurchaseCommand
	}
	if command.Expense.Type() != domain.TransactionTypeExpense || command.Expense.PaymentMethod() != domain.PaymentMethodCredit || command.Expense.Status() != domain.ExpenseStatusRecorded {
		return ErrInvalidCardPurchaseCommand
	}
	cardID, linked := command.Expense.CreditCardID()
	dueOn, hasDue := command.Expense.StatementDueOn()
	if !linked || !hasDue || dueOn.String() == "" || domain.ValidateCreditCardID(cardID) != nil {
		return ErrInvalidCardPurchaseCommand
	}
	if command.InstallmentPlan != nil {
		plan := command.InstallmentPlan
		if plan.Status() != domain.InstallmentPlanStatusActive || plan.OwnerID() != command.Expense.UserID() || plan.CreditCardID() != cardID || plan.ExpenseID() != command.Expense.ID() || plan.TotalAmount() != command.Expense.Amount() {
			return ErrInvalidCardPurchaseCommand
		}
	}
	return nil
}

func rollbackCardPurchaseTransaction(tx pgx.Tx, committed *bool) func() {
	return func() {
		if *committed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(ctx)
	}
}

func reserveCardPurchaseIdempotency(ctx context.Context, tx pgx.Tx, command application.CardPurchaseCommand) (bool, error) {
	var reserved bool
	err := tx.QueryRow(ctx, `
		INSERT INTO card_purchase_idempotency_records (user_id, operation, idempotency_key, request_fingerprint, state, created_at)
		VALUES ($1, $2, $3, $4, 'PENDING', $5)
		ON CONFLICT (user_id, operation, idempotency_key) DO NOTHING
		RETURNING TRUE
	`, command.Expense.UserID(), command.Operation, command.IdempotencyKey, command.Fingerprint[:], command.Expense.CreatedAt()).Scan(&reserved)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, newRepositoryError(ErrReserveCardPurchaseIdempotency, err)
	}
	return reserved, nil
}

func completeCardPurchaseIdempotency(ctx context.Context, tx pgx.Tx, command application.CardPurchaseCommand) error {
	expense := command.Expense
	categoryID, hasCategory := expense.CategoryID()
	var category any
	if hasCategory {
		category = categoryID.String()
	}
	cardID, _ := expense.CreditCardID()
	dueOn, _ := expense.StatementDueOn()
	var planValues [12]any
	if command.InstallmentPlan != nil {
		plan := command.InstallmentPlan
		cancelledOn, _ := plan.CancelledOn()
		planValues = [12]any{plan.ID(), plan.OwnerID(), plan.CreditCardID(), plan.ExpenseID(), plan.TotalAmount().MinorUnits(), plan.TotalAmount().Currency(), plan.InstallmentCount(), postgresDate(plan.FirstDueDate()), plan.DueDayAnchor().Day(), plan.Status(), plan.CreatedAt(), nullableCivilDate(cancelledOn, plan.Status() == domain.InstallmentPlanStatusCancelled)}
	}
	tag, err := tx.Exec(ctx, `
		UPDATE card_purchase_idempotency_records SET
		 state='COMPLETED', expense_id=$4, expense_user_id=$5, expense_description=$6, expense_amount_minor=$7,
		 expense_currency=$8, expense_payment_method=$9, expense_category_id=$10,
		 expense_credit_card_id=$11, expense_statement_due_on=$12, expense_occurred_at=$13,
		 expense_financial_timezone=$14, expense_origin=$15, expense_status=$16,
		 expense_version=$17, expense_created_at=$18, expense_updated_at=$19,
		 plan_id=$20, plan_user_id=$21, plan_credit_card_id=$22, plan_expense_id=$23, plan_total_minor=$24,
		 plan_total_currency=$25, plan_installment_count=$26, plan_first_due_on=$27,
		 plan_due_day=$28, plan_status=$29, plan_created_at=$30, plan_cancelled_on=$31,
		 completed_at=$32
		WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3 AND state='PENDING'
	`, expense.UserID(), command.Operation, command.IdempotencyKey, expense.ID(), expense.UserID(), expense.Description(), expense.Amount().MinorUnits(), expense.Amount().Currency(), expense.PaymentMethod(), category, cardID, postgresDate(dueOn), expense.OccurredAt(), expense.FinancialTimezone(), expense.Origin(), expense.Status(), expense.Version(), expense.CreatedAt(), expense.UpdatedAt(), planValues[0], planValues[1], planValues[2], planValues[3], planValues[4], planValues[5], planValues[6], planValues[7], planValues[8], planValues[9], planValues[10], planValues[11], expense.CreatedAt())
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidCardPurchaseIdempotencyState
		}
		return newRepositoryError(ErrCompleteCardPurchaseIdempotency, err)
	}
	return nil
}

func replayCardPurchase(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID, operation, key string, fingerprint application.RequestFingerprint) (application.CardPurchaseReplayLookup, error) {
	return readCardPurchaseReplay(ctx, querier, application.CardPurchaseReplayQuery{UserID: userID, Operation: operation, IdempotencyKey: key, Fingerprint: fingerprint})
}

func readCardPurchaseReplay(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, query application.CardPurchaseReplayQuery) (application.CardPurchaseReplayLookup, error) {
	var storedFingerprint []byte
	var state string
	es := expenseSnapshot{}
	ps := installmentPlanSnapshot{}
	err := querier.QueryRow(ctx, `
		SELECT request_fingerprint, state, expense_id, expense_description, expense_amount_minor,
		 expense_user_id, expense_currency, expense_payment_method, expense_category_id, expense_credit_card_id,
		 expense_statement_due_on, expense_occurred_at, expense_financial_timezone,
		 expense_origin, expense_status, expense_version, expense_created_at, expense_updated_at,
		 plan_id, plan_user_id, plan_credit_card_id, plan_expense_id, plan_total_minor, plan_total_currency,
		 plan_installment_count, plan_first_due_on, plan_due_day, plan_status, plan_created_at, plan_cancelled_on
		FROM card_purchase_idempotency_records
		WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3
	`, query.UserID, query.Operation, query.IdempotencyKey).Scan(
		&storedFingerprint, &state, &es.id, &es.description, &es.amountMinor, &es.userID, &es.currency, &es.paymentMethod,
		&es.categoryID, &es.creditCardID, &es.statementDueOn, &es.occurredAt, &es.timezone, &es.origin,
		&es.status, &es.version, &es.createdAt, &es.updatedAt, &ps.id, &ps.userID, &ps.creditCardID, &ps.expenseID,
		&ps.totalMinor, &ps.currency, &ps.installmentCount, &ps.firstDueOn, &ps.dueDay, &ps.status,
		&ps.createdAt, &ps.cancelledOn)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.CardPurchaseReplayLookup{}, nil
	}
	if err != nil {
		return application.CardPurchaseReplayLookup{}, newRepositoryError(ErrReadCardPurchaseIdempotency, err)
	}
	if subtle.ConstantTimeCompare(storedFingerprint, query.Fingerprint[:]) != 1 {
		return application.CardPurchaseReplayLookup{}, application.ErrCardPurchaseIdempotencyConflict
	}
	if state != "COMPLETED" {
		return application.CardPurchaseReplayLookup{}, ErrInvalidCardPurchaseIdempotencyState
	}
	expense, err := es.rehydrate()
	if err != nil {
		return application.CardPurchaseReplayLookup{}, newRepositoryError(ErrInvalidCardPurchaseIdempotencyState, err)
	}
	if expense.UserID() != query.UserID {
		return application.CardPurchaseReplayLookup{}, ErrInvalidCardPurchaseIdempotencyState
	}
	var plan *domain.InstallmentPlan
	if ps.id != nil {
		rehydrated, planErr := ps.rehydrate(expense.UserID())
		if planErr != nil {
			return application.CardPurchaseReplayLookup{}, newRepositoryError(ErrInvalidCardPurchaseIdempotencyState, planErr)
		}
		cardID, linked := expense.CreditCardID()
		if !linked || rehydrated.OwnerID() != expense.UserID() || rehydrated.CreditCardID() != cardID || rehydrated.ExpenseID() != expense.ID() {
			return application.CardPurchaseReplayLookup{}, ErrInvalidCardPurchaseIdempotencyState
		}
		plan = &rehydrated
	} else if hasInstallmentPlanSnapshot(ps) {
		return application.CardPurchaseReplayLookup{}, ErrInvalidCardPurchaseIdempotencyState
	}
	return application.CardPurchaseReplayLookup{Expense: expense, InstallmentPlan: plan, Found: true}, nil
}

type expenseSnapshot struct {
	id, userID, description, currency, paymentMethod, timezone, origin, status *string
	amountMinor, version                                                       *int64
	categoryID, creditCardID                                                   *string
	statementDueOn                                                             pgtype.Date
	occurredAt, createdAt, updatedAt                                           *time.Time
}

func (snapshot expenseSnapshot) rehydrate() (domain.Expense, error) {
	if snapshot.id == nil || snapshot.userID == nil || snapshot.description == nil || snapshot.currency == nil || snapshot.paymentMethod == nil || snapshot.timezone == nil || snapshot.origin == nil || snapshot.status == nil || snapshot.amountMinor == nil || snapshot.version == nil || snapshot.occurredAt == nil || snapshot.createdAt == nil || snapshot.updatedAt == nil || !snapshot.statementDueOn.Valid {
		return domain.Expense{}, ErrInvalidCardPurchaseIdempotencyState
	}
	amount, err := domain.NewMoney(*snapshot.amountMinor, domain.Currency(*snapshot.currency))
	if err != nil {
		return domain.Expense{}, err
	}
	if *snapshot.version != 1 || *snapshot.status != string(domain.ExpenseStatusRecorded) || !snapshot.updatedAt.Equal(*snapshot.createdAt) {
		return domain.Expense{}, ErrInvalidCardPurchaseIdempotencyState
	}
	dueOn, err := civilDateFromPostgres(snapshot.statementDueOn)
	if err != nil {
		return domain.Expense{}, err
	}
	var category *domain.CategoryID
	if snapshot.categoryID != nil {
		value, categoryErr := domain.NewCategoryID(*snapshot.categoryID)
		if categoryErr != nil {
			return domain.Expense{}, categoryErr
		}
		category = &value
	}
	return domain.NewExpense(domain.ExpenseParams{ID: *snapshot.id, Details: domain.ExpenseDetails{UserID: *snapshot.userID, Description: *snapshot.description, Amount: amount, PaymentMethod: domain.PaymentMethod(*snapshot.paymentMethod), CategoryID: category, CreditCardID: snapshot.creditCardID, StatementDueOn: &dueOn, OccurredAt: *snapshot.occurredAt, FinancialTimezone: *snapshot.timezone, Origin: domain.Origin(*snapshot.origin)}, CreatedAt: *snapshot.createdAt})
}

type installmentPlanSnapshot struct {
	id, userID, creditCardID, expenseID, currency, status *string
	totalMinor                                            *int64
	installmentCount, dueDay                              *int16
	firstDueOn                                            pgtype.Date
	createdAt                                             *time.Time
	cancelledOn                                           pgtype.Date
}

func (snapshot installmentPlanSnapshot) rehydrate(ownerID string) (domain.InstallmentPlan, error) {
	if snapshot.id == nil || snapshot.userID == nil || snapshot.creditCardID == nil || snapshot.expenseID == nil || snapshot.currency == nil || snapshot.status == nil || snapshot.totalMinor == nil || snapshot.installmentCount == nil || snapshot.dueDay == nil || snapshot.createdAt == nil || !snapshot.firstDueOn.Valid {
		return domain.InstallmentPlan{}, ErrInvalidStoredInstallmentPlan
	}
	firstDue, err := civilDateFromPostgres(snapshot.firstDueOn)
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	amount, err := domain.NewMoney(*snapshot.totalMinor, domain.Currency(*snapshot.currency))
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	status := domain.InstallmentPlanStatus(*snapshot.status)
	var cancelled *domain.CivilDate
	switch status {
	case domain.InstallmentPlanStatusActive:
		if snapshot.cancelledOn.Valid {
			return domain.InstallmentPlan{}, ErrInvalidStoredInstallmentPlan
		}
	case domain.InstallmentPlanStatusCancelled:
		if !snapshot.cancelledOn.Valid {
			return domain.InstallmentPlan{}, ErrInvalidStoredInstallmentPlan
		}
		value, dateErr := civilDateFromPostgres(snapshot.cancelledOn)
		if dateErr != nil {
			return domain.InstallmentPlan{}, dateErr
		}
		cancelled = &value
	default:
		return domain.InstallmentPlan{}, ErrInvalidStoredInstallmentPlan
	}
	anchor, err := domain.NewDayOfMonthAnchor(int(*snapshot.dueDay))
	if err != nil {
		return domain.InstallmentPlan{}, err
	}
	if *snapshot.userID != ownerID {
		return domain.InstallmentPlan{}, ErrInvalidStoredInstallmentPlan
	}
	return domain.RehydrateInstallmentPlan(domain.InstallmentPlanRehydrationParams{ID: *snapshot.id, OwnerID: *snapshot.userID, CreditCardID: *snapshot.creditCardID, ExpenseID: *snapshot.expenseID, TotalAmount: amount, InstallmentCount: int(*snapshot.installmentCount), FirstDueDate: firstDue, DueDayAnchor: anchor, Status: status, CreatedAt: *snapshot.createdAt, CancelledOn: cancelled})
}

func hasInstallmentPlanSnapshot(snapshot installmentPlanSnapshot) bool {
	return snapshot.userID != nil || snapshot.creditCardID != nil || snapshot.expenseID != nil || snapshot.totalMinor != nil || snapshot.installmentCount != nil || snapshot.firstDueOn.Valid || snapshot.dueDay != nil || snapshot.status != nil || snapshot.createdAt != nil || snapshot.cancelledOn.Valid
}

func cloneInstallmentPlan(value *domain.InstallmentPlan) *domain.InstallmentPlan {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func postgresDate(value domain.CivilDate) pgtype.Date {
	return pgtype.Date{Time: time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}

func nullableCivilDate(value domain.CivilDate, valid bool) any {
	if !valid {
		return nil
	}
	return postgresDate(value)
}

func civilDateTimestamp(value domain.CivilDate) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func insertInstallmentPlan(ctx context.Context, tx pgx.Tx, plan domain.InstallmentPlan) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO installment_plans (id,user_id,credit_card_id,expense_id,total_minor,total_currency,installment_count,first_due_on,due_day,status,created_at,cancelled_on)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, plan.ID(), plan.OwnerID(), plan.CreditCardID(), plan.ExpenseID(), plan.TotalAmount().MinorUnits(), plan.TotalAmount().Currency(), plan.InstallmentCount(), postgresDate(plan.FirstDueDate()), plan.DueDayAnchor().Day(), plan.Status(), plan.CreatedAt(), nil)
	if err != nil {
		return newRepositoryError(ErrInsertCardPurchasePlan, err)
	}
	return nil
}

func insertInstallmentPlanAudit(ctx context.Context, tx pgx.Tx, plan domain.InstallmentPlan, event string, createdAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO installment_plan_audit_events (user_id,installment_plan_id,credit_card_id,expense_id,event_type,created_at) VALUES ($1,$2,$3,$4,$5,$6)`, plan.OwnerID(), plan.ID(), plan.CreditCardID(), plan.ExpenseID(), event, createdAt)
	if err != nil {
		return newRepositoryError(ErrInsertInstallmentPlanAudit, err)
	}
	return nil
}

func queryInstallmentPlan(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, ownerID, planID string, forUpdate bool) (installmentPlanSnapshot, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var snapshot installmentPlanSnapshot
	err := querier.QueryRow(ctx, `SELECT id,user_id,credit_card_id,expense_id,total_minor,total_currency,installment_count,first_due_on,due_day,status,created_at,cancelled_on FROM installment_plans WHERE user_id=$1 AND id=$2`+lock, ownerID, planID).Scan(&snapshot.id, &snapshot.userID, &snapshot.creditCardID, &snapshot.expenseID, &snapshot.totalMinor, &snapshot.currency, &snapshot.installmentCount, &snapshot.firstDueOn, &snapshot.dueDay, &snapshot.status, &snapshot.createdAt, &snapshot.cancelledOn)
	return snapshot, err
}

func scanInstallmentPlanSnapshot(scanner interface{ Scan(...any) error }) (installmentPlanSnapshot, error) {
	var snapshot installmentPlanSnapshot
	err := scanner.Scan(&snapshot.id, &snapshot.userID, &snapshot.creditCardID, &snapshot.expenseID, &snapshot.totalMinor, &snapshot.currency, &snapshot.installmentCount, &snapshot.firstDueOn, &snapshot.dueDay, &snapshot.status, &snapshot.createdAt, &snapshot.cancelledOn)
	return snapshot, err
}

func updateInstallmentPlanCancelled(ctx context.Context, tx pgx.Tx, plan domain.InstallmentPlan) error {
	cancelledOn, ok := plan.CancelledOn()
	if !ok {
		return ErrInvalidInstallmentPlanIdemState
	}
	tag, err := tx.Exec(ctx, `UPDATE installment_plans SET status='CANCELLED', cancelled_on=$3 WHERE id=$1 AND user_id=$2 AND status='ACTIVE' AND cancelled_on IS NULL`, plan.ID(), plan.OwnerID(), postgresDate(cancelledOn))
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidStoredInstallmentPlan
		}
		return newRepositoryError(ErrUpdateInstallmentPlan, err)
	}
	return nil
}

func validateCancellationCommand(command application.IdempotentInstallmentPlanCancellationCommand) error {
	if command.Operation != application.CancelInstallmentPlanOperation || !validIdempotencyKey(command.IdempotencyKey) || domain.ValidateUserID(command.OwnerID) != nil || domain.ValidateInstallmentPlanID(command.PlanID) != nil || command.InstallmentPlan.ID() != command.PlanID || command.InstallmentPlan.OwnerID() != command.OwnerID || command.InstallmentPlan.Status() != domain.InstallmentPlanStatusCancelled {
		return ErrInvalidInstallmentPlanIdemState
	}
	cancelledOn, ok := command.InstallmentPlan.CancelledOn()
	if !ok || !cancelledOn.Equal(command.ExpectedCancelledOn) {
		return ErrInvalidInstallmentPlanIdemState
	}
	params := domain.InstallmentPlanRehydrationParams{
		ID: command.InstallmentPlan.ID(), OwnerID: command.InstallmentPlan.OwnerID(),
		CreditCardID: command.InstallmentPlan.CreditCardID(), ExpenseID: command.InstallmentPlan.ExpenseID(),
		TotalAmount: command.InstallmentPlan.TotalAmount(), InstallmentCount: command.InstallmentPlan.InstallmentCount(),
		FirstDueDate: command.InstallmentPlan.FirstDueDate(), DueDayAnchor: command.InstallmentPlan.DueDayAnchor(),
		Status: command.InstallmentPlan.Status(), CreatedAt: command.InstallmentPlan.CreatedAt(), CancelledOn: &cancelledOn,
	}
	if _, err := domain.RehydrateInstallmentPlan(params); err != nil {
		return ErrInvalidInstallmentPlanIdemState
	}
	return nil
}

func reserveInstallmentPlanCancellation(ctx context.Context, tx pgx.Tx, command application.IdempotentInstallmentPlanCancellationCommand) (bool, error) {
	var reserved bool
	err := tx.QueryRow(ctx, `INSERT INTO installment_plan_idempotency_records (user_id,operation,idempotency_key,request_fingerprint,state,created_at) VALUES ($1,$2,$3,$4,'PENDING',$5) ON CONFLICT (user_id,operation,idempotency_key) DO NOTHING RETURNING TRUE`, command.OwnerID, command.Operation, command.IdempotencyKey, command.Fingerprint[:], command.InstallmentPlan.CreatedAt()).Scan(&reserved)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, newRepositoryError(ErrReserveInstallmentPlanIdempotency, err)
	}
	return reserved, nil
}

func readInstallmentPlanCancellationReplay(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, command application.IdempotentInstallmentPlanCancellationCommand) (domain.InstallmentPlan, error) {
	var fingerprint []byte
	var state string
	snapshot := installmentPlanSnapshot{}
	err := querier.QueryRow(ctx, `SELECT request_fingerprint,state,plan_id,plan_user_id,plan_credit_card_id,plan_expense_id,plan_total_minor,plan_total_currency,plan_installment_count,plan_first_due_on,plan_due_day,plan_status,plan_created_at,plan_cancelled_on FROM installment_plan_idempotency_records WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, command.OwnerID, command.Operation, command.IdempotencyKey).Scan(&fingerprint, &state, &snapshot.id, &snapshot.userID, &snapshot.creditCardID, &snapshot.expenseID, &snapshot.totalMinor, &snapshot.currency, &snapshot.installmentCount, &snapshot.firstDueOn, &snapshot.dueDay, &snapshot.status, &snapshot.createdAt, &snapshot.cancelledOn)
	if err != nil {
		return domain.InstallmentPlan{}, newRepositoryError(ErrReadInstallmentPlanIdempotency, err)
	}
	if subtle.ConstantTimeCompare(fingerprint, command.Fingerprint[:]) != 1 {
		return domain.InstallmentPlan{}, application.ErrInstallmentCancelConflict
	}
	if state != "COMPLETED" {
		return domain.InstallmentPlan{}, ErrInvalidInstallmentPlanIdemState
	}
	return snapshot.rehydrate(command.OwnerID)
}

func completeInstallmentPlanCancellation(ctx context.Context, tx pgx.Tx, command application.IdempotentInstallmentPlanCancellationCommand, plan domain.InstallmentPlan) error {
	cancelledOn, _ := plan.CancelledOn()
	tag, err := tx.Exec(ctx, `UPDATE installment_plan_idempotency_records SET state='COMPLETED',plan_id=$4,plan_user_id=$5,plan_credit_card_id=$6,plan_expense_id=$7,plan_total_minor=$8,plan_total_currency=$9,plan_installment_count=$10,plan_first_due_on=$11,plan_due_day=$12,plan_status=$13,plan_created_at=$14,plan_cancelled_on=$15,completed_at=$16 WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3 AND state='PENDING'`, command.OwnerID, command.Operation, command.IdempotencyKey, plan.ID(), plan.OwnerID(), plan.CreditCardID(), plan.ExpenseID(), plan.TotalAmount().MinorUnits(), plan.TotalAmount().Currency(), plan.InstallmentCount(), postgresDate(plan.FirstDueDate()), plan.DueDayAnchor().Day(), plan.Status(), plan.CreatedAt(), postgresDate(cancelledOn), plan.CreatedAt())
	if err != nil || tag.RowsAffected() != 1 {
		if err == nil {
			err = ErrInvalidInstallmentPlanIdemState
		}
		return newRepositoryError(ErrCompleteInstallmentPlanIdempotency, err)
	}
	return nil
}
