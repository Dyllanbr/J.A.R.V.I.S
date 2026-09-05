import Foundation
import XCTest
@testable import JARVIS

@MainActor
final class SafeAvailableViewModelTests: XCTestCase {
    func testLoadsExplicitPeriodAndPublishesPositiveResult() async throws {
        let start = try RecurrenceCivilDate("2026-09-01")
        let end = try RecurrenceCivilDate("2026-09-30")
        let api = SafeAvailableAPISpy(result: .success(try Self.positiveResponse(start: start, end: end)))
        let model = SafeAvailableViewModel(api: api, periodStart: start, periodEnd: end)

        await model.loadIfNeeded()

        guard case let .loaded(response) = model.state else {
            return XCTFail("Expected loaded state")
        }
        XCTAssertEqual(response.finalAmount.minor, 9_500)
        XCTAssertEqual(api.periods.count, 1)
        XCTAssertEqual(api.periods.first?.0, start)
        XCTAssertEqual(api.periods.first?.1, end)
        XCTAssertEqual(api.callCount, 1)
    }

    func testEmptyStateAndBudgetAbsenceAreExplicit() async throws {
        let response = try Self.emptyResponse()
        let api = SafeAvailableAPISpy(result: .success(response))
        let model = SafeAvailableViewModel(api: api, periodStart: response.periodStart, periodEnd: response.periodEnd)

        await model.load()

        XCTAssertEqual(model.state, .empty(response))
        XCTAssertEqual(response.finalAmount.minor, 0)
        XCTAssertEqual(response.missingData, [.budget])
    }

    func testNegativeResultIsPreservedWithoutMoneyClamping() async throws {
        let response = try Self.negativeResponse()
        let api = SafeAvailableAPISpy(result: .success(response))
        let model = SafeAvailableViewModel(api: api, periodStart: response.periodStart, periodEnd: response.periodEnd)

        await model.load()

        XCTAssertEqual(model.response?.finalAmount.minor, -2_500)
        XCTAssertEqual(model.state, .loaded(response))
    }

    func testFailureCanBeRetried() async throws {
        let success = try Self.emptyResponse()
        let api = SafeAvailableAPISpy(result: .failure(FinancialAPIError.serviceUnavailable))
        let model = SafeAvailableViewModel(api: api, periodStart: success.periodStart, periodEnd: success.periodEnd)

        await model.load()
        XCTAssertEqual(model.state, .failed(FinancialAPIError.serviceUnavailable.userMessage))

        api.result = .success(success)
        await model.retry()
        XCTAssertEqual(model.state, .empty(success))
        XCTAssertEqual(api.callCount, 2)
    }

    func testInvertedPeriodFailsBeforeReader() async throws {
        let api = SafeAvailableAPISpy(result: .success(try Self.emptyResponse()))
        let start = try RecurrenceCivilDate("2026-09-30")
        let end = try RecurrenceCivilDate("2026-09-01")
        let model = SafeAvailableViewModel(api: api, periodStart: start, periodEnd: end)

        await model.load()

        XCTAssertEqual(model.state, .failed(FinancialAPIError.invalidData.userMessage))
        XCTAssertEqual(api.callCount, 0)
    }

    func testConcurrentLoadsUseSingleFlight() async throws {
        let response = try Self.positiveResponse(
            start: try RecurrenceCivilDate("2026-09-01"),
            end: try RecurrenceCivilDate("2026-09-30")
        )
        let api = SafeAvailableAPISpy(result: .success(response), yieldsBeforeResult: true)
        let model = SafeAvailableViewModel(api: api, periodStart: response.periodStart, periodEnd: response.periodEnd)

        let first = Task { await model.loadIfNeeded() }
        await Task.yield()
        let second = Task { await model.loadIfNeeded() }
        await first.value
        await second.value

        XCTAssertEqual(api.callCount, 1)
        XCTAssertEqual(model.state, .loaded(response))
    }

    func testCancellationLeavesModelIdle() async throws {
        let api = SafeAvailableAPISpy(result: .failure(CancellationError()))
        let model = SafeAvailableViewModel(api: api)

        await model.load()

        XCTAssertEqual(model.state, .idle)
    }

    func testChangingPeriodInvalidatesAnInFlightResponse() async throws {
        let response = try Self.emptyResponse()
        let api = SafeAvailableAPISpy(result: .success(response), blocksUntilReleased: true)
        let model = SafeAvailableViewModel(api: api, periodStart: response.periodStart, periodEnd: response.periodEnd)
        let task = Task { await model.load() }
        await api.waitForCall()
        model.setPeriodEnd(try RecurrenceCivilDate("2026-10-01").pickerDate)
        api.release()
        await task.value

        XCTAssertEqual(model.state, .idle)
        XCTAssertEqual(api.callCount, 1)
    }

