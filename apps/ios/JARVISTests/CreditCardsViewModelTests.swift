import XCTest
@testable import JARVIS

@MainActor
final class CreditCardsViewModelTests: XCTestCase {
    func testPreviewFreezesCanonicalServerDataAndEditInvalidatesReview() async {
        let api = FinancialAPISpy()
        api.creditCardPreviewResult = .success(syntheticCreditCardPreview(name: "Nome canônico"))
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { "create-key" })
        model.beginCreation()
        model.name = "  Nome local  "
        model.lastFour = "4821"
        model.brand = .visa
        model.closingDay = 5
        model.dueDay = 12
        model.creditLimitText = "2500,00"
        await model.review()

        XCTAssertEqual(model.reviewedCard?.preview.name, "Nome canônico")
        XCTAssertEqual(model.reviewedCard?.request.name, "Nome canônico")
        XCTAssertEqual(model.reviewedCard?.idempotencyKey, "create-key")
        model.editCreation()
        XCTAssertNil(model.reviewedCard)
    }

    func testCreateRetryReusesKeyNewIntentGetsNewKeyAndDoubleTapIsBlocked() async {
        let api = FinancialAPISpy()
        var keys = ["key-one", "key-two"]
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { keys.removeFirst() })
        model.beginCreation()
        model.name = "Cartão"
        await model.review()
        api.creditCardCreateResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(RecordedCreditCard(card: syntheticCreditCard(), replayed: true))
        ]
        await model.confirmCreation()
        await model.confirmCreation()
        XCTAssertEqual(api.creditCardCreateRequests.map(\.key), ["key-one", "key-one"])

        model.finishCreation()
        model.beginCreation()
        model.name = "Outro"
        await model.review()
        XCTAssertEqual(model.reviewedCard?.idempotencyKey, "key-two")
    }

    func testConcurrentCreateConfirmationProducesOneRequest() async {
        let api = SuspendedCardMutationAPI()
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { "create-key" })
        model.beginCreation()
        model.name = "Cartão"
        await model.review()

        let first = Task { await model.confirmCreation() }
        await api.waitForCreateCall()
        let second = Task { await model.confirmCreation() }
        await second.value
        XCTAssertEqual(api.creditCardCreateRequests.count, 1)
        api.resolveCreate(.success(RecordedCreditCard(card: syntheticCreditCard(), replayed: false)))
        await first.value
        XCTAssertEqual(api.creditCardCreateRequests.map(\.key), ["create-key"])
    }

    func testConcurrentArchiveConfirmationProducesOneRequest() async {
        let api = SuspendedCardMutationAPI()
        let active = syntheticCreditCard()
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { "archive-key" })
        model.requestArchive(active)

        let first = Task { await model.confirmArchive(active) }
        await api.waitForArchiveCall()
        let second = Task { await model.confirmArchive(active) }
        await second.value
        XCTAssertEqual(api.creditCardArchiveRequests.count, 1)
        api.resolveArchive(
            .success(
                RecordedCreditCard(
                    card: syntheticCreditCard(status: .archived, archivedAt: "2026-08-27T12:00:00Z"),
                    replayed: false
                )
            )
        )
        await first.value
        XCTAssertEqual(api.creditCardArchiveRequests.map(\.key), ["archive-key"])
    }

    func testArchiveRetryReusesKeyAndArchivedCardRemainsVisible() async {
        let api = FinancialAPISpy()
        let active = syntheticCreditCard()
        api.creditCardListResult = .success(CreditCardList(items: [active]))
        api.creditCardArchiveResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(RecordedCreditCard(card: syntheticCreditCard(status: .archived, archivedAt: "2026-08-27T12:00:00Z"), replayed: true))
        ]
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { "archive-key" })
        await model.loadIfNeeded()
        model.requestArchive(active)
        await model.confirmArchive(active)
        await model.retryArchive(id: active.id)
        XCTAssertEqual(api.creditCardArchiveRequests.map(\.key), ["archive-key", "archive-key"])
        XCTAssertEqual(model.cards.count, 1)
        XCTAssertEqual(model.cards.first?.status, .archived)
    }

    func testOlderListCannotOverwriteCreateAndForcesOneReconciliation() async {
        let api = SuspendedCardAPI()
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { "key" })
        let load = Task { await model.loadIfNeeded() }
        await api.waitForListCall(1)

        model.beginCreation()
        model.name = "Novo cartão"
        await model.review()
        await model.confirmCreation()
        XCTAssertEqual(model.cards.first?.name, "Cartão sintético")

        api.resolveList(.success(CreditCardList(items: [])))
        await api.waitForListCall(2)
        XCTAssertEqual(model.cards.count, 1)
        api.resolveList(.success(CreditCardList(items: [syntheticCreditCard()])))
        await load.value
        XCTAssertEqual(api.creditCardListRequestCount, 2)
        XCTAssertEqual(model.cards.count, 1)
    }

    func testOlderListCannotOverwriteSuccessfulArchiveAndForcesOneReconciliation() async {
        let api = SuspendedCardAPI()
        let active = syntheticCreditCard()
        let archived = syntheticCreditCard(status: .archived, archivedAt: "2026-08-27T12:00:00Z")
        api.creditCardListResult = .success(CreditCardList(items: [active]))
        api.creditCardArchiveResults = [.success(RecordedCreditCard(card: archived, replayed: false))]
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { "archive-key" })
        api.resolveImmediately = true
        await model.loadIfNeeded()
        api.resolveImmediately = false

        let refresh = Task { await model.refresh() }
        await api.waitForListCall(2)
        model.requestArchive(active)
        await model.confirmArchive(active)
        XCTAssertEqual(model.cards.first?.status, .archived)

        api.resolveList(.success(CreditCardList(items: [active])))
        await api.waitForListCall(3)
        XCTAssertEqual(model.cards.first?.status, .archived)
        api.resolveList(.success(CreditCardList(items: [archived])))
        await refresh.value
        XCTAssertEqual(api.creditCardListRequestCount, 3)
        XCTAssertEqual(model.cards.first?.status, .archived)
    }

    func testCancelledWaiterDoesNotCancelSharedListLoad() async {
        let api = SuspendedCardAPI()
        let model = CreditCardsViewModel(api: api)
        let first = Task { await model.loadIfNeeded() }
        await api.waitForListCall(1)
        let waiter = Task { await model.refresh() }
        await Task.yield()
        waiter.cancel()

        api.resolveList(.success(CreditCardList(items: [syntheticCreditCard()])))
        await first.value
        await waiter.value
        XCTAssertEqual(api.creditCardListRequestCount, 1)
        XCTAssertEqual(model.cards.count, 1)
    }

    func testAlreadyArchivedDuringOlderListRequiresAuthoritativeReload() async {
        let api = SuspendedCardAPI()
        let active = syntheticCreditCard()
        api.creditCardListResult = .success(CreditCardList(items: [active]))
        let model = CreditCardsViewModel(api: api, makeIdempotencyKey: { "key" })
        api.resolveImmediately = true
        await model.loadIfNeeded()
        api.resolveImmediately = false
        let refresh = Task { await model.refresh() }
        await api.waitForListCall(2)
        api.creditCardArchiveResults = [.failure(FinancialAPIError.creditCardAlreadyArchived)]
        model.requestArchive(active)
        let archive = Task { await model.confirmArchive(active) }
        await api.waitForArchiveCall()
        api.resolveList(.success(CreditCardList(items: [active])))
        await api.waitForListCall(3)
        let archived = syntheticCreditCard(status: .archived, archivedAt: "2026-08-27T12:00:00Z")
        api.resolveList(.success(CreditCardList(items: [archived])))
        await archive.value
        await refresh.value
        XCTAssertEqual(model.cards.first?.status, .archived)
        XCTAssertEqual(api.creditCardListRequestCount, 3)
    }

    func testListFailureAndOptionalInputValidationRemainRetryable() async {
        let api = FinancialAPISpy()
        api.creditCardListResult = .failure(FinancialAPIError.connectionUnavailable)
        let model = CreditCardsViewModel(api: api)
        await model.loadIfNeeded()
        guard case .failed = model.listState else { return XCTFail("Expected failed state") }
        model.beginCreation()
        model.name = "Card"
        model.lastFour = "１２３４"
        await model.review()
        XCTAssertTrue(api.creditCardPreviewRequests.isEmpty)
        XCTAssertNotNil(model.creationErrorMessage)
    }
}

