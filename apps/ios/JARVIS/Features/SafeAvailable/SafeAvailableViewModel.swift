import Foundation
import Observation

enum SafeAvailableState: Equatable {
    case idle
    case loading
    case loaded(SafeAvailableResponse)
    case empty(SafeAvailableResponse)
    case failed(String)
}

@MainActor
@Observable
final class SafeAvailableViewModel {
    private(set) var state: SafeAvailableState = .idle

    var periodStart: RecurrenceCivilDate {
        didSet { periodChanged(from: oldValue, to: periodStart) }
    }

    var periodEnd: RecurrenceCivilDate {
        didSet { periodChanged(from: oldValue, to: periodEnd) }
    }

    private let api: any FinancialAPI
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var loadGeneration: UInt64 = 0

    init(
        api: any FinancialAPI,
        periodStart: RecurrenceCivilDate? = nil,
        periodEnd: RecurrenceCivilDate? = nil,
        now: Date = Date()
    ) {
        let current = Calendar.financial.dateComponents([.year, .month], from: now)
        let year = current.year ?? 2026
        let month = current.month ?? 1
        let defaultStart = (try? RecurrenceCivilDate(year: year, month: month, day: 1))
            ?? (try! RecurrenceCivilDate("2026-01-01"))
        let defaultEnd = (try? RecurrenceCivilDate(year: year, month: month, day: Self.daysInMonth(year: year, month: month)))
            ?? (try! RecurrenceCivilDate("2026-01-31"))
        self.periodStart = periodStart ?? defaultStart
        self.periodEnd = periodEnd ?? defaultEnd
        self.api = api
    }

    var response: SafeAvailableResponse? {
        switch state {
        case let .loaded(response), let .empty(response): response
        case .idle, .loading, .failed: nil
        }
    }

    var errorMessage: String? {
        guard case let .failed(message) = state else { return nil }
        return message
    }

    var isEmpty: Bool {
        guard let response else { return false }
        return response.breakdown.allSatisfy { $0.kind == .availableBalance }
    }

    var periodStartPickerDate: Date { periodStart.pickerDate }
    var periodEndPickerDate: Date { periodEnd.pickerDate }

    func setPeriodStart(_ date: Date) {
        guard let value = try? RecurrenceCivilDate(pickerDate: date) else { return }
        periodStart = value
    }

    func setPeriodEnd(_ date: Date) {
        guard let value = try? RecurrenceCivilDate(pickerDate: date) else { return }
        periodEnd = value
    }

    func loadIfNeeded() async {
        guard case .idle = state else { return }
        await load()
    }

    func load(forceRefresh: Bool = false) async {
        guard !(periodEnd < periodStart) else {
            state = .failed(FinancialAPIError.invalidData.userMessage)
            return
        }
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
        let start = periodStart
        let end = periodEnd
        let task = Task { @MainActor [weak self] in
            guard let self else { return }
            let result: SafeAvailableState
            do {
                let response = try await self.api.safeAvailable(periodStart: start, periodEnd: end)
                result = response.breakdown.allSatisfy { $0.kind == .availableBalance }
                    ? .empty(response)
                    : .loaded(response)
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

    func cancel() {
        loadGeneration &+= 1
        loadTask?.cancel()
        loadTask = nil
        state = .idle
    }

    private func periodChanged(from oldValue: RecurrenceCivilDate, to newValue: RecurrenceCivilDate) {
        guard oldValue != newValue else { return }
        loadGeneration &+= 1
        loadTask?.cancel()
        loadTask = nil
        state = .idle
    }

    private func message(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível carregar o Disponível Seguro. Tente novamente."
    }

    private static func daysInMonth(year: Int, month: Int) -> Int {
        let calendar = Calendar.financial
        let date = calendar.date(from: DateComponents(year: year, month: month, day: 1))!
        return calendar.range(of: .day, in: .month, for: date)!.count
    }

}
