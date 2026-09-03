package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

const cardPurchaseTestUser = "user-card-purchase"

type cardPurchaseCardReader struct {
	card  domain.CreditCard
	found bool
	force bool
	calls int
}

func (reader *cardPurchaseCardReader) FindCreditCard(_ context.Context, owner, id string) (application.CreditCardLookup, error) {
	reader.calls++
	if !reader.found || (!reader.force && (owner != reader.card.UserID() || id != reader.card.ID())) {
		return application.CreditCardLookup{}, nil
	}
	return application.CreditCardLookup{CreditCard: reader.card, Found: true}, nil
}

type cardPurchaseCategoryCatalog struct {
	definition application.CategoryDefinition
	calls      int
}

func (catalog *cardPurchaseCategoryCatalog) FindCategory(_ context.Context, id domain.CategoryID) (application.CategoryDefinition, error) {
	catalog.calls++
	if id != catalog.definition.ID() {
		return application.CategoryDefinition{}, application.ErrCategoryNotFound
	}
	return catalog.definition, nil
}

func (catalog *cardPurchaseCategoryCatalog) ListCategories(context.Context, domain.TransactionType) ([]application.CategoryDefinition, error) {
	return []application.CategoryDefinition{catalog.definition}, nil
}

type cardPurchaseReplay struct {
	lookup application.CardPurchaseReplayLookup
	calls  int
	query  application.CardPurchaseReplayQuery
	err    error
}

func (replay *cardPurchaseReplay) FindCardPurchaseReplay(_ context.Context, query application.CardPurchaseReplayQuery) (application.CardPurchaseReplayLookup, error) {
	replay.calls++
	replay.query = query
	return replay.lookup, replay.err
}

type cardPurchaseStore struct {
	result  application.CardPurchaseCommandResult
	calls   int
	command application.CardPurchaseCommand
	err     error
}

func (store *cardPurchaseStore) RecordCardPurchase(_ context.Context, command application.CardPurchaseCommand) (application.CardPurchaseCommandResult, error) {
	store.calls++
	store.command = command
	if store.err != nil {
		return application.CardPurchaseCommandResult{}, store.err
	}
	if store.result.Expense.ID() == "" {
		return application.CardPurchaseCommandResult{Expense: command.Expense, InstallmentPlan: command.InstallmentPlan}, nil
	}
	return store.result, nil
}

type cardPurchaseIDGenerator struct {
	id    string
	calls int
	err   error
}

func (generator *cardPurchaseIDGenerator) NewExpenseID() (string, error) {
	generator.calls++
	return generator.id, generator.err
}

type cardPurchasePlanIDGenerator struct {
	id    string
	calls int
	err   error
}

func (generator *cardPurchasePlanIDGenerator) NewInstallmentPlanID() (string, error) {
	generator.calls++
	return generator.id, generator.err
}

type cardPurchaseClock struct {
	now   time.Time
	calls int
}

func (clock *cardPurchaseClock) Now() time.Time {
	clock.calls++
	return clock.now
}

func cardPurchaseTestCard(t *testing.T, status domain.CreditCardStatus) domain.CreditCard {
	t.Helper()
	name, _ := domain.NewCreditCardName("Platinum")
	lastFour, _ := domain.NewCreditCardLastFour("4821")
	params := domain.CreditCardRehydrationParams{
		ID:      "card_0123456789abcdef0123456789abcdef",
		Details: domain.CreditCardDetails{UserID: cardPurchaseTestUser, Name: name, LastFour: &lastFour, ClosingDayAnchor: mustCardAnchor(t, 25), DueDayAnchor: mustCardAnchor(t, 10)},
		Status:  status, CreatedAt: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
	}
	if status == domain.CreditCardStatusArchived {
		archivedAt := time.Date(2026, time.February, 1, 12, 0, 0, 0, time.UTC)
		params.ArchivedAt = &archivedAt
	}
	card, err := domain.RehydrateCreditCard(params)
	if err != nil {
		t.Fatalf("card fixture: %v", err)
	}
	return card
}

