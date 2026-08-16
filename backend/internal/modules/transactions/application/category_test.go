package application_test

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestApprovedCategoryDefinitionsAreValidAndKeepIdentitySeparateFromDisplay(t *testing.T) {
	for _, specification := range approvedCategorySpecifications() {
		specification := specification
		t.Run(specification.id.String(), func(t *testing.T) {
			definition, err := application.NewCategoryDefinition(
				specification.id,
				specification.transactionType,
				specification.displayName,
				specification.sortOrder,
			)
			if err != nil {
				t.Fatalf("NewCategoryDefinition() error = %v", err)
			}
			if definition.ID() != specification.id ||
				definition.TransactionType() != specification.transactionType ||
				definition.DisplayName() != specification.displayName ||
				definition.SortOrder() != specification.sortOrder {
				t.Fatal("CategoryDefinition did not preserve trusted catalog fields")
			}
		})
	}
}

func TestCategoryDefinitionRejectsInvalidCatalogData(t *testing.T) {
	tests := []struct {
		name            string
		id              domain.CategoryID
		transactionType domain.TransactionType
		displayName     string
		sortOrder       uint16
	}{
		{name: "invalid id", id: domain.CategoryID("EXPENSE.FOOD"), transactionType: domain.TransactionTypeExpense, displayName: "Alimentação", sortOrder: 1},
		{name: "invalid type", id: domain.CategoryIDExpenseFood, transactionType: domain.TransactionType("TRANSFER"), displayName: "Alimentação", sortOrder: 1},
		{name: "empty display", id: domain.CategoryIDExpenseFood, transactionType: domain.TransactionTypeExpense, sortOrder: 1},
		{name: "display whitespace", id: domain.CategoryIDExpenseFood, transactionType: domain.TransactionTypeExpense, displayName: " Alimentação", sortOrder: 1},
		{name: "display control", id: domain.CategoryIDExpenseFood, transactionType: domain.TransactionTypeExpense, displayName: "Alimentação\nprivada", sortOrder: 1},
		{name: "display too long", id: domain.CategoryIDExpenseFood, transactionType: domain.TransactionTypeExpense, displayName: strings.Repeat("a", application.MaxCategoryDisplayNameRunes+1), sortOrder: 1},
		{name: "zero order", id: domain.CategoryIDExpenseFood, transactionType: domain.TransactionTypeExpense, displayName: "Alimentação"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := application.NewCategoryDefinition(test.id, test.transactionType, test.displayName, test.sortOrder)
			if !errors.Is(err, application.ErrInvalidCategoryDefinition) {
				t.Fatalf("NewCategoryDefinition() error = %v, want ErrInvalidCategoryDefinition", err)
			}
		})
	}
}

func TestPreviewExpenseValidatesOptionalCategoryThroughCatalog(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	preview := mustPreviewExpenseWithCatalog(t, catalog)

	uncategorized, err := preview.Execute(context.Background(), validInput())
	if err != nil {
		t.Fatalf("Execute(uncategorized) error = %v", err)
	}
	if uncategorized.Details.CategoryID != nil || catalog.findCalls != 0 {
		t.Fatal("uncategorized preview consulted the catalog or invented a category")
	}

	input := validInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)
	result, err := preview.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute(categorized) error = %v", err)
	}
	if result.Details.CategoryID == nil || *result.Details.CategoryID != domain.CategoryIDExpenseFood {
		t.Fatal("categorized preview lost the validated CategoryID")
	}

	unknown := validInput()
	unknown.CategoryID = categoryIDPointer(domain.CategoryID("expense.unknown"))
	if _, err := preview.Execute(context.Background(), unknown); !errors.Is(err, application.ErrCategoryNotFound) {
		t.Fatalf("Execute(unknown) error = %v, want ErrCategoryNotFound", err)
	}

	incompatible := validInput()
	incompatible.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	if _, err := preview.Execute(context.Background(), incompatible); !errors.Is(err, application.ErrCategoryNotApplicable) {
		t.Fatalf("Execute(incompatible) error = %v, want ErrCategoryNotApplicable", err)
	}
}

