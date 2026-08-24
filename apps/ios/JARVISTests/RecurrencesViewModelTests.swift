import Observation
import XCTest
@testable import JARVIS

@MainActor
final class RecurrencesViewModelTests: XCTestCase {
    private let fixedNow = Date(timeIntervalSinceReferenceDate: 808_000_000)

    func testInitialValidationAndCanonicalPreviewFreezeTheReview() async {
        let api = FinancialAPISpy()
        api.recurrencePreviewResult = .success(
            syntheticRecurrencePreview(description: "Academia canônica", amount: 12_345, startsOn: "2026-08-31")
        )
        let model = makeModel(api: api)

        XCTAssertEqual(model.listState, .idle)
        XCTAssertEqual(model.creationState, .editing)
        model.beginCreation()
        await model.review()
        XCTAssertTrue(api.recurrencePreviewRequests.isEmpty)
        XCTAssertNotNil(model.creationErrorMessage)

        fillValidDraft(model, description: "  Academia digitada  ", amount: "123,45")
        await model.review()

        XCTAssertEqual(api.recurrencePreviewRequests.count, 1)
        XCTAssertEqual(api.recurrencePreviewRequests[0].type, .expense)
        XCTAssertEqual(api.recurrencePreviewRequests[0].frequency, .monthly)
        XCTAssertEqual(api.recurrencePreviewRequests[0].startsOn.canonicalValue, model.startsOn.canonicalValue)
        let reviewed = try? XCTUnwrap(model.reviewedRecurrence)
        XCTAssertEqual(reviewed?.request.description, "Academia canônica")
        XCTAssertEqual(reviewed?.request.expectedAmount.minor, 12_345)
        XCTAssertEqual(reviewed?.request.startsOn.canonicalValue, "2026-08-31")
        XCTAssertEqual(reviewed?.idempotencyKey, "key-1")
    }

    func testInitialCivilDateUsesDeviceCalendarWithoutChangingTransportSemantics() throws {
        let api = FinancialAPISpy()
        var saoPaulo = Calendar(identifier: .gregorian)
        saoPaulo.timeZone = try XCTUnwrap(TimeZone(identifier: "America/Sao_Paulo"))
        let instant = try XCTUnwrap(ISO8601DateFormatter().date(from: "2026-08-17T01:00:00Z"))

        let model = RecurrencesViewModel(api: api, now: instant, calendar: saoPaulo)

        XCTAssertEqual(model.startsOn.canonicalValue, "2026-08-16")
        XCTAssertEqual(model.startsOn.displayValue, "16/08/2026")
    }

    func testStalePreviewCannotReplaceEditedDraftOrGenerateAKey() async {
        let api = SuspendedRecurrencePreviewAPI()
        var keyCalls = 0
        let model = RecurrencesViewModel(api: api, now: fixedNow) {
            keyCalls += 1
            return "unexpected-key"
        }
        model.beginCreation()
        fillValidDraft(model, description: "Academia A", amount: "119,00")

        let task = Task { await model.review() }
        await api.waitForPreview()
        model.description = "Academia B"
        model.amountText = "129,00"
        api.resolvePreview(syntheticRecurrencePreview(description: "Academia A"))
        await task.value

        XCTAssertEqual(model.creationState, .editing)
        XCTAssertEqual(model.description, "Academia B")
        XCTAssertEqual(model.amountText, "129,00")
        XCTAssertNil(model.reviewedRecurrence)
        XCTAssertEqual(keyCalls, 0)
        XCTAssertTrue(api.recurrenceCreateRequests.isEmpty)
    }

    func testCreateTransientFailureRetriesFrozenRequestAndKeyAndAcceptsReplay() async {
        let api = FinancialAPISpy()
        api.recurrenceCreateResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(RecordedRecurrence(recurrence: syntheticRecurrence(), replayed: true))
        ]
        let model = makeModel(api: api)
        model.beginCreation()
        fillValidDraft(model)
        await model.review()
        let frozen = model.reviewedRecurrence

        await model.confirm()
        guard case .retryable = model.creationState else {
            return XCTFail("Expected retryable create after a transient error")
        }
        await model.confirm()

