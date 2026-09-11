import Foundation
import Observation

enum HistoryState: Equatable {
    case idle
    case loading
    case loaded([FinancialTransaction])
    case failed(String)
}

enum HistoryComparisonState: Equatable {
    case idle
    case loading
    case loaded(HistoryMonthComparison)
    case unavailable
}

struct HistoryMonthComparison: Equatable, Sendable {
    let month: FinancialMonth
    let income: Int64
    let expense: Int64

    var net: Int64 {
        let (value, overflow) = income.subtractingReportingOverflow(expense)
        if !overflow { return value }
        return income >= 0 ? Int64.max : Int64.min
    }
}

enum HistoryTypeFilter: String, CaseIterable, Identifiable {
    case all
    case expense
    case income

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .all: "Todos"
        case .expense: "Despesas"
        case .income: "Receitas"
        }
    }

    var transactionType: TransactionType? {
        switch self {
        case .all: nil
        case .expense: .expense
        case .income: .income
        }
    }
}

enum HistoryCategoryFilter: Hashable, Identifiable {
    case all
    case uncategorized
    case category(String)

    var id: String {
        switch self {
        case .all: "all"
        case .uncategorized: "uncategorized"
        case let .category(id): "category:\(id)"
        }
    }
}

@MainActor
@Observable
final class HistoryViewModel {
    private(set) var month: FinancialMonth
    private(set) var state: HistoryState = .idle
    private(set) var refreshRevision = 0
    private(set) var typeFilter: HistoryTypeFilter = .all
    private(set) var categoryFilter: HistoryCategoryFilter = .all
    private(set) var comparisonState: HistoryComparisonState = .idle

    private let api: any FinancialAPI
    private let categories: CategoryCatalogModel

    init(
        api: any FinancialAPI,
        categories: CategoryCatalogModel? = nil,
        now: Date = Date()
    ) {
        self.api = api
        self.categories = categories ?? CategoryCatalogModel(api: api)
        month = FinancialMonth(date: now)
    }

    var categoryCatalogState: CategoryCatalogState {
        categories.state
    }

    var availableCategoryDefinitions: [CategoryDefinition] {
        guard let type = typeFilter.transactionType else { return categories.definitions }
        return categories.definitions(for: type)
    }

    var transactions: [FinancialTransaction] {
        guard case let .loaded(items) = state else { return [] }
        return items
    }

    var filteredTransactions: [FinancialTransaction] {
        transactions.filter { transaction in
            let matchesType = typeFilter.transactionType.map { $0 == transaction.type } ?? true
            let matchesCategory = switch categoryFilter {
            case .all:
                true
            case .uncategorized:
                transaction.categoryID == nil
            case let .category(categoryID):
                transaction.categoryID == categoryID
            }
            return matchesType && matchesCategory
        }
    }

    var categoryFilterDisplayName: String {
        switch categoryFilter {
        case .all: "Todas as categorias"
        case .uncategorized: "Sem categoria"
        case let .category(id): categories.displayName(for: id)
        }
    }

    func categoryDisplayName(for transaction: FinancialTransaction) -> String {
        categories.displayName(for: transaction.categoryID)
    }

    func loadCategoriesIfNeeded() async {
        await categories.loadIfNeeded()
    }

    func retryCategories() async {
        await categories.retry()
    }

    func selectTypeFilter(_ filter: HistoryTypeFilter) {
        guard filter != typeFilter else { return }
        typeFilter = filter

        guard case let .category(categoryID) = categoryFilter,
              let selectedType = categories.definition(for: categoryID)?.type,
              let requiredType = filter.transactionType,
              selectedType != requiredType
        else { return }
        categoryFilter = .all
    }

    func selectCategoryFilter(_ filter: HistoryCategoryFilter) {
        guard filter != categoryFilter else { return }
        if case let .category(categoryID) = filter {
            guard let definition = categories.definition(for: categoryID) else { return }
            if let type = typeFilter.transactionType, definition.type != type { return }
        }
        categoryFilter = filter
    }

    func load() async {
        state = .loading
        comparisonState = .idle
        do {
            let response = try await api.transactions(month: month.apiValue)
            state = .loaded(response.items)
        } catch is CancellationError {
            state = .idle
        } catch {
            let message = (error as? FinancialAPIError)?.userMessage
                ?? "Não foi possível carregar o histórico. Tente novamente."
            state = .failed(message)
        }
    }

    /// Loads the immediately preceding civil month for a read-only dashboard
    /// comparison. A comparison failure never hides a successfully loaded
    /// current month; it is represented explicitly as unavailable.
    func loadComparison() async {
        guard case .loaded = state else {
            comparisonState = .unavailable
            return
        }

        let requestedMonth = month
        let comparisonMonth = month.adding(months: -1)
        comparisonState = .loading

        do {
            let response = try await api.transactions(month: comparisonMonth.apiValue)
            guard requestedMonth == month else { return }
            comparisonState = .loaded(
                HistoryMonthComparison(
                    month: comparisonMonth,
                    income: Self.totalIncome(in: response.items),
                    expense: Self.totalExpense(in: response.items)
                )
            )
        } catch is CancellationError {
            guard requestedMonth == month else { return }
            comparisonState = .unavailable
        } catch {
            guard requestedMonth == month else { return }
            comparisonState = .unavailable
        }
    }

    func showPreviousMonth() {
        month = month.adding(months: -1)
        refreshRevision += 1
    }

    func showNextMonth() {
        month = month.adding(months: 1)
        refreshRevision += 1
    }

    func retry() {
        refreshRevision += 1
    }

    func transactionWasRecorded() {
        refreshRevision += 1
    }

    private static func totalIncome(in transactions: [FinancialTransaction]) -> Int64 {
        saturatingSum(transactions.compactMap { transaction in
            if case let .income(income) = transaction { return income.amount.minor }
            return nil
        })
    }

    private static func totalExpense(in transactions: [FinancialTransaction]) -> Int64 {
        saturatingSum(transactions.compactMap { transaction in
            if case let .expense(expense) = transaction { return expense.amount.minor }
            return nil
        })
    }

    private static func saturatingSum(_ values: [Int64]) -> Int64 {
        values.reduce(into: Int64(0)) { result, value in
            let (next, overflow) = result.addingReportingOverflow(value)
            result = overflow ? (value >= 0 ? Int64.max : Int64.min) : next
        }
    }
}