@MainActor
private final class SuspendedCardMutationAPI: FinancialAPISpy {
    private var createContinuation: CheckedContinuation<RecordedCreditCard, Error>?
    private var archiveContinuation: CheckedContinuation<RecordedCreditCard, Error>?
    private var createWaiter: CheckedContinuation<Void, Never>?
    private var archiveWaiter: CheckedContinuation<Void, Never>?

    override func createCreditCard(
        _ request: CreditCardRequest,
        idempotencyKey: String
    ) async throws -> RecordedCreditCard {
        creditCardCreateRequests.append((request, idempotencyKey))
        createWaiter?.resume()
        createWaiter = nil
        return try await withCheckedThrowingContinuation { createContinuation = $0 }
    }

    override func archiveCreditCard(id: String, idempotencyKey: String) async throws -> RecordedCreditCard {
        creditCardArchiveRequests.append((id, idempotencyKey))
        archiveWaiter?.resume()
        archiveWaiter = nil
        return try await withCheckedThrowingContinuation { archiveContinuation = $0 }
    }

    func waitForCreateCall() async {
        guard creditCardCreateRequests.isEmpty else { return }
        await withCheckedContinuation { createWaiter = $0 }
    }

    func waitForArchiveCall() async {
        guard creditCardArchiveRequests.isEmpty else { return }
        await withCheckedContinuation { archiveWaiter = $0 }
    }

