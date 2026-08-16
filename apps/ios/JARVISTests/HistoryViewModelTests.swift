import XCTest
@testable import JARVIS

@MainActor
final class HistoryViewModelTests: XCTestCase {
    func testFormatsAndNavigatesMonthsDeterministically() {
        let august = FinancialMonth(year: 2026, month: 8)
        XCTAssertEqual(august.apiValue, "2026-08")
        XCTAssertEqual(august.adding(months: -1).apiValue, "2026-07")
        XCTAssertEqual(august.adding(months: 1).apiValue, "2026-09")
        XCTAssertEqual(FinancialMonth(year: 2026, month: 12).adding(months: 1).apiValue, "2027-01")
    }

    func testLoadPreservesServerOrderingAndSupportsEmptyState() async {
        let api = FinancialAPISpy()
        let first = syntheticExpense(id: "exp_002", description: "Restaurante QA")
        let second = syntheticExpense(id: "exp_001", description: "Mercado sintético")
        api.monthResult = .success(TransactionMonth(month: "2026-08", items: [.expense(first), .expense(second)]))
        let model = HistoryViewModel(api: api, now: syntheticAugustDate())

        await model.load()
        guard case let .loaded(items) = model.state else {
            return XCTFail("Expected loaded history")
        }
        XCTAssertEqual(items.map(\.id), ["exp_002", "exp_001"])
        XCTAssertEqual(api.requestedMonths, ["2026-08"])

        api.monthResult = .success(TransactionMonth(month: "2026-08", items: []))
        await model.load()
        XCTAssertEqual(model.state, .loaded([]))
    }

    func testLoadPreservesMixedExpenseAndIncomeOrdering() async {
        let api = FinancialAPISpy()
        let expense = syntheticExpense(id: "exp_001")
        let income = syntheticIncome(id: "inc_001")
        api.monthResult = .success(
            TransactionMonth(month: "2026-08", items: [.income(income), .expense(expense)])
        )
        let model = HistoryViewModel(api: api, now: syntheticAugustDate())

        await model.load()

        guard case let .loaded(items) = model.state else {
            return XCTFail("Expected loaded history")
        }
        XCTAssertEqual(items, [.income(income), .expense(expense)])
        XCTAssertEqual(items.map(\.type), [.income, .expense])
    }

    func testCategoryLabelsDistinguishUncategorizedUnknownAndKnownValues() {
        let api = FinancialAPISpy()
        let model = makeModel(api: api)
        let known = FinancialTransaction.expense(
            syntheticExpense(categoryID: "expense.food")
        )
        let unknown = FinancialTransaction.income(
            syntheticIncome(categoryID: "income.future")
        )
        let uncategorized = FinancialTransaction.income(syntheticIncome())

        XCTAssertEqual(model.categoryDisplayName(for: known), "Alimentação")
        XCTAssertEqual(model.categoryDisplayName(for: unknown), "Categoria indisponível")
        XCTAssertEqual(model.categoryDisplayName(for: uncategorized), "Sem categoria")
    }

    func testTypeAndCategoryFiltersCombineWithoutReorderingOrNewNetworkRequests() async {
        let api = FinancialAPISpy()
        let items: [FinancialTransaction] = [
            .income(syntheticIncome(id: "inc_salary", categoryID: "income.salary")),
            .expense(syntheticExpense(id: "exp_none")),
            .expense(syntheticExpense(id: "exp_food", categoryID: "expense.food")),
            .income(syntheticIncome(id: "inc_none"))
        ]
        api.monthResult = .success(TransactionMonth(month: "2026-08", items: items))
        let model = makeModel(api: api)
        await model.load()

        model.selectTypeFilter(.expense)
        XCTAssertEqual(model.filteredTransactions.map(\.id), ["exp_none", "exp_food"])
        model.selectCategoryFilter(.category("expense.food"))
        XCTAssertEqual(model.filteredTransactions.map(\.id), ["exp_food"])
        XCTAssertEqual(api.requestedMonths, ["2026-08"])

        model.selectTypeFilter(.all)
        XCTAssertEqual(model.filteredTransactions.map(\.id), ["exp_food"])
        model.selectCategoryFilter(.uncategorized)
        XCTAssertEqual(model.filteredTransactions.map(\.id), ["exp_none", "inc_none"])
        XCTAssertEqual(api.requestedMonths, ["2026-08"], "Local filters must not request backend data")
    }

    func testIncompatibleTypeFilterResetsCategoryAndOptionsPreserveCatalogOrder() {
        let api = FinancialAPISpy()
        let model = makeModel(api: api)

        XCTAssertEqual(model.availableCategoryDefinitions.map(\.id), syntheticCategories.map(\.id))
        model.selectCategoryFilter(.category("expense.food"))
        model.selectTypeFilter(.income)

        XCTAssertEqual(model.categoryFilter, .all)
        XCTAssertEqual(model.availableCategoryDefinitions.map(\.id), ["income.salary", "income.other"])
        model.selectTypeFilter(.all)
        XCTAssertEqual(model.categoryFilter, .all, "Returning to all must not restore the old Category")
    }

    func testFiltersPersistAcrossMonthNavigationAndExposeFilteredEmptySeparately() async {
        let api = FinancialAPISpy()
        api.monthResult = .success(
            TransactionMonth(
                month: "2026-08",
                items: [.expense(syntheticExpense(categoryID: "expense.food"))]
            )
        )
        let model = makeModel(api: api)
        await model.load()
        model.selectCategoryFilter(.uncategorized)

        XCTAssertFalse(model.transactions.isEmpty)
        XCTAssertTrue(model.filteredTransactions.isEmpty)
        model.showPreviousMonth()
        XCTAssertEqual(model.categoryFilter, .uncategorized)
        XCTAssertEqual(model.typeFilter, .all)
    }

    func testCatalogFailureKeepsHistoryUsableAndRetryRecovers() async {
        let api = FinancialAPISpy()
        api.categoriesResult = .failure(FinancialAPIError.serviceUnavailable)
        let catalog = CategoryCatalogModel(api: api)
        let model = HistoryViewModel(api: api, categories: catalog, now: syntheticAugustDate())

        await model.loadCategoriesIfNeeded()
        guard case .failed = model.categoryCatalogState else {
            return XCTFail("Expected a separate catalog failure")
        }
        XCTAssertEqual(
            model.categoryDisplayName(for: .expense(syntheticExpense(categoryID: "expense.food"))),
            "Categoria indisponível"
        )

        api.categoriesResult = .success(syntheticCategories)
        await model.retryCategories()
        XCTAssertEqual(model.categoryDisplayName(for: .expense(syntheticExpense(categoryID: "expense.food"))), "Alimentação")
    }

    func testLoadMapsErrorsToSafeUIState() async {
        let api = FinancialAPISpy()
        api.monthResult = .failure(FinancialAPIError.connectionUnavailable)
        let model = HistoryViewModel(api: api, now: syntheticAugustDate())

        await model.load()
        XCTAssertEqual(model.state, .failed(FinancialAPIError.connectionUnavailable.userMessage))
    }

    private func syntheticAugustDate() -> Date {
        Calendar.financial.date(from: DateComponents(year: 2026, month: 8, day: 14)) ?? Date(timeIntervalSince1970: 0)
    }

    private func makeModel(api: any FinancialAPI) -> HistoryViewModel {
        HistoryViewModel(
            api: api,
            categories: CategoryCatalogModel(api: api, definitions: syntheticCategories),
            now: syntheticAugustDate()
        )
    }
}