func mustCardAnchor(t *testing.T, day int) domain.DayOfMonthAnchor {
	t.Helper()
	anchor, err := domain.NewDayOfMonthAnchor(day)
	if err != nil {
		t.Fatal(err)
	}
	return anchor
}

func validCardPurchaseInput() application.CardPurchaseInput {
	return application.CardPurchaseInput{
		UserID: cardPurchaseTestUser, Description: "Assinatura", AmountMinor: 10_000, Currency: domain.CurrencyBRL,
		OccurredAt: time.Date(2026, time.August, 20, 15, 0, 0, 0, time.UTC), CreditCardID: "card_0123456789abcdef0123456789abcdef", Origin: domain.OriginIOS,
	}
}

func TestPreviewCardPurchaseSupportsOneTimeAndInstallmentsWithoutWrites(t *testing.T) {
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	preview, err := application.NewPreviewCardPurchase(reader)
	if err != nil {
		t.Fatal(err)
	}
	input := validCardPurchaseInput()
	result, err := preview.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("one-time preview: %v", err)
	}
	if result.Preview.Mode != application.CardPurchaseModeOneTime || result.Preview.InstallmentCount != nil || len(result.Preview.Installments) != 0 {
		t.Fatal("nil count did not canonicalize to one-time")
	}
	if result.Preview.Expense.PaymentMethod != domain.PaymentMethodCredit || result.Preview.Expense.CreditCardID == nil || result.Preview.Expense.StatementDueOn == nil {
		t.Fatal("preview did not link the CREDIT expense to its card cycle")
	}
	count := 3
	input.InstallmentCount = &count
	installmentResult, err := preview.Execute(context.Background(), input)
	if err != nil || installmentResult.Preview.Mode != application.CardPurchaseModeInstallment || len(installmentResult.Preview.Installments) != 3 {
		t.Fatalf("installment preview = %v/%d", err, len(installmentResult.Preview.Installments))
	}
	maxCount := domain.MaxInstallmentCount
	input.InstallmentCount = &maxCount
	maxResult, err := preview.Execute(context.Background(), input)
	if err != nil || len(maxResult.Preview.Installments) != domain.MaxInstallmentCount {
		t.Fatalf("maximum installment preview = %v/%d", err, len(maxResult.Preview.Installments))
	}
	if reader.calls != 3 {
		t.Fatalf("card calls = %d, want 3", reader.calls)
	}
}

func TestCardPurchaseInstallmentCountOneAndOutOfRangeAreRejected(t *testing.T) {
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	preview, _ := application.NewPreviewCardPurchase(reader)
	for _, count := range []int{1, 0, -1, 121} {
		input := validCardPurchaseInput()
		input.InstallmentCount = &count
		if _, err := preview.Execute(context.Background(), input); !errors.Is(err, domain.ErrInvalidInstallmentCount) {
			t.Fatalf("count %d error = %v", count, err)
		}
	}
	if reader.calls != 0 {
		t.Fatalf("invalid counts queried card %d times", reader.calls)
	}
}

func TestRecordCardPurchaseRejectsInstallmentCountOneBeforeMutableDependencies(t *testing.T) {
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	replay := &cardPurchaseReplay{}
	store := &cardPurchaseStore{}
	expenseIDs := &cardPurchaseIDGenerator{id: "unused-expense"}
	planIDs := &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}
	clock := &cardPurchaseClock{now: fixedPurchaseCreatedAt}
	useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, expenseIDs, planIDs, clock)
	if err != nil {
		t.Fatal(err)
	}
	count := 1
	input := validCardPurchaseInput()
	input.InstallmentCount = &count
	if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "count-one"}); !errors.Is(err, domain.ErrInvalidInstallmentCount) {
		t.Fatalf("count one error = %v", err)
	}
	if replay.calls != 0 || reader.calls != 0 || expenseIDs.calls != 0 || planIDs.calls != 0 || clock.calls != 0 || store.calls != 0 {
		t.Fatalf("count one consumed dependencies: replay=%d card=%d ids=%d/%d clock=%d store=%d", replay.calls, reader.calls, expenseIDs.calls, planIDs.calls, clock.calls, store.calls)
	}
}