func TestPreviewIncomeValidatesOptionalCategoryThroughCatalog(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	preview := mustPreviewIncomeWithCatalog(t, catalog)

	uncategorized, err := preview.Execute(context.Background(), validIncomeInput())
	if err != nil {
		t.Fatalf("Execute(uncategorized) error = %v", err)
	}
	if uncategorized.Details.CategoryID != nil || catalog.findCalls != 0 {
		t.Fatal("uncategorized preview consulted the catalog or invented a category")
	}

	input := validIncomeInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	result, err := preview.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute(categorized) error = %v", err)
	}
	if result.Details.CategoryID == nil || *result.Details.CategoryID != domain.CategoryIDIncomeSalary {
		t.Fatal("categorized preview lost the validated CategoryID")
	}

	unknown := validIncomeInput()
	unknown.CategoryID = categoryIDPointer(domain.CategoryID("income.unknown"))
	if _, err := preview.Execute(context.Background(), unknown); !errors.Is(err, application.ErrCategoryNotFound) {
		t.Fatalf("Execute(unknown) error = %v, want ErrCategoryNotFound", err)
	}

	incompatible := validIncomeInput()
	incompatible.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)
	if _, err := preview.Execute(context.Background(), incompatible); !errors.Is(err, application.ErrCategoryNotApplicable) {
		t.Fatalf("Execute(incompatible) error = %v, want ErrCategoryNotApplicable", err)
	}
}

func TestCategorizedPreviewFailsClosedWithoutCatalog(t *testing.T) {
	expense := validInput()
	expense.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)
	if _, err := (application.PreviewExpense{}).Execute(context.Background(), expense); !errors.Is(err, application.ErrCategoryCatalogUnavailable) {
		t.Fatalf("PreviewExpense.Execute() error = %v, want ErrCategoryCatalogUnavailable", err)
	}

	income := validIncomeInput()
	income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	if _, err := (application.PreviewIncome{}).Execute(context.Background(), income); !errors.Is(err, application.ErrCategoryCatalogUnavailable) {
		t.Fatalf("PreviewIncome.Execute() error = %v, want ErrCategoryCatalogUnavailable", err)
	}
}

func TestCategorizedPreviewAndRecordShareCanonicalSemantics(t *testing.T) {
	catalog := newTestCategoryCatalog(t)

	expenseInput := validInput()
	expenseInput.Description = "  Synthetic categorized expense  "
	expenseInput.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)
	expensePreview, err := mustPreviewExpenseWithCatalog(t, catalog).Execute(context.Background(), expenseInput)
	if err != nil {
		t.Fatalf("PreviewExpense.Execute() error = %v", err)
	}
	expenseStore := &recordingCommandStore{}
	expenseRecord := mustRecordExpenseWithCatalog(
		t,
		expenseStore,
		&fixedIDGenerator{id: "expense-preview-record-category"},
		&fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)},
		catalog,
	)
	if _, err := expenseRecord.Execute(context.Background(), application.RecordExpenseInput{Expense: expenseInput, IdempotencyKey: "expense-preview-record-category"}); err != nil {
		t.Fatalf("RecordExpense.Execute() error = %v", err)
	}
	assertExpensePreviewMatchesRecord(t, expensePreview.Details, expenseStore.command.Expense)

	incomeInput := validIncomeInput()
	incomeInput.Description = "  Synthetic categorized income  "
	incomeInput.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	incomePreview, err := mustPreviewIncomeWithCatalog(t, catalog).Execute(context.Background(), incomeInput)
	if err != nil {
		t.Fatalf("PreviewIncome.Execute() error = %v", err)
	}
	incomeStore := &recordingIncomeCommandStore{}
	incomeRecord := mustRecordIncomeWithCatalog(
		t,
		incomeStore,
		&fixedIncomeIDGenerator{id: "income-preview-record-category"},
		&fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)},
		catalog,
	)
	if _, err := incomeRecord.Execute(context.Background(), application.RecordIncomeInput{Income: incomeInput, IdempotencyKey: "income-preview-record-category"}); err != nil {
		t.Fatalf("RecordIncome.Execute() error = %v", err)
	}
	assertIncomePreviewMatchesRecord(t, incomePreview.Details, incomeStore.command.Income)
}

