import Foundation
import Observation

enum SafeAvailableState: Equatable {
    case idle
    case loading
    case loaded(SafeAvailableResponse)
    case empty(SafeAvailableResponse)
    case failed(String)
}

enum MonthlyBudgetState: Equatable {
    case idle
    case loading
    case absent
    case loaded(MonthlyBudget)
    case saving
    case failed(String)
}

@MainActor
@Observable
final class SafeAvailableViewModel {
    private(set) var state: SafeAvailableState = .idle
    private(set) var budgetState: MonthlyBudgetState = .idle
    var budgetAmountText: String = ""

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

    func loadBudgetIfNeeded() async {
        guard case .idle = budgetState else { return }
        await loadBudget()
    }

    func loadBudget() async {
        guard let month = budgetMonth else {
            budgetState = .absent
            return
        }
        budgetState = .loading
        do {
            let budget = try await api.monthlyBudget(month: month)
            budgetAmountText = Self.displayBudgetMinor(budget.amount.minor)
            budgetState = .loaded(budget)
        } catch is CancellationError {
            budgetState = .idle
        } catch let error as FinancialAPIError where error == .monthlyBudgetNotFound {
            budgetAmountText = ""
            budgetState = .absent
        } catch {
            budgetState = .failed(message(for: error))
        }
    }

    func saveBudget() async {
        guard let month = budgetMonth, let minor = Self.parseBudgetMinor(budgetAmountText) else {
            budgetState = .failed(FinancialAPIError.invalidData.userMessage)
            return
        }
        guard let amount = try? MonthlyBudgetAmount(minor: minor) else {
            budgetState = .failed(FinancialAPIError.invalidData.userMessage)
            return
        }
        budgetState = .saving
        do {
            let budget = try await api.replaceMonthlyBudget(month: month, amount: amount)
            budgetAmountText = Self.displayBudgetMinor(budget.amount.minor)
            budgetState = .loaded(budget)
            await load(forceRefresh: true)
        } catch is CancellationError {
            budgetState = .idle
        } catch {
            budgetState = .failed(message(for: error))
        }
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
        budgetState = .idle
    }

    private func message(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível carregar o Disponível Seguro. Tente novamente."
    }

    private var budgetMonth: String? {
        let start = periodStart.canonicalValue
        let end = periodEnd.canonicalValue
        guard start.count >= 7, end.count >= 7, start.prefix(7) == end.prefix(7) else { return nil }
        return String(start.prefix(7))
    }

    private static func parseBudgetMinor(_ input: String) -> Int64? {
        let value = input.trimmingCharacters(in: .whitespacesAndNewlines).replacingOccurrences(of: ",", with: ".")
        guard !value.isEmpty, !value.contains("-"), !value.contains("+") else { return nil }
        let parts = value.split(separator: ".", omittingEmptySubsequences: false)
		guard parts.count <= 2, let whole = parts.first, !whole.isEmpty, whole.allSatisfy({ $0.isNumber }) else { return nil }
		let fraction = parts.count == 2 ? parts[1] : Substring()
		guard fraction.count <= 2, fraction.allSatisfy({ $0.isNumber }) else { return nil }
        guard let wholeMinor = Int64(whole), wholeMinor <= (Int64.max / 100) else { return nil }
        let fractionMinor: Int64
        switch fraction.count {
        case 0: fractionMinor = 0
        case 1: fractionMinor = Int64(fraction)! * 10
        case 2: fractionMinor = Int64(fraction)!
        default: return nil
        }
        let (scaled, overflow) = wholeMinor.multipliedReportingOverflow(by: 100)
        guard !overflow else { return nil }
        let (minor, addOverflow) = scaled.addingReportingOverflow(fractionMinor)
        return addOverflow ? nil : minor
    }

    private static func displayBudgetMinor(_ minor: Int64) -> String {
        let whole = minor / 100
        let fraction = String(format: "%02lld", minor % 100)
        return "\(whole),\(fraction)"
    }

    private static func daysInMonth(year: Int, month: Int) -> Int {
        let calendar = Calendar.financial
        let date = calendar.date(from: DateComponents(year: year, month: month, day: 1))!
        return calendar.range(of: .day, in: .month, for: date)!.count
    }

}
