//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
	"jarvis/backend/internal/platform/postgres/migrations"
)

type expectedCategory struct {
	id          domain.CategoryID
	typeValue   domain.TransactionType
	displayName string
	sortOrder   uint16
}

var expectedSystemCategories = []expectedCategory{
	{id: domain.CategoryID("expense.food"), typeValue: domain.TransactionTypeExpense, displayName: "Alimentação", sortOrder: 10},
	{id: domain.CategoryID("expense.transport"), typeValue: domain.TransactionTypeExpense, displayName: "Transporte", sortOrder: 20},
	{id: domain.CategoryID("expense.housing"), typeValue: domain.TransactionTypeExpense, displayName: "Moradia", sortOrder: 30},
	{id: domain.CategoryID("expense.health"), typeValue: domain.TransactionTypeExpense, displayName: "Saúde", sortOrder: 40},
	{id: domain.CategoryID("expense.leisure"), typeValue: domain.TransactionTypeExpense, displayName: "Lazer", sortOrder: 50},
	{id: domain.CategoryID("expense.education"), typeValue: domain.TransactionTypeExpense, displayName: "Educação", sortOrder: 60},
	{id: domain.CategoryID("expense.subscriptions"), typeValue: domain.TransactionTypeExpense, displayName: "Assinaturas", sortOrder: 70},
	{id: domain.CategoryID("expense.shopping"), typeValue: domain.TransactionTypeExpense, displayName: "Compras", sortOrder: 80},
	{id: domain.CategoryID("expense.taxes_fees"), typeValue: domain.TransactionTypeExpense, displayName: "Impostos e taxas", sortOrder: 90},
	{id: domain.CategoryID("expense.other"), typeValue: domain.TransactionTypeExpense, displayName: "Outros", sortOrder: 100},
	{id: domain.CategoryID("income.salary"), typeValue: domain.TransactionTypeIncome, displayName: "Salário", sortOrder: 10},
	{id: domain.CategoryID("income.freelance"), typeValue: domain.TransactionTypeIncome, displayName: "Freelance", sortOrder: 20},
	{id: domain.CategoryID("income.refund"), typeValue: domain.TransactionTypeIncome, displayName: "Reembolso", sortOrder: 30},
	{id: domain.CategoryID("income.sale"), typeValue: domain.TransactionTypeIncome, displayName: "Venda", sortOrder: 40},
	{id: domain.CategoryID("income.investment_return"), typeValue: domain.TransactionTypeIncome, displayName: "Rendimentos", sortOrder: 50},
	{id: domain.CategoryID("income.benefits"), typeValue: domain.TransactionTypeIncome, displayName: "Benefícios", sortOrder: 60},
	{id: domain.CategoryID("income.other"), typeValue: domain.TransactionTypeIncome, displayName: "Outros", sortOrder: 70},
}