    private static func positiveResponse(start: RecurrenceCivilDate, end: RecurrenceCivilDate) throws -> SafeAvailableResponse {
        try SafeAvailableResponse(
            periodStart: start,
            periodEnd: end,
            availableBalance: SafeAvailableAmount(minor: 10_000),
            totalConfirmedIncome: SafeAvailableAmount(minor: 5_000),
            totalConfirmedExpense: SafeAvailableAmount(minor: 2_500),
            totalConfirmedCommitments: SafeAvailableAmount(minor: 3_000),
            finalAmount: SafeAvailableAmount(minor: 9_500),
            breakdown: [
                SafeAvailableBreakdown(kind: .availableBalance, sourceID: "available-balance", sequence: 0, dueOn: start, amount: SafeAvailableAmount(minor: 10_000)),
                SafeAvailableBreakdown(kind: .income, sourceID: "inc_safe_001", sequence: 0, dueOn: start, amount: SafeAvailableAmount(minor: 5_000)),
                SafeAvailableBreakdown(kind: .expense, sourceID: "exp_safe_001", sequence: 0, dueOn: start, amount: SafeAvailableAmount(minor: 2_500)),
                SafeAvailableBreakdown(kind: .commitment, sourceID: "ipl_safe_001", sequence: 1, dueOn: start, amount: SafeAvailableAmount(minor: 3_000))
            ],
            missingData: [.budget]
        )
    }

    private static func emptyResponse() throws -> SafeAvailableResponse {
        let start = try RecurrenceCivilDate("2026-09-01")
        let end = try RecurrenceCivilDate("2026-09-30")
        return try SafeAvailableResponse(
            periodStart: start,
            periodEnd: end,
            availableBalance: SafeAvailableAmount(minor: 0),
            totalConfirmedIncome: SafeAvailableAmount(minor: 0),
            totalConfirmedExpense: SafeAvailableAmount(minor: 0),
            totalConfirmedCommitments: SafeAvailableAmount(minor: 0),
            finalAmount: SafeAvailableAmount(minor: 0),
            breakdown: [SafeAvailableBreakdown(kind: .availableBalance, sourceID: "available-balance", sequence: 0, dueOn: start, amount: SafeAvailableAmount(minor: 0))],
            missingData: [.budget]
        )
    }

    private static func negativeResponse() throws -> SafeAvailableResponse {
        let start = try RecurrenceCivilDate("2026-09-01")
        let end = try RecurrenceCivilDate("2026-09-30")
        return try SafeAvailableResponse(
            periodStart: start,
            periodEnd: end,
            availableBalance: SafeAvailableAmount(minor: 1_000),
            totalConfirmedIncome: SafeAvailableAmount(minor: 0),
            totalConfirmedExpense: SafeAvailableAmount(minor: 2_500),
            totalConfirmedCommitments: SafeAvailableAmount(minor: 1_000),
            finalAmount: SafeAvailableAmount(minor: -2_500),
            breakdown: [
                SafeAvailableBreakdown(kind: .availableBalance, sourceID: "available-balance", sequence: 0, dueOn: start, amount: SafeAvailableAmount(minor: 1_000)),
                SafeAvailableBreakdown(kind: .expense, sourceID: "exp_safe_001", sequence: 0, dueOn: start, amount: SafeAvailableAmount(minor: 2_500)),
                SafeAvailableBreakdown(kind: .commitment, sourceID: "ipl_safe_001", sequence: 1, dueOn: start, amount: SafeAvailableAmount(minor: 1_000))
            ],
            missingData: [.budget]
        )
    }
}

@MainActor
private final class SafeAvailableAPISpy: FinancialAPI {
    var result: Result<SafeAvailableResponse, Error>
    let yieldsBeforeResult: Bool
    let blocksUntilReleased: Bool
    private(set) var callCount = 0
    private(set) var periods: [(RecurrenceCivilDate, RecurrenceCivilDate)] = []
    private var callStarted: CheckedContinuation<Void, Never>?
    private var releaseContinuation: CheckedContinuation<Void, Never>?

    init(
        result: Result<SafeAvailableResponse, Error>,
        yieldsBeforeResult: Bool = false,
        blocksUntilReleased: Bool = false
    ) {
        self.result = result
        self.yieldsBeforeResult = yieldsBeforeResult
        self.blocksUntilReleased = blocksUntilReleased
    }

    func safeAvailable(periodStart: RecurrenceCivilDate, periodEnd: RecurrenceCivilDate) async throws -> SafeAvailableResponse {
        callCount += 1
        periods.append((periodStart, periodEnd))
        callStarted?.resume()
        callStarted = nil
        if yieldsBeforeResult { await Task.yield() }
        if blocksUntilReleased {
            await withCheckedContinuation { continuation in releaseContinuation = continuation }
        }
        return try result.get()
    }

    func waitForCall() async {
        guard callCount == 0 else { return }
        await withCheckedContinuation { continuation in callStarted = continuation }
    }

    func release() {
        releaseContinuation?.resume()
        releaseContinuation = nil
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
