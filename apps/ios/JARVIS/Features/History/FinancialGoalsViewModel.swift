import Foundation
import Observation

enum FinancialGoalsState: Equatable {
    case idle
    case loading
    case empty
    case loaded(FinancialGoalsResponse)
    case saving
    case failed(String)
}

@MainActor
@Observable
final class FinancialGoalsViewModel {
    private(set) var state: FinancialGoalsState = .idle

    private let api: any FinancialAPI
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var loadGeneration: UInt64 = 0

    init(api: any FinancialAPI) {
        self.api = api
    }

    var response: FinancialGoalsResponse? {
        if case let .loaded(response) = state { return response }
        return nil
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
        let task = Task { @MainActor [weak self] in
            guard let self else { return }
            let result: FinancialGoalsState
            do {
                let response = try await api.financialGoals()
                result = response.goals.isEmpty && response.protectedValues.isEmpty ? .empty : .loaded(response)
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
        state = .loading
        await task.value
    }

    func retry() async {
        guard case .failed = state else { return }
        await load()
    }

    func replaceGoal(id: String, title: String, targetAmount: FinancialGoalAmount) async {
        state = .saving
        do {
            _ = try await api.replaceFinancialGoal(id: id, title: title, targetAmount: targetAmount)
            await load(forceRefresh: true)
        } catch is CancellationError {
            state = .idle
        } catch {
            state = .failed(self.message(for: error))
        }
    }

    func replaceProtectedValue(id: String, label: String, amount: ProtectedValueAmount) async {
        state = .saving
        do {
            _ = try await api.replaceProtectedValue(id: id, label: label, amount: amount)
            await load(forceRefresh: true)
        } catch is CancellationError {
            state = .idle
        } catch {
            state = .failed(self.message(for: error))
        }
    }

    func cancel() {
        loadGeneration &+= 1
        loadTask?.cancel()
        loadTask = nil
        state = .idle
    }

    private func message(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível carregar suas metas financeiras. Tente novamente."
    }
}