func TestMigration004SeedsExactCatalogAndAdapterReadsIt(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repository := newRepository(t, pool)

	var storedCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM categories").Scan(&storedCount); err != nil {
		t.Fatal("category count lookup failed")
	}
	if storedCount != len(expectedSystemCategories) {
		t.Fatalf("category count = %d, want %d", storedCount, len(expectedSystemCategories))
	}
	var uncategorizedCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM categories
		WHERE lower(id) LIKE '%uncategorized%' OR lower(display_name_pt_br) = 'sem categoria'
	`).Scan(&uncategorizedCount); err != nil {
		t.Fatal("uncategorized sentinel lookup failed")
	}
	if uncategorizedCount != 0 {
		t.Fatal("catalog contains a Sem categoria sentinel")
	}

	for _, transactionType := range []domain.TransactionType{domain.TransactionTypeExpense, domain.TransactionTypeIncome} {
		definitions, err := repository.ListCategories(ctx, transactionType)
		if err != nil {
			t.Fatalf("ListCategories(%s) error = %v", transactionType, err)
		}
		expected := filterExpectedCategories(transactionType)
		if len(definitions) != len(expected) {
			t.Fatalf("ListCategories(%s) count = %d, want %d", transactionType, len(definitions), len(expected))
		}
		for index := range expected {
			definition := definitions[index]
			want := expected[index]
			if definition.ID() != want.id || definition.TransactionType() != want.typeValue ||
				definition.DisplayName() != want.displayName || definition.SortOrder() != want.sortOrder {
				t.Fatalf("ListCategories(%s)[%d] does not match the approved catalog", transactionType, index)
			}
		}
	}

	definition, err := repository.FindCategory(ctx, domain.CategoryIDExpenseFood)
	if err != nil {
		t.Fatalf("FindCategory(expense.food) error = %v", err)
	}
	if definition.ID() != domain.CategoryIDExpenseFood || definition.TransactionType() != domain.TransactionTypeExpense ||
		definition.DisplayName() != "Alimentação" || definition.SortOrder() != 10 {
		t.Fatal("FindCategory(expense.food) returned unexpected metadata")
	}
	unknown, err := domain.NewCategoryID("expense.future")
	if err != nil {
		t.Fatal("synthetic unknown CategoryID is invalid")
	}
	if _, err := repository.FindCategory(ctx, unknown); !errors.Is(err, application.ErrCategoryNotFound) {
		t.Fatalf("FindCategory(unknown) error = %v, want ErrCategoryNotFound", err)
	}
	if _, err := repository.ListCategories(ctx, domain.TransactionType("TRANSFER")); !errors.Is(err, adapter.ErrInvalidCategoryTransactionType) {
		t.Fatalf("ListCategories(invalid type) error = %v", err)
	}
	canceled, cancelLookup := context.WithCancel(context.Background())
	cancelLookup()
	if _, err := repository.FindCategory(canceled, domain.CategoryIDExpenseFood); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindCategory(canceled) error = %v, want context.Canceled", err)
	}
}

func TestMigration004CategoryConstraintMatrix(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

	valid := []struct {
		id, transactionType string
		paymentMethod       any
		categoryID          any
	}{
		{id: "exp_category_valid", transactionType: "EXPENSE", paymentMethod: "PIX", categoryID: "expense.food"},
		{id: "inc_category_valid", transactionType: "INCOME", categoryID: "income.salary"},
		{id: "exp_uncategorized_valid", transactionType: "EXPENSE", paymentMethod: "CASH"},
		{id: "inc_uncategorized_valid", transactionType: "INCOME"},
	}
	for _, value := range valid {
		if err := insertRawTransactionWithCategory(ctx, pool, value.id, syntheticUserID, value.transactionType, value.paymentMethod, value.categoryID, now); err != nil {
			t.Fatalf("valid transaction %s was rejected: %v", value.id, err)
		}
	}

	invalid := []struct {
		name, id, transactionType string
		paymentMethod             any
		categoryID                any
	}{
		{name: "Expense with Income category", id: "exp_wrong_category", transactionType: "EXPENSE", paymentMethod: "PIX", categoryID: "income.salary"},
		{name: "Income with Expense category", id: "inc_wrong_category", transactionType: "INCOME", categoryID: "expense.food"},
		{name: "unknown Expense category", id: "exp_unknown_category", transactionType: "EXPENSE", paymentMethod: "PIX", categoryID: "expense.future"},
		{name: "unknown Income category", id: "inc_unknown_category", transactionType: "INCOME", categoryID: "income.future"},
	}
	for _, value := range invalid {
		t.Run(value.name, func(t *testing.T) {
			if err := insertRawTransactionWithCategory(ctx, pool, value.id, syntheticUserID, value.transactionType, value.paymentMethod, value.categoryID, now); err == nil {
				t.Fatal("invalid transaction/category combination was accepted")
			}
		})
	}

	invalidDefinitions := []struct {
		name, id, transactionType, displayName string
		sortOrder                              int
	}{
		{name: "uppercase id", id: "expense.Food", transactionType: "EXPENSE", displayName: "Teste", sortOrder: 110},
		{name: "Unicode id", id: "expense.saúde", transactionType: "EXPENSE", displayName: "Teste", sortOrder: 110},
		{name: "consecutive separators", id: "expense..future", transactionType: "EXPENSE", displayName: "Teste", sortOrder: 110},
		{name: "trailing separator", id: "expense.future_", transactionType: "EXPENSE", displayName: "Teste", sortOrder: 110},
		{name: "oversized id", id: "expense." + strings.Repeat("a", 57), transactionType: "EXPENSE", displayName: "Teste", sortOrder: 110},
		{name: "unknown transaction type", id: "future.valid", transactionType: "TRANSFER", displayName: "Teste", sortOrder: 10},
		{name: "blank display", id: "expense.blank", transactionType: "EXPENSE", displayName: " ", sortOrder: 110},
		{name: "control in display", id: "expense.control", transactionType: "EXPENSE", displayName: "Teste\ninválido", sortOrder: 110},
		{name: "zero sort order", id: "expense.zero", transactionType: "EXPENSE", displayName: "Teste", sortOrder: 0},
		{name: "duplicate type sort order", id: "expense.duplicate_order", transactionType: "EXPENSE", displayName: "Teste", sortOrder: 10},
	}
	for _, value := range invalidDefinitions {
		t.Run(value.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
				INSERT INTO categories (id, transaction_type, display_name_pt_br, sort_order)
				VALUES ($1, $2, $3, $4)
			`, value.id, value.transactionType, value.displayName, value.sortOrder); err == nil {
				t.Fatal("invalid category definition was accepted")
			}
		})
	}

	assertCategorySchema(t, ctx, pool)
	var categoryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM categories").Scan(&categoryCount); err != nil {
		t.Fatal("category count after rejected definitions failed")
	}
	if categoryCount != len(expectedSystemCategories) {
		t.Fatalf("category count after rejected definitions = %d, want %d", categoryCount, len(expectedSystemCategories))
	}
}