        XCTAssertEqual(api.recurrenceCreateRequests.map(\.request), [frozen?.request, frozen?.request].compactMap { $0 })
        XCTAssertEqual(api.recurrenceCreateRequests.map(\.key), ["key-1", "key-1"])
        XCTAssertEqual(model.successfulRecurrence, syntheticRecurrence())
        XCTAssertEqual(model.recurrences, [syntheticRecurrence()])
    }

    func testEditingAfterReviewCreatesANewLogicalKey() async {
        let api = FinancialAPISpy()
        var keys = ["first-key", "second-key"]
        let model = RecurrencesViewModel(api: api, now: fixedNow) { keys.removeFirst() }
        model.beginCreation()
        fillValidDraft(model, description: "Primeiro compromisso")
        await model.review()
        XCTAssertEqual(model.reviewedRecurrence?.idempotencyKey, "first-key")

        model.editCreation()
        model.description = "Segundo compromisso"
        api.recurrencePreviewResult = .success(
            syntheticRecurrencePreview(description: "Segundo compromisso")
        )
        await model.review()
        XCTAssertEqual(model.reviewedRecurrence?.idempotencyKey, "second-key")
        await model.confirm()

        XCTAssertEqual(api.recurrenceCreateRequests.count, 1)
        XCTAssertEqual(api.recurrenceCreateRequests[0].key, "second-key")
        XCTAssertEqual(api.recurrenceCreateRequests[0].request.description, "Segundo compromisso")
    }

    func testCreateDoubleSubmitIsSuppressedWhileRequestIsSuspended() async {
        let api = SuspendedRecurrenceCreateAPI()
        let model = makeModel(api: api)
        model.beginCreation()
        fillValidDraft(model)
        await model.review()

        let first = Task { await model.confirm() }
        await api.waitForCreate()
        await model.confirm()

        XCTAssertEqual(api.recurrenceCreateRequests.count, 1)
        api.resolveCreate(RecordedRecurrence(recurrence: syntheticRecurrence(), replayed: false))
        await first.value
        XCTAssertEqual(model.successfulRecurrence, syntheticRecurrence())
    }

    func testListSupportsEmptyPopulatedErrorRetryAndPreservesBackendOrdering() async {
        let active = syntheticRecurrence(id: "rec_active")
        let cancelled = syntheticRecurrence(
            id: "rec_cancelled",
            status: .cancelled,
            cancelledAt: "2026-08-17T18:00:00Z"
        )
        let api = FinancialAPISpy()
        api.recurrenceListResult = .success(RecurrenceList(items: []))
        let emptyModel = makeModel(api: api)
        await emptyModel.loadIfNeeded()
        XCTAssertEqual(emptyModel.listState, .loaded([]))

        api.recurrenceListResult = .failure(FinancialAPIError.connectionUnavailable)
        let model = makeModel(api: api)
        await model.loadIfNeeded()
        XCTAssertEqual(model.listState, .failed(FinancialAPIError.connectionUnavailable.userMessage))

        api.recurrenceListResult = .success(RecurrenceList(items: [cancelled, active]))
        await model.retryList()
        XCTAssertEqual(model.listState, .loaded([cancelled, active]))
        XCTAssertEqual(api.recurrenceListRequestCount, 3)
    }

    func testListRequestIsModelOwnedSingleFlightAndSurvivesCallerCancellation() async {
        let api = SuspendedRecurrenceListAPI()
        let model = makeModel(api: api)

        let callerA = Task { await model.loadIfNeeded() }
        await api.waitForList()
        var callerBCompleted = false
        let callerBStarted = RecurrenceTestSignal()
        let callerB = Task {
            callerBStarted.signal()
            await model.loadIfNeeded()
            callerBCompleted = true
        }
        await callerBStarted.wait()

        XCTAssertEqual(api.recurrenceListRequestCount, 1)
        XCTAssertFalse(callerBCompleted)
        callerA.cancel()
        await Task.yield()
        XCTAssertEqual(api.listCancellationCount, 0)

        api.resolveList(RecurrenceList(items: [syntheticRecurrence()]))
        await callerA.value
        await callerB.value
        XCTAssertTrue(callerBCompleted)
        XCTAssertEqual(model.listState, .loaded([syntheticRecurrence()]))
        XCTAssertEqual(api.recurrenceListRequestCount, 1)
    }

    func testCreateDuringOlderListKeepsLocalResultAndRunsOneReconciliation() async {
        let created = syntheticRecurrence(id: "rec_created_during_list")
        let api = SequencedSuspendedRecurrenceListAPI(
            listResults: [
                .success(RecurrenceList(items: [])),
                .success(RecurrenceList(items: [created]))
            ]
        )
        api.recurrencePreviewResult = .success(
            syntheticRecurrencePreview(description: created.description)
        )
        api.recurrenceCreateResults = [
            .success(RecordedRecurrence(recurrence: created, replayed: false))
        ]
        let model = makeModel(api: api)

        let olderLoad = Task { await model.loadIfNeeded() }
        await api.waitForList(call: 1)
        model.beginCreation()
        fillValidDraft(model, description: created.description)
        await model.review()
        await model.confirm()

        XCTAssertEqual(model.recurrences, [created])
        model.finishCreation()
        api.resolveList(call: 1)
        await api.waitForList(call: 2)

        XCTAssertEqual(model.recurrences, [created], "the older empty snapshot removed the created recurrence")
        XCTAssertEqual(api.recurrenceListRequestCount, 2)

        let reconciliationWaiterStarted = RecurrenceTestSignal()
        let reconciliationWaiter = Task {
            reconciliationWaiterStarted.signal()
            await model.refresh()
        }
        await reconciliationWaiterStarted.wait()
        api.resolveList(call: 2)
        await olderLoad.value
        await reconciliationWaiter.value

        XCTAssertEqual(model.recurrences, [created])
        XCTAssertEqual(api.recurrenceListRequestCount, 2)
    }

    func testCancelDuringOlderListKeepsCancelledResultAndRunsOneReconciliation() async {
        let active = syntheticRecurrence(id: "rec_cancelled_during_list")
        let cancelled = syntheticRecurrence(
            id: active.id,
            status: .cancelled,
            cancelledAt: "2026-08-17T18:00:00Z"
        )
        let api = SequencedSuspendedRecurrenceListAPI(
            listResults: [
                .success(RecurrenceList(items: [active])),
                .success(RecurrenceList(items: [active])),
                .success(RecurrenceList(items: [cancelled]))
            ]
        )
        api.recurrenceCancelResults = [
            .success(RecordedRecurrence(recurrence: cancelled, replayed: false))
        ]
        let model = makeModel(api: api)

        let initialLoad = Task { await model.loadIfNeeded() }
        await api.waitForList(call: 1)
        api.resolveList(call: 1)
        await initialLoad.value
        XCTAssertEqual(model.recurrences, [active])

        let olderRefresh = Task { await model.refresh() }
        await api.waitForList(call: 2)
        model.requestCancellation(active)
        await model.confirmCancellation()
        XCTAssertEqual(model.recurrences, [cancelled])

        api.resolveList(call: 2)
        await api.waitForList(call: 3)
        XCTAssertEqual(model.recurrences, [cancelled], "the older ACTIVE snapshot replaced CANCELLED")
        XCTAssertEqual(model.recurrences.first?.cancelledAt, cancelled.cancelledAt)
        XCTAssertFalse(model.canRetryCancellation(id: active.id))
        XCTAssertEqual(api.recurrenceListRequestCount, 3)

        let reconciliationWaiterStarted = RecurrenceTestSignal()
        let reconciliationWaiter = Task {
            reconciliationWaiterStarted.signal()
            await model.refresh()
        }
        await reconciliationWaiterStarted.wait()
        api.resolveList(call: 3)
        await olderRefresh.value
        await reconciliationWaiter.value

        XCTAssertEqual(model.recurrences, [cancelled])
        XCTAssertEqual(model.recurrences.first?.status, .cancelled)
        XCTAssertEqual(api.recurrenceListRequestCount, 3)
    }

    func testStaleListErrorAfterCreateCannotReplaceLocalResult() async {
        let created = syntheticRecurrence(id: "rec_created_before_stale_error")
        let api = SequencedSuspendedRecurrenceListAPI(
            listResults: [
                .failure(FinancialAPIError.connectionUnavailable),
                .success(RecurrenceList(items: [created]))
            ]
        )
        api.recurrencePreviewResult = .success(
            syntheticRecurrencePreview(description: created.description)
        )
        api.recurrenceCreateResults = [
            .success(RecordedRecurrence(recurrence: created, replayed: false))
        ]
        let model = makeModel(api: api)

        let olderLoad = Task { await model.loadIfNeeded() }
        await api.waitForList(call: 1)
        model.beginCreation()
        fillValidDraft(model, description: created.description)
        await model.review()
        await model.confirm()

        api.resolveList(call: 1)
        await api.waitForList(call: 2)
        XCTAssertEqual(model.recurrences, [created])

        let reconciliationWaiterStarted = RecurrenceTestSignal()
        let reconciliationWaiter = Task {
            reconciliationWaiterStarted.signal()
            await model.refresh()
        }
        await reconciliationWaiterStarted.wait()
        api.resolveList(call: 2)
        await olderLoad.value
        await reconciliationWaiter.value

        XCTAssertEqual(model.listState, .loaded([created]))
        XCTAssertEqual(api.recurrenceListRequestCount, 2)
    }

    func testCancelRequiresConfirmationAndTransientRetryKeepsKeyAndItem() async {
        let active = syntheticRecurrence()
        let cancelled = syntheticRecurrence(status: .cancelled, cancelledAt: "2026-08-17T18:00:00Z")
        let api = FinancialAPISpy()
        api.recurrenceListResult = .success(RecurrenceList(items: [active]))
        api.recurrenceCancelResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(RecordedRecurrence(recurrence: cancelled, replayed: true))
        ]
        let model = makeModel(api: api)
        await model.loadIfNeeded()

        model.requestCancellation(active)
        XCTAssertTrue(api.recurrenceCancelRequests.isEmpty)
        XCTAssertEqual(model.cancellationConfirmation, active)
        await model.confirmCancellation()

        XCTAssertEqual(model.recurrences, [active])
        XCTAssertTrue(model.canRetryCancellation(id: active.id))
        await model.retryCancellation(id: active.id)

        XCTAssertEqual(api.recurrenceCancelRequests.map(\.key), ["key-1", "key-1"])
        XCTAssertEqual(model.recurrences, [cancelled])
        XCTAssertFalse(model.canRetryCancellation(id: active.id))
        XCTAssertNil(model.cancellationErrors[active.id])
    }

    func testConfirmedItemCanBeCancelledAfterAlertBindingClearsPresentationState() async {
        let active = syntheticRecurrence()
        let cancelled = syntheticRecurrence(status: .cancelled, cancelledAt: "2026-08-17T18:00:00Z")
        let api = FinancialAPISpy()
        api.recurrenceListResult = .success(RecurrenceList(items: [active]))
        api.recurrenceCancelResults = [
            .success(RecordedRecurrence(recurrence: cancelled, replayed: false))
        ]
        let model = makeModel(api: api)
        await model.loadIfNeeded()

        model.requestCancellation(active)
        model.dismissCancellationConfirmation()
        await model.confirmCancellation(active)

        XCTAssertEqual(api.recurrenceCancelRequests.count, 1)
        XCTAssertEqual(model.recurrences, [cancelled])
    }

    func testCancelDoubleSubmitIsSuppressedAndReplayResultRemainsVisible() async {
        let active = syntheticRecurrence()
        let cancelled = syntheticRecurrence(status: .cancelled, cancelledAt: "2026-08-17T18:00:00Z")
        let api = SuspendedRecurrenceCancelAPI()
        api.recurrenceListResult = .success(RecurrenceList(items: [active]))
        let model = makeModel(api: api)
        await model.loadIfNeeded()
        model.requestCancellation(active)

        let first = Task { await model.confirmCancellation() }
        await api.waitForCancel()
        await model.retryCancellation(id: active.id)

        XCTAssertEqual(api.recurrenceCancelRequests.count, 1)
        api.resolveCancel(RecordedRecurrence(recurrence: cancelled, replayed: true))
        await first.value
        XCTAssertEqual(model.recurrences, [cancelled])
    }

    func testAlreadyCancelledAndUnknownStaleResponsesRefreshWithoutFabricatingState() async {
        let active = syntheticRecurrence()
        let cancelled = syntheticRecurrence(status: .cancelled, cancelledAt: "2026-08-17T18:00:00Z")
        let api = FinancialAPISpy()
        api.recurrenceListResult = .success(RecurrenceList(items: [active]))
        api.recurrenceCancelResults = [.failure(FinancialAPIError.alreadyCancelled)]
        let model = makeModel(api: api)
        await model.loadIfNeeded()
        api.recurrenceListResult = .success(RecurrenceList(items: [cancelled]))

        model.requestCancellation(active)
        await model.confirmCancellation()

        XCTAssertEqual(model.recurrences, [cancelled])
        XCTAssertNil(model.cancellationErrors[active.id])
        XCTAssertFalse(model.canRetryCancellation(id: active.id))
        XCTAssertEqual(api.recurrenceCancelRequests.count, 1)
        XCTAssertEqual(api.recurrenceListRequestCount, 2)
    }

    func testAlreadyCancelledDuringOlderListRequiresOneLaterReconciliation() async {
        let active = syntheticRecurrence(id: "rec_already_cancelled_during_list")
        let cancelled = syntheticRecurrence(
            id: active.id,
            status: .cancelled,
            cancelledAt: "2026-08-17T18:00:00Z"
        )
        let api = SequencedSuspendedRecurrenceListAPI(
            listResults: [
                .success(RecurrenceList(items: [active])),
                .success(RecurrenceList(items: [cancelled]))
            ],
            suspendCancellation: true
        )
        api.recurrencePreviewResult = .success(
            syntheticRecurrencePreview(description: active.description)
        )
        api.recurrenceCreateResults = [
            .success(RecordedRecurrence(recurrence: active, replayed: false))
        ]
        let model = makeModel(api: api)
        model.beginCreation()
        fillValidDraft(model, description: active.description)
        await model.review()
        await model.confirm()
        XCTAssertEqual(model.recurrences, [active])

        let olderRefresh = Task { await model.refresh() }
        await api.waitForList(call: 1)
        model.requestCancellation(active)
        let cancellation = Task { await model.confirmCancellation() }
        await api.waitForCancel()

        let observationRegistered = RecurrenceTestSignal()
        let errorObserved = Task {
            await waitForCancellationError(
                model,
                recurrenceID: active.id,
                registration: observationRegistered
            )
        }
        await observationRegistered.wait()
        api.resolveCancel(.failure(FinancialAPIError.alreadyCancelled))
        await errorObserved.value

        XCTAssertEqual(api.recurrenceListRequestCount, 1)
        XCTAssertEqual(model.recurrences, [active], "already-cancelled must not fabricate local state")

        api.resolveList(call: 1)
        await api.waitForList(call: 2)
        XCTAssertEqual(model.recurrences, [active], "the older load is not the requested reconciliation")
        XCTAssertEqual(api.recurrenceListRequestCount, 2)

        api.resolveList(call: 2)
        await olderRefresh.value
        await cancellation.value

        XCTAssertEqual(model.recurrences, [cancelled])
        XCTAssertEqual(model.recurrences.first?.cancelledAt, cancelled.cancelledAt)
        XCTAssertFalse(model.canRetryCancellation(id: active.id))
        XCTAssertNil(model.cancellationErrors[active.id])
        XCTAssertEqual(api.recurrenceListRequestCount, 2)
    }

    func testNotFoundDuringOlderListRequiresOneLaterReconciliation() async {
        let active = syntheticRecurrence(id: "rec_not_found_during_list")
        let api = SequencedSuspendedRecurrenceListAPI(
            listResults: [
                .success(RecurrenceList(items: [active])),
                .success(RecurrenceList(items: []))
            ],
            suspendCancellation: true
        )
        api.recurrencePreviewResult = .success(
            syntheticRecurrencePreview(description: active.description)
        )
        api.recurrenceCreateResults = [
            .success(RecordedRecurrence(recurrence: active, replayed: false))
        ]
        let model = makeModel(api: api)
        model.beginCreation()
        fillValidDraft(model, description: active.description)
        await model.review()
        await model.confirm()
        XCTAssertEqual(model.recurrences, [active])

        let olderRefresh = Task { await model.refresh() }
        await api.waitForList(call: 1)
        model.requestCancellation(active)
        let cancellation = Task { await model.confirmCancellation() }
        await api.waitForCancel()

        let observationRegistered = RecurrenceTestSignal()
        let errorObserved = Task {
            await waitForCancellationError(
                model,
                recurrenceID: active.id,
                registration: observationRegistered
            )
        }
        await observationRegistered.wait()
        api.resolveCancel(.failure(FinancialAPIError.notFound))
        await errorObserved.value

        XCTAssertEqual(api.recurrenceListRequestCount, 1)
        XCTAssertEqual(model.recurrences, [active], "not-found must not remove local state before GET")

        api.resolveList(call: 1)
        await api.waitForList(call: 2)
        XCTAssertEqual(model.recurrences, [active], "the older load is not the requested reconciliation")
        XCTAssertEqual(api.recurrenceListRequestCount, 2)

        api.resolveList(call: 2)
        await olderRefresh.value
        await cancellation.value

        XCTAssertEqual(model.recurrences, [])
        XCTAssertFalse(model.canRetryCancellation(id: active.id))
        XCTAssertNil(model.cancellationErrors[active.id])
        XCTAssertEqual(api.recurrenceListRequestCount, 2)
    }

    func testDismissAfterSuccessResetsCreationState() async {
        let api = FinancialAPISpy()
        let model = makeModel(api: api)
        model.beginCreation()
        fillValidDraft(model)
        await model.review()
        await model.confirm()
        XCTAssertNotNil(model.successfulRecurrence)

        model.finishCreation()
        XCTAssertFalse(model.isPresentingCreate)
        XCTAssertEqual(model.creationState, .editing)
        XCTAssertEqual(model.description, "")
        XCTAssertEqual(model.amountText, "")
    }

    private func makeModel(api: any FinancialAPI) -> RecurrencesViewModel {
        var nextKey = 0
        return RecurrencesViewModel(api: api, now: fixedNow) {
            nextKey += 1
            return "key-\(nextKey)"
        }
    }

    private func fillValidDraft(
        _ model: RecurrencesViewModel,
        description: String = "Academia sintética",
        amount: String = "119,00"
    ) {
        model.description = description
        model.amountText = amount
    }

    private func waitForCancellationError(
        _ model: RecurrencesViewModel,
        recurrenceID: String,
        registration: RecurrenceTestSignal
    ) async {
        if model.cancellationErrors[recurrenceID] != nil {
            registration.signal()
            return
        }

        await withCheckedContinuation { continuation in
            withObservationTracking {
                _ = model.cancellationErrors[recurrenceID]
            } onChange: {
                Task { @MainActor in continuation.resume() }
            }
            registration.signal()
        }
    }
}

