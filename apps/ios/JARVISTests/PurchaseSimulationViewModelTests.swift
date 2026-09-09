import Foundation
import XCTest
@testable import JARVIS

@MainActor
final class PurchaseSimulationViewModelTests: XCTestCase {
    func testSimulatesOneTimePurchaseAndPublishesProjectedResult() async throws {
        let response = try Self.response()
        let api = PurchaseSimulationAPISpy(result: .success(response))
        let model = PurchaseSimulationViewModel(api: api, now: Self.now)
        model.configure(card: Self.card)
        model.amountText = "12,00"

        await model.simulate()

        XCTAssertEqual(model.response, response)
        XCTAssertEqual(api.callCount, 1)
        XCTAssertEqual(api.requests.first?.creditCardID, Self.card.id)
        XCTAssertEqual(api.requests.first?.amount.minor, 1_200)
    }

    func testInvalidInputIsRejectedBeforeReader() async throws {
        let api = PurchaseSimulationAPISpy(result: .success(try Self.response()))
        let model = PurchaseSimulationViewModel(api: api, now: Self.now)
        model.configure(card: Self.card)
        model.amountText = "0,00"

        await model.simulate()

        XCTAssertEqual(model.state, .failed(FinancialAPIError.invalidData.userMessage))
        XCTAssertEqual(api.callCount, 0)
    }

    func testFailureCanBeRetriedWithoutChangingRequest() async throws {
        let response = try Self.response()
        let api = PurchaseSimulationAPISpy(result: .failure(FinancialAPIError.serviceUnavailable))
        let model = PurchaseSimulationViewModel(api: api, now: Self.now)
        model.configure(card: Self.card)
        model.amountText = "12,00"

        await model.simulate()
        XCTAssertEqual(model.state, .failed(FinancialAPIError.serviceUnavailable.userMessage))

        api.result = .success(response)
        await model.retry()

        XCTAssertEqual(model.response, response)
        XCTAssertEqual(api.callCount, 2)
        XCTAssertEqual(api.requests.map(\.amount.minor), [1_200, 1_200])
    }

    func testConcurrentSimulationUsesSingleFlight() async throws {
        let response = try Self.response()
        let api = PurchaseSimulationAPISpy(result: .success(response), yieldsBeforeResult: true)
        let model = PurchaseSimulationViewModel(api: api, now: Self.now)
        model.configure(card: Self.card)
        model.amountText = "12,00"

        let first = Task { await model.simulate() }
        await Task.yield()
        let second = Task { await model.simulate() }
        await first.value
        await second.value

        XCTAssertEqual(api.callCount, 1)
        XCTAssertEqual(model.response, response)
    }

    func testCancellationReturnsToIdle() async throws {
        let api = PurchaseSimulationAPISpy(result: .failure(CancellationError()))
        let model = PurchaseSimulationViewModel(api: api, now: Self.now)
        model.configure(card: Self.card)
        model.amountText = "12,00"

        await model.simulate()

        XCTAssertEqual(model.state, .idle)
    }

    private static let now = Date(timeIntervalSince1970: 1_757_462_400)

    private static var card: CreditCard {
        CreditCard(
            id: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name: "Cartão beta", lastFour: "1234", brand: .visa,
            closingDay: 1, dueDay: 10, creditLimit: nil, status: .active,
            createdAt: "2026-09-01T12:00:00Z", archivedAt: nil
        )
    }

    private static func response() throws -> PurchaseSimulationResponse {
        let start = try RecurrenceCivilDate("2026-09-01")
        let end = try RecurrenceCivilDate("2026-09-30")
        let balance = try SafeAvailableAmount(minor: 10_000)
        let empty = try SafeAvailableAmount(minor: 0)
        let projectedCommitments = try SafeAvailableAmount(minor: 1_200)
        let projectedFinal = try SafeAvailableAmount(minor: 8_800)
        let baseline = try SafeAvailableResponse(
            periodStart: start, periodEnd: end, availableBalance: balance,
            totalConfirmedIncome: empty, totalConfirmedExpense: empty,
            totalConfirmedCommitments: empty, finalAmount: balance,
            breakdown: [SafeAvailableBreakdown(
                kind: .availableBalance, sourceID: "available-balance", sequence: 0,
                dueOn: start, amount: balance
            )], missingData: [.budget]
        )
        let line = try SafeAvailableBreakdown(
            kind: .commitment, sourceID: "sim_ui_synthetic", sequence: 1,
            dueOn: start, amount: projectedCommitments
        )
        let projected = try SafeAvailableResponse(
            periodStart: start, periodEnd: end, availableBalance: balance,
            totalConfirmedIncome: empty, totalConfirmedExpense: empty,
            totalConfirmedCommitments: projectedCommitments, finalAmount: projectedFinal,
            breakdown: [baseline.breakdown[0], line], missingData: [.budget]
        )
        return try PurchaseSimulationResponse(
            creditCardID: card.id, purchaseOn: try RecurrenceCivilDate("2026-09-10"),
            purchaseMode: .oneTime, periodStart: start, periodEnd: end,
            baseline: baseline, projected: projected,
            impact: try SafeAvailableAmount(minor: -1_200), hypotheticalCommitments: [line],
            assumptions: [.notPersisted, .noExpenseCreated]
        )
    }
}