func TestRecordExpenseValidatesCategoryBeforeGeneratedValuesAndStore(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	store := &recordingCommandStore{}
	idGenerator := &fixedIDGenerator{id: "expense-category-record"}
	clock := &fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	useCase := mustRecordExpenseWithCatalog(t, store, idGenerator, clock, catalog)
	input := validInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)

	result, err := useCase.Execute(context.Background(), application.RecordExpenseInput{Expense: input, IdempotencyKey: "expense-category-key"})
	if err != nil {
		t.Fatalf("Execute(valid category) error = %v", err)
	}
	categoryID, present := result.Expense.CategoryID()
	if !present || categoryID != domain.CategoryIDExpenseFood || store.calls != 1 {
		t.Fatal("RecordExpense did not persist the catalog-validated CategoryID")
	}

	for _, test := range []struct {
		name       string
		categoryID domain.CategoryID
		want       error
	}{
		{name: "unknown", categoryID: domain.CategoryID("expense.unknown"), want: application.ErrCategoryNotFound},
		{name: "income category", categoryID: domain.CategoryIDIncomeSalary, want: application.ErrCategoryNotApplicable},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingCommandStore{}
			idGenerator := &fixedIDGenerator{id: "unused-expense-category"}
			clock := &fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
			useCase := mustRecordExpenseWithCatalog(t, store, idGenerator, clock, catalog)
			input := validInput()
			input.CategoryID = categoryIDPointer(test.categoryID)
			_, err := useCase.Execute(context.Background(), application.RecordExpenseInput{Expense: input, IdempotencyKey: "expense-category-rejected"})
			if !errors.Is(err, test.want) {
				t.Fatalf("Execute() error = %v, want %v", err, test.want)
			}
			if store.calls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
				t.Fatal("invalid category consumed write, ID, or Clock dependencies")
			}
		})
	}
}

func TestRecordIncomeValidatesCategoryBeforeGeneratedValuesAndStore(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	store := &recordingIncomeCommandStore{}
	idGenerator := &fixedIncomeIDGenerator{id: "income-category-record"}
	clock := &fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	useCase := mustRecordIncomeWithCatalog(t, store, idGenerator, clock, catalog)
	input := validIncomeInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)

	result, err := useCase.Execute(context.Background(), application.RecordIncomeInput{Income: input, IdempotencyKey: "income-category-key"})
	if err != nil {
		t.Fatalf("Execute(valid category) error = %v", err)
	}
	categoryID, present := result.Income.CategoryID()
	if !present || categoryID != domain.CategoryIDIncomeSalary || store.calls != 1 {
		t.Fatal("RecordIncome did not persist the catalog-validated CategoryID")
	}

	for _, test := range []struct {
		name       string
		categoryID domain.CategoryID
		want       error
	}{
		{name: "unknown", categoryID: domain.CategoryID("income.unknown"), want: application.ErrCategoryNotFound},
		{name: "expense category", categoryID: domain.CategoryIDExpenseFood, want: application.ErrCategoryNotApplicable},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingIncomeCommandStore{}
			idGenerator := &fixedIncomeIDGenerator{id: "unused-income-category"}
			clock := &fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
			useCase := mustRecordIncomeWithCatalog(t, store, idGenerator, clock, catalog)
			input := validIncomeInput()
			input.CategoryID = categoryIDPointer(test.categoryID)
			_, err := useCase.Execute(context.Background(), application.RecordIncomeInput{Income: input, IdempotencyKey: "income-category-rejected"})
			if !errors.Is(err, test.want) {
				t.Fatalf("Execute() error = %v, want %v", err, test.want)
			}
			if store.calls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
				t.Fatal("invalid category consumed write, ID, or Clock dependencies")
			}
		})
	}
}

func TestRecordCommandsSkipCatalogForUncategorizedInputs(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	expenseStore := &recordingCommandStore{}
	expenseUseCase := mustRecordExpenseWithCatalog(
		t,
		expenseStore,
		&fixedIDGenerator{id: "expense-uncategorized-catalog"},
		&fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)},
		catalog,
	)
	if _, err := expenseUseCase.Execute(context.Background(), application.RecordExpenseInput{Expense: validInput(), IdempotencyKey: "expense-uncategorized-catalog"}); err != nil {
		t.Fatalf("RecordExpense.Execute() error = %v", err)
	}

	incomeStore := &recordingIncomeCommandStore{}
	incomeUseCase := mustRecordIncomeWithCatalog(
		t,
		incomeStore,
		&fixedIncomeIDGenerator{id: "income-uncategorized-catalog"},
		&fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)},
		catalog,
	)
	if _, err := incomeUseCase.Execute(context.Background(), application.RecordIncomeInput{Income: validIncomeInput(), IdempotencyKey: "income-uncategorized-catalog"}); err != nil {
		t.Fatalf("RecordIncome.Execute() error = %v", err)
	}

	if catalog.findCalls != 0 || expenseStore.calls != 1 || incomeStore.calls != 1 {
		t.Fatal("uncategorized records consulted the catalog or failed to reach their stores")
	}
}

func TestCategoryCatalogFailureIsSafeAndCancellationIsPreserved(t *testing.T) {
	const marker = "SENSITIVE_CATEGORY_ADAPTER_DETAIL"
	input := validInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)

	catalog := newTestCategoryCatalog(t)
	catalog.findErr = errors.New(marker)
	preview := mustPreviewExpenseWithCatalog(t, catalog)
	_, err := preview.Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCategoryCatalogUnavailable) || strings.Contains(err.Error(), marker) {
		t.Fatalf("Execute(adapter failure) error = %v, want safe category", err)
	}

	catalog.findErr = context.Canceled
	_, err = preview.Execute(context.Background(), input)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(canceled lookup) error = %v, want context.Canceled", err)
	}
}

