import Foundation
import Observation

enum HistoryState: Equatable {
    case idle
    case loading
    case loaded([FinancialTransaction])
    case failed(String)
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
}