func TestPreviewCardPurchaseRequiresActiveOwnerScopedCardAndExpenseCategory(t *testing.T) {
	archivedReader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusArchived), found: true}
	archivedPreview, _ := application.NewPreviewCardPurchase(archivedReader)
	if _, err := archivedPreview.Execute(context.Background(), validCardPurchaseInput()); !errors.Is(err, application.ErrCardPurchaseCreditCardArchived) {
		t.Fatalf("archived card error = %v", err)
	}
	missingReader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: false}
	missingPreview, _ := application.NewPreviewCardPurchase(missingReader)
	if _, err := missingPreview.Execute(context.Background(), validCardPurchaseInput()); !errors.Is(err, application.ErrCardPurchaseCreditCardNotFound) {
		t.Fatalf("missing card error = %v", err)
	}
	categoryID := domain.CategoryID("cat_food")
	definition, err := application.NewCategoryDefinition(categoryID, domain.TransactionTypeIncome, "Income", 1)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &cardPurchaseCategoryCatalog{definition: definition}
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	preview, _ := application.NewPreviewCardPurchaseWithCategoryCatalog(reader, catalog)
	input := validCardPurchaseInput()
	input.CategoryID = &categoryID
	if _, err := preview.Execute(context.Background(), input); !errors.Is(err, application.ErrCardPurchaseCategoryNotApplicable) {
		t.Fatalf("income category error = %v", err)
	}
}

func TestCardPurchaseRejectsDependencySnapshotFromAnotherOwner(t *testing.T) {
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true, force: true}
	preview, err := application.NewPreviewCardPurchase(reader)
	if err != nil {
		t.Fatal(err)
	}
	input := validCardPurchaseInput()
	input.UserID = "other-owner"
	if _, err := preview.Execute(context.Background(), input); !errors.Is(err, application.ErrCardPurchaseDependencyResult) {
		t.Fatalf("owner mismatch error = %v", err)
	}
}

func TestCardPurchaseFingerprintChangesWithSemanticInput(t *testing.T) {
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	replay := &cardPurchaseReplay{}
	store := &cardPurchaseStore{}
	ids := &cardPurchaseIDGenerator{id: "expense-fingerprint"}
	planIDs := &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}
	clock := &cardPurchaseClock{now: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)}
	useCase, _ := application.NewRecordCardPurchase(store, replay, reader, nil, ids, planIDs, clock)
	input := validCardPurchaseInput()
	if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "fingerprint-a"}); err != nil {
		t.Fatal(err)
	}
	first := replay.query.Fingerprint
	input.AmountMinor++
	if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "fingerprint-b"}); err != nil {
		t.Fatal(err)
	}
	if first == replay.query.Fingerprint {
		t.Fatal("semantic amount change did not change fingerprint")
	}
}

