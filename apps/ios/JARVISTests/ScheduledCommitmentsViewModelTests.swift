import Foundation
import XCTest
@testable import JARVIS

@MainActor
final class ScheduledCommitmentsViewModelTests: XCTestCase {
    func testLoadUsesFinancialCalendarAndPublishesItems() async throws {
        let planID = "ipl_" + String(repeating: "a", count: 32)
        let item = try ScheduledCommitment(
            source: .installmentPlan,
            sourceID: planID,
            sequence: 1,
            dueOn: try RecurrenceCivilDate("2026-10-10"),
            amount: FinancialMoney(minor: 10_000, currency: .brl)
        )
        let api = ScheduledCommitmentsAPISpy(result: .success(try ScheduledCommitmentListResponse(items: [item])))
        let instant = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-08-17T01:00:00Z"))
        let model = ScheduledCommitmentsViewModel(api: api, now: { instant })

        await model.loadIfNeeded()

        XCTAssertEqual(model.state, .loaded([item]))
        XCTAssertEqual(api.scheduledCommitmentDates, [try RecurrenceCivilDate("2026-08-16")])
        XCTAssertEqual(api.scheduledCommitmentCallCount, 1)
    }

    func testEmptyAndFailureStatesAreExplicitAndRetryable() async throws {
        let api = ScheduledCommitmentsAPISpy(
            result: .success(try ScheduledCommitmentListResponse(items: []))
        )
        let model = ScheduledCommitmentsViewModel(api: api, now: { Date(timeIntervalSince1970: 1_750_000_000) })

        await model.loadIfNeeded()
        XCTAssertEqual(model.state, .empty)

        api.result = .failure(FinancialAPIError.serviceUnavailable)
        await model.refresh()
        XCTAssertEqual(model.state, .failed(FinancialAPIError.serviceUnavailable.userMessage))

        api.result = .success(try ScheduledCommitmentListResponse(items: []))
        await model.retry()
        XCTAssertEqual(model.state, .empty)
        XCTAssertEqual(api.scheduledCommitmentCallCount, 3)
    }

    func testCancellationLeavesModelIdle() async {
        let api = ScheduledCommitmentsAPISpy(result: .failure(CancellationError()))
        let model = ScheduledCommitmentsViewModel(api: api)

        await model.load()

        XCTAssertEqual(model.state, .idle)
    }

    func testConcurrentLoadsUseSingleFlightAndPreserveOrder() async throws {
        let first = try ScheduledCommitment(
            source: .recurrence,
            sourceID: "rec_single_flight",
            sequence: 1,
            dueOn: try RecurrenceCivilDate("2026-10-10"),
            amount: FinancialMoney(minor: 2990, currency: .brl)
        )
        let second = try ScheduledCommitment(
            source: .installmentPlan,
            sourceID: "ipl_single_flight",
            sequence: 1,
            dueOn: try RecurrenceCivilDate("2026-11-10"),
            amount: FinancialMoney(minor: 10000, currency: .brl)
        )
        let api = ScheduledCommitmentsAPISpy(
            result: .success(try ScheduledCommitmentListResponse(items: [first, second])),
            yieldsBeforeResult: true
        )
        let model = ScheduledCommitmentsViewModel(api: api)

        let firstLoad = Task { await model.loadIfNeeded() }
        await Task.yield()
        let secondLoad = Task { await model.loadIfNeeded() }
        await firstLoad.value
        await secondLoad.value

        XCTAssertEqual(api.scheduledCommitmentCallCount, 1)
        XCTAssertEqual(model.state, .loaded([first, second]))
    }
}

@MainActor
private final class ScheduledCommitmentsAPISpy: FinancialAPI {
    var result: Result<ScheduledCommitmentListResponse, Error>
    let yieldsBeforeResult: Bool
    private(set) var scheduledCommitmentCallCount = 0
    private(set) var scheduledCommitmentDates: [RecurrenceCivilDate] = []

    init(
        result: Result<ScheduledCommitmentListResponse, Error>,
        yieldsBeforeResult: Bool = false
    ) {
        self.result = result
        self.yieldsBeforeResult = yieldsBeforeResult
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

    func scheduledCommitments(evaluationDate: RecurrenceCivilDate) async throws -> ScheduledCommitmentListResponse {
        scheduledCommitmentCallCount += 1
        scheduledCommitmentDates.append(evaluationDate)
        if yieldsBeforeResult { await Task.yield() }
        return try result.get()
    }
}