func TestMigration004PreservesVersion3LegacyDataReplayAndDown(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration UP failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 007 DOWN failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 006 DOWN failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 005 DOWN failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 004 DOWN failed")
		}
		assertMigrationVersion(t, ctx, connection, 3)
	})
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	expenseInput := integrationRecordInput(syntheticUserID, "legacy-expense-category-upgrade")
	expenseCapture := &captureExpenseCommandStore{}
	expenseUseCase, err := application.NewRecordExpense(expenseCapture, fixedIntegrationIDGenerator{id: "exp_legacy_004"}, fixedIntegrationClock{})
	if err != nil {
		t.Fatal("building legacy Expense command capture failed")
	}
	if _, err := expenseUseCase.Execute(ctx, expenseInput); err != nil {
		t.Fatal("capturing legacy Expense command failed")
	}
	incomeInput := integrationIncomeInput(syntheticUserID, "legacy-income-category-upgrade")
	incomeCapture := &captureIncomeCommandStore{}
	incomeUseCase, err := application.NewRecordIncome(incomeCapture, fixedIncomeIntegrationIDGenerator{id: "inc_legacy_004"}, fixedIntegrationClock{})
	if err != nil {
		t.Fatal("building legacy Income command capture failed")
	}
	if _, err := incomeUseCase.Execute(ctx, incomeInput); err != nil {
		t.Fatal("capturing legacy Income command failed")
	}

	insertLegacyExpenseCommand(t, ctx, pool, expenseCapture.command)
	insertLegacyIncomeCommand(t, ctx, pool, incomeCapture.command)
	beforeExpenseFingerprint := storedFingerprint(t, ctx, pool, "CREATE_EXPENSE", expenseInput.IdempotencyKey)
	beforeIncomeFingerprint := storedFingerprint(t, ctx, pool, "CREATE_INCOME", incomeInput.IdempotencyKey)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration 003 to 004 failed")
		}
		assertMigrationVersion(t, ctx, connection, 7)
	})
	assertAllTransactionsUncategorized(t, ctx, pool, 2)
	if !bytes.Equal(beforeExpenseFingerprint, storedFingerprint(t, ctx, pool, "CREATE_EXPENSE", expenseInput.IdempotencyKey)) ||
		!bytes.Equal(beforeIncomeFingerprint, storedFingerprint(t, ctx, pool, "CREATE_INCOME", incomeInput.IdempotencyKey)) {
		t.Fatal("migration 004 changed legacy idempotency fingerprints")
	}

	repository := newRepository(t, pool)
	expenseReplay := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: "exp_unused_legacy_replay"})
	replayedExpense, err := expenseReplay.Execute(ctx, expenseInput)
	if err != nil || !replayedExpense.Replayed || replayedExpense.Expense.ID() != "exp_legacy_004" {
		t.Fatalf("legacy Expense replay after migration 004 failed: result=%+v err=%v", replayedExpense, err)
	}
	incomeReplay := newRecordIncomeUseCase(t, repository, fixedIncomeIntegrationIDGenerator{id: "inc_unused_legacy_replay"})
	replayedIncome, err := incomeReplay.Execute(ctx, incomeInput)
	if err != nil || !replayedIncome.Replayed || replayedIncome.Income.ID() != "inc_legacy_004" {
		t.Fatalf("legacy Income replay after migration 004 failed: result=%+v err=%v", replayedIncome, err)
	}
	assertFinancialRowCounts(t, ctx, pool, 2, 2, 2)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 007 DOWN with no credit card data failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 006 DOWN with no suppression data failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 005 DOWN with no recurrence data failed")
		}
		if err := migrations.Down(ctx, connection); err != nil {
			t.Fatal("migration 004 DOWN with uncategorized legacy data failed")
		}
		assertMigrationVersion(t, ctx, connection, 3)
	})
	assertTableExists(t, ctx, pool, "categories", false)
	assertColumnExists(t, ctx, pool, "transactions", "category_id", false)
	assertFinancialRowCounts(t, ctx, pool, 2, 2, 2)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		if err := migrations.Up(ctx, connection); err != nil {
			t.Fatal("migration 004 reapply after safe DOWN failed")
		}
		assertMigrationVersion(t, ctx, connection, 7)
	})
	assertAllTransactionsUncategorized(t, ctx, pool, 2)
}

func TestMigration004DownRefusesCategorizedFullStateAtomically(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	moveToMigration004(t, ctx, pool)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	repository := newRepository(t, pool)
	useCase := newCategorizedIncomeUseCase(t, repository, fixedIncomeIntegrationIDGenerator{id: "inc_category_down_guard"})
	input := integrationIncomeInput(syntheticUserID, "category-down-guard")
	input.Income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	if _, err := useCase.Execute(ctx, input); err != nil {
		t.Fatalf("categorized Income setup failed: %v", err)
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		err := migrations.Down(ctx, connection)
		if !errors.Is(err, migrations.ErrRollback) {
			t.Fatalf("migration 004 DOWN error = %v, want ErrRollback", err)
		}
		assertMigrationVersion(t, ctx, connection, 4)
	})
	assertTableExists(t, ctx, pool, "categories", true)
	assertColumnExists(t, ctx, pool, "transactions", "category_id", true)
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
	assertStoredCategory(t, ctx, pool, "inc_category_down_guard", domain.CategoryIDIncomeSalary)
	assertCategorySchema(t, ctx, pool)
}

func TestMigration004DownWaitsForCategorizedWriteAlreadyInProgress(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigration004(t, ctx, pool)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	writerConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring categorized writer connection failed")
	}
	defer writerConnection.Release()
	writerPID := connectionBackendPID(t, ctx, writerConnection)
	writerTransaction, err := writerConnection.Begin(ctx)
	if err != nil {
		t.Fatal("beginning categorized writer transaction failed")
	}
	defer func() { _ = writerTransaction.Rollback(context.Background()) }()
	now := time.Date(2026, time.August, 15, 13, 0, 0, 0, time.UTC)
	if err := insertRawTransactionWithCategory(
		ctx,
		writerTransaction,
		"exp_category_down_concurrent_before",
		syntheticUserID,
		"EXPENSE",
		"PIX",
		"expense.food",
		now,
	); err != nil {
		t.Fatal("inserting uncommitted categorized transaction failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring concurrent DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() {
		downResult <- migrations.Down(ctx, downConnection.Conn())
	}()

	waitForBackendBlockedBy(t, ctx, pool, downPID, writerPID)
	if err := writerTransaction.Commit(ctx); err != nil {
		t.Fatal("committing categorized writer transaction failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); !errors.Is(err, migrations.ErrRollback) {
		t.Fatalf("concurrent migration 004 DOWN error = %v, want ErrRollback", err)
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 4)
	})
	assertTableExists(t, ctx, pool, "categories", true)
	assertColumnExists(t, ctx, pool, "transactions", "category_id", true)
	assertStoredCategory(t, ctx, pool, "exp_category_down_concurrent_before", domain.CategoryIDExpenseFood)
	assertCategorySchema(t, ctx, pool)
}