func TestRecordCardPurchaseCreatesOneTimeOrInstallmentAndUsesSingleClock(t *testing.T) {
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	replay := &cardPurchaseReplay{}
	store := &cardPurchaseStore{}
	expenseIDs := &cardPurchaseIDGenerator{id: "expense-card-purchase"}
	planIDs := &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}
	clock := &cardPurchaseClock{now: time.Date(2026, time.August, 20, 18, 0, 0, 999_999_999, time.FixedZone("fixture", -3*60*60))}
	useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, expenseIDs, planIDs, clock)
	if err != nil {
		t.Fatal(err)
	}
	first, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: validCardPurchaseInput(), IdempotencyKey: "card-purchase-key"})
	if err != nil {
		t.Fatalf("one-time record: %v", err)
	}
	if first.Replayed || first.InstallmentPlan != nil || first.Expense.PaymentMethod() != domain.PaymentMethodCredit {
		t.Fatal("one-time record has wrong result")
	}
	if store.calls != 1 || expenseIDs.calls != 1 || planIDs.calls != 0 || clock.calls != 1 {
		t.Fatalf("calls store/expense/plan/clock = %d/%d/%d/%d", store.calls, expenseIDs.calls, planIDs.calls, clock.calls)
	}
	count := 3
	input := validCardPurchaseInput()
	input.InstallmentCount = &count
	second, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "card-purchase-installment"})
	if err != nil {
		t.Fatalf("installment record: %v", err)
	}
	if second.InstallmentPlan == nil || second.InstallmentPlan.InstallmentCount() != count || store.command.InstallmentPlan == nil {
		t.Fatal("installment plan was not atomically included")
	}
	if planIDs.calls != 1 || clock.calls != 2 {
		t.Fatalf("installment calls plan/clock = %d/%d", planIDs.calls, clock.calls)
	}
}

func TestRecordCardPurchaseReplayPrecedesMutableDependenciesAndIDs(t *testing.T) {
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusArchived), found: true}
	input := validCardPurchaseInput()
	amount, _ := domain.NewMoney(input.AmountMinor, input.Currency)
	cardID := input.CreditCardID
	due := mustAppDate(t, 2026, time.September, 10)
	expense, err := domain.NewExpense(domain.ExpenseParams{ID: "expense-card-replay", Details: domain.ExpenseDetails{UserID: cardPurchaseTestUser, Description: input.Description, Amount: amount, PaymentMethod: domain.PaymentMethodCredit, CreditCardID: &cardID, StatementDueOn: &due, OccurredAt: input.OccurredAt, FinancialTimezone: application.FinancialTimezone, Origin: input.Origin}, CreatedAt: time.Date(2026, time.August, 20, 18, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	replay := &cardPurchaseReplay{lookup: application.CardPurchaseReplayLookup{Found: true, Expense: expense}}
	store := &cardPurchaseStore{}
	expenseIDs := &cardPurchaseIDGenerator{id: "unused-expense"}
	planIDs := &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}
	clock := &cardPurchaseClock{now: time.Date(2026, time.September, 1, 12, 0, 0, 123_000_000, time.UTC)}
	useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, expenseIDs, planIDs, clock)
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "historical-replay"})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !result.Replayed || result.Expense.ID() != expense.ID() || reader.calls != 0 || store.calls != 0 || expenseIDs.calls != 0 || planIDs.calls != 0 || clock.calls != 0 {
		t.Fatalf("replay did not short-circuit dependencies: replay=%v card=%d store=%d ids=%d/%d clock=%d", result.Replayed, reader.calls, store.calls, expenseIDs.calls, planIDs.calls, clock.calls)
	}
}

