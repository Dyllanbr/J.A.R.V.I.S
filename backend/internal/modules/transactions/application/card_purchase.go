package application

import (
	"context"
	"errors"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

const (
	CardPurchaseOperationCreate    = "CREATE_CARD_PURCHASE"
	cardPurchaseFingerprintVersion = "card-purchase-create-v1"
)

type CardPurchaseMode string

const (
	CardPurchaseModeOneTime     CardPurchaseMode = "ONE_TIME"
	CardPurchaseModeInstallment CardPurchaseMode = "INSTALLMENT"
)

var (
	ErrMissingCardPurchaseCardReader      = errors.New("card purchase: credit card reader is required")
	ErrMissingCardPurchaseStore           = errors.New("card purchase: command store is required")
	ErrMissingCardPurchaseReplay          = errors.New("card purchase: replay reader is required")
	ErrMissingCardPurchaseExpenseID       = errors.New("card purchase: expense id generator is required")
	ErrMissingCardPurchasePlanID          = errors.New("card purchase: installment plan id generator is required")
	ErrCardPurchaseIdempotencyKeyRequired = errors.New("card purchase: idempotency key is required")
	ErrCardPurchaseIdempotencyKeyInvalid  = errors.New("card purchase: idempotency key is invalid")
	ErrCardPurchaseIdempotencyConflict    = errors.New("card purchase: idempotency key was reused")
	ErrCardPurchaseCreditCardNotFound     = errors.New("card purchase: credit card not found")
	ErrCardPurchaseCreditCardArchived     = errors.New("card purchase: credit card is archived")
	ErrCardPurchaseCategoryNotApplicable  = errors.New("card purchase: category is not applicable")
	ErrCardPurchaseReplayLookup           = errors.New("card purchase: replay lookup failed")
	ErrCardPurchasePersistence            = errors.New("card purchase: persistence failed")
	ErrCardPurchaseDependencyResult       = errors.New("card purchase: invalid dependency result")
	ErrCardPurchaseExpenseIDGeneration    = errors.New("card purchase: expense id generation failed")
	ErrCardPurchasePlanIDGeneration       = errors.New("card purchase: installment plan id generation failed")
)

// CardPurchaseInput contains only semantic purchase data. Payment method is
// deliberately absent: this command always records CREDIT.
type CardPurchaseInput struct {
	UserID           string
	Description      string
	AmountMinor      int64
	Currency         domain.Currency
	OccurredAt       time.Time
	CategoryID       *domain.CategoryID
	CreditCardID     string
	InstallmentCount *int
	Origin           domain.Origin
}

type CardPurchasePreview struct {
	Expense          domain.ExpenseDetails
	Cycle            domain.CardCycle
	Mode             CardPurchaseMode
	InstallmentCount *int
	Installments     []domain.Installment
}

type PreviewCardPurchaseResult struct{ Preview CardPurchasePreview }

// PreviewCardPurchase validates and derives a purchase without IDs, Clock or
// persistence side effects.
type PreviewCardPurchase struct {
	cardReader      CreditCardLookupReader
	categoryCatalog CategoryCatalog
}

func NewPreviewCardPurchase(cardReader CreditCardLookupReader) (*PreviewCardPurchase, error) {
	return NewPreviewCardPurchaseWithCategoryCatalog(cardReader, nil)
}

func NewPreviewCardPurchaseWithCategoryCatalog(cardReader CreditCardLookupReader, categoryCatalog CategoryCatalog) (*PreviewCardPurchase, error) {
	if cardReader == nil {
		return nil, ErrMissingCardPurchaseCardReader
	}
	return &PreviewCardPurchase{cardReader: cardReader, categoryCatalog: categoryCatalog}, nil
}

func (useCase *PreviewCardPurchase) Execute(ctx context.Context, input CardPurchaseInput) (PreviewCardPurchaseResult, error) {
	normalized, err := normalizeCardPurchaseInput(ctx, input)
	if err != nil {
		return PreviewCardPurchaseResult{}, err
	}
	lookup, err := useCase.cardReader.FindCreditCard(ctx, normalized.userID, normalized.creditCardID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PreviewCardPurchaseResult{}, err
		}
		return PreviewCardPurchaseResult{}, newSafeOperationError(ErrCreditCardLookup, err)
	}
	if err := ctx.Err(); err != nil {
		return PreviewCardPurchaseResult{}, err
	}
	if !lookup.Found {
		return PreviewCardPurchaseResult{}, ErrCardPurchaseCreditCardNotFound
	}
	if err := validateCreditCardDependencySnapshot(lookup.CreditCard, normalized.userID, normalized.creditCardID, nil); err != nil {
		return PreviewCardPurchaseResult{}, ErrCardPurchaseDependencyResult
	}
	if lookup.CreditCard.Status() != domain.CreditCardStatusActive {
		return PreviewCardPurchaseResult{}, ErrCardPurchaseCreditCardArchived
	}
	categoryID, err := validateCategoryForType(ctx, useCase.categoryCatalog, normalized.categoryID, domain.TransactionTypeExpense)
	if err != nil {
		return PreviewCardPurchaseResult{}, mapCardPurchaseCategoryError(err)
	}
	if err := ctx.Err(); err != nil {
		return PreviewCardPurchaseResult{}, err
	}
	purchaseOn, err := financialCivilDate(normalized.occurredAt)
	if err != nil {
		return PreviewCardPurchaseResult{}, err
	}
	cycle, err := domain.CalculateCardCycle(purchaseOn, lookup.CreditCard.ClosingDayAnchor(), lookup.CreditCard.DueDayAnchor())
	if err != nil {
		return PreviewCardPurchaseResult{}, err
	}
	details := domain.ExpenseDetails{
		UserID: normalized.userID, Description: normalized.description, Amount: normalized.amount,
		PaymentMethod: domain.PaymentMethodCredit, CategoryID: categoryID,
		OccurredAt: normalized.occurredAt, FinancialTimezone: FinancialTimezone, Origin: normalized.origin,
	}
	cardID := normalized.creditCardID
	dueOn := cycle.StatementDueOn()
	details.CreditCardID = &cardID
	details.StatementDueOn = &dueOn
	preview := CardPurchasePreview{Expense: details, Cycle: cycle, Mode: normalized.mode, InstallmentCount: cloneOptionalInt(normalized.installmentCount)}
	if normalized.mode == CardPurchaseModeInstallment {
		preview.Installments, err = domain.BuildInstallmentSchedule(normalized.amount, *normalized.installmentCount, dueOn, lookup.CreditCard.DueDayAnchor())
		if err != nil {
			return PreviewCardPurchaseResult{}, err
		}
	}
	return PreviewCardPurchaseResult{Preview: preview}, nil
}

