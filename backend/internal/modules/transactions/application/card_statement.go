package application

import (
	"context"
	"errors"
	"math"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingCardStatementSnapshotReader = errors.New("card statement: snapshot reader is required")
	ErrInvalidCardStatementOwnerID        = errors.New("card statement: invalid owner id")
	ErrCardStatementCreditCardNotFound    = errors.New("card statement: credit card not found")
	ErrCardStatementSnapshotQuery         = errors.New("card statement: snapshot query failed")
	ErrCardStatementDependency            = errors.New("card statement: invalid dependency result")
	ErrCardStatementDuplicate             = errors.New("card statement: duplicate source")
)

// CardStatementSnapshotQuery identifies the owner-scoped, explicit statement
// read. The due date is part of the query boundary even though the adapter may
// load a wider set of rows to preserve schedule semantics.
type CardStatementSnapshotQuery struct {
	OwnerID        string
	CreditCardID   string
	StatementDueOn domain.CivilDate
}

// CardStatementSnapshot contains all persisted inputs needed to derive one
// statement without additional reads. It is never persisted itself.
type CardStatementSnapshot struct {
	CreditCard       domain.CreditCard
	Expenses         []domain.Expense
	InstallmentPlans []domain.InstallmentPlan
	Found            bool
}

// CardStatementSnapshotReader is the single read-only boundary for a
// statement. A Stage 2 adapter must populate the card, its card-linked
// expenses and their plans from one consistent snapshot.
type CardStatementSnapshotReader interface {
	Read(context.Context, CardStatementSnapshotQuery) (CardStatementSnapshot, error)
}

type GetCardStatementInput struct {
	OwnerID        string
	CreditCardID   string
	StatementDueOn domain.CivilDate
}

type GetCardStatementResult struct {
	Statement domain.CardStatement
}

// GetCardStatement derives one explicit card statement in memory. It has no
// clock, ID generator, idempotency or persistence dependency.
type GetCardStatement struct {
	reader CardStatementSnapshotReader
}

func NewGetCardStatement(reader CardStatementSnapshotReader) (*GetCardStatement, error) {
	if reader == nil {
		return nil, ErrMissingCardStatementSnapshotReader
	}
	return &GetCardStatement{reader: reader}, nil
}