func TestRecordCardPurchaseRejectsCriticalGenerationAndClockFailuresBeforeStore(t *testing.T) {
	card := cardPurchaseTestCard(t, domain.CreditCardStatusActive)
	input := validCardPurchaseInput()
	count := 3
	input.InstallmentCount = &count

	t.Run("expense id", func(t *testing.T) {
		reader := &cardPurchaseCardReader{card: card, found: true}
		replay := &cardPurchaseReplay{}
		store := &cardPurchaseStore{}
		expenseIDs := &cardPurchaseIDGenerator{err: errors.New("entropy")}
		planIDs := &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}
		clock := &cardPurchaseClock{now: fixedPurchaseCreatedAt}
		useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, expenseIDs, planIDs, clock)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "expense-id-failure"}); !errors.Is(err, application.ErrCardPurchaseExpenseIDGeneration) {
			t.Fatalf("error = %v", err)
		}
		if expenseIDs.calls != 1 || planIDs.calls != 0 || clock.calls != 0 || store.calls != 0 {
			t.Fatalf("calls expense/plan/clock/store = %d/%d/%d/%d", expenseIDs.calls, planIDs.calls, clock.calls, store.calls)
		}
	})

	t.Run("installment plan id", func(t *testing.T) {
		reader := &cardPurchaseCardReader{card: card, found: true}
		replay := &cardPurchaseReplay{}
		store := &cardPurchaseStore{}
		expenseIDs := &cardPurchaseIDGenerator{id: "expense-plan-id-failure"}
		planIDs := &cardPurchasePlanIDGenerator{err: errors.New("entropy")}
		clock := &cardPurchaseClock{now: fixedPurchaseCreatedAt}
		useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, expenseIDs, planIDs, clock)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "plan-id-failure"}); !errors.Is(err, application.ErrCardPurchasePlanIDGeneration) {
			t.Fatalf("error = %v", err)
		}
		if expenseIDs.calls != 1 || planIDs.calls != 1 || clock.calls != 0 || store.calls != 0 {
			t.Fatalf("calls expense/plan/clock/store = %d/%d/%d/%d", expenseIDs.calls, planIDs.calls, clock.calls, store.calls)
		}
	})

	t.Run("clock zero", func(t *testing.T) {
		reader := &cardPurchaseCardReader{card: card, found: true}
		replay := &cardPurchaseReplay{}
		store := &cardPurchaseStore{}
		expenseIDs := &cardPurchaseIDGenerator{id: "expense-clock-failure"}
		planIDs := &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}
		clock := &cardPurchaseClock{}
		useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, expenseIDs, planIDs, clock)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "clock-failure"}); !errors.Is(err, domain.ErrInvalidCreatedAt) {
			t.Fatalf("error = %v", err)
		}
		if expenseIDs.calls != 1 || planIDs.calls != 1 || clock.calls != 1 || store.calls != 0 {
			t.Fatalf("calls expense/plan/clock/store = %d/%d/%d/%d", expenseIDs.calls, planIDs.calls, clock.calls, store.calls)
		}
	})
}

func TestRecordCardPurchaseWrapsReplayAndPersistenceFailuresSafely(t *testing.T) {
	input := validCardPurchaseInput()
	t.Run("replay lookup", func(t *testing.T) {
		reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
		replay := &cardPurchaseReplay{err: errors.New("database secret")}
		store := &cardPurchaseStore{}
		useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, &cardPurchaseIDGenerator{id: "unused"}, &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}, &cardPurchaseClock{now: fixedPurchaseCreatedAt})
		if err != nil {
			t.Fatal(err)
		}
		result, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "replay-error"})
		if !errors.Is(err, application.ErrCardPurchaseReplayLookup) || result.Expense.ID() != "" || store.calls != 0 || reader.calls != 0 || errors.Is(err, context.Canceled) {
			t.Fatalf("result/error/dependencies = %#v/%v/%d/%d", result, err, store.calls, reader.calls)
		}
		if err != nil && strings.Contains(err.Error(), "database secret") {
			t.Fatal("replay dependency detail leaked")
		}
	})

	t.Run("persistence", func(t *testing.T) {
		reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
		replay := &cardPurchaseReplay{}
		store := &cardPurchaseStore{err: errors.New("database secret")}
		expenseIDs := &cardPurchaseIDGenerator{id: "expense-persistence"}
		useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, expenseIDs, &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}, &cardPurchaseClock{now: fixedPurchaseCreatedAt})
		if err != nil {
			t.Fatal(err)
		}
		_, err = useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "persistence-error"})
		if !errors.Is(err, application.ErrCardPurchasePersistence) || store.calls != 1 || expenseIDs.calls != 1 {
			t.Fatalf("error/calls = %v/%d/%d", err, store.calls, expenseIDs.calls)
		}
		if strings.Contains(err.Error(), "database secret") {
			t.Fatal("persistence detail leaked")
		}
	})
}