func TestCreateExpenseCannotBypassCategoryValidation(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	repository := &recordingRepository{}
	idGenerator := &fixedIDGenerator{id: "expense-category-create"}
	clock := &fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	useCase, err := application.NewCreateExpenseWithCategoryCatalog(repository, idGenerator, clock, catalog)
	if err != nil {
		t.Fatalf("NewCreateExpenseWithCategoryCatalog() error = %v", err)
	}
	input := validInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	if _, err := useCase.Execute(context.Background(), input); !errors.Is(err, application.ErrCategoryNotApplicable) {
		t.Fatalf("Execute() error = %v, want ErrCategoryNotApplicable", err)
	}
	if repository.calls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
		t.Fatal("CreateExpense invalid category consumed persistence dependencies")
	}
}

func TestLegacyUncategorizedFingerprintVectorsRemainByteCompatible(t *testing.T) {
	expenseInput := application.CreateExpenseInput{
		UserID:            "user-legacy-vector",
		Description:       "Legacy synthetic expense",
		AmountMinor:       4250,
		Currency:          domain.CurrencyBRL,
		PaymentMethod:     domain.PaymentMethodPIX,
		OccurredAt:        time.Date(2026, 8, 14, 15, 0, 0, 123_456_000, time.UTC),
		FinancialTimezone: application.FinancialTimezone,
		Origin:            domain.OriginIOS,
	}
	expenseFingerprint := expenseFingerprintFor(t, "legacy-expense-vector", expenseInput, nil)
	assertFingerprintHex(t, expenseFingerprint, "012d214358ff1bdd7085d63fc454eac45144487ef58df8f3a06d3b51e7082d86")

	incomeInput := application.CreateIncomeInput{
		UserID:            "user-legacy-vector",
		Description:       "Legacy synthetic income",
		AmountMinor:       725000,
		Currency:          domain.CurrencyBRL,
		OccurredAt:        time.Date(2026, 8, 14, 15, 0, 0, 123_456_000, time.UTC),
		FinancialTimezone: application.FinancialTimezone,
		Origin:            domain.OriginIOS,
	}
	incomeFingerprint := categoryIncomeFingerprintFor(t, "legacy-income-vector", incomeInput, nil)
	assertFingerprintHex(t, incomeFingerprint, "522f8f2b1fd52ee1f6f3ce5d0bb13a1196b43d97336378b1ef0d79c4222cd026")
}

func TestCategoryIDParticipatesInFingerprintButDisplayNameDoesNot(t *testing.T) {
	expenseInput := validInput()
	uncategorizedExpense := expenseFingerprintFor(t, "expense-uncategorized", expenseInput, nil)
	expenseInput.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)
	foodCatalog := newTestCategoryCatalog(t)
	foodFingerprint := expenseFingerprintFor(t, "expense-food", expenseInput, foodCatalog)
	expenseInput.CategoryID = categoryIDPointer(domain.CategoryIDExpenseOther)
	otherFingerprint := expenseFingerprintFor(t, "expense-other", expenseInput, foodCatalog)
	if uncategorizedExpense == foodFingerprint || foodFingerprint == otherFingerprint {
		t.Fatal("Expense CategoryID did not change fingerprint semantics")
	}

	incomeInput := validIncomeInput()
	uncategorizedIncome := categoryIncomeFingerprintFor(t, "income-uncategorized", incomeInput, nil)
	incomeInput.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	defaultCatalog := newTestCategoryCatalog(t)
	defaultFingerprint := categoryIncomeFingerprintFor(t, "income-salary-default", incomeInput, defaultCatalog)
	incomeInput.CategoryID = categoryIDPointer(domain.CategoryIDIncomeOther)
	otherIncomeFingerprint := categoryIncomeFingerprintFor(t, "income-other", incomeInput, defaultCatalog)
	if defaultFingerprint == otherIncomeFingerprint {
		t.Fatal("different Income CategoryIDs produced the same fingerprint")
	}
	incomeInput.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	localizedCatalog := newTestCategoryCatalog(t)
	localizedDefinition, err := application.NewCategoryDefinition(domain.CategoryIDIncomeSalary, domain.TransactionTypeIncome, "Renda principal", 1)
	if err != nil {
		t.Fatalf("NewCategoryDefinition(localized) error = %v", err)
	}
	localizedCatalog.definitions[domain.CategoryIDIncomeSalary] = localizedDefinition
	localizedFingerprint := categoryIncomeFingerprintFor(t, "income-salary-localized", incomeInput, localizedCatalog)
	if uncategorizedIncome == defaultFingerprint {
		t.Fatal("categorized and uncategorized Income produced the same fingerprint")
	}
	if defaultFingerprint != localizedFingerprint {
		t.Fatal("localized display name changed the CategoryID fingerprint")
	}
}

