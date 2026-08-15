import XCTest
@testable import JARVIS

@MainActor
final class HistoryViewModelTests: XCTestCase {
    func testFormatsAndNavigatesMonthsDeterministically() {
        let august = FinancialMonth(year: 2026, month: 8)
        XCTAssertEqual(august.apiValue, "2026-08")
        XCTAssertEqual(august.adding(months: -1).apiValue, "2026-07")
        XCTAssertEqual(august.adding(months: 1).apiValue, "2026-09")
        XCTAssertEqual(FinancialMonth(year: 2026, month: 12).adding(months: 1).apiValue, "2027-01")
    }

    func testLoadPreservesServerOrderingAndSupportsEmptyState() async {
        let api = FinancialAPISpy()
        let first = syntheticExpense(id: "exp_002", description: "Restaurante QA")
        let second = syntheticExpense(id: "exp_001", description: "Mercado sintético")
        api.monthResult = .success(TransactionMonth(month: "2026-08", items: [.expense(first), .expense(second)]))
        let model = HistoryViewModel(api: api, now: syntheticAugustDate())

        await model.load()
        guard case let .loaded(items) = model.state else {
            return XCTFail("Expected loaded history")
        }
        XCTAssertEqual(items.map(\.id), ["exp_002", "exp_001"])
        XCTAssertEqual(api.requestedMonths, ["2026-08"])

        api.monthResult = .success(TransactionMonth(month: "2026-08", items: []))
        await model.load()
        XCTAssertEqual(model.state, .loaded([]))
    }

    func testLoadPreservesMixedExpenseAndIncomeOrdering() async {
        let api = FinancialAPISpy()
        let expense = syntheticExpense(id: "exp_001")
        let income = syntheticIncome(id: "inc_001")
        api.monthResult = .success(
            TransactionMonth(month: "2026-08", items: [.income(income), .expense(expense)])
        )
        let model = HistoryViewModel(api: api, now: syntheticAugustDate())

        await model.load()

        guard case let .loaded(items) = model.state else {
            return XCTFail("Expected loaded history")
        }
        XCTAssertEqual(items, [.income(income), .expense(expense)])
        XCTAssertEqual(items.map(\.type), [.income, .expense])
    }

    func testLoadMapsErrorsToSafeUIState() async {
        let api = FinancialAPISpy()
        api.monthResult = .failure(FinancialAPIError.connectionUnavailable)
        let model = HistoryViewModel(api: api, now: syntheticAugustDate())

        await model.load()
        XCTAssertEqual(model.state, .failed(FinancialAPIError.connectionUnavailable.userMessage))
    }

    private func syntheticAugustDate() -> Date {
        Calendar.financial.date(from: DateComponents(year: 2026, month: 8, day: 14)) ?? Date(timeIntervalSince1970: 0)
    }
}