    func resolveCreate(_ result: Result<RecordedCreditCard, Error>) {
        createContinuation?.resume(with: result)
        createContinuation = nil
    }

    func resolveArchive(_ result: Result<RecordedCreditCard, Error>) {
        archiveContinuation?.resume(with: result)
        archiveContinuation = nil
    }
}

@MainActor
private final class SuspendedCardAPI: FinancialAPISpy {
    private var listContinuations: [CheckedContinuation<CreditCardList, Error>] = []
    private var listWaiters: [(Int, CheckedContinuation<Void, Never>)] = []
    private var archiveWaiter: CheckedContinuation<Void, Never>?
    var resolveImmediately = false

    override func creditCards() async throws -> CreditCardList {
        creditCardListRequestCount += 1
        if resolveImmediately { return try creditCardListResult.get() }
        return try await withCheckedThrowingContinuation { continuation in
            listContinuations.append(continuation)
            resumeWaiters()
        }
    }

    override func archiveCreditCard(id: String, idempotencyKey: String) async throws -> RecordedCreditCard {
        creditCardArchiveRequests.append((id, idempotencyKey))
        archiveWaiter?.resume()
        archiveWaiter = nil
        guard !creditCardArchiveResults.isEmpty else { throw FinancialAPIError.serviceUnavailable }
        return try creditCardArchiveResults.removeFirst().get()
    }

    func waitForArchiveCall() async {
        guard creditCardArchiveRequests.isEmpty else { return }
        await withCheckedContinuation { archiveWaiter = $0 }
    }

    func waitForListCall(_ count: Int) async {
        guard creditCardListRequestCount < count else { return }
        await withCheckedContinuation { listWaiters.append((count, $0)) }
    }

    func resolveList(_ result: Result<CreditCardList, Error>) {
        precondition(!listContinuations.isEmpty)
        listContinuations.removeFirst().resume(with: result)
    }

    private func resumeWaiters() {
        var remaining: [(Int, CheckedContinuation<Void, Never>)] = []
        for waiter in listWaiters {
            if creditCardListRequestCount >= waiter.0 { waiter.1.resume() }
            else { remaining.append(waiter) }
        }
        listWaiters = remaining
    }
}
