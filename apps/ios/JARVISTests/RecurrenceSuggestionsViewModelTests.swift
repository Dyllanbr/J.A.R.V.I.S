import Observation
import XCTest
@testable import JARVIS

@MainActor
final class RecurrenceSuggestionsViewModelTests: XCTestCase {
    func testListSupportsEmptyPopulatedFailureAndRetryWithoutChangingRecurrences() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let secondSuggestion = syntheticRecurrenceSuggestion(idSuffix: "b", description: "Academia sintética")
        let api = FinancialAPISpy()
        api.recurrenceSuggestionListResult = .success(RecurrenceSuggestionList(items: []))
        let empty = RecurrenceSuggestionsViewModel(api: api)
        await empty.loadIfNeeded()
        XCTAssertEqual(empty.listState, .loaded([]))

        api.recurrenceSuggestionListResult = .failure(FinancialAPIError.connectionUnavailable)
        let model = RecurrenceSuggestionsViewModel(api: api)
        await model.loadIfNeeded()
        XCTAssertEqual(model.listState, .failed(FinancialAPIError.connectionUnavailable.userMessage))

        api.recurrenceSuggestionListResult = .success(
            RecurrenceSuggestionList(items: [secondSuggestion, suggestion])
        )
        await model.retryList()
        XCTAssertEqual(model.listState, .loaded([secondSuggestion, suggestion]))
        XCTAssertTrue(api.recurrenceCreateRequests.isEmpty)
        XCTAssertTrue(api.recurrenceCancelRequests.isEmpty)
    }

    func testDismissRequiresConfirmationRemovesLocallyAndAcceptsReplay() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let api = FinancialAPISpy()
        api.recurrenceSuggestionListResult = .success(RecurrenceSuggestionList(items: [suggestion]))
        api.recurrenceSuggestionDismissResults = [
            .success(DismissedRecurrenceSuggestion(replayed: true))
        ]
        let model = RecurrenceSuggestionsViewModel(api: api)
        await model.loadIfNeeded()

        model.requestDismissal(suggestion)
        XCTAssertEqual(model.dismissalConfirmation, suggestion)
        XCTAssertTrue(api.recurrenceSuggestionDismissRequests.isEmpty)
        api.recurrenceSuggestionListResult = .success(RecurrenceSuggestionList(items: []))
        await model.confirmDismissal()

        XCTAssertEqual(api.recurrenceSuggestionDismissRequests, [suggestion.id])
        XCTAssertEqual(model.suggestions, [])
        XCTAssertTrue(api.recurrenceCreateRequests.isEmpty)
        XCTAssertTrue(api.createRequests.isEmpty)
    }

    func testDismissFailurePreservesSuggestionAndExposesSafeRetryableError() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let api = FinancialAPISpy()
        api.recurrenceSuggestionListResult = .success(RecurrenceSuggestionList(items: [suggestion]))
        api.recurrenceSuggestionDismissResults = [.failure(FinancialAPIError.serviceUnavailable)]
        let model = RecurrenceSuggestionsViewModel(api: api)
        await model.loadIfNeeded()

        model.requestDismissal(suggestion)
        await model.confirmDismissal()

        XCTAssertEqual(model.suggestions, [suggestion])
        XCTAssertEqual(model.actionErrors[suggestion.id], FinancialAPIError.serviceUnavailable.userMessage)
        XCTAssertFalse(model.dismissingIDs.contains(suggestion.id))
    }

    func testStaleListCannotResurrectSuggestionAfterDismissAndOneReconciliationFollows() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let newEvidenceSuggestion = syntheticRecurrenceSuggestion(
            idSuffix: "b",
            proposedStartsOn: "2026-09-10",
            observedDates: ["2026-06-10", "2026-07-10", "2026-08-10"]
        )
        let api = ControlledSuggestionListAPI(initial: [suggestion])
        let model = RecurrenceSuggestionsViewModel(api: api)
        await model.loadIfNeeded()

        let olderLoad = Task { await model.refresh() }
        await api.waitForList(call: 2)
        let removal = awaitStateChange(of: model) { $0.suggestions.isEmpty }

        model.requestDismissal(suggestion)
        let dismiss = Task { await model.confirmDismissal() }
        await removal.value
        XCTAssertEqual(model.suggestions, [], "successful dismiss must be visible before the older GET ends")
        XCTAssertEqual(api.recurrenceSuggestionListRequestCount, 2)

        api.resolveList(call: 2, items: [suggestion])
        await api.waitForList(call: 3)
        XCTAssertEqual(model.suggestions, [], "the older GET resurrected a dismissed suggestion")

        let reconciliationPublished = awaitStateChange(of: model) {
            $0.suggestions == [newEvidenceSuggestion]
        }
        api.resolveList(call: 3, items: [newEvidenceSuggestion])
        await reconciliationPublished.value
        await olderLoad.value
        await dismiss.value
        XCTAssertEqual(model.suggestions, [newEvidenceSuggestion])
        XCTAssertEqual(api.recurrenceSuggestionListRequestCount, 3)
        XCTAssertEqual(api.recurrenceSuggestionDismissRequests, [suggestion.id])
    }

    func testDoubleDismissCoalescesPerSuggestion() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let api = SuspendedSuggestionActionsAPI(suggestion: suggestion)
        let model = RecurrenceSuggestionsViewModel(api: api)
        await model.loadIfNeeded()

        model.requestDismissal(suggestion)
        let dismissA = Task { await model.confirmDismissal() }
        await api.waitForDismiss()
        model.requestDismissal(suggestion)
        let dismissB = Task { await model.confirmDismissal() }
        XCTAssertEqual(api.recurrenceSuggestionDismissRequests, [suggestion.id])
        api.resolveDismiss(.success(DismissedRecurrenceSuggestion(replayed: true)))
        await dismissA.value
        await dismissB.value

        XCTAssertEqual(api.recurrenceSuggestionDismissRequests, [suggestion.id])
        XCTAssertEqual(model.suggestions, [])
    }

    func testListIsModelOwnedSingleFlightAndWaiterCancellationDoesNotCancelSharedRequest() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let api = ControlledSuggestionListAPI(initial: nil)
        let model = RecurrenceSuggestionsViewModel(api: api)

        let callerA = Task { await model.loadIfNeeded() }
        await api.waitForList(call: 1)
        let callerB = Task { await model.loadIfNeeded() }
        callerA.cancel()
        await Task.yield()
        XCTAssertEqual(api.recurrenceSuggestionListRequestCount, 1)
        XCTAssertEqual(api.listCancellationCount, 0)

        api.resolveList(call: 1, items: [suggestion])
        await callerA.value
        await callerB.value
        XCTAssertEqual(model.suggestions, [suggestion])
        XCTAssertEqual(api.recurrenceSuggestionListRequestCount, 1)
    }

    func testSuggestionPreviewUsesServerDataAndCreatesOnlyAfterExplicitConfirm() async throws {
        let suggestion = syntheticRecurrenceSuggestion(description: "Cliente não é autoridade", amount: 100)
        let serverPreview = syntheticRecurrencePreview(
            description: "Descrição canônica do servidor",
            amount: 12_345,
            startsOn: "2026-10-10"
        )
        let created = syntheticRecurrence(
            id: "rec_from_suggestion",
            description: serverPreview.description,
            amount: serverPreview.expectedAmount.minor,
            startsOn: serverPreview.startsOn.canonicalValue
        )
        let api = FinancialAPISpy()
        api.recurrenceSuggestionListResult = .success(RecurrenceSuggestionList(items: [suggestion]))
        api.recurrenceSuggestionPreviewResults = [.success(serverPreview)]
        api.recurrenceCreateResults = [.success(RecordedRecurrence(recurrence: created, replayed: false))]
        let suggestions = RecurrenceSuggestionsViewModel(api: api)
        let recurrences = RecurrencesViewModel(
            api: api,
            now: Date(timeIntervalSinceReferenceDate: 808_000_000),
            makeIdempotencyKey: { "suggestion-create-key" },
            onRecurrenceConfirmed: { [weak suggestions] suggestionID in
                suggestions?.recurrenceWasConfirmed(suggestionID: suggestionID)
            }
        )
        await suggestions.loadIfNeeded()

        let preparedPreview = await suggestions.prepareForReview(suggestion)
        let preview = try XCTUnwrap(preparedPreview)
        recurrences.beginSuggestionReview(preview: preview, suggestionID: suggestion.id)

        XCTAssertTrue(api.recurrenceCreateRequests.isEmpty)
        XCTAssertEqual(recurrences.reviewedRecurrence?.source, .suggestion(id: suggestion.id))
        XCTAssertEqual(recurrences.reviewedRecurrence?.request.description, serverPreview.description)
        XCTAssertEqual(recurrences.reviewedRecurrence?.request.expectedAmount, serverPreview.expectedAmount)
        XCTAssertEqual(recurrences.reviewedRecurrence?.request.startsOn, serverPreview.startsOn)

        api.recurrenceSuggestionListResult = .success(RecurrenceSuggestionList(items: []))
        await recurrences.confirm()

        XCTAssertEqual(api.recurrenceCreateRequests.count, 1)
        XCTAssertEqual(api.recurrenceCreateRequests[0].request.description, serverPreview.description)
        XCTAssertEqual(api.recurrenceCreateRequests[0].key, "suggestion-create-key")
        XCTAssertEqual(recurrences.successfulRecurrence, created)
        XCTAssertEqual(suggestions.suggestions, [])
        XCTAssertTrue(api.createRequests.isEmpty)
        XCTAssertTrue(api.incomeCreateRequests.isEmpty)
    }

    func testConfirmedRecurrenceRemovesSourceSuggestionAndForcesServerReconciliation() async throws {
        let suggestion = syntheticRecurrenceSuggestion()
        let api = ControlledSuggestionListAPI(initial: [suggestion])
        let suggestions = RecurrenceSuggestionsViewModel(api: api)
        let recurrences = RecurrencesViewModel(
            api: api,
            now: Date(timeIntervalSinceReferenceDate: 808_000_000),
            onRecurrenceConfirmed: { [weak suggestions] suggestionID in
                suggestions?.recurrenceWasConfirmed(suggestionID: suggestionID)
            }
        )
        await suggestions.loadIfNeeded()
        let preparedValue = await suggestions.prepareForReview(suggestion)
        let prepared = try XCTUnwrap(preparedValue)
        recurrences.beginSuggestionReview(preview: prepared, suggestionID: suggestion.id)

        await recurrences.confirm()
        await api.waitForList(call: 2)

        XCTAssertEqual(suggestions.suggestions, [])
        XCTAssertEqual(api.recurrenceCreateRequests.count, 1)
        XCTAssertEqual(api.recurrenceSuggestionListRequestCount, 2)
        api.resolveList(call: 2, items: [])
    }

    func testPreviewDoubleTapIsSingleFlightAndStaleOrSuppressedRemovesItemWithoutCreate() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let api = SuspendedSuggestionPreviewAPI(suggestion: suggestion)
        let model = RecurrenceSuggestionsViewModel(api: api)
        await model.loadIfNeeded()

        let first = Task { await model.prepareForReview(suggestion) }
        await api.waitForPreview()
        let second = Task { await model.prepareForReview(suggestion) }
        XCTAssertEqual(api.recurrenceSuggestionPreviewRequests, [suggestion.id])
        api.resolvePreview(.failure(FinancialAPIError.suggestionSuppressed))
        _ = await first.value
        _ = await second.value

        XCTAssertEqual(api.recurrenceSuggestionPreviewRequests, [suggestion.id])
        XCTAssertEqual(model.suggestions, [])
        XCTAssertEqual(model.noticeMessage, FinancialAPIError.suggestionSuppressed.userMessage)
        XCTAssertTrue(api.recurrenceCreateRequests.isEmpty)
    }

    func testPreviewNetworkFailurePreservesSuggestionForRetry() async {
        let suggestion = syntheticRecurrenceSuggestion()
        let api = FinancialAPISpy()
        api.recurrenceSuggestionListResult = .success(RecurrenceSuggestionList(items: [suggestion]))
        api.recurrenceSuggestionPreviewResults = [.failure(FinancialAPIError.connectionUnavailable)]
        let model = RecurrenceSuggestionsViewModel(api: api)
        await model.loadIfNeeded()

        let preview = await model.prepareForReview(suggestion)
        XCTAssertNil(preview)

        XCTAssertEqual(model.suggestions, [suggestion])
        XCTAssertEqual(model.actionErrors[suggestion.id], FinancialAPIError.connectionUnavailable.userMessage)
        XCTAssertFalse(model.previewingIDs.contains(suggestion.id))
        XCTAssertTrue(api.recurrenceCreateRequests.isEmpty)
    }

    func testRecurrenceAndSuggestionFailuresRemainIndependent() async {
        let api = FinancialAPISpy()
        api.recurrenceListResult = .failure(FinancialAPIError.connectionUnavailable)
        api.recurrenceSuggestionListResult = .success(
            RecurrenceSuggestionList(items: [syntheticRecurrenceSuggestion()])
        )
        let app = AppModel(api: api)

        async let recurrences: Void = app.recurrences.loadIfNeeded()
        async let suggestions: Void = app.recurrenceSuggestions.loadIfNeeded()
        _ = await (recurrences, suggestions)

        guard case .failed = app.recurrences.listState else {
            return XCTFail("Expected only the confirmed recurrence list to fail")
        }
        XCTAssertEqual(app.recurrenceSuggestions.suggestions, [syntheticRecurrenceSuggestion()])
    }

    private func awaitStateChange(
        of model: RecurrenceSuggestionsViewModel,
        until predicate: @escaping @MainActor (RecurrenceSuggestionsViewModel) -> Bool
    ) -> Task<Void, Never> {
        Task { @MainActor in
            while !predicate(model) {
                await withCheckedContinuation { continuation in
                    withObservationTracking {
                        _ = model.listState
                    } onChange: {
                        Task { @MainActor in continuation.resume() }
                    }
                }
            }
        }
    }

}