@MainActor
private final class SuspendedRecurrencePreviewAPI: FinancialAPISpy {
    private var continuation: CheckedContinuation<RecurrencePreview, Error>?
    private var waiter: CheckedContinuation<Void, Never>?

    override func previewRecurrence(_ request: RecurrenceRequest) async throws -> RecurrencePreview {
        recurrencePreviewRequests.append(request)
        return try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            waiter?.resume()
            waiter = nil
        }
    }

    func waitForPreview() async {
        guard continuation == nil else { return }
        await withCheckedContinuation { waiter = $0 }
    }

    func resolvePreview(_ preview: RecurrencePreview) {
        let continuation = self.continuation
        self.continuation = nil
        continuation?.resume(returning: preview)
    }
}

@MainActor
private final class SuspendedRecurrenceCreateAPI: FinancialAPISpy {
    private var continuation: CheckedContinuation<RecordedRecurrence, Error>?
    private var waiter: CheckedContinuation<Void, Never>?

    override func createRecurrence(
        _ request: RecurrenceRequest,
        idempotencyKey: String
    ) async throws -> RecordedRecurrence {
        recurrenceCreateRequests.append((request, idempotencyKey))
        return try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            waiter?.resume()
            waiter = nil
        }
    }

    func waitForCreate() async {
        guard continuation == nil else { return }
        await withCheckedContinuation { waiter = $0 }
    }

    func resolveCreate(_ result: RecordedRecurrence) {
        let continuation = self.continuation
        self.continuation = nil
        continuation?.resume(returning: result)
    }
}

