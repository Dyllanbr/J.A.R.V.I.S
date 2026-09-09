import XCTest
@testable import JARVIS

@MainActor
final class FinancialGoalsViewModelTests: XCTestCase {
    func testLoadsEmptyAndLoadedStates() async throws {
        let api = FinancialAPISpy()
        let model = FinancialGoalsViewModel(api: api)
        await model.loadIfNeeded()
        XCTAssertEqual(model.state, .empty)

        api.financialGoalsResult = .success(try FinancialGoalsResponse(
            goals: [try FinancialGoal(id: "goal_1", title: "Viagem", targetAmount: try FinancialGoalAmount(minor: 1))],
            protectedValues: [try ProtectedValue(id: "value_1", label: "Reserva", amount: try ProtectedValueAmount(minor: 0))]
        ))
        await model.load(forceRefresh: true)
        guard case let .loaded(response) = model.state else { return XCTFail("Expected loaded state") }
        XCTAssertEqual(response.goals.count, 1)
        XCTAssertEqual(response.protectedValues.count, 1)
    }

    func testFailureAndRetryAreExplicit() async {
        let api = FinancialAPISpy()
        api.financialGoalsResult = .failure(FinancialAPIError.serviceUnavailable)
        let model = FinancialGoalsViewModel(api: api)
        await model.loadIfNeeded()
        XCTAssertEqual(model.state, .failed(FinancialAPIError.serviceUnavailable.userMessage))

        api.financialGoalsResult = .success(try! FinancialGoalsResponse(goals: [], protectedValues: []))
        await model.retry()
        XCTAssertEqual(model.state, .empty)
        XCTAssertEqual(api.financialGoalsRequestCount, 2)
    }

    func testReplacementReloadsTheSingleSnapshot() async throws {
        let api = FinancialAPISpy()
        let model = FinancialGoalsViewModel(api: api)
        await model.replaceProtectedValue(
            id: "value_1",
            label: "Reserva",
            amount: try ProtectedValueAmount(minor: 5_000)
        )
        XCTAssertEqual(api.protectedValueReplacementRequests.count, 1)
        XCTAssertEqual(api.financialGoalsRequestCount, 1)
        XCTAssertEqual(model.state, .empty)
    }
}