func TestRecordCardPurchaseRejectsCorruptReplayAndStoreSnapshots(t *testing.T) {
	input := validCardPurchaseInput()
	baseExpense, basePlan := recordCardPurchaseFixture(t, input, true)

	t.Run("replay expense", func(t *testing.T) {
		bad := copyCardPurchaseExpense(t, baseExpense, func(details *domain.ExpenseDetails) { details.Description = "Corrompida" })
		testCardPurchaseReplaySnapshotError(t, input, application.CardPurchaseReplayLookup{Found: true, Expense: bad, InstallmentPlan: basePlan})
	})
	t.Run("replay plan", func(t *testing.T) {
		bad, _ := basePlan.Cancel(mustAppDate(t, 2026, time.September, 15))
		testCardPurchaseReplaySnapshotError(t, func() application.CardPurchaseInput {
			value := input
			count := 3
			value.InstallmentCount = &count
			return value
		}(), application.CardPurchaseReplayLookup{Found: true, Expense: baseExpense, InstallmentPlan: &bad})
	})

	for _, tc := range []struct {
		name   string
		mutate func(*domain.ExpenseDetails)
	}{
		{name: "owner", mutate: func(details *domain.ExpenseDetails) { details.UserID = "other-owner" }},
		{name: "occurred at", mutate: func(details *domain.ExpenseDetails) { details.OccurredAt = details.OccurredAt.Add(time.Minute) }},
		{name: "timezone", mutate: func(details *domain.ExpenseDetails) { details.FinancialTimezone = "UTC" }},
		{name: "origin", mutate: func(details *domain.ExpenseDetails) { details.Origin = domain.OriginWhatsApp }},
		{name: "statement due", mutate: func(details *domain.ExpenseDetails) {
			due := mustAppDate(t, 2026, time.October, 10)
			details.StatementDueOn = &due
		}},
		{name: "card", mutate: func(details *domain.ExpenseDetails) {
			cardID := "card_fedcba9876543210fedcba9876543210"
			details.CreditCardID = &cardID
		}},
	} {
		t.Run("expense "+tc.name, func(t *testing.T) {
			bad := copyCardPurchaseExpense(t, baseExpense, tc.mutate)
			testCardPurchaseStoreSnapshotError(t, input, bad, basePlan)
		})
	}

	categoryID := domain.CategoryID("cat_food")
	definition, err := application.NewCategoryDefinition(categoryID, domain.TransactionTypeExpense, "Food", 1)
	if err != nil {
		t.Fatal(err)
	}
	categoryInput := input
	categoryInput.CategoryID = &categoryID
	categoryExpense, categoryPlan := recordCardPurchaseFixtureWithCatalog(t, categoryInput, true, definition)
	t.Run("category", func(t *testing.T) {
		bad := copyCardPurchaseExpense(t, categoryExpense, func(details *domain.ExpenseDetails) { details.CategoryID = nil })
		testCardPurchaseStoreSnapshotErrorWithCatalog(t, categoryInput, bad, categoryPlan, definition)
	})

	t.Run("one-time plan is forbidden", func(t *testing.T) {
		oneTimeExpense, _ := recordCardPurchaseFixture(t, input, false)
		_, plan := recordCardPurchaseFixture(t, input, true)
		testCardPurchaseStoreSnapshotError(t, input, oneTimeExpense, plan)
	})

	t.Run("installment plan is required", func(t *testing.T) {
		installmentInput := input
		count := 3
		installmentInput.InstallmentCount = &count
		testCardPurchaseStoreSnapshotError(t, installmentInput, baseExpense, nil)
	})

	for _, tc := range []struct {
		name   string
		mutate func(domain.InstallmentPlan) domain.InstallmentPlan
	}{
		{name: "cancelled", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			cancelled, _ := plan.Cancel(mustAppDate(t, 2026, time.September, 15))
			return cancelled
		}},
		{name: "owner", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			params := rehydrationParams(plan)
			params.OwnerID = "other-owner"
			value, _ := domain.RehydrateInstallmentPlan(params)
			return value
		}},
		{name: "card", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			params := rehydrationParams(plan)
			params.CreditCardID = "card_fedcba9876543210fedcba9876543210"
			value, _ := domain.RehydrateInstallmentPlan(params)
			return value
		}},
		{name: "expense", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			params := rehydrationParams(plan)
			params.ExpenseID = "expense-other"
			value, _ := domain.RehydrateInstallmentPlan(params)
			return value
		}},
		{name: "total", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			params := rehydrationParams(plan)
			params.TotalAmount, _ = domain.NewMoney(10_001, domain.CurrencyBRL)
			value, _ := domain.RehydrateInstallmentPlan(params)
			return value
		}},
		{name: "count", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			params := rehydrationParams(plan)
			params.InstallmentCount = 4
			value, _ := domain.RehydrateInstallmentPlan(params)
			return value
		}},
		{name: "first due", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			params := rehydrationParams(plan)
			params.FirstDueDate = mustAppDate(t, 2026, time.October, 10)
			value, _ := domain.RehydrateInstallmentPlan(params)
			return value
		}},
		{name: "created at", mutate: func(plan domain.InstallmentPlan) domain.InstallmentPlan {
			params := rehydrationParams(plan)
			params.CreatedAt = plan.CreatedAt().Add(time.Hour)
			value, _ := domain.RehydrateInstallmentPlan(params)
			return value
		}},
	} {
		t.Run("plan "+tc.name, func(t *testing.T) {
			badPlan := tc.mutate(*basePlan)
			testCardPurchaseStoreSnapshotError(t, input, baseExpense, &badPlan)
		})
	}
}