type normalizedCardPurchaseInput struct {
	userID           string
	description      string
	amount           domain.Money
	occurredAt       time.Time
	categoryID       *domain.CategoryID
	creditCardID     string
	installmentCount *int
	mode             CardPurchaseMode
	origin           domain.Origin
}

func normalizeCardPurchaseInput(ctx context.Context, input CardPurchaseInput) (normalizedCardPurchaseInput, error) {
	if err := ctx.Err(); err != nil {
		return normalizedCardPurchaseInput{}, err
	}
	if err := domain.ValidateUserID(input.UserID); err != nil {
		return normalizedCardPurchaseInput{}, err
	}
	if err := domain.ValidateCreditCardID(input.CreditCardID); err != nil {
		return normalizedCardPurchaseInput{}, domain.ErrInvalidCreditCardID
	}
	amount, err := domain.NewMoney(input.AmountMinor, input.Currency)
	if err != nil {
		return normalizedCardPurchaseInput{}, err
	}
	details, err := domain.NormalizeExpenseDetails(domain.ExpenseDetails{
		UserID: input.UserID, Description: input.Description, Amount: amount,
		PaymentMethod: domain.PaymentMethodCredit, CategoryID: input.CategoryID,
		OccurredAt: input.OccurredAt, FinancialTimezone: FinancialTimezone, Origin: input.Origin,
	})
	if err != nil {
		return normalizedCardPurchaseInput{}, err
	}
	mode, count, err := normalizeInstallmentCount(input.InstallmentCount)
	if err != nil {
		return normalizedCardPurchaseInput{}, err
	}
	return normalizedCardPurchaseInput{
		userID: details.UserID, description: details.Description, amount: details.Amount,
		occurredAt: canonicalizeFinancialInstant(details.OccurredAt), categoryID: details.CategoryID,
		creditCardID: input.CreditCardID, installmentCount: count, mode: mode, origin: details.Origin,
	}, nil
}