@MainActor
private final class SuspendedRecurrenceListAPI: FinancialAPISpy {
    private var continuation: CheckedContinuation<RecurrenceList, Error>?
    private var waiter: CheckedContinuation<Void, Never>?
    private(set) var listCancellationCount = 0

    override func recurrences() async throws -> RecurrenceList {
        recurrenceListRequestCount += 1
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                self.continuation = continuation
                waiter?.resume()
                waiter = nil
            }
        } onCancel: { [weak self] in
            Task { @MainActor in self?.listCancellationCount += 1 }
        }
    }

    func waitForList() async {
        guard continuation == nil else { return }
        await withCheckedContinuation { waiter = $0 }
    }

    func resolveList(_ result: RecurrenceList) {
        let continuation = self.continuation
        self.continuation = nil
        continuation?.resume(returning: result)
    }
}

@MainActor
private final class SequencedSuspendedRecurrenceListAPI: FinancialAPISpy {
    private var listResults: [Result<RecurrenceList, Error>]
    private var resolvedResults: [Int: Result<RecurrenceList, Error>] = [:]
    private var continuations: [Int: CheckedContinuation<RecurrenceList, Error>] = [:]
    private var waiters: [Int: CheckedContinuation<Void, Never>] = [:]
    private let suspendCancellation: Bool
    private var cancellationContinuation: CheckedContinuation<RecordedRecurrence, Error>?
    private var cancellationWaiter: CheckedContinuation<Void, Never>?

