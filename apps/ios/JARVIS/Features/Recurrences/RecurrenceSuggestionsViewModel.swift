import Foundation
import Observation

enum RecurrenceSuggestionListState: Equatable {
    case idle
    case loading
    case loaded([RecurrenceSuggestion])
    case failed(String)
}

@MainActor
@Observable
final class RecurrenceSuggestionsViewModel {
    private(set) var listState: RecurrenceSuggestionListState = .idle
    private(set) var dismissalConfirmation: RecurrenceSuggestion?
    private(set) var dismissingIDs: Set<String> = []
    private(set) var previewingIDs: Set<String> = []
    private(set) var actionErrors: [String: String] = [:]
    private(set) var noticeMessage: String?

    private let api: any FinancialAPI
    @ObservationIgnored private var loadGeneration: UInt64 = 0
    @ObservationIgnored private var completedLoadGeneration: UInt64 = 0
    @ObservationIgnored private var listRevision: UInt64 = 0
    @ObservationIgnored private var activeLoadRevision: UInt64?
    @ObservationIgnored private var needsReconciliationAfterLoad = false
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var dismissTasks: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var previewTasks: [String: Task<RecurrencePreview?, Never>] = [:]

    init(api: any FinancialAPI) {
        self.api = api
    }

    var suggestions: [RecurrenceSuggestion] {
        guard case let .loaded(items) = listState else { return [] }
        return items
    }

    func loadIfNeeded() async {
        switch listState {
        case .idle:
            await startLoad()
        case .loading:
            await waitForActiveLoad()
        case .loaded, .failed:
            return
        }
    }

    func retryList() async {
        guard case .failed = listState else { return }
        await startLoad()
    }

    func refresh() async {
        await reconcileAfterCurrentLoad()
    }

    func requestDismissal(_ suggestion: RecurrenceSuggestion) {
        guard !dismissingIDs.contains(suggestion.id), !previewingIDs.contains(suggestion.id) else { return }
        dismissalConfirmation = suggestion
    }

    func cancelDismissal() {
        dismissalConfirmation = nil
    }

    func confirmDismissal() async {
        guard let suggestion = dismissalConfirmation else { return }
        await confirmDismissal(suggestion)
    }

    func confirmDismissal(_ suggestion: RecurrenceSuggestion) async {
        dismissalConfirmation = nil
        await dismiss(suggestion)
    }

    func prepareForReview(_ suggestion: RecurrenceSuggestion) async -> RecurrencePreview? {
        guard suggestions.contains(where: { $0.id == suggestion.id }),
              !dismissingIDs.contains(suggestion.id)
        else { return nil }
        if let task = previewTasks[suggestion.id] {
            _ = await task.value
            return nil
        }

        actionErrors[suggestion.id] = nil
        previewingIDs.insert(suggestion.id)
        let task = Task<RecurrencePreview?, Never> { @MainActor [api] in
            do {
                return try await api.previewRecurrenceSuggestion(id: suggestion.id)
            } catch is CancellationError {
                self.actionErrors[suggestion.id] = "A preparação foi interrompida. Tente novamente."
            } catch let error as FinancialAPIError
                where error == .suggestionNotFound || error == .suggestionSuppressed
            {
                self.installRemoval(id: suggestion.id)
                self.noticeMessage = error.userMessage
                await self.reconcileCurrentRevision()
            } catch {
                self.actionErrors[suggestion.id] = self.userMessage(for: error)
            }
            return nil
        }
        previewTasks[suggestion.id] = task
        let preview = await task.value
        previewTasks[suggestion.id] = nil
        previewingIDs.remove(suggestion.id)
        return preview
    }

    func recurrenceWasConfirmed(suggestionID: String?) {
        if let suggestionID {
            installRemoval(id: suggestionID)
        }
        Task { await reconcileAfterCurrentLoad() }
    }

    func clearNotice() {
        noticeMessage = nil
    }

