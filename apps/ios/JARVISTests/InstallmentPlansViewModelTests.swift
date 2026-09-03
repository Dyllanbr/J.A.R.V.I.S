import Foundation
import XCTest
@testable import JARVIS

@MainActor
final class InstallmentPlansViewModelTests: XCTestCase {
    func testListIsSortedAndOwnerIndependentAPIIsUsedOnce() async throws {
        let spy = FinancialAPISpy()
        let first = try plan(idSuffix: "b", status: .active, firstDueDate: "2026-10-12")
        let second = try plan(idSuffix: "a", status: .active, firstDueDate: "2026-09-12")
        let cancelled = try plan(idSuffix: "c", status: .cancelled, firstDueDate: "2026-08-12", cancelledOn: "2026-08-20")
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: [first, cancelled, second]))
        let model = InstallmentPlansViewModel(api: spy)

        await model.load()

        XCTAssertEqual(model.plans.map(\.id), [second.id, first.id, cancelled.id])
        XCTAssertEqual(spy.installmentPlanListRequestCount, 1)
    }

    func testCancellationPreviewAndRetryPreserveKeyAndRefreshDetail() async throws {
        let spy = FinancialAPISpy()
        let active = try plan(idSuffix: "a")
        let cancellation = try InstallmentPlanCancellationPreview(
            installmentPlanID: active.id,
            expectedCancelledOn: try RecurrenceCivilDate("2026-08-20"),
            plan: active
        )
        let cancelled = try plan(idSuffix: "a", status: .cancelled, firstDueDate: "2026-08-12", cancelledOn: "2026-08-20")
        spy.installmentPlanCancellationPreviewResults = [.success(cancellation)]
        spy.installmentPlanCancelResults = [
            .failure(FinancialAPIError.serviceUnavailable),
            .success(RecordedInstallmentPlan(plan: cancelled, replayed: true))
        ]
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: [active]))
        var keyCounter = 0
        let model = InstallmentPlansViewModel(api: spy, makeIdempotencyKey: {
            keyCounter += 1
            return "cancel-key-\(keyCounter)"
        })

        await model.previewCancellation(id: active.id)
        guard case .reviewing = model.cancellationStates[active.id] else { return XCTFail("Expected cancellation review") }
        await model.cancel(id: active.id)
        guard case .retryable = model.cancellationStates[active.id] else { return XCTFail("Expected retryable cancellation") }
        await model.retryCancellation(id: active.id)

        guard case let .cancelled(result) = model.cancellationStates[active.id] else { return XCTFail("Expected cancelled state") }
        XCTAssertEqual(result, cancelled)
        XCTAssertEqual(keyCounter, 1)
        XCTAssertEqual(spy.installmentPlanCancelRequests.map(\.key), ["cancel-key-1", "cancel-key-1"])
        XCTAssertEqual(model.detailStates[active.id], .loaded(cancelled))
    }

    func testCancellationPreviewNotFoundReconcilesAndInvalidatesPlan() async throws {
        let spy = FinancialAPISpy()
        let active = try plan(idSuffix: "a")
        spy.installmentPlanCancellationPreviewResults = [.failure(FinancialAPIError.installmentPlanNotFound)]
        spy.installmentPlanDetailResults = [.failure(FinancialAPIError.installmentPlanNotFound)]
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: []))
        let model = InstallmentPlansViewModel(api: spy)

        await model.previewCancellation(id: active.id)

        XCTAssertEqual(model.cancellationStates[active.id], .idle)
        XCTAssertEqual(model.detailStates[active.id], .failed(FinancialAPIError.installmentPlanNotFound.userMessage))
        XCTAssertEqual(model.plans, [])
        XCTAssertEqual(model.errors[active.id], FinancialAPIError.installmentPlanNotFound.userMessage)
        XCTAssertEqual(spy.installmentPlanCancelRequests.count, 0)
    }

    func testCancellationStartNotFoundReconcilesAndCannotBeRetried() async throws {
        let spy = FinancialAPISpy()
        let active = try plan(idSuffix: "a")
        let cancellation = try InstallmentPlanCancellationPreview(
            installmentPlanID: active.id,
            expectedCancelledOn: try RecurrenceCivilDate("2026-08-20"),
            plan: active
        )
        spy.installmentPlanCancellationPreviewResults = [.success(cancellation)]
        spy.installmentPlanCancelResults = [.failure(FinancialAPIError.installmentPlanNotFound)]
        spy.installmentPlanDetailResults = [.failure(FinancialAPIError.installmentPlanNotFound)]
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: []))
        let model = InstallmentPlansViewModel(api: spy)

        await model.previewCancellation(id: active.id)
        await model.cancel(id: active.id)
        await model.cancel(id: active.id)

        XCTAssertEqual(model.cancellationStates[active.id], .idle)
        XCTAssertEqual(model.detailStates[active.id], .failed(FinancialAPIError.installmentPlanNotFound.userMessage))
        XCTAssertEqual(model.plans, [])
        XCTAssertEqual(spy.installmentPlanCancelRequests.count, 1)
    }

    func testAlreadyCancelledReconcilesStatusAndRemovesCancellationAction() async throws {
        let spy = FinancialAPISpy()
        let active = try plan(idSuffix: "a")
        let cancelled = try plan(idSuffix: "a", status: .cancelled, cancelledOn: "2026-08-20")
        spy.installmentPlanCancellationPreviewResults = [.failure(FinancialAPIError.installmentPlanAlreadyCancelled)]
        spy.installmentPlanDetailResults = [.success(cancelled)]
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: [cancelled]))
        let model = InstallmentPlansViewModel(api: spy)

        await model.previewCancellation(id: active.id)
        await model.cancel(id: active.id)

        XCTAssertEqual(model.cancellationStates[active.id], .idle)
        XCTAssertEqual(model.detailStates[active.id], .loaded(cancelled))
        XCTAssertEqual(model.plans, [cancelled])
        XCTAssertEqual(cancelled.status, .cancelled)
        XCTAssertEqual(model.errors[active.id], FinancialAPIError.installmentPlanAlreadyCancelled.userMessage)
        XCTAssertEqual(spy.installmentPlanCancelRequests.count, 0)
    }

    func testCancellationDateStaleInvalidatesReviewAndRefreshesCurrentPlan() async throws {
        let spy = FinancialAPISpy()
        let active = try plan(idSuffix: "a")
        let cancellation = try InstallmentPlanCancellationPreview(
            installmentPlanID: active.id,
            expectedCancelledOn: try RecurrenceCivilDate("2026-08-20"),
            plan: active
        )
        spy.installmentPlanCancellationPreviewResults = [.success(cancellation)]
        spy.installmentPlanCancelResults = [.failure(FinancialAPIError.installmentCancellationDateStale)]
        spy.installmentPlanDetailResults = [.success(active)]
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: [active]))
        let model = InstallmentPlansViewModel(api: spy)

        await model.previewCancellation(id: active.id)
        await model.cancel(id: active.id)
        await model.cancel(id: active.id)

        XCTAssertEqual(model.cancellationStates[active.id], .idle)
        XCTAssertEqual(model.detailStates[active.id], .loaded(active))
        XCTAssertEqual(model.plans, [active])
        XCTAssertEqual(model.errors[active.id], FinancialAPIError.installmentCancellationDateStale.userMessage)
        XCTAssertEqual(spy.installmentPlanCancelRequests.count, 1)
    }

    func testObsoleteCancellationPreviewCannotOverwriteRefreshedDetail() async throws {
        let spy = FinancialAPISpy()
        let active = try plan(idSuffix: "a")
        let cancellation = try InstallmentPlanCancellationPreview(
            installmentPlanID: active.id,
            expectedCancelledOn: try RecurrenceCivilDate("2026-08-20"),
            plan: active
        )
        spy.installmentPlanCancellationPreviewResults = [.success(cancellation)]
        spy.installmentPlanDetailResults = [.success(active)]
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: [active]))
        spy.blockInstallmentPlanCancellationPreview = true
        let model = InstallmentPlansViewModel(api: spy)

        let previewTask = Task { @MainActor in await model.previewCancellation(id: active.id) }
        await spy.waitForInstallmentPlanCancellationPreviewStart()
        await model.refreshDetail(id: active.id)
        spy.releaseInstallmentPlanCancellationPreview()
        await previewTask.value

        XCTAssertEqual(model.cancellationStates[active.id], .idle)
        XCTAssertEqual(model.detailStates[active.id], .loaded(active))
    }

    func testConfirmationStartsSubmissionBeforeAlertDismissal() async throws {
        let spy = FinancialAPISpy()
        let active = try plan(idSuffix: "a")
        let cancellation = try InstallmentPlanCancellationPreview(
            installmentPlanID: active.id,
            expectedCancelledOn: try RecurrenceCivilDate("2026-08-20"),
            plan: active
        )
        let cancelled = try plan(idSuffix: "a", status: .cancelled, firstDueDate: "2026-08-12", cancelledOn: "2026-08-20")
        spy.installmentPlanCancellationPreviewResults = [.success(cancellation)]
        spy.installmentPlanCancelResults = [.success(RecordedInstallmentPlan(plan: cancelled, replayed: false))]
        spy.installmentPlanListResult = .success(InstallmentPlanListResponse(items: [cancelled]))
        let model = InstallmentPlansViewModel(api: spy, makeIdempotencyKey: { "cancel-key" })

        await model.previewCancellation(id: active.id)
        let task = try XCTUnwrap(model.confirmCancellation(id: active.id))
        await task.value

        XCTAssertEqual(spy.installmentPlanCancelRequests.map(\.key), ["cancel-key"])
        guard case let .cancelled(result) = model.cancellationStates[active.id] else {
            return XCTFail("Expected cancelled state")
        }
        XCTAssertEqual(result, cancelled)
    }

    func testInvalidPlanIDFailsWithoutCallingAPI() async {
        let spy = FinancialAPISpy()
        let model = InstallmentPlansViewModel(api: spy)

        await model.loadDetail(id: "not-a-plan")

        XCTAssertTrue(spy.installmentPlanDetailRequests.isEmpty)
        guard case .failed = model.detailStates["not-a-plan"] else { return XCTFail("Expected not found state") }
    }

    private func plan(idSuffix: Character, status: InstallmentPlanStatus = .active, firstDueDate: String = "2026-08-12", cancelledOn: String? = nil) throws -> InstallmentPlan {
        let planID = "ipl_\(String(repeating: idSuffix, count: 32))"
        let cardID = "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        let expenseID = "exp_\(String(repeating: idSuffix, count: 12))"
        let statusJSON = status == .active ? "\"status\":\"ACTIVE\"" : "\"status\":\"CANCELLED\",\"cancelledOn\":\"\(cancelledOn!)\""
        let json = """
        {"id":"\(planID)","creditCardId":"\(cardID)","expenseId":"\(expenseID)","totalAmount":{"minor":12000,"currency":"BRL"},"installmentCount":2,"firstDueDate":"\(firstDueDate)","dueDayAnchor":12,\(statusJSON),"createdAt":"2026-08-14T15:00:00Z","schedule":[{"number":1,"totalCount":2,"dueDate":"\(firstDueDate)","amount":{"minor":6000,"currency":"BRL"}},{"number":2,"totalCount":2,"dueDate":"2026-09-12","amount":{"minor":6000,"currency":"BRL"}}]}
        """
        return try JSONDecoder().decode(InstallmentPlan.self, from: Data(json.utf8))
    }
}