var fixedPurchaseCreatedAt = time.Date(2026, time.September, 1, 12, 0, 0, 123_000_000, time.UTC)

func recordCardPurchaseFixture(t *testing.T, input application.CardPurchaseInput, installment bool) (domain.Expense, *domain.InstallmentPlan) {
	t.Helper()
	return recordCardPurchaseFixtureWithCatalog(t, input, installment)
}

func recordCardPurchaseFixtureWithCatalog(t *testing.T, input application.CardPurchaseInput, installment bool, definitions ...application.CategoryDefinition) (domain.Expense, *domain.InstallmentPlan) {
	t.Helper()
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	replay := &cardPurchaseReplay{}
	store := &cardPurchaseStore{}
	expenseIDs := &cardPurchaseIDGenerator{id: "expense-fixture"}
	planIDs := &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}
	createdAt := fixedPurchaseCreatedAt
	clock := &cardPurchaseClock{now: createdAt}
	var catalog application.CategoryCatalog
	if len(definitions) > 0 {
		catalog = &cardPurchaseCategoryCatalog{definition: definitions[0]}
	}
	useCase, err := application.NewRecordCardPurchase(store, replay, reader, catalog, expenseIDs, planIDs, clock)
	if err != nil {
		t.Fatal(err)
	}
	if installment {
		count := 3
		input.InstallmentCount = &count
	} else {
		input.InstallmentCount = nil
	}
	result, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "fixture-" + input.Description})
	if err != nil {
		t.Fatal(err)
	}
	return result.Expense, result.InstallmentPlan
}

