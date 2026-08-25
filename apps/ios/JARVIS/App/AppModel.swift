import Foundation
import Observation

enum CategoryCatalogState: Equatable {
    case idle
    case loading
    case loaded([CategoryDefinition])
    case failed(String)
}

@MainActor
@Observable
final class CategoryCatalogModel {
    private(set) var state: CategoryCatalogState

    private let api: any FinancialAPI
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var loadGeneration: UInt64 = 0

    init(api: any FinancialAPI, definitions: [CategoryDefinition]? = nil) {
        self.api = api
        state = definitions.map(CategoryCatalogState.loaded) ?? .idle
    }

    var definitions: [CategoryDefinition] {
        guard case let .loaded(definitions) = state else { return [] }
        return definitions
    }

    func definitions(for type: TransactionType) -> [CategoryDefinition] {
        definitions.filter { $0.type == type }
    }

    func definition(for id: String) -> CategoryDefinition? {
        definitions.first { $0.id == id }
    }

    func displayName(for categoryID: String?) -> String {
        guard let categoryID else { return "Sem categoria" }
        return definition(for: categoryID)?.displayName ?? "Categoria indisponível"
    }

    func loadIfNeeded() async {
        switch state {
        case .idle:
            await startLoad()
        case .loading:
            await waitForActiveLoad()
        case .loaded, .failed:
            return
        }
    }

    func retry() async {
        switch state {
        case .failed:
            await startLoad()
        case .loading:
            await waitForActiveLoad()
        case .idle, .loaded:
            return
        }
    }

    private func startLoad() async {
        if loadTask != nil {
            await waitForActiveLoad()
            return
        }

        loadGeneration &+= 1
        let generation = loadGeneration
        let task = Task { @MainActor [api] in
            let result: CategoryCatalogState
            do {
                let definitions = try await api.categories()
                guard Set(definitions.map(\.id)).count == definitions.count else {
                    throw FinancialAPIError.invalidResponse
                }
                result = .loaded(definitions)
            } catch {
                let message = (error as? FinancialAPIError)?.userMessage
                    ?? "Não foi possível carregar as categorias. Tente novamente."
                result = .failed(message)
            }
            self.completeLoad(result, generation: generation)
        }

        loadTask = task
        state = .loading
        await task.value
    }

    private func waitForActiveLoad() async {
        guard let loadTask else {
            state = .failed("Não foi possível carregar as categorias. Tente novamente.")
            return
        }
        await loadTask.value
    }

    private func completeLoad(_ result: CategoryCatalogState, generation: UInt64) {
        guard generation == loadGeneration else { return }
        state = result
        loadTask = nil
    }
}

@MainActor
@Observable
final class AppModel {
    let categories: CategoryCatalogModel
    let registration: RegistrationViewModel
    let history: HistoryViewModel
    let recurrences: RecurrencesViewModel
    let recurrenceSuggestions: RecurrenceSuggestionsViewModel

    init(api: any FinancialAPI, now: Date = Date()) {
        let categories = CategoryCatalogModel(api: api)
        self.categories = categories
        let history = HistoryViewModel(api: api, categories: categories, now: now)
        self.history = history
        let recurrenceSuggestions = RecurrenceSuggestionsViewModel(api: api)
        self.recurrenceSuggestions = recurrenceSuggestions
        recurrences = RecurrencesViewModel(
            api: api,
            now: now,
            onRecurrenceConfirmed: { [weak recurrenceSuggestions] suggestionID in
                recurrenceSuggestions?.recurrenceWasConfirmed(suggestionID: suggestionID)
            }
        )
        registration = RegistrationViewModel(
            api: api,
            categories: categories,
            now: now,
            onTransactionRecorded: { [weak history] in
                history?.transactionWasRecorded()
            }
        )
    }
}
