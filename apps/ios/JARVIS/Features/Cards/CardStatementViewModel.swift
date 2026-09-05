import Foundation
import Observation

enum CardStatementState: Equatable {
    case idle
    case loading
    case loaded(CardStatement)
    case empty(CardStatement)
    case failed(String)
}

@MainActor
@Observable
final class CardStatementViewModel {
    private(set) var state: CardStatementState = .idle

    let cardID: String
    var statementDueOn: RecurrenceCivilDate {
        didSet {
            guard oldValue != statementDueOn else { return }
            loadGeneration &+= 1
            loadTask?.cancel()
            loadTask = nil
            state = .idle
        }
    }

    private let api: any FinancialAPI
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var loadGeneration: UInt64 = 0

    init(cardID: String, statementDueOn: RecurrenceCivilDate, api: any FinancialAPI) {
        self.cardID = cardID
        self.statementDueOn = statementDueOn
        self.api = api
    }

    var statement: CardStatement? {
        switch state {
        case let .loaded(statement), let .empty(statement): statement
        case .idle, .loading, .failed: nil
        }
    }

    var errorMessage: String? {
        guard case let .failed(message) = state else { return nil }
        return message
    }

    func loadIfNeeded() async {
        guard case .idle = state else { return }
        await load()
    }

    func load(forceRefresh: Bool = false) async {
        if forceRefresh {
            loadGeneration &+= 1
            loadTask?.cancel()
            loadTask = nil
        } else if let loadTask {
            await loadTask.value
            return
        }

        loadGeneration &+= 1
        let generation = loadGeneration
        let cardID = self.cardID
        let dueOn = self.statementDueOn
        state = .loading

        let task = Task { @MainActor [weak self] in
            guard let self else { return }
            let result: CardStatementState
            do {
                let statement = try await self.api.cardStatement(
                    creditCardID: cardID,
                    statementDueOn: dueOn
                )
                result = statement.lines.isEmpty ? .empty(statement) : .loaded(statement)
            } catch is CancellationError {
                result = .idle
            } catch {
                result = .failed(self.message(for: error))
            }
            guard generation == self.loadGeneration else { return }
            self.state = result
            self.loadTask = nil
        }
        loadTask = task
        await task.value
    }

    func retry() async {
        guard case .failed = state else { return }
        await load()
    }

    func refresh() async {
        await load(forceRefresh: true)
    }

    func cancel() {
        loadGeneration &+= 1
        loadTask?.cancel()
        loadTask = nil
        state = .idle
    }

    private func message(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível carregar o statement. Tente novamente."
    }
}