func TestMigration004DownBlocksCategorizedWriteAfterLock(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	moveToMigration004(t, ctx, pool)
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	categoryBlocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring category blocker connection failed")
	}
	defer categoryBlocker.Release()
	blockerPID := connectionBackendPID(t, ctx, categoryBlocker)
	blockerTransaction, err := categoryBlocker.Begin(ctx)
	if err != nil {
		t.Fatal("beginning category blocker transaction failed")
	}
	defer func() { _ = blockerTransaction.Rollback(context.Background()) }()
	if _, err := blockerTransaction.Exec(ctx, "LOCK TABLE categories IN ACCESS SHARE MODE"); err != nil {
		t.Fatal("locking category catalog fixture failed")
	}

	downConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring locked DOWN connection failed")
	}
	defer downConnection.Release()
	downPID := connectionBackendPID(t, ctx, downConnection)
	downResult := make(chan error, 1)
	go func() {
		downResult <- migrations.Down(ctx, downConnection.Conn())
	}()
	waitForDownTransactionLockAndBlocker(t, ctx, pool, downPID, blockerPID)

	writerConnection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquiring post-lock writer connection failed")
	}
	defer writerConnection.Release()
	writerPID := connectionBackendPID(t, ctx, writerConnection)
	writerResult := make(chan error, 1)
	now := time.Date(2026, time.August, 15, 13, 30, 0, 0, time.UTC)
	go func() {
		writerResult <- insertRawTransactionWithCategory(
			ctx,
			writerConnection,
			"exp_category_down_concurrent_after",
			syntheticUserID,
			"EXPENSE",
			"PIX",
			"expense.food",
			now,
		)
	}()
	waitForBackendBlockedBy(t, ctx, pool, writerPID, downPID)

	if err := blockerTransaction.Commit(ctx); err != nil {
		t.Fatal("releasing category blocker transaction failed")
	}
	if err := waitForMigrationResult(t, ctx, downResult); err != nil {
		t.Fatalf("migration 004 DOWN after lock error = %v", err)
	}
	if err := waitForMigrationResult(t, ctx, writerResult); err == nil {
		t.Fatal("categorized writer crossed completed migration 004 DOWN")
	}

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 3)
	})
	assertTableExists(t, ctx, pool, "categories", false)
	assertColumnExists(t, ctx, pool, "transactions", "category_id", false)
	var transactionCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM transactions WHERE id = 'exp_category_down_concurrent_after'
	`).Scan(&transactionCount); err != nil {
		t.Fatal("post-lock categorized writer count failed")
	}
	if transactionCount != 0 {
		t.Fatal("categorized writer survived DOWN without its Category")
	}
}

func TestCategorizedExpenseIncomeReplayConflictRestartMonthlyAndOwnerIsolation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerB = "usr_category_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)
	repository := newRepository(t, pool)

	expenseUseCase := newCategorizedExpenseUseCase(t, repository, &sequenceIDGenerator{prefix: "exp_category"})
	expenseInput := integrationRecordInput(syntheticUserID, "categorized-expense-key")
	expenseInput.Expense.CategoryID = categoryIDPointer(domain.CategoryIDExpenseFood)
	createdExpense, err := expenseUseCase.Execute(ctx, expenseInput)
	if err != nil {
		t.Fatalf("categorized Expense first write failed: %v", err)
	}
	replayedExpense, err := expenseUseCase.Execute(ctx, expenseInput)
	if err != nil || !replayedExpense.Replayed || replayedExpense.Expense.ID() != createdExpense.Expense.ID() {
		t.Fatalf("categorized Expense replay failed: result=%+v err=%v", replayedExpense, err)
	}
	expenseConflict := expenseInput
	expenseConflict.Expense.CategoryID = categoryIDPointer(domain.CategoryIDExpenseOther)
	if _, err := expenseUseCase.Execute(ctx, expenseConflict); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("categorized Expense category conflict error = %v", err)
	}

	incomeUseCase := newCategorizedIncomeUseCase(t, repository, &sequenceIncomeIDGenerator{prefix: "inc_category"})
	incomeInput := integrationIncomeInput(syntheticUserID, "categorized-income-key")
	incomeInput.Income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	createdIncome, err := incomeUseCase.Execute(ctx, incomeInput)
	if err != nil {
		t.Fatalf("categorized Income first write failed: %v", err)
	}
	replayedIncome, err := incomeUseCase.Execute(ctx, incomeInput)
	if err != nil || !replayedIncome.Replayed || replayedIncome.Income.ID() != createdIncome.Income.ID() {
		t.Fatalf("categorized Income replay failed: result=%+v err=%v", replayedIncome, err)
	}
	incomeConflict := incomeInput
	incomeConflict.Income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeOther)
	if _, err := incomeUseCase.Execute(ctx, incomeConflict); !errors.Is(err, application.ErrIncomeIdempotencyConflict) {
		t.Fatalf("categorized Income category conflict error = %v", err)
	}

	wrongType := integrationRecordInput(syntheticUserID, "wrong-category-type")
	wrongType.Expense.CategoryID = categoryIDPointer(domain.CategoryIDIncomeSalary)
	if _, err := expenseUseCase.Execute(ctx, wrongType); !errors.Is(err, application.ErrCategoryNotApplicable) {
		t.Fatalf("Expense with Income category error = %v", err)
	}

	ownerBInput := integrationIncomeInput(ownerB, "categorized-income-key")
	ownerBInput.Income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeBenefits)
	if _, err := incomeUseCase.Execute(ctx, ownerBInput); err != nil {
		t.Fatalf("owner B categorized Income failed: %v", err)
	}

	poolB, err := pgxpool.New(ctx, pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal("opening independent categorized replay pool failed")
	}
	defer poolB.Close()
	if err := poolB.Ping(ctx); err != nil {
		t.Fatal("independent categorized replay pool readiness failed")
	}
	repositoryB := newRepository(t, poolB)
	restartUseCase := newCategorizedIncomeUseCase(t, repositoryB, fixedIncomeIntegrationIDGenerator{id: "inc_unused_category_restart"})
	restarted, err := restartUseCase.Execute(ctx, incomeInput)
	if err != nil || !restarted.Replayed || restarted.Income.ID() != createdIncome.Income.ID() {
		t.Fatalf("categorized cross-restart replay failed: result=%+v err=%v", restarted, err)
	}

	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		t.Fatal("financial timezone unavailable")
	}
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, location).UTC()
	end := time.Date(2026, time.September, 1, 0, 0, 0, 0, location).UTC()
	items, err := repository.ListMonthlyTransactions(ctx, application.ExpenseMonthQuery{
		UserID: syntheticUserID, Start: start, End: end, FinancialTimezone: application.FinancialTimezone,
	})
	if err != nil {
		t.Fatalf("categorized monthly history failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("owner A monthly item count = %d, want 2", len(items))
	}
	seen := map[domain.TransactionType]domain.CategoryID{}
	for _, item := range items {
		if item.UserID != syntheticUserID || item.CategoryID == nil {
			t.Fatal("monthly history lost category or leaked another owner")
		}
		seen[item.Type] = *item.CategoryID
		if item.Type == domain.TransactionTypeExpense && item.PaymentMethod == nil {
			t.Fatal("categorized Expense lost payment method")
		}
		if item.Type == domain.TransactionTypeIncome && item.PaymentMethod != nil {
			t.Fatal("categorized Income acquired payment method")
		}
	}
	if seen[domain.TransactionTypeExpense] != domain.CategoryIDExpenseFood ||
		seen[domain.TransactionTypeIncome] != domain.CategoryIDIncomeSalary {
		t.Fatal("monthly history returned incorrect categories")
	}
	expenses, err := repository.ListByFinancialMonth(ctx, application.ExpenseMonthQuery{
		UserID: syntheticUserID, Start: start, End: end, FinancialTimezone: application.FinancialTimezone,
	})
	if err != nil || len(expenses) != 1 {
		t.Fatalf("categorized Expense-only history result count/error = %d/%v", len(expenses), err)
	}
	expenseCategoryID, ok := expenses[0].CategoryID()
	if !ok || expenseCategoryID != domain.CategoryIDExpenseFood {
		t.Fatal("Expense-only history lost CategoryID")
	}
	assertCategoryFinancialMetadata(
		t, ctx, pool, createdExpense.Expense.ID(), domain.TransactionTypeExpense,
		domain.CategoryIDExpenseFood, "PIX", "EXPENSE_RECORDED", "CREATE_EXPENSE", expenseInput.IdempotencyKey,
	)
	assertCategoryFinancialMetadata(
		t, ctx, pool, createdIncome.Income.ID(), domain.TransactionTypeIncome,
		domain.CategoryIDIncomeSalary, nil, "INCOME_RECORDED", "CREATE_INCOME", incomeInput.IdempotencyKey,
	)
	assertFinancialRowCounts(t, ctx, pool, 3, 3, 3)
}

func TestCategorizedIncomeCommandIsConcurrencySafe(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	repository := newRepository(t, pool)
	useCase := newCategorizedIncomeUseCase(t, repository, &sequenceIncomeIDGenerator{prefix: "inc_category_concurrent"})
	input := integrationIncomeInput(syntheticUserID, "categorized-concurrent-key")
	input.Income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeFreelance)

	const requests = 8
	start := make(chan struct{})
	results := make(chan application.RecordIncomeResult, requests)
	errorsChannel := make(chan error, requests)
	var wait sync.WaitGroup
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := useCase.Execute(ctx, input)
			results <- result
			errorsChannel <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent categorized Income error = %v", err)
		}
	}
	ids := map[string]struct{}{}
	newWrites := 0
	for result := range results {
		ids[result.Income.ID()] = struct{}{}
		if !result.Replayed {
			newWrites++
		}
		categoryID, ok := result.Income.CategoryID()
		if !ok || categoryID != domain.CategoryIDIncomeFreelance {
			t.Fatal("concurrent replay lost Income category")
		}
	}
	if len(ids) != 1 || newWrites != 1 {
		t.Fatalf("concurrent categorized Income IDs/new writes = %d/%d, want 1/1", len(ids), newWrites)
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestUncategorizedAndCategorizedPayloadsConflictInBothDirectionsInPostgres(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	repository := newRepository(t, pool)

	expenseUseCase := newCategorizedExpenseUseCase(t, repository, &sequenceIDGenerator{prefix: "exp_uncategorized_conflict"})
	expenseInput := integrationRecordInput(syntheticUserID, "uncategorized-expense-conflict")
	if _, err := expenseUseCase.Execute(ctx, expenseInput); err != nil {
		t.Fatalf("uncategorized Expense setup failed: %v", err)
	}
	categorizedExpense := expenseInput
	categorizedExpense.Expense.CategoryID = categoryIDPointer(domain.CategoryIDExpenseTransport)
	if _, err := expenseUseCase.Execute(ctx, categorizedExpense); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("uncategorized to categorized Expense error = %v", err)
	}

	incomeUseCase := newCategorizedIncomeUseCase(t, repository, &sequenceIncomeIDGenerator{prefix: "inc_uncategorized_conflict"})
	incomeInput := integrationIncomeInput(syntheticUserID, "uncategorized-income-conflict")
	if _, err := incomeUseCase.Execute(ctx, incomeInput); err != nil {
		t.Fatalf("uncategorized Income setup failed: %v", err)
	}
	categorizedIncome := incomeInput
	categorizedIncome.Income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeRefund)
	if _, err := incomeUseCase.Execute(ctx, categorizedIncome); !errors.Is(err, application.ErrIncomeIdempotencyConflict) {
		t.Fatalf("uncategorized to categorized Income error = %v", err)
	}
	assertAllTransactionsUncategorized(t, ctx, pool, 2)
	assertFinancialRowCounts(t, ctx, pool, 2, 2, 2)

	reverseExpenseInput := integrationRecordInput(syntheticUserID, "categorized-expense-reverse-conflict")
	reverseExpenseInput.Expense.CategoryID = categoryIDPointer(domain.CategoryIDExpenseTransport)
	reverseExpense, err := expenseUseCase.Execute(ctx, reverseExpenseInput)
	if err != nil {
		t.Fatalf("categorized Expense reverse setup failed: %v", err)
	}
	uncategorizedExpense := reverseExpenseInput
	uncategorizedExpense.Expense.CategoryID = nil
	if _, err := expenseUseCase.Execute(ctx, uncategorizedExpense); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("categorized to uncategorized Expense error = %v", err)
	}

	reverseIncomeInput := integrationIncomeInput(syntheticUserID, "categorized-income-reverse-conflict")
	reverseIncomeInput.Income.CategoryID = categoryIDPointer(domain.CategoryIDIncomeRefund)
	reverseIncome, err := incomeUseCase.Execute(ctx, reverseIncomeInput)
	if err != nil {
		t.Fatalf("categorized Income reverse setup failed: %v", err)
	}
	uncategorizedIncome := reverseIncomeInput
	uncategorizedIncome.Income.CategoryID = nil
	if _, err := incomeUseCase.Execute(ctx, uncategorizedIncome); !errors.Is(err, application.ErrIncomeIdempotencyConflict) {
		t.Fatalf("categorized to uncategorized Income error = %v", err)
	}

	assertStoredCategory(t, ctx, pool, reverseExpense.Expense.ID(), domain.CategoryIDExpenseTransport)
	assertStoredCategory(t, ctx, pool, reverseIncome.Income.ID(), domain.CategoryIDIncomeRefund)
	assertFinancialRowCounts(t, ctx, pool, 4, 4, 4)
}

func TestStoredInvalidCategoryFailsClosedInReaders(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	repository := newRepository(t, pool)
	expense := validExpense(t, "exp_corrupt_category", syntheticUserID)
	if err := repository.Save(ctx, expense); err != nil {
		t.Fatal("uncategorized Expense setup failed")
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE transactions DROP CONSTRAINT transactions_type_category_fkey"); err != nil {
		t.Fatal("dropping category FK for corruption fixture failed")
	}
	if _, err := pool.Exec(ctx, "UPDATE transactions SET category_id = 'INVALID!' WHERE id = $1", expense.ID()); err != nil {
		t.Fatal("installing corrupt category fixture failed")
	}
	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		t.Fatal("financial timezone unavailable")
	}
	_, err = repository.ListMonthlyTransactions(ctx, application.ExpenseMonthQuery{
		UserID:            syntheticUserID,
		Start:             time.Date(2026, time.July, 1, 0, 0, 0, 0, location).UTC(),
		End:               time.Date(2026, time.August, 1, 0, 0, 0, 0, location).UTC(),
		FinancialTimezone: application.FinancialTimezone,
	})
	if !errors.Is(err, adapter.ErrLoadMonthlyTransaction) {
		t.Fatalf("reader corruption error = %v, want ErrLoadMonthlyTransaction", err)
	}
}

func TestCategoryCatalogRejectsCorruptStoredDefinition(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "ALTER TABLE categories DROP CONSTRAINT categories_transaction_type_valid"); err != nil {
		t.Fatal("dropping type constraint for corruption fixture failed")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE categories SET transaction_type = 'TRANSFER' WHERE id = 'expense.food'
	`); err != nil {
		t.Fatal("installing corrupt category definition failed")
	}
	repository := newRepository(t, pool)
	_, err := repository.FindCategory(ctx, domain.CategoryIDExpenseFood)
	if !errors.Is(err, adapter.ErrLoadCategoryDefinition) {
		t.Fatalf("corrupt catalog error = %v, want ErrLoadCategoryDefinition", err)
	}
	if strings.Contains(err.Error(), "TRANSFER") || strings.Contains(err.Error(), "expense.food") {
		t.Fatal("catalog error exposed stored category data")
	}
}