    init(
        listResults: [Result<RecurrenceList, Error>],
        suspendCancellation: Bool = false
    ) {
        self.listResults = listResults
        self.suspendCancellation = suspendCancellation
        super.init()
    }

    override func recurrences() async throws -> RecurrenceList {
        recurrenceListRequestCount += 1
        let call = recurrenceListRequestCount
        guard !listResults.isEmpty else { throw FinancialAPIError.serviceUnavailable }
        let result = listResults.removeFirst()
        resolvedResults[call] = result

        return try await withCheckedThrowingContinuation { continuation in
            continuations[call] = continuation
            waiters.removeValue(forKey: call)?.resume()
        }
    }

    func waitForList(call: Int) async {
        guard recurrenceListRequestCount < call else { return }
        await withCheckedContinuation { waiters[call] = $0 }
    }

    func resolveList(call: Int) {
        guard let continuation = continuations.removeValue(forKey: call) else {
            return XCTFail("List call \(call) is not suspended")
        }
        continuation.resume(with: listResultsResult(for: call))
    }

    private func listResultsResult(for call: Int) -> Result<RecurrenceList, Error> {
        guard let result = resolvedResults.removeValue(forKey: call) else {
            preconditionFailure("Missing result for list call \(call)")
        }
        return result
    }

    override func cancelRecurrence(id: String, idempotencyKey: String) async throws -> RecordedRecurrence {
        guard suspendCancellation else {
            return try await super.cancelRecurrence(id: id, idempotencyKey: idempotencyKey)
        }
        recurrenceCancelRequests.append((id, idempotencyKey))
        return try await withCheckedThrowingContinuation { continuation in
            cancellationContinuation = continuation
            cancellationWaiter?.resume()
            cancellationWaiter = nil
        }
    }