@MainActor
private final class ControlledSuggestionListAPI: FinancialAPISpy {
    private var initial: [RecurrenceSuggestion]?
    private var listContinuations: [Int: CheckedContinuation<RecurrenceSuggestionList, Error>] = [:]
    private var listWaiters: [Int: CheckedContinuation<Void, Never>] = [:]
    private(set) var listCancellationCount = 0

    init(initial: [RecurrenceSuggestion]?) {
        self.initial = initial
        super.init()
    }

    override func recurrenceSuggestions() async throws -> RecurrenceSuggestionList {
        recurrenceSuggestionListRequestCount += 1
        let call = recurrenceSuggestionListRequestCount
        if call == 1, let initial {
            self.initial = nil
            return RecurrenceSuggestionList(items: initial)
        }
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                listContinuations[call] = continuation
                listWaiters.removeValue(forKey: call)?.resume()
            }
        } onCancel: { [weak self] in
            Task { @MainActor in self?.listCancellationCount += 1 }
        }
    }

    override func dismissRecurrenceSuggestion(id: String) async throws -> DismissedRecurrenceSuggestion {
        recurrenceSuggestionDismissRequests.append(id)
        return DismissedRecurrenceSuggestion(replayed: false)
    }

    func waitForList(call: Int) async {
        guard listContinuations[call] == nil else { return }
        await withCheckedContinuation { listWaiters[call] = $0 }
    }

    func resolveList(call: Int, items: [RecurrenceSuggestion]) {
        guard let continuation = listContinuations.removeValue(forKey: call) else {
            preconditionFailure("List call \(call) is not suspended")
        }
        continuation.resume(returning: RecurrenceSuggestionList(items: items))
    }
}