type captureExpenseCommandStore struct {
	command application.IdempotentExpenseCommand
}

func (store *captureExpenseCommandStore) Record(_ context.Context, command application.IdempotentExpenseCommand) (application.IdempotentExpenseResult, error) {
	store.command = command
	return application.IdempotentExpenseResult{Expense: command.Expense}, nil
}

type captureIncomeCommandStore struct {
	command application.IdempotentIncomeCommand
}

func (store *captureIncomeCommandStore) RecordIncome(_ context.Context, command application.IdempotentIncomeCommand) (application.IdempotentIncomeResult, error) {
	store.command = command
	return application.IdempotentIncomeResult{Income: command.Income}, nil
}

func filterExpectedCategories(transactionType domain.TransactionType) []expectedCategory {
	filtered := make([]expectedCategory, 0)
	for _, category := range expectedSystemCategories {
		if category.typeValue == transactionType {
			filtered = append(filtered, category)
		}
	}
	return filtered
}

func categoryIDPointer(categoryID domain.CategoryID) *domain.CategoryID {
	return &categoryID
}

func newCategorizedExpenseUseCase(
	t *testing.T,
	repository *adapter.ExpenseRepository,
	idGenerator application.ExpenseIDGenerator,
) *application.RecordExpense {
	t.Helper()
	useCase, err := application.NewRecordExpenseWithCategoryCatalog(repository, idGenerator, fixedIntegrationClock{}, repository)
	if err != nil {
		t.Fatalf("NewRecordExpenseWithCategoryCatalog() error = %v", err)
	}
	return useCase
}

