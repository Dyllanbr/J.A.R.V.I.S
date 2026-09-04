import XCTest
@testable import JARVIS

@MainActor
final class CardStatementViewModelTests: XCTestCase {
    func testLoadsNonEmptyStatement() async throws {
        let api = CardStatementTestAPI(result: .success(try Self.statement(empty: false)))
        let model = CardStatementViewModel(
            cardID: Self.cardID,
            statementDueOn: try RecurrenceCivilDate("2026-09-10"),
            api: api
        )

        await model.loadIfNeeded()

        guard case let .loaded(statement) = model.state else {
            return XCTFail("Expected a loaded statement")
        }
        XCTAssertEqual(statement.totalAmount.minor, 5_000)
        XCTAssertEqual(api.calls, 1)
    }

    func testLoadsEmptyStatementWithoutTreatingZeroAsAnError() async throws {
        let api = CardStatementTestAPI(result: .success(try Self.statement(empty: true)))
        let model = CardStatementViewModel(
            cardID: Self.cardID,
            statementDueOn: try RecurrenceCivilDate("2026-09-10"),
            api: api
        )

        await model.loadIfNeeded()

        guard case let .empty(statement) = model.state else {
            return XCTFail("Expected the empty state")
        }
        XCTAssertEqual(statement.totalAmount.minor, 0)
        XCTAssertTrue(statement.lines.isEmpty)
    }

    func testFailureUsesSafeMessageAndRetryRecovers() async throws {
        let api = CardStatementTestAPI(result: .failure(FinancialAPIError.serviceUnavailable))
        let model = CardStatementViewModel(
            cardID: Self.cardID,
            statementDueOn: try RecurrenceCivilDate("2026-09-10"),
            api: api
        )

        await model.loadIfNeeded()
        XCTAssertEqual(model.errorMessage, FinancialAPIError.serviceUnavailable.userMessage)
        XCTAssertEqual(api.calls, 1)

        api.result = .success(try Self.statement(empty: false))
        await model.retry()

        guard case .loaded = model.state else { return XCTFail("Retry must recover") }
        XCTAssertEqual(api.calls, 2)
    }

    func testConcurrentLoadsUseSingleFlight() async throws {
        let api = CardStatementTestAPI(result: .success(try Self.statement(empty: false)), blocksFirstCall: true)
        let model = CardStatementViewModel(
            cardID: Self.cardID,
            statementDueOn: try RecurrenceCivilDate("2026-09-10"),
            api: api
        )

        let first = Task { await model.load() }
        await api.waitForFirstCall()
        let second = Task { await model.load() }
        await Task.yield()
        api.releaseFirstCall()
        await first.value
        await second.value

        XCTAssertEqual(api.calls, 1)
        guard case .loaded = model.state else { return XCTFail("Expected loaded state") }
    }

    func testCancellationReturnsToIdleAndDoesNotExposeInternalError() async throws {
        let api = CardStatementTestAPI(result: .failure(CancellationError()))
        let model = CardStatementViewModel(
            cardID: Self.cardID,
            statementDueOn: try RecurrenceCivilDate("2026-09-10"),
            api: api
        )

        await model.load()

        XCTAssertEqual(model.state, .idle)
        XCTAssertNil(model.errorMessage)
    }

    private static func statement(empty: Bool) throws -> CardStatement {
        let date = try RecurrenceCivilDate("2026-09-10")
        if empty {
            return try CardStatement(
                creditCardID: cardID,
                statementDueOn: date,
                totalAmount: try CardStatementTotalAmount(minor: 0),
                lines: []
            )
        }
        let line = try CardStatementLine(
            expenseID: "exp_statement_vm",
            description: "Compra sintética",
            amount: FinancialMoney(minor: 5_000, currency: .brl),
            occurredAt: try RecurrenceCivilDate("2026-08-10"),
            purchaseMode: .oneTime
        )
        return try CardStatement(
            creditCardID: cardID,
            statementDueOn: date,
            totalAmount: try CardStatementTotalAmount(minor: 5_000),
            lines: [line]
        )
    }

    private static let cardID = "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

@MainActor
private final class CardStatementTestAPI: FinancialAPI {
    var result: Result<CardStatement, Error>
    private(set) var calls = 0
    private let blocksFirstCall: Bool
    private var firstCallStarted = false
    private var firstCallContinuation: CheckedContinuation<Void, Never>?
    private var releaseContinuation: CheckedContinuation<Void, Never>?

    init(result: Result<CardStatement, Error>, blocksFirstCall: Bool = false) {
        self.result = result
        self.blocksFirstCall = blocksFirstCall
    }

    func cardStatement(creditCardID _: String, statementDueOn _: RecurrenceCivilDate) async throws -> CardStatement {
        calls += 1
        if blocksFirstCall, !firstCallStarted {
            firstCallStarted = true
            firstCallContinuation?.resume()
            firstCallContinuation = nil
            await withCheckedContinuation { continuation in
                releaseContinuation = continuation
            }
        }
        return try result.get()
    }

    func waitForFirstCall() async {
        guard firstCallStarted else {
            await withCheckedContinuation { continuation in
                firstCallContinuation = continuation
            }
            return
        }
    }

    func releaseFirstCall() {
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
}