@MainActor
private final class SuspendedSuggestionPreviewAPI: FinancialAPISpy {
    private let suggestion: RecurrenceSuggestion
    private var previewContinuation: CheckedContinuation<RecurrencePreview, Error>?
    private var previewWaiter: CheckedContinuation<Void, Never>?

    init(suggestion: RecurrenceSuggestion) {
        self.suggestion = suggestion
        super.init()
    }

    override func recurrenceSuggestions() async throws -> RecurrenceSuggestionList {
        recurrenceSuggestionListRequestCount += 1
        return RecurrenceSuggestionList(items: recurrenceSuggestionPreviewRequests.isEmpty ? [suggestion] : [])
    }

    override func previewRecurrenceSuggestion(id: String) async throws -> RecurrencePreview {
        recurrenceSuggestionPreviewRequests.append(id)
        return try await withCheckedThrowingContinuation { continuation in
            previewContinuation = continuation
            previewWaiter?.resume()
            previewWaiter = nil
        }
    }

    func waitForPreview() async {
        guard previewContinuation == nil else { return }
        await withCheckedContinuation { previewWaiter = $0 }
    }

    func resolvePreview(_ result: Result<RecurrencePreview, Error>) {
        guard let continuation = previewContinuation else {
            preconditionFailure("Preview is not suspended")
        }
        previewContinuation = nil
        continuation.resume(with: result)
    }
}