    func waitForCancel() async {
        guard cancellationContinuation == nil else { return }
        await withCheckedContinuation { cancellationWaiter = $0 }
    }

    func resolveCancel(_ result: Result<RecordedRecurrence, Error>) {
        let continuation = cancellationContinuation
        cancellationContinuation = nil
        continuation?.resume(with: result)
    }
}

@MainActor
private final class SuspendedRecurrenceCancelAPI: FinancialAPISpy {
    private var continuation: CheckedContinuation<RecordedRecurrence, Error>?
    private var waiter: CheckedContinuation<Void, Never>?

    override func cancelRecurrence(id: String, idempotencyKey: String) async throws -> RecordedRecurrence {
        recurrenceCancelRequests.append((id, idempotencyKey))
        return try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            waiter?.resume()
            waiter = nil
        }
    }

    func waitForCancel() async {
        guard continuation == nil else { return }
        await withCheckedContinuation { waiter = $0 }
    }

    func resolveCancel(_ result: RecordedRecurrence) {
        let continuation = self.continuation
        self.continuation = nil
        continuation?.resume(returning: result)
    }
}

@MainActor
private final class RecurrenceTestSignal {
    private var signalled = false
    private var waiter: CheckedContinuation<Void, Never>?

    func signal() {
        signalled = true
        waiter?.resume()
        waiter = nil
    }

    func wait() async {
        guard !signalled else { return }
        await withCheckedContinuation { waiter = $0 }
    }
}