func normalizeInstallmentCount(value *int) (CardPurchaseMode, *int, error) {
	if value == nil {
		return CardPurchaseModeOneTime, nil, nil
	}
	if *value == 1 || *value < 1 || *value > domain.MaxInstallmentCount {
		return "", nil, domain.ErrInvalidInstallmentCount
	}
	count := *value
	return CardPurchaseModeInstallment, &count, nil
}

func financialCivilDate(value time.Time) (domain.CivilDate, error) {
	location, err := time.LoadLocation(FinancialTimezone)
	if err != nil {
		return domain.CivilDate{}, ErrFinancialTimezoneUnavailable
	}
	local := value.In(location)
	date, err := domain.NewCivilDate(local.Year(), local.Month(), local.Day())
	if err != nil {
		return domain.CivilDate{}, err
	}
	return date, nil
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func mapCardPurchaseCategoryError(err error) error {
	if errors.Is(err, ErrCategoryNotApplicable) {
		return ErrCardPurchaseCategoryNotApplicable
	}
	return err
}

type CardPurchaseReplayQuery struct {
	UserID         string
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
}

type CardPurchaseReplayLookup struct {
	Expense         domain.Expense
	InstallmentPlan *domain.InstallmentPlan
	Found           bool
}

type CardPurchaseReplayReader interface {
	FindCardPurchaseReplay(context.Context, CardPurchaseReplayQuery) (CardPurchaseReplayLookup, error)
}

type CardPurchaseCommand struct {
	Operation       string
	IdempotencyKey  string
	Fingerprint     RequestFingerprint
	Expense         domain.Expense
	InstallmentPlan *domain.InstallmentPlan
}

type CardPurchaseCommandResult struct {
	Expense         domain.Expense
	InstallmentPlan *domain.InstallmentPlan
	Replayed        bool
}

type cardPurchaseSnapshotExpectation struct {
	cycle        domain.CardCycle
	dueDayAnchor domain.DayOfMonthAnchor
	createdAt    time.Time
}

type CardPurchaseCommandStore interface {
	RecordCardPurchase(context.Context, CardPurchaseCommand) (CardPurchaseCommandResult, error)
}

type RecordCardPurchaseInput struct {
	Purchase       CardPurchaseInput
	IdempotencyKey string
}

type RecordCardPurchaseResult struct {
	Expense         domain.Expense
	InstallmentPlan *domain.InstallmentPlan
	Replayed        bool
}

type RecordCardPurchase struct {
	store              CardPurchaseCommandStore
	replayReader       CardPurchaseReplayReader
	cardReader         CreditCardLookupReader
	categoryCatalog    CategoryCatalog
	expenseIDGenerator ExpenseIDGenerator
	planIDGenerator    InstallmentPlanIDGenerator
	clock              Clock
}

func NewRecordCardPurchase(
	store CardPurchaseCommandStore,
	replayReader CardPurchaseReplayReader,
	cardReader CreditCardLookupReader,
	categoryCatalog CategoryCatalog,
	expenseIDGenerator ExpenseIDGenerator,
	planIDGenerator InstallmentPlanIDGenerator,
	clock Clock,
) (*RecordCardPurchase, error) {
	if store == nil {
		return nil, ErrMissingCardPurchaseStore
	}
	if replayReader == nil {
		return nil, ErrMissingCardPurchaseReplay
	}
	if cardReader == nil {
		return nil, ErrMissingCardPurchaseCardReader
	}
	if expenseIDGenerator == nil {
		return nil, ErrMissingCardPurchaseExpenseID
	}
	if planIDGenerator == nil {
		return nil, ErrMissingCardPurchasePlanID
	}
	if clock == nil {
		return nil, ErrMissingClock
	}
	return &RecordCardPurchase{store: store, replayReader: replayReader, cardReader: cardReader, categoryCatalog: categoryCatalog, expenseIDGenerator: expenseIDGenerator, planIDGenerator: planIDGenerator, clock: clock}, nil
}

func (useCase *RecordCardPurchase) Execute(ctx context.Context, input RecordCardPurchaseInput) (RecordCardPurchaseResult, error) {
	if err := ctx.Err(); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	if err := validateCardPurchaseIdempotencyKey(input.IdempotencyKey); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	normalized, err := normalizeCardPurchaseInput(ctx, input.Purchase)
	if err != nil {
		return RecordCardPurchaseResult{}, err
	}
	fingerprint := fingerprintCardPurchase(normalized)
	replayed, found, err := findCardPurchaseReplay(ctx, useCase.replayReader, CardPurchaseReplayQuery{UserID: normalized.userID, Operation: CardPurchaseOperationCreate, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
	if err != nil {
		return RecordCardPurchaseResult{}, err
	}
	if found {
		if err := validateStoredCardPurchase(CardPurchaseCommandResult{Expense: replayed.Expense, InstallmentPlan: replayed.InstallmentPlan}, normalized, nil); err != nil {
			return RecordCardPurchaseResult{}, err
		}
		return RecordCardPurchaseResult{Expense: replayed.Expense, InstallmentPlan: clonePlan(replayed.InstallmentPlan), Replayed: true}, nil
	}
	if err := ctx.Err(); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	lookup, err := useCase.cardReader.FindCreditCard(ctx, normalized.userID, normalized.creditCardID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return RecordCardPurchaseResult{}, err
		}
		return RecordCardPurchaseResult{}, newSafeOperationError(ErrCreditCardLookup, err)
	}
	if !lookup.Found {
		return RecordCardPurchaseResult{}, ErrCardPurchaseCreditCardNotFound
	}
	if err := validateCreditCardDependencySnapshot(lookup.CreditCard, normalized.userID, normalized.creditCardID, nil); err != nil {
		return RecordCardPurchaseResult{}, ErrCardPurchaseDependencyResult
	}
	if lookup.CreditCard.Status() != domain.CreditCardStatusActive {
		return RecordCardPurchaseResult{}, ErrCardPurchaseCreditCardArchived
	}
	categoryID, err := validateCategoryForType(ctx, useCase.categoryCatalog, normalized.categoryID, domain.TransactionTypeExpense)
	if err != nil {
		return RecordCardPurchaseResult{}, mapCardPurchaseCategoryError(err)
	}
	if err := ctx.Err(); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	purchaseOn, err := financialCivilDate(normalized.occurredAt)
	if err != nil {
		return RecordCardPurchaseResult{}, err
	}
	cycle, err := domain.CalculateCardCycle(purchaseOn, lookup.CreditCard.ClosingDayAnchor(), lookup.CreditCard.DueDayAnchor())
	if err != nil {
		return RecordCardPurchaseResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	expenseID, err := useCase.expenseIDGenerator.NewExpenseID()
	if err != nil {
		return RecordCardPurchaseResult{}, newSafeOperationError(ErrCardPurchaseExpenseIDGeneration, err)
	}
	if err := ctx.Err(); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	var planID string
	if normalized.mode == CardPurchaseModeInstallment {
		planID, err = useCase.planIDGenerator.NewInstallmentPlanID()
		if err != nil {
			return RecordCardPurchaseResult{}, newSafeOperationError(ErrCardPurchasePlanIDGeneration, err)
		}
		if err := ctx.Err(); err != nil {
			return RecordCardPurchaseResult{}, err
		}
	}
	createdAt := canonicalizeFinancialInstant(useCase.clock.Now())
	cardID := normalized.creditCardID
	dueOn := cycle.StatementDueOn()
	expense, err := domain.NewExpense(domain.ExpenseParams{ID: expenseID, Details: domain.ExpenseDetails{UserID: normalized.userID, Description: normalized.description, Amount: normalized.amount, PaymentMethod: domain.PaymentMethodCredit, CategoryID: categoryID, CreditCardID: &cardID, StatementDueOn: &dueOn, OccurredAt: normalized.occurredAt, FinancialTimezone: FinancialTimezone, Origin: normalized.origin}, CreatedAt: createdAt})
	if err != nil {
		return RecordCardPurchaseResult{}, err
	}
	var plan *domain.InstallmentPlan
	if normalized.mode == CardPurchaseModeInstallment {
		created, err := domain.NewInstallmentPlan(domain.InstallmentPlanParams{ID: planID, OwnerID: normalized.userID, CreditCardID: normalized.creditCardID, ExpenseID: expense.ID(), TotalAmount: normalized.amount, InstallmentCount: *normalized.installmentCount, FirstDueDate: dueOn, DueDayAnchor: lookup.CreditCard.DueDayAnchor(), CreatedAt: createdAt})
		if err != nil {
			return RecordCardPurchaseResult{}, err
		}
		plan = &created
	}
	if err := ctx.Err(); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	stored, err := useCase.store.RecordCardPurchase(ctx, CardPurchaseCommand{Operation: CardPurchaseOperationCreate, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint, Expense: expense, InstallmentPlan: plan})
	if err != nil {
		if errors.Is(err, ErrCardPurchaseIdempotencyConflict) {
			return RecordCardPurchaseResult{}, ErrCardPurchaseIdempotencyConflict
		}
		return RecordCardPurchaseResult{}, newSafeOperationError(ErrCardPurchasePersistence, err)
	}
	if err := validateStoredCardPurchase(stored, normalized, &cardPurchaseSnapshotExpectation{cycle: cycle, dueDayAnchor: lookup.CreditCard.DueDayAnchor(), createdAt: createdAt}); err != nil {
		return RecordCardPurchaseResult{}, err
	}
	if !stored.Replayed && !cardPurchaseResultsEqual(stored, CardPurchaseCommandResult{Expense: expense, InstallmentPlan: plan}) {
		return RecordCardPurchaseResult{}, ErrCardPurchaseDependencyResult
	}
	return RecordCardPurchaseResult{Expense: stored.Expense, InstallmentPlan: clonePlan(stored.InstallmentPlan), Replayed: stored.Replayed}, nil
}

func validateCardPurchaseIdempotencyKey(key string) error {
	if key == "" {
		return ErrCardPurchaseIdempotencyKeyRequired
	}
	if !isValidIdempotencyKey(key) {
		return ErrCardPurchaseIdempotencyKeyInvalid
	}
	return nil
}

func findCardPurchaseReplay(ctx context.Context, reader CardPurchaseReplayReader, query CardPurchaseReplayQuery) (CardPurchaseReplayLookup, bool, error) {
	lookup, err := reader.FindCardPurchaseReplay(ctx, query)
	if err == nil {
		return lookup, lookup.Found, nil
	}
	switch {
	case errors.Is(err, ErrCardPurchaseIdempotencyConflict):
		return CardPurchaseReplayLookup{}, false, ErrCardPurchaseIdempotencyConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return CardPurchaseReplayLookup{}, false, err
	default:
		return CardPurchaseReplayLookup{}, false, newSafeOperationError(ErrCardPurchaseReplayLookup, err)
	}
}

func fingerprintCardPurchase(input normalizedCardPurchaseInput) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, CardPurchaseOperationCreate)
	writeFingerprintString(digest, cardPurchaseFingerprintVersion)
	writeFingerprintString(digest, input.description)
	writeFingerprintInt64(digest, input.amount.MinorUnits())
	writeFingerprintString(digest, string(input.amount.Currency()))
	writeFingerprintString(digest, input.occurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	writeFingerprintString(digest, FinancialTimezone)
	writeFingerprintString(digest, string(input.origin))
	writeOptionalFingerprintString(digest, input.categoryID != nil, categoryString(input.categoryID))
	writeFingerprintString(digest, input.creditCardID)
	writeFingerprintString(digest, string(input.mode))
	if input.mode == CardPurchaseModeInstallment {
		writeFingerprintInt64(digest, int64(*input.installmentCount))
	}
	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}

func categoryString(value *domain.CategoryID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func clonePlan(value *domain.InstallmentPlan) *domain.InstallmentPlan {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func validateStoredCardPurchase(result CardPurchaseCommandResult, input normalizedCardPurchaseInput, expected *cardPurchaseSnapshotExpectation) error {
	cardID, linked := result.Expense.CreditCardID()
	dueOn, hasDue := result.Expense.StatementDueOn()
	if result.Expense.ID() == "" || result.Expense.UserID() != input.userID || result.Expense.PaymentMethod() != domain.PaymentMethodCredit || !linked || cardID != input.creditCardID || !hasDue || dueOn.String() == "" || result.Expense.Type() != domain.TransactionTypeExpense || result.Expense.Status() != domain.ExpenseStatusRecorded || result.Expense.Version() == 0 || result.Expense.Description() != input.description || !result.Expense.Amount().Equal(input.amount) || result.Expense.Amount().Currency() != input.amount.Currency() || result.Expense.OccurredAt() != input.occurredAt || result.Expense.FinancialTimezone() != FinancialTimezone || result.Expense.Origin() != input.origin || result.Expense.CreatedAt().IsZero() || result.Expense.UpdatedAt() != result.Expense.CreatedAt() {
		return ErrCardPurchaseDependencyResult
	}
	expectedCategory, expectedCategoryPresent := input.categoryID, input.categoryID != nil
	actualCategory, actualCategoryPresent := result.Expense.CategoryID()
	if actualCategoryPresent != expectedCategoryPresent || actualCategoryPresent && actualCategory != *expectedCategory {
		return ErrCardPurchaseDependencyResult
	}
	if expected != nil && (!dueOn.Equal(expected.cycle.StatementDueOn()) || result.Expense.CreatedAt() != expected.createdAt) {
		return ErrCardPurchaseDependencyResult
	}
	if input.mode == CardPurchaseModeOneTime {
		if result.InstallmentPlan != nil {
			return ErrCardPurchaseDependencyResult
		}
		return nil
	}
	if result.InstallmentPlan == nil || result.InstallmentPlan.Status() != domain.InstallmentPlanStatusActive || result.InstallmentPlan.OwnerID() != input.userID || result.InstallmentPlan.CreditCardID() != input.creditCardID || result.InstallmentPlan.ExpenseID() != result.Expense.ID() || result.InstallmentPlan.TotalAmount().MinorUnits() != input.amount.MinorUnits() || result.InstallmentPlan.TotalAmount().Currency() != input.amount.Currency() || result.InstallmentPlan.InstallmentCount() != *input.installmentCount || !result.InstallmentPlan.FirstDueDate().Equal(dueOn) || result.InstallmentPlan.CreatedAt() != result.Expense.CreatedAt() {
		return ErrCardPurchaseDependencyResult
	}
	if expected != nil && result.InstallmentPlan.DueDayAnchor() != expected.dueDayAnchor {
		return ErrCardPurchaseDependencyResult
	}
	if _, ok := result.InstallmentPlan.CancelledOn(); ok {
		return ErrCardPurchaseDependencyResult
	}
	if !result.InstallmentPlan.FirstDueDate().Equal(dueOn) {
		return ErrCardPurchaseDependencyResult
	}
	planParams := domain.InstallmentPlanRehydrationParams{
		ID: result.InstallmentPlan.ID(), OwnerID: result.InstallmentPlan.OwnerID(), CreditCardID: result.InstallmentPlan.CreditCardID(), ExpenseID: result.InstallmentPlan.ExpenseID(),
		TotalAmount: result.InstallmentPlan.TotalAmount(), InstallmentCount: result.InstallmentPlan.InstallmentCount(), FirstDueDate: result.InstallmentPlan.FirstDueDate(),
		DueDayAnchor: result.InstallmentPlan.DueDayAnchor(), Status: result.InstallmentPlan.Status(), CreatedAt: result.InstallmentPlan.CreatedAt(),
	}
	if _, err := domain.RehydrateInstallmentPlan(planParams); err != nil {
		return ErrCardPurchaseDependencyResult
	}
	schedule, err := result.InstallmentPlan.Schedule()
	if err != nil || len(schedule) != *input.installmentCount {
		return ErrCardPurchaseDependencyResult
	}
	var sum int64
	for _, installment := range schedule {
		if installment.Amount().MinorUnits() <= 0 || installment.TotalCount() != *input.installmentCount {
			return ErrCardPurchaseDependencyResult
		}
		sum += installment.Amount().MinorUnits()
	}
	if sum != input.amount.MinorUnits() {
		return ErrCardPurchaseDependencyResult
	}
	return nil
}

func cardPurchaseResultsEqual(left, right CardPurchaseCommandResult) bool {
	if left.Expense.ID() != right.Expense.ID() || left.Expense.CreatedAt() != right.Expense.CreatedAt() {
		return false
	}
	if (left.InstallmentPlan == nil) != (right.InstallmentPlan == nil) {
		return false
	}
	if left.InstallmentPlan == nil {
		return true
	}
	return left.InstallmentPlan.ID() == right.InstallmentPlan.ID() && left.InstallmentPlan.CreatedAt() == right.InstallmentPlan.CreatedAt()
}