    private func dismiss(_ suggestion: RecurrenceSuggestion) async {
        if let task = dismissTasks[suggestion.id] {
            await task.value
            return
        }
        guard suggestions.contains(where: { $0.id == suggestion.id }) else { return }

        actionErrors[suggestion.id] = nil
        dismissingIDs.insert(suggestion.id)
        let task = Task { @MainActor [api] in
            do {
                _ = try await api.dismissRecurrenceSuggestion(id: suggestion.id)
                self.installRemoval(id: suggestion.id)
                await self.reconcileCurrentRevision()
            } catch is CancellationError {
                self.actionErrors[suggestion.id] = "O descarte foi interrompido. Tente novamente."
            } catch let error as FinancialAPIError where error == .suggestionNotFound {
                self.installRemoval(id: suggestion.id)
                self.noticeMessage = error.userMessage
                await self.reconcileCurrentRevision()
            } catch {
                self.actionErrors[suggestion.id] = self.userMessage(for: error)
            }
        }
        dismissTasks[suggestion.id] = task
        await task.value
        dismissTasks[suggestion.id] = nil
        dismissingIDs.remove(suggestion.id)
    }

    private func startLoad() async {
        let task = beginLoadIfNeeded()
        await task.value
    }

    private func beginLoadIfNeeded() -> Task<Void, Never> {
        if let loadTask { return loadTask }

        loadGeneration &+= 1
        let generation = loadGeneration
        let revision = listRevision
        let task = Task { @MainActor [api] in
            let result: RecurrenceSuggestionListState
            do {
                result = .loaded(try await api.recurrenceSuggestions().items)
            } catch {
                result = .failed(
                    (error as? FinancialAPIError)?.userMessage
                        ?? "Não foi possível carregar as sugestões. Tente novamente."
                )
            }
            self.completeLoad(result, generation: generation, revision: revision)
        }
        loadTask = task
        activeLoadRevision = revision
        if case .loaded = listState {
            // Preserve visible suggestions while a reconciliation is in flight.
        } else {
            listState = .loading
        }
        return task
    }

    private func waitForActiveLoad() async {
        guard let loadTask else {
            listState = .failed("Não foi possível carregar as sugestões. Tente novamente.")
            return
        }
        await loadTask.value
    }

    private func completeLoad(
        _ result: RecurrenceSuggestionListState,
        generation: UInt64,
        revision: UInt64
    ) {
        guard generation == loadGeneration else { return }

        let reconcileAgain = needsReconciliationAfterLoad
        needsReconciliationAfterLoad = false
        if revision == listRevision {
            listState = result
            if case let .loaded(items) = result {
                let currentIDs = Set(items.map(\.id))
                actionErrors = actionErrors.filter { currentIDs.contains($0.key) }
            }
        }
        loadTask = nil
        activeLoadRevision = nil
        completedLoadGeneration = generation

        if reconcileAgain {
            _ = beginLoadIfNeeded()
        }
    }

    private func reconcileAfterCurrentLoad() async {
        let generationBeforeRequest = loadGeneration
        if loadTask != nil {
            needsReconciliationAfterLoad = true
        } else {
            _ = beginLoadIfNeeded()
        }

        while completedLoadGeneration <= generationBeforeRequest {
            guard let loadTask else {
                _ = beginLoadIfNeeded()
                continue
            }
            await loadTask.value
        }
    }

    private func reconcileCurrentRevision() async {
        if let loadTask {
            if activeLoadRevision != listRevision {
                needsReconciliationAfterLoad = true
            }
            await loadTask.value
            return
        }
        await startLoad()
    }

    private func installRemoval(id: String) {
        guard suggestions.contains(where: { $0.id == id }) else { return }
        listRevision &+= 1
        listState = .loaded(suggestions.filter { $0.id != id })
        actionErrors[id] = nil
        if loadTask != nil {
            needsReconciliationAfterLoad = true
        }
    }

    private func userMessage(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível concluir a operação. Tente novamente."
    }
}
