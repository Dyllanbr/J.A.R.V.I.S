import Foundation
@testable import JARVIS

func syntheticPreview(
    description: String = "Mercado sintético",
    amount: Int64 = 4_250,
    occurredAt: String = "2026-08-14T15:00:00Z"
) -> ExpensePreview {
    ExpensePreview(
        type: .expense,
        description: description,
        amount: ExpenseMoney(minor: amount, currency: .brl),
        paymentMethod: .pix,
        occurredAt: occurredAt,
        financialTimezone: "America/Sao_Paulo",
        origin: .ios
    )
}

func syntheticExpense(
    id: String = "exp_test_ios_001",
    description: String = "Mercado sintético",
    amount: Int64 = 4_250,
    occurredAt: String = "2026-08-14T15:00:00Z"
) -> Expense {
    Expense(
        id: id,
        type: .expense,
        description: description,
        amount: ExpenseMoney(minor: amount, currency: .brl),
        paymentMethod: .pix,
        occurredAt: occurredAt,
        financialTimezone: "America/Sao_Paulo",
        origin: .ios,
        status: .recorded,
        version: 1,
        createdAt: "2026-08-14T18:00:00Z",
        updatedAt: "2026-08-14T18:00:00Z"
    )
}

@MainActor
final class FinancialAPISpy: FinancialAPI {
    var previewRequests: [ExpenseRequest] = []
    var createRequests: [(request: ExpenseRequest, key: String)] = []
    var requestedMonths: [String] = []

    var previewResult: Result<ExpensePreview, Error> = .success(syntheticPreview())
    var createResults: [Result<RecordedExpense, Error>] = [
        .success(RecordedExpense(expense: syntheticExpense(), replayed: false))
    ]
    var monthResult: Result<ExpenseMonth, Error> = .success(ExpenseMonth(month: "2026-08", items: []))

    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview {
        previewRequests.append(request)
        return try previewResult.get()
    }

    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense {
        createRequests.append((request, idempotencyKey))
        guard !createResults.isEmpty else { throw FinancialAPIError.serviceUnavailable }
        return try createResults.removeFirst().get()
    }

    func expenses(month: String) async throws -> ExpenseMonth {
        requestedMonths.append(month)
        return try monthResult.get()
    }
}
