import XCTest
@testable import JARVIS

@MainActor
final class RegistrationViewModelTests: XCTestCase {
    private let fixedNow = Date(timeIntervalSinceReferenceDate: 808_000_000)

    func testPreviewMustCompleteBeforeCreateAndConfirmationIsExplicit() async {
        let api = FinancialAPISpy()
        let model = makeModel(api: api)
        fillValidDraft(model)

        await model.confirm()
        XCTAssertTrue(api.createRequests.isEmpty)

        await model.review()
        XCTAssertEqual(api.previewRequests.count, 1)
        XCTAssertTrue(api.createRequests.isEmpty)
        guard case .reviewing = model.state else {
            return XCTFail("Expected the reviewed state")
        }

        await model.confirm()
        XCTAssertEqual(api.createRequests.count, 1)
        guard case .success = model.state else {
            return XCTFail("Expected success after explicit confirmation")
        }
    }

    func testPreviewFreezesServerCanonicalFieldsForConfirmation() async {
        let api = FinancialAPISpy()
        api.previewResult = .success(syntheticPreview(description: "Mercado sintético", amount: 4_250))
        let model = makeModel(api: api)
        fillValidDraft(model)
        model.description = "  Mercado sintético  "

        await model.review()
        model.description = "Rascunho alterado após preview"
        model.amountText = "99,99"
        await model.confirm()

        let request = try? XCTUnwrap(api.createRequests.first?.request)
        XCTAssertEqual(request?.description, "Mercado sintético")
        XCTAssertEqual(request?.amount.minor, 4_250)
    }

    func testEditingInvalidatesReviewAndPendingOperation() async {
        let api = FinancialAPISpy()
        api.createResults = [.failure(FinancialAPIError.connectionUnavailable)]
        var keys = ["key-first", "key-second"]
        let model = makeModel(api: api, makeKey: { keys.removeFirst() })
        fillValidDraft(model)

        await model.review()
        await model.confirm()
        guard case .retryable = model.state else {
            return XCTFail("Expected retryable state")
        }

        model.edit()
        guard case .editing = model.state else {
            return XCTFail("Expected editing state")
        }

        api.previewResult = .success(syntheticPreview(description: "Transporte teste"))
        api.createResults = [.success(RecordedExpense(expense: syntheticExpense(), replayed: false))]
        model.description = "Transporte teste"
        await model.review()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["key-first", "key-second"])
    }

    func testRetryReusesIdempotencyKeyAndSuccessClearsIt() async {
        let api = FinancialAPISpy()
        api.createResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(RecordedExpense(expense: syntheticExpense(), replayed: true)),
            .success(RecordedExpense(expense: syntheticExpense(id: "exp_test_ios_002"), replayed: false))
        ]
        var keys = ["key-first", "key-second"]
        var refreshes = 0
        let model = makeModel(
            api: api,
            makeKey: { keys.removeFirst() },
            onRecorded: { refreshes += 1 }
        )
        fillValidDraft(model)

        await model.review()
        await model.confirm()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["key-first", "key-first"])
        XCTAssertEqual(refreshes, 1)

        model.startNewExpense(now: fixedNow)
        fillValidDraft(model)
        await model.review()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["key-first", "key-first", "key-second"])
        XCTAssertEqual(refreshes, 2)
    }

    func testPreviewAndDeterministicCreateFailuresExposeSafeMessagesAndRequireEditing() async {
        let api = FinancialAPISpy()
        api.previewResult = .failure(FinancialAPIError.serviceUnavailable)
        let model = makeModel(api: api)
        fillValidDraft(model)

        await model.review()
        guard case .editing = model.state else {
            return XCTFail("Preview failure must return to editing")
        }
        XCTAssertEqual(model.errorMessage, FinancialAPIError.serviceUnavailable.userMessage)

        api.previewResult = .success(syntheticPreview())
        api.createResults = [.failure(FinancialAPIError.conflict)]
        await model.review()
        await model.confirm()
        guard case .requiresEditing = model.state else {
            return XCTFail("A deterministic conflict must require editing")
        }
        XCTAssertEqual(model.errorMessage, FinancialAPIError.conflict.userMessage)

        await model.confirm()
        XCTAssertEqual(api.createRequests.count, 1, "A deterministic error cannot retry the same operation")
    }

    func testInvalidRequestRequiresEditingInsteadOfRetrying() async {
        let api = FinancialAPISpy()
        api.createResults = [.failure(FinancialAPIError.invalidData)]
        let model = makeModel(api: api)
        fillValidDraft(model)

        await model.review()
        await model.confirm()

        guard case .requiresEditing = model.state else {
            return XCTFail("An invalid request must require editing")
        }
        await model.confirm()
        XCTAssertEqual(api.createRequests.count, 1)
    }

    func testServiceUnavailableRetryReusesTheSameIdempotencyKey() async {
        let api = FinancialAPISpy()
        api.createResults = [
            .failure(FinancialAPIError.serviceUnavailable),
            .success(RecordedExpense(expense: syntheticExpense(), replayed: true))
        ]
        let model = makeModel(api: api, makeKey: { "key-service-unavailable" })
        fillValidDraft(model)

        await model.review()
        await model.confirm()
        guard case .retryable = model.state else {
            return XCTFail("A service failure must remain retryable")
        }
        await model.confirm()

        XCTAssertEqual(
            api.createRequests.map(\.key),
            ["key-service-unavailable", "key-service-unavailable"]
        )
    }

    private func makeModel(
        api: FinancialAPISpy,
        makeKey: @escaping () -> String = { "key-synthetic" },
        onRecorded: @escaping () -> Void = {}
    ) -> RegistrationViewModel {
        RegistrationViewModel(
            api: api,
            now: fixedNow,
            makeIdempotencyKey: makeKey,
            onExpenseRecorded: onRecorded
        )
    }

    private func fillValidDraft(_ model: RegistrationViewModel) {
        model.description = "Mercado sintético"
        model.amountText = "42,50"
        model.paymentMethod = .pix
        model.occurredAt = fixedNow
    }
}