@MainActor
private final class PurchaseSimulationAPISpy: FinancialAPI {
    var result: Result<PurchaseSimulationResponse, Error>
    let yieldsBeforeResult: Bool
    private(set) var callCount = 0
    private(set) var requests: [PurchaseSimulationRequest] = []

    init(result: Result<PurchaseSimulationResponse, Error>, yieldsBeforeResult: Bool = false) {
        self.result = result
        self.yieldsBeforeResult = yieldsBeforeResult
    }

    func simulatePurchase(_ request: PurchaseSimulationRequest) async throws -> PurchaseSimulationResponse {
        callCount += 1
        requests.append(request)
        if yieldsBeforeResult { await Task.yield() }
        return try result.get()
    }

    func categories() async throws -> [CategoryDefinition] { throw FinancialAPIError.configuration }
    func preview(_: ExpenseRequest) async throws -> ExpensePreview { throw FinancialAPIError.configuration }
    func preview(_: IncomeRequest) async throws -> IncomePreview { throw FinancialAPIError.configuration }
    func create(_: ExpenseRequest, idempotencyKey _: String) async throws -> RecordedExpense { throw FinancialAPIError.configuration }
    func create(_: IncomeRequest, idempotencyKey _: String) async throws -> RecordedIncome { throw FinancialAPIError.configuration }
    func transactions(month _: String) async throws -> TransactionMonth { throw FinancialAPIError.configuration }
    func previewRecurrence(_: RecurrenceRequest) async throws -> RecurrencePreview { throw FinancialAPIError.configuration }
    func createRecurrence(_: RecurrenceRequest, idempotencyKey _: String) async throws -> RecordedRecurrence { throw FinancialAPIError.configuration }
    func recurrences() async throws -> RecurrenceList { throw FinancialAPIError.configuration }
    func cancelRecurrence(id _: String, idempotencyKey _: String) async throws -> RecordedRecurrence { throw FinancialAPIError.configuration }
    func recurrenceSuggestions() async throws -> RecurrenceSuggestionList { throw FinancialAPIError.configuration }
    func dismissRecurrenceSuggestion(id _: String) async throws -> DismissedRecurrenceSuggestion { throw FinancialAPIError.configuration }
    func previewRecurrenceSuggestion(id _: String) async throws -> RecurrencePreview { throw FinancialAPIError.configuration }
    func previewCreditCard(_: CreditCardRequest) async throws -> CreditCardPreview { throw FinancialAPIError.configuration }
    func createCreditCard(_: CreditCardRequest, idempotencyKey _: String) async throws -> RecordedCreditCard { throw FinancialAPIError.configuration }
    func creditCards() async throws -> CreditCardList { throw FinancialAPIError.configuration }
    func creditCard(id _: String) async throws -> CreditCard { throw FinancialAPIError.configuration }
    func archiveCreditCard(id _: String, idempotencyKey _: String) async throws -> RecordedCreditCard { throw FinancialAPIError.configuration }
    func previewCardPurchase(_: CardPurchasePreviewRequest) async throws -> CardPurchasePreview { throw FinancialAPIError.configuration }
    func createCardPurchase(_: CardPurchaseCreateRequest, idempotencyKey _: String) async throws -> RecordedCardPurchase { throw FinancialAPIError.configuration }
    func installmentPlans() async throws -> InstallmentPlanListResponse { throw FinancialAPIError.configuration }
    func installmentPlan(id _: String) async throws -> InstallmentPlan { throw FinancialAPIError.configuration }
    func previewInstallmentPlanCancellation(id _: String) async throws -> InstallmentPlanCancellationPreview { throw FinancialAPIError.configuration }
    func cancelInstallmentPlan(id _: String, expectedCancelledOn _: RecurrenceCivilDate, idempotencyKey _: String) async throws -> RecordedInstallmentPlan { throw FinancialAPIError.configuration }
    func scheduledCommitments(evaluationDate _: RecurrenceCivilDate) async throws -> ScheduledCommitmentListResponse { throw FinancialAPIError.configuration }
    func cardStatement(creditCardID _: String, statementDueOn _: RecurrenceCivilDate) async throws -> CardStatement { throw FinancialAPIError.configuration }
}