@MainActor
private final class SuspendedSuggestionActionsAPI: FinancialAPISpy {
    private let suggestion: RecurrenceSuggestion
    private var dismissContinuation: CheckedContinuation<DismissedRecurrenceSuggestion, Error>?
    private var dismissWaiter: CheckedContinuation<Void, Never>?

    init(suggestion: RecurrenceSuggestion) {
        self.suggestion = suggestion
        super.init()
    }

    override func recurrenceSuggestions() async throws -> RecurrenceSuggestionList {
        recurrenceSuggestionListRequestCount += 1
        return RecurrenceSuggestionList(items: recurrenceSuggestionDismissRequests.isEmpty ? [suggestion] : [])
    }

    override func dismissRecurrenceSuggestion(id: String) async throws -> DismissedRecurrenceSuggestion {
        recurrenceSuggestionDismissRequests.append(id)
        return try await withCheckedThrowingContinuation { continuation in
            dismissContinuation = continuation
            dismissWaiter?.resume()
            dismissWaiter = nil
        }
    }

    func waitForDismiss() async {
        guard dismissContinuation == nil else { return }
        await withCheckedContinuation { dismissWaiter = $0 }
    }

    func resolveDismiss(_ result: Result<DismissedRecurrenceSuggestion, Error>) {
        guard let continuation = dismissContinuation else {
            preconditionFailure("Dismiss is not suspended")
        }
        dismissContinuation = nil
        continuation.resume(with: result)
    }
}
