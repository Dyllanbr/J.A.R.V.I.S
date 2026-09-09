import Foundation
import Observation

enum PurchaseSimulationState: Equatable {
    case idle
    case loading
    case loaded(PurchaseSimulationResponse)
    case failed(String)
}

@MainActor
@Observable
final class PurchaseSimulationViewModel {
    private(set) var state: PurchaseSimulationState = .idle
    private(set) var errorMessage: String?

    var creditCardID: String?
    var creditCardName: String = ""
    var amountText = ""
    var purchaseOn: RecurrenceCivilDate
    var purchaseMode: PurchaseSimulationMode = .oneTime
    var installmentCountText = ""
    var periodStart: RecurrenceCivilDate
    var periodEnd: RecurrenceCivilDate

    private let api: any FinancialAPI
    private let moneyParser: BRLMoneyParser
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored private var generation: UInt64 = 0

    init(api: any FinancialAPI, now: Date = Date(), moneyParser: BRLMoneyParser = BRLMoneyParser()) {
        self.api = api
        self.moneyParser = moneyParser
        let calendar = Calendar.financial
        let components = calendar.dateComponents([.year, .month, .day], from: now)
        let year = components.year ?? 2026
        let month = components.month ?? 1
        let day = components.day ?? 1
        let start = (try? RecurrenceCivilDate(year: year, month: month, day: 1)) ?? (try! RecurrenceCivilDate("2026-01-01"))
        // Use the civil-date calendar for month boundaries. Calendar.financial's
        // local timezone can represent UTC midnight as the previous civil day.
        let endDay = RecurrenceCivilDate.pickerCalendar
            .range(of: .day, in: .month, for: start.pickerDate)?.count ?? 31
        let end = (try? RecurrenceCivilDate(year: year, month: month, day: endDay)) ?? (try! RecurrenceCivilDate("2026-01-31"))
        purchaseOn = (try? RecurrenceCivilDate(year: year, month: month, day: day)) ?? start
        periodStart = start
        periodEnd = end
    }

    var response: PurchaseSimulationResponse? {
        guard case let .loaded(response) = state else { return nil }
        return response
    }

    var isBusy: Bool { state == .loading }

    func configure(card: CreditCard) {
        guard creditCardID != card.id else {
            creditCardName = card.name
            return
        }
        cancel()
        creditCardID = card.id
        creditCardName = card.name
        state = .idle
        errorMessage = nil
    }

    func setPurchaseOn(_ date: Date) {
        if let value = try? RecurrenceCivilDate(pickerDate: date) { purchaseOn = value }
    }

    func setPeriodStart(_ date: Date) {
        if let value = try? RecurrenceCivilDate(pickerDate: date) { periodStart = value }
    }

    func setPeriodEnd(_ date: Date) {
        if let value = try? RecurrenceCivilDate(pickerDate: date) { periodEnd = value }
    }

    func simulate() async {
        guard task == nil else { return }
        guard let creditCardID, CreditCard.isValidID(creditCardID),
              let minor = try? moneyParser.parseMinorUnits(amountText), minor > 0,
              !(periodEnd < periodStart)
        else {
            state = .failed(FinancialAPIError.invalidData.userMessage)
            return
        }
        let amount = FinancialMoney(minor: minor, currency: .brl)
        guard let request = try? PurchaseSimulationRequest(
            creditCardID: creditCardID, amount: amount, purchaseOn: purchaseOn,
            purchaseMode: purchaseMode,
            installmentCount: installmentCount,
            periodStart: periodStart, periodEnd: periodEnd
        ) else {
            state = .failed(FinancialAPIError.invalidData.userMessage)
            return
        }
        generation &+= 1
        let currentGeneration = generation
        errorMessage = nil
        state = .loading
        let activeTask = Task { @MainActor [weak self] in
            guard let self else { return }
            do {
                let response = try await self.api.simulatePurchase(request)
                guard currentGeneration == self.generation else { return }
                self.state = .loaded(response)
            } catch is CancellationError {
                guard currentGeneration == self.generation else { return }
                self.state = .idle
            } catch {
                guard currentGeneration == self.generation else { return }
                self.state = .failed(self.message(for: error))
            }
            self.task = nil
        }
        task = activeTask
        await activeTask.value
    }

    func retry() async {
        guard case .failed = state else { return }
        await simulate()
    }

    func cancel() {
        generation &+= 1
        task?.cancel()
        task = nil
        state = .idle
    }

    private var installmentCount: Int? {
        let value = installmentCountText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return nil }
        return Int(value)
    }

    private func message(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage ?? "Não foi possível simular a compra. Tente novamente."
    }
}
