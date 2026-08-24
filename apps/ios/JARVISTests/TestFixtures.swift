import Foundation
@testable import JARVIS

func syntheticPreview(
    description: String = "Mercado sintético",
    amount: Int64 = 4_250,
    categoryID: String? = nil,
    occurredAt: String = "2026-08-14T15:00:00Z"
) -> ExpensePreview {
    ExpensePreview(
        description: description,
        amount: ExpenseMoney(minor: amount, currency: .brl),
        paymentMethod: .pix,
        categoryID: categoryID,
        occurredAt: occurredAt,
        financialTimezone: "America/Sao_Paulo",
        origin: .ios
    )
}

func syntheticExpense(
    id: String = "exp_test_ios_001",
    description: String = "Mercado sintético",
    amount: Int64 = 4_250,
    categoryID: String? = nil,
    occurredAt: String = "2026-08-14T15:00:00Z"
) -> Expense {
    Expense(
        id: id,
        description: description,
        amount: ExpenseMoney(minor: amount, currency: .brl),
        paymentMethod: .pix,
        categoryID: categoryID,
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
    categoryID: String? = nil,
    occurredAt: String = "2026-08-14T16:00:00Z"
) -> IncomePreview {
    IncomePreview(
        description: description,
        amount: FinancialMoney(minor: amount, currency: .brl),
        categoryID: categoryID,
        occurredAt: occurredAt,
        financialTimezone: "America/Sao_Paulo",
        origin: .ios
    )
}

func syntheticIncome(
    id: String = "inc_test_ios_001",
    description: String = "Receita sintética",
    amount: Int64 = 8_500,
    categoryID: String? = nil,
    occurredAt: String = "2026-08-14T16:00:00Z"
) -> Income {
    Income(
        id: id,
        description: description,
        amount: FinancialMoney(minor: amount, currency: .brl),
        categoryID: categoryID,
        occurredAt: occurredAt,
        financialTimezone: "America/Sao_Paulo",
        origin: .ios,
        status: .recorded,
        version: 1,
        createdAt: "2026-08-14T18:00:00Z",
        updatedAt: "2026-08-14T18:00:00Z"
    )
}

func syntheticRecurrencePreview(
    description: String = "Academia sintética",
    amount: Int64 = 11_900,
    startsOn: String = "2026-08-31"
) -> RecurrencePreview {
    RecurrencePreview(
        description: description,
        expectedAmount: FinancialMoney(minor: amount, currency: .brl),
        startsOn: try! RecurrenceCivilDate(startsOn)
    )
}

func syntheticRecurrence(
    id: String = "rec_test_ios_001",
    description: String = "Academia sintética",
    amount: Int64 = 11_900,
    startsOn: String = "2026-08-31",
    status: RecurrenceStatus = .active,
    cancelledAt: String? = nil
) -> Recurrence {
    Recurrence(
        id: id,
        description: description,
        expectedAmount: FinancialMoney(minor: amount, currency: .brl),
        startsOn: try! RecurrenceCivilDate(startsOn),
        status: status,
        createdAt: "2026-08-16T18:00:00Z",
        cancelledAt: cancelledAt
    )
}

let syntheticCategories = [
    CategoryDefinition(id: "expense.food", type: .expense, displayName: "Alimentação"),
    CategoryDefinition(id: "expense.other", type: .expense, displayName: "Outros"),
    CategoryDefinition(id: "income.salary", type: .income, displayName: "Salário"),
    CategoryDefinition(id: "income.other", type: .income, displayName: "Outros")
]

@MainActor
class FinancialAPISpy: FinancialAPI {
    var categoryRequestCount = 0
    var previewRequests: [ExpenseRequest] = []
    var createRequests: [(request: ExpenseRequest, key: String)] = []
    var incomePreviewRequests: [IncomeRequest] = []
    var incomeCreateRequests: [(request: IncomeRequest, key: String)] = []
    var requestedMonths: [String] = []
    var recurrencePreviewRequests: [RecurrenceRequest] = []
    var recurrenceCreateRequests: [(request: RecurrenceRequest, key: String)] = []
    var recurrenceCancelRequests: [(id: String, key: String)] = []
    var recurrenceListRequestCount = 0

    var categoriesResult: Result<[CategoryDefinition], Error> = .success(syntheticCategories)

    var previewResult: Result<ExpensePreview, Error> = .success(syntheticPreview())
    var createResults: [Result<RecordedExpense, Error>] = [
        .success(RecordedExpense(expense: syntheticExpense(), replayed: false))
    ]
    var incomePreviewResult: Result<IncomePreview, Error> = .success(syntheticIncomePreview())
    var incomeCreateResults: [Result<RecordedIncome, Error>] = [
        .success(RecordedIncome(income: syntheticIncome(), replayed: false))
    ]
    var monthResult: Result<TransactionMonth, Error> = .success(TransactionMonth(month: "2026-08", items: []))
    var recurrencePreviewResult: Result<RecurrencePreview, Error> = .success(syntheticRecurrencePreview())
    var recurrenceCreateResults: [Result<RecordedRecurrence, Error>] = [
        .success(RecordedRecurrence(recurrence: syntheticRecurrence(), replayed: false))
    ]
    var recurrenceCancelResults: [Result<RecordedRecurrence, Error>] = [
        .success(
            RecordedRecurrence(
                recurrence: syntheticRecurrence(
                    status: .cancelled,
                    cancelledAt: "2026-08-17T18:00:00Z"
                ),
                replayed: false
            )
        )
    ]
    var recurrenceListResult: Result<RecurrenceList, Error> = .success(RecurrenceList(items: []))

    func categories() async throws -> [CategoryDefinition] {
        categoryRequestCount += 1
        return try categoriesResult.get()
    }

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

    func previewRecurrence(_ request: RecurrenceRequest) async throws -> RecurrencePreview {
        recurrencePreviewRequests.append(request)
        return try recurrencePreviewResult.get()
    }

    func createRecurrence(
        _ request: RecurrenceRequest,
        idempotencyKey: String
    ) async throws -> RecordedRecurrence {
        recurrenceCreateRequests.append((request, idempotencyKey))
        guard !recurrenceCreateResults.isEmpty else { throw FinancialAPIError.serviceUnavailable }
        return try recurrenceCreateResults.removeFirst().get()
    }

    func recurrences() async throws -> RecurrenceList {
        recurrenceListRequestCount += 1
        return try recurrenceListResult.get()
    }

    func cancelRecurrence(id: String, idempotencyKey: String) async throws -> RecordedRecurrence {
        recurrenceCancelRequests.append((id, idempotencyKey))
        guard !recurrenceCancelResults.isEmpty else { throw FinancialAPIError.serviceUnavailable }
        return try recurrenceCancelResults.removeFirst().get()
    }
}
