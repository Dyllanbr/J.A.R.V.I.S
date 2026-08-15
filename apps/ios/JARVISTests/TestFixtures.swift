import Foundation
@testable import JARVIS

func syntheticPreview(
    description: String = "Mercado sintético",
    amount: Int64 = 4_250,
    occurredAt: String = "2026-08-14T15:00:00Z"
) -> ExpensePreview {
    ExpensePreview(
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

func syntheticIncomePreview(
    description: String = "Receita sintética",
    amount: Int64 = 8_500,
    occurredAt: String = "2026-08-14T16:00:00Z"
) -> IncomePreview {
    IncomePreview(
        description: description,
        amount: FinancialMoney(minor: amount, currency: .brl),
        occurredAt: occurredAt,
        financialTimezone: "America/Sao_Paulo",
        origin: .ios
    )
}

func syntheticIncome(
    id: String = "inc_test_ios_001",
    description: String = "Receita sintética",
    amount: Int64 = 8_500,
    occurredAt: String = "2026-08-14T16:00:00Z"
) -> Income {
    Income(
        id: id,
        description: description,
        amount: FinancialMoney(minor: amount, currency: .brl),
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
    var incomePreviewRequests: [IncomeRequest] = []
    var incomeCreateRequests: [(request: IncomeRequest, key: String)] = []
    var requestedMonths: [String] = []

    var previewResult: Result<ExpensePreview, Error> = .success(syntheticPreview())
    var createResults: [Result<RecordedExpense, Error>] = [
        .success(RecordedExpense(expense: syntheticExpense(), replayed: false))
    ]
    var incomePreviewResult: Result<IncomePreview, Error> = .success(syntheticIncomePreview())
    var incomeCreateResults: [Result<RecordedIncome, Error>] = [
        .success(RecordedIncome(income: syntheticIncome(), replayed: false))
    ]
    var monthResult: Result<TransactionMonth, Error> = .success(TransactionMonth(month: "2026-08", items: []))

    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview {
        previewRequests.append(request)
        return try previewResult.get()
    }

    func preview(_ request: IncomeRequest) async throws -> IncomePreview {
        incomePreviewRequests.append(request)
        return try incomePreviewResult.get()
    }

    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense {
        createRequests.append((request, idempotencyKey))
        guard !createResults.isEmpty else { throw FinancialAPIError.serviceUnavailable }
        return try createResults.removeFirst().get()
    }

    func create(_ request: IncomeRequest, idempotencyKey: String) async throws -> RecordedIncome {
        incomeCreateRequests.append((request, idempotencyKey))
        guard !incomeCreateResults.isEmpty else { throw FinancialAPIError.serviceUnavailable }
        return try incomeCreateResults.removeFirst().get()
    }

    func transactions(month: String) async throws -> TransactionMonth {
        requestedMonths.append(month)
        return try monthResult.get()
    }
}