func newCategorizedIncomeUseCase(
	t *testing.T,
	repository *adapter.ExpenseRepository,
	idGenerator application.IncomeIDGenerator,
) *application.RecordIncome {
	t.Helper()
	useCase, err := application.NewRecordIncomeWithCategoryCatalog(repository, idGenerator, fixedIntegrationClock{}, repository)
	if err != nil {
		t.Fatalf("NewRecordIncomeWithCategoryCatalog() error = %v", err)
	}
	return useCase
}

func insertRawTransactionWithCategory(
	ctx context.Context,
	executor databaseExecer,
	id, userID, transactionType string,
	paymentMethod any,
	categoryID any,
	createdAt time.Time,
) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, type, description, amount_minor, currency, payment_method,
			category_id, occurred_at, financial_timezone, origin, status, version, created_at, updated_at
		) VALUES ($1, $2, $3, 'Transação sintética', 1, 'BRL', $4, $5, $6,
			'America/Sao_Paulo', 'IOS', 'RECORDED', 1, $6, $6)
	`, id, userID, transactionType, paymentMethod, categoryID, createdAt)
	return err
}

func insertLegacyExpenseCommand(t *testing.T, ctx context.Context, pool *pgxpool.Pool, command application.IdempotentExpenseCommand) {
	t.Helper()
	expense := command.Expense
	if _, err := pool.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, type, description, amount_minor, currency, payment_method,
			occurred_at, financial_timezone, origin, status, version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, expense.ID(), expense.UserID(), expense.Type(), expense.Description(), expense.Amount().MinorUnits(),
		expense.Amount().Currency(), expense.PaymentMethod(), expense.OccurredAt(), expense.FinancialTimezone(),
		expense.Origin(), expense.Status(), expense.Version(), expense.CreatedAt(), expense.UpdatedAt()); err != nil {
		t.Fatal("inserting migration 003 Expense fixture failed")
	}
	if err := insertRawTypedAuditEvent(ctx, pool, expense.UserID(), expense.ID(), "EXPENSE", "EXPENSE_RECORDED", expense.CreatedAt()); err != nil {
		t.Fatal("inserting migration 003 Expense audit fixture failed")
	}
	insertLegacyIdempotency(t, ctx, pool, expense.UserID(), command.Operation, command.IdempotencyKey, command.Fingerprint[:], expense.ID(), expense.CreatedAt())
}

func insertLegacyIncomeCommand(t *testing.T, ctx context.Context, pool *pgxpool.Pool, command application.IdempotentIncomeCommand) {
	t.Helper()
	income := command.Income
	if _, err := pool.Exec(ctx, `
		INSERT INTO transactions (
			id, user_id, type, description, amount_minor, currency, payment_method,
			occurred_at, financial_timezone, origin, status, version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, $8, $9, $10, $11, $12, $13)
	`, income.ID(), income.UserID(), income.Type(), income.Description(), income.Amount().MinorUnits(),
		income.Amount().Currency(), income.OccurredAt(), income.FinancialTimezone(), income.Origin(),
		income.Status(), income.Version(), income.CreatedAt(), income.UpdatedAt()); err != nil {
		t.Fatal("inserting migration 003 Income fixture failed")
	}
	if err := insertRawTypedAuditEvent(ctx, pool, income.UserID(), income.ID(), "INCOME", "INCOME_RECORDED", income.CreatedAt()); err != nil {
		t.Fatal("inserting migration 003 Income audit fixture failed")
	}
	insertLegacyIdempotency(t, ctx, pool, income.UserID(), command.Operation, command.IdempotencyKey, command.Fingerprint[:], income.ID(), income.CreatedAt())
}

func insertLegacyIdempotency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, operation, key string,
	fingerprint []byte,
	transactionID string,
	createdAt time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint,
			state, transaction_id, created_at, completed_at
		) VALUES ($1, $2, $3, $4, 'COMPLETED', $5, $6, $6)
	`, userID, operation, key, fingerprint, transactionID, createdAt); err != nil {
		t.Fatal("inserting migration 003 idempotency fixture failed")
	}
}

