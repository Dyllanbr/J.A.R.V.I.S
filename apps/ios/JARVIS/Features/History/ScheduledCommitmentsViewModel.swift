import Foundation
import Observation

enum ScheduledCommitmentsState: Equatable {
    case idle
    case loading
    case loaded([ScheduledCommitment])
    case empty
    case failed(String)
}

@MainActor
@Observable
final class ScheduledCommitmentsViewModel {
    private(set) var state: ScheduledCommitmentsState = .idle

    private let api: any FinancialAPI
    private let now: () -> Date
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var loadGeneration: UInt64 = 0

    init(api: any FinancialAPI, now: @escaping () -> Date = { Date() }) {
        self.api = api
        self.now = now
    }

    var items: [ScheduledCommitment] {
        switch state {
        case let .loaded(items): items
        case .idle, .loading, .empty, .failed: []
        }
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
        let task = Task { @MainActor in
            let result: ScheduledCommitmentsState
            do {
                let evaluationDate = try self.evaluationDate()
                let response = try await self.api.scheduledCommitments(evaluationDate: evaluationDate)
                result = response.items.isEmpty ? .empty : .loaded(response.items)
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

    func refresh() async {
        await load(forceRefresh: true)
    }

    private func evaluationDate() throws -> RecurrenceCivilDate {
        let components = Calendar.financial.dateComponents([.year, .month, .day], from: now())
        guard let year = components.year, let month = components.month, let day = components.day else {
            throw FinancialAPIError.invalidData
        }
        return try RecurrenceCivilDate(year: year, month: month, day: day)
    }

    private func message(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível carregar os compromissos futuros. Tente novamente."
    }
}