func (useCase *GetCardStatement) Execute(ctx context.Context, input GetCardStatementInput) (GetCardStatementResult, error) {
	if err := ctx.Err(); err != nil {
		return GetCardStatementResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return GetCardStatementResult{}, ErrInvalidCardStatementOwnerID
	}
	if err := domain.ValidateCreditCardID(input.CreditCardID); err != nil {
		return GetCardStatementResult{}, err
	}
	if _, err := domain.NewCivilDate(input.StatementDueOn.Year(), input.StatementDueOn.Month(), input.StatementDueOn.Day()); err != nil {
		return GetCardStatementResult{}, domain.ErrInvalidCardStatementDueDate
	}

	snapshot, err := useCase.reader.Read(ctx, CardStatementSnapshotQuery{
		OwnerID: input.OwnerID, CreditCardID: input.CreditCardID, StatementDueOn: input.StatementDueOn,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return GetCardStatementResult{}, err
		}
		return GetCardStatementResult{}, newSafeOperationError(ErrCardStatementSnapshotQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return GetCardStatementResult{}, err
	}
	if !snapshot.Found {
		if len(snapshot.Expenses) != 0 || len(snapshot.InstallmentPlans) != 0 {
			return GetCardStatementResult{}, newSafeOperationError(ErrCardStatementDependency, errors.New("not found snapshot contains rows"))
		}
		return GetCardStatementResult{}, ErrCardStatementCreditCardNotFound
	}

	statement, err := deriveCardStatement(input, snapshot)
	if err != nil {
		return GetCardStatementResult{}, err
	}
	return GetCardStatementResult{Statement: statement}, nil
}

func deriveCardStatement(input GetCardStatementInput, snapshot CardStatementSnapshot) (domain.CardStatement, error) {
	if err := validateCreditCardDependencySnapshot(snapshot.CreditCard, input.OwnerID, input.CreditCardID, nil); err != nil {
		return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, err)
	}

	plansByExpenseID := make(map[string]domain.InstallmentPlan, len(snapshot.InstallmentPlans))
	seenPlans := make(map[string]struct{}, len(snapshot.InstallmentPlans))
	for _, plan := range snapshot.InstallmentPlans {
		if err := validateInstallmentPlanDependencySnapshot(plan, input.OwnerID, ""); err != nil || plan.CreditCardID() != input.CreditCardID {
			return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, errors.New("invalid installment plan snapshot"))
		}
		if _, exists := seenPlans[plan.ID()]; exists {
			return domain.CardStatement{}, ErrCardStatementDuplicate
		}
		seenPlans[plan.ID()] = struct{}{}
		if _, exists := plansByExpenseID[plan.ExpenseID()]; exists {
			return domain.CardStatement{}, ErrCardStatementDuplicate
		}
		plansByExpenseID[plan.ExpenseID()] = plan
	}

	expensesByID := make(map[string]domain.Expense, len(snapshot.Expenses))
	for _, expense := range snapshot.Expenses {
		if err := validateCardStatementExpense(expense, input.OwnerID, input.CreditCardID); err != nil {
			return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, err)
		}
		if _, exists := expensesByID[expense.ID()]; exists {
			return domain.CardStatement{}, ErrCardStatementDuplicate
		}
		expensesByID[expense.ID()] = expense
	}

	lines := make([]domain.CardStatementLine, 0)
	plannedExpenseIDs := make(map[string]struct{}, len(plansByExpenseID))
	for _, plan := range snapshot.InstallmentPlans {
		expense, exists := expensesByID[plan.ExpenseID()]
		if !exists {
			return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, errors.New("installment plan expense is missing"))
		}
		dueOn, hasDue := expense.StatementDueOn()
		linkedCard, hasCard := expense.CreditCardID()
		if !hasDue || !hasCard || !dueOn.Equal(plan.FirstDueDate()) || linkedCard != input.CreditCardID || !expense.Amount().Equal(plan.TotalAmount()) {
			return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, errors.New("installment plan expense linkage is invalid"))
		}

		var schedule []domain.Installment
		var err error
		switch plan.Status() {
		case domain.InstallmentPlanStatusActive:
			schedule, err = plan.Schedule()
		case domain.InstallmentPlanStatusCancelled:
			schedule, err = plan.EffectiveSchedule()
		default:
			err = domain.ErrInvalidInstallmentPlanStatus
		}
		if err != nil {
			return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, err)
		}
		plannedExpenseIDs[expense.ID()] = struct{}{}
		for _, installment := range schedule {
			if !installment.DueDate().Equal(input.StatementDueOn) {
				continue
			}
			number, count := installment.Number(), installment.TotalCount()
			line, lineErr := domain.NewCardStatementLine(domain.CardStatementLineParams{
				ExpenseID: expense.ID(), Description: expense.Description(), Amount: installment.Amount(),
				OccurredAt: expense.OccurredAt(), PurchaseMode: domain.CardStatementPurchaseModeInstallment,
				InstallmentNumber: &number, InstallmentCount: &count,
			})
			if lineErr != nil {
				return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, lineErr)
			}
			lines = append(lines, line)
		}
	}

	for _, expense := range snapshot.Expenses {
		if _, planned := plannedExpenseIDs[expense.ID()]; planned {
			continue
		}
		dueOn, _ := expense.StatementDueOn()
		if !dueOn.Equal(input.StatementDueOn) {
			continue
		}
		line, err := domain.NewCardStatementLine(domain.CardStatementLineParams{
			ExpenseID: expense.ID(), Description: expense.Description(), Amount: expense.Amount(),
			OccurredAt: expense.OccurredAt(), PurchaseMode: domain.CardStatementPurchaseModeOneTime,
		})
		if err != nil {
			return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, err)
		}
		lines = append(lines, line)
	}

	totalMinor := int64(0)
	for _, line := range lines {
		if line.Amount().MinorUnits() > math.MaxInt64-totalMinor {
			return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, domain.ErrCardStatementAmountOverflow)
		}
		totalMinor += line.Amount().MinorUnits()
	}
	total, err := domain.NewMoney(totalMinor, domain.CurrencyBRL)
	if err != nil {
		return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, err)
	}
	statement, err := domain.NewCardStatement(domain.CardStatementParams{
		CreditCardID: input.CreditCardID, StatementDueOn: input.StatementDueOn,
		TotalAmount: total, Lines: lines,
	})
	if err != nil {
		if errors.Is(err, domain.ErrCardStatementDuplicateLine) {
			return domain.CardStatement{}, ErrCardStatementDuplicate
		}
		return domain.CardStatement{}, newSafeOperationError(ErrCardStatementDependency, err)
	}
	return statement, nil
}