func storedFingerprint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, operation, key string) []byte {
	t.Helper()
	var fingerprint []byte
	if err := pool.QueryRow(ctx, `
		SELECT request_fingerprint FROM idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, syntheticUserID, operation, key).Scan(&fingerprint); err != nil {
		t.Fatal("stored fingerprint lookup failed")
	}
	return fingerprint
}

func assertAllTransactionsUncategorized(t *testing.T, ctx context.Context, pool *pgxpool.Pool, expected int) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM transactions WHERE category_id IS NULL").Scan(&count); err != nil {
		t.Fatal("uncategorized transaction count failed")
	}
	if count != expected {
		t.Fatalf("uncategorized transaction count = %d, want %d", count, expected)
	}
}

func assertStoredCategory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, transactionID string, expected domain.CategoryID) {
	t.Helper()
	var categoryID string
	if err := pool.QueryRow(ctx, "SELECT category_id FROM transactions WHERE id = $1", transactionID).Scan(&categoryID); err != nil {
		t.Fatal("stored category lookup failed")
	}
	if categoryID != expected.String() {
		t.Fatalf("stored category = %q, want %q", categoryID, expected)
	}
}

func assertCategoryFinancialMetadata(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	transactionID string,
	transactionType domain.TransactionType,
	categoryID domain.CategoryID,
	paymentMethod any,
	eventType, operation, key string,
) {
	t.Helper()
	var storedType, storedCategory string
	var storedPaymentMethod *string
	if err := pool.QueryRow(ctx, `
		SELECT type, category_id, payment_method FROM transactions WHERE id = $1
	`, transactionID).Scan(&storedType, &storedCategory, &storedPaymentMethod); err != nil {
		t.Fatal("categorized transaction metadata lookup failed")
	}
	if storedType != string(transactionType) || storedCategory != categoryID.String() {
		t.Fatal("categorized transaction metadata is inconsistent")
	}
	if paymentMethod == nil {
		if storedPaymentMethod != nil {
			t.Fatal("Income persisted a payment method")
		}
	} else if storedPaymentMethod == nil || *storedPaymentMethod != paymentMethod {
		t.Fatal("Expense payment method changed during categorized persistence")
	}

	var storedEventType string
	if err := pool.QueryRow(ctx, `
		SELECT event_type FROM audit_events
		WHERE user_id = $1 AND aggregate_id = $2 AND aggregate_type = $3
	`, syntheticUserID, transactionID, transactionType).Scan(&storedEventType); err != nil {
		t.Fatal("categorized audit lookup failed")
	}
	if storedEventType != eventType {
		t.Fatal("categorized write changed the audit event type")
	}
	var state, storedTransactionID string
	if err := pool.QueryRow(ctx, `
		SELECT state, transaction_id FROM idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3
	`, syntheticUserID, operation, key).Scan(&state, &storedTransactionID); err != nil {
		t.Fatal("categorized idempotency lookup failed")
	}
	if state != "COMPLETED" || storedTransactionID != transactionID {
		t.Fatal("categorized idempotency metadata is inconsistent")
	}
}

func connectionBackendPID(t *testing.T, ctx context.Context, connection *pgxpool.Conn) int32 {
	t.Helper()
	var pid int32
	if err := connection.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatal("PostgreSQL backend PID lookup failed")
	}
	return pid
}

func waitForBackendBlockedBy(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	blockedPID int32,
	blockingPID int32,
) {
	t.Helper()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `
			SELECT $2::integer = ANY(pg_blocking_pids($1::integer))
		`, blockedPID, blockingPID).Scan(&blocked); err != nil {
			t.Fatal("PostgreSQL blocking relationship lookup failed")
		}
		if blocked {
			return
		}
	}
}

func waitForDownTransactionLockAndBlocker(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	downPID int32,
	blockingPID int32,
) {
	t.Helper()
	for {
		var ready bool
		if err := pool.QueryRow(ctx, `
			SELECT
				EXISTS (
					SELECT 1 FROM pg_locks
					WHERE pid = $1
					  AND relation = 'public.transactions'::regclass
					  AND mode = 'AccessExclusiveLock'
					  AND granted
				)
				AND $2::integer = ANY(pg_blocking_pids($1::integer))
		`, downPID, blockingPID).Scan(&ready); err != nil {
			t.Fatal("migration 004 DOWN lock state lookup failed")
		}
		if ready {
			return
		}
	}
}

func waitForMigrationResult(t *testing.T, ctx context.Context, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatal("timed out waiting for PostgreSQL operation")
		return ctx.Err()
	}
}

func assertColumnExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, column string, expected bool) {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)
	`, table, column).Scan(&exists); err != nil {
		t.Fatal("schema column lookup failed")
	}
	if exists != expected {
		t.Fatalf("column %s.%s exists = %t, want %t", table, column, exists, expected)
	}
}

func assertCategorySchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	assertColumnExists(t, ctx, pool, "transactions", "category_id", true)
	rows, err := pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'categories'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatal("category schema columns lookup failed")
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal("category schema column scan failed")
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal("category schema columns iteration failed")
	}
	expectedColumns := []string{"id", "transaction_type", "display_name_pt_br", "sort_order"}
	if strings.Join(columns, ",") != strings.Join(expectedColumns, ",") {
		t.Fatalf("category columns = %v, want %v", columns, expectedColumns)
	}
	var nullable string
	if err := pool.QueryRow(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'transactions' AND column_name = 'category_id'
	`).Scan(&nullable); err != nil {
		t.Fatal("category_id nullability lookup failed")
	}
	if nullable != "YES" {
		t.Fatalf("transactions.category_id nullable = %q, want YES", nullable)
	}

	expectedConstraints := map[string]string{
		"categories_pkey":                               "PRIMARY KEY (id)",
		"categories_id_format":                          "CHECK",
		"categories_transaction_type_valid":             "CHECK",
		"categories_display_name_valid":                 "CHECK",
		"categories_sort_order_positive":                "CHECK",
		"categories_transaction_type_id_unique":         "UNIQUE (transaction_type, id)",
		"categories_transaction_type_sort_order_unique": "UNIQUE (transaction_type, sort_order)",
		"transactions_type_category_fkey":               "FOREIGN KEY (type, category_id) REFERENCES categories(transaction_type, id) ON UPDATE RESTRICT ON DELETE RESTRICT",
	}
	for name, fragment := range expectedConstraints {
		var definition string
		if err := pool.QueryRow(ctx, `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1
		`, name).Scan(&definition); err != nil {
			t.Fatalf("constraint %s lookup failed", name)
		}
		if !strings.Contains(definition, fragment) {
			t.Fatalf("constraint %s definition = %q, want fragment %q", name, definition, fragment)
		}
	}

	var speculativeIndexes int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pg_indexes
		WHERE schemaname = 'public' AND tablename = 'transactions'
		  AND indexdef ILIKE '%category_id%'
	`).Scan(&speculativeIndexes); err != nil {
		t.Fatal("category index lookup failed")
	}
	if speculativeIndexes != 0 {
		t.Fatalf("transactions has %d speculative category index(es)", speculativeIndexes)
	}
}
