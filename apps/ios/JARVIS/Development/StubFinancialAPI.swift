#if DEBUG
import Foundation

@MainActor
final class StubFinancialAPI: FinancialAPI {
    private var transactionsByID: [String: FinancialTransaction] = [:]
    private var idempotency: [String: String] = [:]
    private var nextSequence = 1
    private let timestampCodec = RFC3339DateCodec()

    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview {
        try await Task.sleep(for: .milliseconds(80))
        let description = request.description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty, request.amount.minor > 0 else {
            throw FinancialAPIError.invalidData
        }
        let occurredAt = try canonicalTimestamp(request.occurredAt)
        return ExpensePreview(
            description: description,
            amount: request.amount,
            paymentMethod: request.paymentMethod,
            occurredAt: occurredAt,
            financialTimezone: "America/Sao_Paulo",
            origin: .ios
        )
    }

    func preview(_ request: IncomeRequest) async throws -> IncomePreview {
        try await Task.sleep(for: .milliseconds(80))
        let description = request.description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty, request.amount.minor > 0 else {
            throw FinancialAPIError.invalidData
        }
        let occurredAt = try canonicalTimestamp(request.occurredAt)
        return IncomePreview(
            description: description,
            amount: request.amount,
            occurredAt: occurredAt,
            financialTimezone: "America/Sao_Paulo",
            origin: .ios
        )
    }

    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense {
        try await Task.sleep(for: .milliseconds(80))
        let scopedKey = "EXPENSE:\(idempotencyKey)"
        if let existingID = idempotency[scopedKey],
           case let .expense(existing)? = transactionsByID[existingID]
        {
            return RecordedExpense(expense: existing, replayed: true)
        }

        let preview = try await preview(request)
        let id = nextID(prefix: "exp")
        let now = timestampCodec.encode(Date())
        let expense = Expense(
            id: id,
            description: preview.description,
            amount: preview.amount,
            paymentMethod: preview.paymentMethod,
            occurredAt: preview.occurredAt,
            financialTimezone: preview.financialTimezone,
            origin: preview.origin,
            status: .recorded,
            version: 1,
            createdAt: now,
            updatedAt: now
        )
        transactionsByID[id] = .expense(expense)
        idempotency[scopedKey] = id
        return RecordedExpense(expense: expense, replayed: false)
    }

    func create(_ request: IncomeRequest, idempotencyKey: String) async throws -> RecordedIncome {
        try await Task.sleep(for: .milliseconds(80))
        let scopedKey = "INCOME:\(idempotencyKey)"
        if let existingID = idempotency[scopedKey],
           case let .income(existing)? = transactionsByID[existingID]
        {
            return RecordedIncome(income: existing, replayed: true)
        }

        let preview = try await preview(request)
        let id = nextID(prefix: "inc")
        let now = timestampCodec.encode(Date())
        let income = Income(
            id: id,
            description: preview.description,
            amount: preview.amount,
            occurredAt: preview.occurredAt,
            financialTimezone: preview.financialTimezone,
            origin: preview.origin,
            status: .recorded,
            version: 1,
            createdAt: now,
            updatedAt: now
        )
        transactionsByID[id] = .income(income)
        idempotency[scopedKey] = id
        return RecordedIncome(income: income, replayed: false)
    }

    func transactions(month: String) async throws -> TransactionMonth {
        try await Task.sleep(for: .milliseconds(80))
        let items = transactionsByID.values
            .filter { transaction in
                guard let date = try? timestampCodec.decode(transaction.occurredAt) else { return false }
                return FinancialMonth(date: date).apiValue == month
            }
            .sorted {
                if $0.occurredAt == $1.occurredAt { return $0.id > $1.id }
                return $0.occurredAt > $1.occurredAt
            }
        return TransactionMonth(month: month, items: items)
    }

    private func canonicalTimestamp(_ value: String) throws -> String {
        timestampCodec.encode(try timestampCodec.decode(value))
    }

    private func nextID(prefix: String) -> String {
        defer { nextSequence += 1 }
        return "\(prefix)_ui_synthetic_\(String(format: "%03d", nextSequence))"
    }
}
#endif