func TestRecordExpenseCategoryParticipatesInDirectIdempotencySemantics(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	store := &categoryIdempotentExpenseStore{}
	idGenerator := &fixedIDGenerator{id: "expense-category-idempotency-first"}
	useCase := mustRecordExpenseWithCatalog(
		t,
		store,
		idGenerator,
		&fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)},
		catalog,
	)
	input := validInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)
	command := application.RecordExpenseInput{Expense: input, IdempotencyKey: "expense-category-idempotency"}

	first, err := useCase.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute(first categorized Expense) error = %v", err)
	}
	idGenerator.id = "expense-category-idempotency-replay-candidate"
	replay, err := useCase.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute(replayed categorized Expense) error = %v", err)
	}
	if !replay.Replayed || replay.Expense.ID() != first.Expense.ID() {
		t.Fatal("same key, financial data, and Expense Category did not replay the original aggregate")
	}

	differentCategory := input
	differentCategory.CategoryID = categoryIDPointer(domain.CategoryIDExpenseOther)
	_, err = useCase.Execute(context.Background(), application.RecordExpenseInput{
		Expense:        differentCategory,
		IdempotencyKey: command.IdempotencyKey,
	})
	if !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("Execute(different Expense Category) error = %v, want ErrIdempotencyConflict", err)
	}

	uncategorized := input
	uncategorized.CategoryID = nil
	_, err = useCase.Execute(context.Background(), application.RecordExpenseInput{
		Expense:        uncategorized,
		IdempotencyKey: command.IdempotencyKey,
	})
	if !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("Execute(categorized to uncategorized Expense) error = %v, want ErrIdempotencyConflict", err)
	}
	categoryID, present := store.original.Expense.CategoryID()
	if store.calls != 4 || !present || categoryID != domain.CategoryIDExpenseFood {
		t.Fatal("Expense conflicts changed the original idempotent result")
	}
}

func TestRecordIncomeCategoryParticipatesInDirectIdempotencySemantics(t *testing.T) {
	catalog := newTestCategoryCatalog(t)
	store := &categoryIdempotentIncomeStore{}
	idGenerator := &fixedIncomeIDGenerator{id: "income-category-idempotency-first"}
	useCase := mustRecordIncomeWithCatalog(
		t,
		store,
		idGenerator,
		&fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)},
		catalog,
	)
	input := validIncomeInput()
	input.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	command := application.RecordIncomeInput{Income: input, IdempotencyKey: "income-category-idempotency"}

	first, err := useCase.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute(first categorized Income) error = %v", err)
	}
	idGenerator.id = "income-category-idempotency-replay-candidate"
	replay, err := useCase.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute(replayed categorized Income) error = %v", err)
	}
	if !replay.Replayed || replay.Income.ID() != first.Income.ID() {
		t.Fatal("same key, financial data, and Income Category did not replay the original aggregate")
	}

	differentCategory := input
	differentCategory.CategoryID = categoryIDPointer(domain.CategoryIDIncomeOther)
	_, err = useCase.Execute(context.Background(), application.RecordIncomeInput{
		Income:         differentCategory,
		IdempotencyKey: command.IdempotencyKey,
	})
	if !errors.Is(err, application.ErrIncomeIdempotencyConflict) {
		t.Fatalf("Execute(different Income Category) error = %v, want ErrIncomeIdempotencyConflict", err)
	}

	uncategorized := input
	uncategorized.CategoryID = nil
	_, err = useCase.Execute(context.Background(), application.RecordIncomeInput{
		Income:         uncategorized,
		IdempotencyKey: command.IdempotencyKey,
	})
	if !errors.Is(err, application.ErrIncomeIdempotencyConflict) {
		t.Fatalf("Execute(categorized to uncategorized Income) error = %v, want ErrIncomeIdempotencyConflict", err)
	}
	categoryID, present := store.original.Income.CategoryID()
	if store.calls != 4 || !present || categoryID != domain.CategoryIDIncomeSalary {
		t.Fatal("Income conflicts changed the original idempotent result")
	}
}