func validateCardStatementExpense(expense domain.Expense, ownerID, creditCardID string) error {
	if expense.UserID() != ownerID || expense.Type() != domain.TransactionTypeExpense || expense.PaymentMethod() != domain.PaymentMethodCredit || expense.Status() != domain.ExpenseStatusRecorded {
		return errors.New("invalid card statement expense")
	}
	linkedCard, hasCard := expense.CreditCardID()
	dueOn, hasDue := expense.StatementDueOn()
	if !hasCard || !hasDue || linkedCard != creditCardID || domain.ValidateCreditCardID(linkedCard) != nil {
		return errors.New("invalid card statement expense linkage")
	}

	categoryID, hasCategory := expense.CategoryID()
	var category *domain.CategoryID
	if hasCategory {
		category = &categoryID
	}
	cardID := linkedCard
	due := dueOn
	canonical, err := domain.NewExpense(domain.ExpenseParams{
		ID: expense.ID(),
		Details: domain.ExpenseDetails{
			UserID: expense.UserID(), Description: expense.Description(), Amount: expense.Amount(),
			PaymentMethod: expense.PaymentMethod(), CategoryID: category, CreditCardID: &cardID,
			StatementDueOn: &due, OccurredAt: expense.OccurredAt(), FinancialTimezone: expense.FinancialTimezone(), Origin: expense.Origin(),
		},
		CreatedAt: expense.CreatedAt(),
	})
	if err != nil || canonical.ID() != expense.ID() || canonical.UserID() != expense.UserID() || canonical.Description() != expense.Description() || !canonical.Amount().Equal(expense.Amount()) || canonical.PaymentMethod() != expense.PaymentMethod() || canonical.OccurredAt() != expense.OccurredAt() || canonical.FinancialTimezone() != expense.FinancialTimezone() || canonical.Origin() != expense.Origin() || canonical.Status() != expense.Status() || canonical.Version() != expense.Version() || canonical.CreatedAt() != expense.CreatedAt() || canonical.UpdatedAt() != expense.UpdatedAt() {
		return errors.New("invalid card statement expense snapshot")
	}
	canonicalCard, canonicalHasCard := canonical.CreditCardID()
	canonicalDue, canonicalHasDue := canonical.StatementDueOn()
	if !canonicalHasCard || !canonicalHasDue || canonicalCard != linkedCard || !canonicalDue.Equal(dueOn) {
		return errors.New("invalid card statement expense linkage")
	}
	actualCategory, actualHasCategory := expense.CategoryID()
	if actualHasCategory != hasCategory || (hasCategory && actualCategory != categoryID) {
		return errors.New("invalid card statement expense category")
	}
	return nil
}