func copyCardPurchaseExpense(t *testing.T, base domain.Expense, mutate func(*domain.ExpenseDetails)) domain.Expense {
	t.Helper()
	amount := base.Amount()
	cardID, hasCard := base.CreditCardID()
	due, hasDue := base.StatementDueOn()
	var cardPointer *string
	if hasCard {
		cardPointer = &cardID
	}
	var duePointer *domain.CivilDate
	if hasDue {
		duePointer = &due
	}
	details := domain.ExpenseDetails{UserID: base.UserID(), Description: base.Description(), Amount: amount, PaymentMethod: base.PaymentMethod(), CategoryID: categoryPointer(base), CreditCardID: cardPointer, StatementDueOn: duePointer, OccurredAt: base.OccurredAt(), FinancialTimezone: base.FinancialTimezone(), Origin: base.Origin()}
	mutate(&details)
	value, err := domain.NewExpense(domain.ExpenseParams{ID: base.ID(), Details: details, CreatedAt: base.CreatedAt()})
	if err != nil {
		t.Fatalf("corrupt expense fixture: %v", err)
	}
	return value
}

func categoryPointer(expense domain.Expense) *domain.CategoryID {
	category, ok := expense.CategoryID()
	if !ok {
		return nil
	}
	return &category
}

func rehydrationParams(plan domain.InstallmentPlan) domain.InstallmentPlanRehydrationParams {
	return domain.InstallmentPlanRehydrationParams{ID: plan.ID(), OwnerID: plan.OwnerID(), CreditCardID: plan.CreditCardID(), ExpenseID: plan.ExpenseID(), TotalAmount: plan.TotalAmount(), InstallmentCount: plan.InstallmentCount(), FirstDueDate: plan.FirstDueDate(), DueDayAnchor: plan.DueDayAnchor(), Status: plan.Status(), CreatedAt: plan.CreatedAt()}
}

func testCardPurchaseReplaySnapshotError(t *testing.T, input application.CardPurchaseInput, lookup application.CardPurchaseReplayLookup) {
	t.Helper()
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusArchived), found: true}
	replay := &cardPurchaseReplay{lookup: lookup}
	store := &cardPurchaseStore{}
	useCase, err := application.NewRecordCardPurchase(store, replay, reader, nil, &cardPurchaseIDGenerator{id: "unused"}, &cardPurchasePlanIDGenerator{id: "ipl_0123456789abcdef0123456789abcdef"}, &cardPurchaseClock{now: time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "corrupt-replay"}); !errors.Is(err, application.ErrCardPurchaseDependencyResult) {
		t.Fatalf("error = %v", err)
	}
	if reader.calls != 0 || store.calls != 0 {
		t.Fatalf("corrupt replay reached mutable dependencies: card=%d store=%d", reader.calls, store.calls)
	}
}

func testCardPurchaseStoreSnapshotError(t *testing.T, input application.CardPurchaseInput, expense domain.Expense, plan *domain.InstallmentPlan) {
	t.Helper()
	testCardPurchaseStoreSnapshotErrorWithCatalog(t, input, expense, plan)
}

func testCardPurchaseStoreSnapshotErrorWithCatalog(t *testing.T, input application.CardPurchaseInput, expense domain.Expense, plan *domain.InstallmentPlan, definitions ...application.CategoryDefinition) {
	t.Helper()
	reader := &cardPurchaseCardReader{card: cardPurchaseTestCard(t, domain.CreditCardStatusActive), found: true}
	store := &cardPurchaseStore{result: application.CardPurchaseCommandResult{Expense: expense, InstallmentPlan: plan}}
	createdAt := fixedPurchaseCreatedAt
	var catalog application.CategoryCatalog
	if len(definitions) > 0 {
		catalog = &cardPurchaseCategoryCatalog{definition: definitions[0]}
	}
	useCase, err := application.NewRecordCardPurchase(store, &cardPurchaseReplay{}, reader, catalog, &cardPurchaseIDGenerator{id: "new-expense"}, &cardPurchasePlanIDGenerator{id: "ipl_fedcba9876543210fedcba9876543210"}, &cardPurchaseClock{now: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := useCase.Execute(context.Background(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: "corrupt-store-" + expense.ID()}); !errors.Is(err, application.ErrCardPurchaseDependencyResult) {
		t.Fatalf("error = %v", err)
	}
}

func mustAppDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