func TestMonthlyTransactionProjectsOptionalCategoryWithoutChangingTypeSemantics(t *testing.T) {
	uncategorizedExpense := application.NewMonthlyTransactionFromExpense(mustSyntheticExpense(t, "expense-category-none"))
	if uncategorizedExpense.CategoryID != nil {
		t.Fatal("uncategorized Expense projection invented a category")
	}
	categorizedExpense := application.NewMonthlyTransactionFromExpense(mustCategorizedExpense(t, "expense-category-food", domain.CategoryIDExpenseFood))
	if categorizedExpense.CategoryID == nil || *categorizedExpense.CategoryID != domain.CategoryIDExpenseFood || categorizedExpense.PaymentMethod == nil || categorizedExpense.Type != domain.TransactionTypeExpense {
		t.Fatal("categorized Expense projection lost CategoryID or Expense semantics")
	}

	uncategorizedIncome := application.NewMonthlyTransactionFromIncome(mustSyntheticIncome(t, "income-category-none"))
	if uncategorizedIncome.CategoryID != nil {
		t.Fatal("uncategorized Income projection invented a category")
	}
	categorizedIncome := application.NewMonthlyTransactionFromIncome(mustCategorizedIncome(t, "income-category-salary", domain.CategoryIDIncomeSalary))
	if categorizedIncome.CategoryID == nil || *categorizedIncome.CategoryID != domain.CategoryIDIncomeSalary || categorizedIncome.PaymentMethod != nil || categorizedIncome.Type != domain.TransactionTypeIncome {
		t.Fatal("categorized Income projection lost CategoryID or Income semantics")
	}
}

type categorySpecification struct {
	id              domain.CategoryID
	transactionType domain.TransactionType
	displayName     string
	sortOrder       uint16
}

func approvedCategorySpecifications() []categorySpecification {
	return []categorySpecification{
		{id: domain.CategoryIDExpenseFood, transactionType: domain.TransactionTypeExpense, displayName: "Alimentação", sortOrder: 1},
		{id: domain.CategoryIDExpenseTransport, transactionType: domain.TransactionTypeExpense, displayName: "Transporte", sortOrder: 2},
		{id: domain.CategoryIDExpenseHousing, transactionType: domain.TransactionTypeExpense, displayName: "Moradia", sortOrder: 3},
		{id: domain.CategoryIDExpenseHealth, transactionType: domain.TransactionTypeExpense, displayName: "Saúde", sortOrder: 4},
		{id: domain.CategoryIDExpenseLeisure, transactionType: domain.TransactionTypeExpense, displayName: "Lazer", sortOrder: 5},
		{id: domain.CategoryIDExpenseEducation, transactionType: domain.TransactionTypeExpense, displayName: "Educação", sortOrder: 6},
		{id: domain.CategoryIDExpenseSubscriptions, transactionType: domain.TransactionTypeExpense, displayName: "Assinaturas", sortOrder: 7},
		{id: domain.CategoryIDExpenseShopping, transactionType: domain.TransactionTypeExpense, displayName: "Compras", sortOrder: 8},
		{id: domain.CategoryIDExpenseTaxesFees, transactionType: domain.TransactionTypeExpense, displayName: "Impostos e taxas", sortOrder: 9},
		{id: domain.CategoryIDExpenseOther, transactionType: domain.TransactionTypeExpense, displayName: "Outros", sortOrder: 10},
		{id: domain.CategoryIDIncomeSalary, transactionType: domain.TransactionTypeIncome, displayName: "Salário", sortOrder: 1},
		{id: domain.CategoryIDIncomeFreelance, transactionType: domain.TransactionTypeIncome, displayName: "Freelance", sortOrder: 2},
		{id: domain.CategoryIDIncomeRefund, transactionType: domain.TransactionTypeIncome, displayName: "Reembolso", sortOrder: 3},
		{id: domain.CategoryIDIncomeSale, transactionType: domain.TransactionTypeIncome, displayName: "Venda", sortOrder: 4},
		{id: domain.CategoryIDIncomeInvestmentReturn, transactionType: domain.TransactionTypeIncome, displayName: "Rendimentos", sortOrder: 5},
		{id: domain.CategoryIDIncomeBenefits, transactionType: domain.TransactionTypeIncome, displayName: "Benefícios", sortOrder: 6},
		{id: domain.CategoryIDIncomeOther, transactionType: domain.TransactionTypeIncome, displayName: "Outros", sortOrder: 7},
	}
}

type testCategoryCatalog struct {
	definitions map[domain.CategoryID]application.CategoryDefinition
	findCalls   int
	listCalls   int
	findErr     error
}

