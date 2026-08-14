import Foundation
import Observation

enum HistoryState: Equatable {
    case idle
    case loading
    case loaded([Expense])
    case failed(String)
}

@MainActor
@Observable
final class HistoryViewModel {
    private(set) var month: FinancialMonth
    private(set) var state: HistoryState = .idle
    private(set) var refreshRevision = 0

    private let api: any FinancialAPI

    init(api: any FinancialAPI, now: Date = Date()) {
        self.api = api
        month = FinancialMonth(date: now)
    }

    func load() async {
        state = .loading
        do {
            let response = try await api.expenses(month: month.apiValue)
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

    func expenseWasRecorded() {
        refreshRevision += 1
    }
}