type categoryIdempotentExpenseStore struct {
	calls    int
	original application.IdempotentExpenseCommand
}

func (store *categoryIdempotentExpenseStore) Record(
	_ context.Context,
	command application.IdempotentExpenseCommand,
) (application.IdempotentExpenseResult, error) {
	store.calls++
	if store.original.Expense.ID() == "" {
		store.original = command
		return application.IdempotentExpenseResult{Expense: command.Expense}, nil
	}
	if command.Operation != store.original.Operation ||
		command.IdempotencyKey != store.original.IdempotencyKey ||
		command.Fingerprint != store.original.Fingerprint {
		return application.IdempotentExpenseResult{}, application.ErrIdempotencyConflict
	}
	return application.IdempotentExpenseResult{Expense: store.original.Expense, Replayed: true}, nil
}

type categoryIdempotentIncomeStore struct {
	calls    int
	original application.IdempotentIncomeCommand
}

func (store *categoryIdempotentIncomeStore) RecordIncome(
	_ context.Context,
	command application.IdempotentIncomeCommand,
) (application.IdempotentIncomeResult, error) {
	store.calls++
	if store.original.Income.ID() == "" {
		store.original = command
		return application.IdempotentIncomeResult{Income: command.Income}, nil
	}
	if command.Operation != store.original.Operation ||
		command.IdempotencyKey != store.original.IdempotencyKey ||
		command.Fingerprint != store.original.Fingerprint {
		return application.IdempotentIncomeResult{}, application.ErrIncomeIdempotencyConflict
	}
	return application.IdempotentIncomeResult{Income: store.original.Income, Replayed: true}, nil
}

func newTestCategoryCatalog(t testing.TB) *testCategoryCatalog {
	t.Helper()
	catalog := &testCategoryCatalog{definitions: make(map[domain.CategoryID]application.CategoryDefinition)}
	for _, specification := range approvedCategorySpecifications() {
		definition, err := application.NewCategoryDefinition(specification.id, specification.transactionType, specification.displayName, specification.sortOrder)
		if err != nil {
			t.Fatalf("NewCategoryDefinition(%q) error = %v", specification.id, err)
		}
		catalog.definitions[specification.id] = definition
	}
	return catalog
}

func (catalog *testCategoryCatalog) FindCategory(_ context.Context, categoryID domain.CategoryID) (application.CategoryDefinition, error) {
	catalog.findCalls++
	if catalog.findErr != nil {
		return application.CategoryDefinition{}, catalog.findErr
	}
	definition, exists := catalog.definitions[categoryID]
	if !exists {
		return application.CategoryDefinition{}, application.ErrCategoryNotFound
	}
	return definition, nil
}

func (catalog *testCategoryCatalog) ListCategories(_ context.Context, transactionType domain.TransactionType) ([]application.CategoryDefinition, error) {
	catalog.listCalls++
	definitions := make([]application.CategoryDefinition, 0)
	for _, definition := range catalog.definitions {
		if definition.TransactionType() == transactionType {
			definitions = append(definitions, definition)
		}
	}
	return definitions, nil
}

func mustPreviewExpenseWithCatalog(t testing.TB, catalog application.CategoryCatalog) application.PreviewExpense {
	t.Helper()
	preview, err := application.NewPreviewExpenseWithCategoryCatalog(catalog)
	if err != nil {
		t.Fatalf("NewPreviewExpenseWithCategoryCatalog() error = %v", err)
	}
	return preview
}

func mustPreviewIncomeWithCatalog(t testing.TB, catalog application.CategoryCatalog) application.PreviewIncome {
	t.Helper()
	preview, err := application.NewPreviewIncomeWithCategoryCatalog(catalog)
	if err != nil {
		t.Fatalf("NewPreviewIncomeWithCategoryCatalog() error = %v", err)
	}
	return preview
}

func mustRecordExpenseWithCatalog(
	t testing.TB,
	store application.ExpenseCommandStore,
	idGenerator application.ExpenseIDGenerator,
	clock application.Clock,
	catalog application.CategoryCatalog,
) *application.RecordExpense {
	t.Helper()
	useCase, err := application.NewRecordExpenseWithCategoryCatalog(store, idGenerator, clock, catalog)
	if err != nil {
		t.Fatalf("NewRecordExpenseWithCategoryCatalog() error = %v", err)
	}
	return useCase
}

func mustRecordIncomeWithCatalog(
	t testing.TB,
	store application.IncomeCommandStore,
	idGenerator application.IncomeIDGenerator,
	clock application.Clock,
	catalog application.CategoryCatalog,
) *application.RecordIncome {
	t.Helper()
	useCase, err := application.NewRecordIncomeWithCategoryCatalog(store, idGenerator, clock, catalog)
	if err != nil {
		t.Fatalf("NewRecordIncomeWithCategoryCatalog() error = %v", err)
	}
	return useCase
}

func expenseFingerprintFor(
	t testing.TB,
	id string,
	input application.CreateExpenseInput,
	catalog application.CategoryCatalog,
) application.RequestFingerprint {
	t.Helper()
	store := &recordingCommandStore{}
	idGenerator := &fixedIDGenerator{id: id}
	clock := &fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	var useCase *application.RecordExpense
	if catalog == nil {
		useCase = mustRecordExpense(t, store, idGenerator, clock)
	} else {
		useCase = mustRecordExpenseWithCatalog(t, store, idGenerator, clock, catalog)
	}
	if _, err := useCase.Execute(context.Background(), application.RecordExpenseInput{Expense: input, IdempotencyKey: "category-fingerprint-key"}); err != nil {
		t.Fatalf("RecordExpense.Execute() error = %v", err)
	}
	return store.command.Fingerprint
}

func categoryIncomeFingerprintFor(
	t testing.TB,
	id string,
	input application.CreateIncomeInput,
	catalog application.CategoryCatalog,
) application.RequestFingerprint {
	t.Helper()
	store := &recordingIncomeCommandStore{}
	idGenerator := &fixedIncomeIDGenerator{id: id}
	clock := &fixedClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	var useCase *application.RecordIncome
	if catalog == nil {
		useCase = mustRecordIncome(t, store, idGenerator, clock)
	} else {
		useCase = mustRecordIncomeWithCatalog(t, store, idGenerator, clock, catalog)
	}
	if _, err := useCase.Execute(context.Background(), application.RecordIncomeInput{Income: input, IdempotencyKey: "category-fingerprint-key"}); err != nil {
		t.Fatalf("RecordIncome.Execute() error = %v", err)
	}
	return store.command.Fingerprint
}

func assertFingerprintHex(t testing.TB, fingerprint application.RequestFingerprint, expected string) {
	t.Helper()
	actual := hex.EncodeToString(fingerprint[:])
	if actual != expected {
		t.Fatalf("fingerprint = %s, want legacy vector %s", actual, expected)
	}
}

func categoryIDPointer(categoryID domain.CategoryID) *domain.CategoryID {
	return &categoryID
}

func assertExpensePreviewMatchesRecord(t testing.TB, preview domain.ExpenseDetails, expense domain.Expense) {
	t.Helper()
	categoryID, present := expense.CategoryID()
	if preview.Description != expense.Description() ||
		!preview.Amount.Equal(expense.Amount()) ||
		preview.PaymentMethod != expense.PaymentMethod() ||
		preview.OccurredAt != expense.OccurredAt() ||
		preview.FinancialTimezone != expense.FinancialTimezone() ||
		preview.Origin != expense.Origin() ||
		preview.CategoryID == nil || !present || *preview.CategoryID != categoryID {
		t.Fatal("categorized Expense preview and record semantics diverged")
	}
}

func assertIncomePreviewMatchesRecord(t testing.TB, preview domain.IncomeDetails, income domain.Income) {
	t.Helper()
	categoryID, present := income.CategoryID()
	if preview.Description != income.Description() ||
		!preview.Amount.Equal(income.Amount()) ||
		preview.OccurredAt != income.OccurredAt() ||
		preview.FinancialTimezone != income.FinancialTimezone() ||
		preview.Origin != income.Origin() ||
		preview.CategoryID == nil || !present || *preview.CategoryID != categoryID {
		t.Fatal("categorized Income preview and record semantics diverged")
	}
}

func mustCategorizedExpense(t testing.TB, id string, categoryID domain.CategoryID) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(4250, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID:            "user-synthetic-category",
			Description:       "Synthetic categorized expense",
			Amount:            amount,
			PaymentMethod:     domain.PaymentMethodPIX,
			CategoryID:        categoryIDPointer(categoryID),
			OccurredAt:        time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewExpense() error = %v", err)
	}
	return expense
}

func mustCategorizedIncome(t testing.TB, id string, categoryID domain.CategoryID) domain.Income {
	t.Helper()
	amount, err := domain.NewMoney(725000, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	income, err := domain.NewIncome(domain.IncomeParams{
		ID: id,
		Details: domain.IncomeDetails{
			UserID:            "user-synthetic-category",
			Description:       "Synthetic categorized income",
			Amount:            amount,
			CategoryID:        categoryIDPointer(categoryID),
			OccurredAt:        time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewIncome() error = %v", err)
	}
	return income
}
