#if DEBUG
import Foundation

@MainActor
final class StubFinancialAPI: FinancialAPI {
    private var expensesByID: [String: Expense] = [:]
    private var idempotency: [String: String] = [:]
    private let timestampCodec = RFC3339DateCodec()

    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview {
        try await Task.sleep(for: .milliseconds(80))
        let description = request.description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty, request.amount.minor > 0 else {
            throw FinancialAPIError.invalidData
        }
        let occurredAt = try canonicalTimestamp(request.occurredAt)
        return ExpensePreview(
            type: .expense,
            description: description,
            amount: request.amount,
            paymentMethod: request.paymentMethod,
            occurredAt: occurredAt,
            financialTimezone: "America/Sao_Paulo",
            origin: .ios
        )
    }

    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense {
        try await Task.sleep(for: .milliseconds(80))
        if let existingID = idempotency[idempotencyKey], let existing = expensesByID[existingID] {
            return RecordedExpense(expense: existing, replayed: true)
        }

        let preview = try await preview(request)
        let id = "exp_ui_synthetic_001"
        let now = timestampCodec.encode(Date())
        let expense = Expense(
            id: id,
            type: .expense,
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
        expensesByID[id] = expense
        idempotency[idempotencyKey] = id
        return RecordedExpense(expense: expense, replayed: false)
    }

    func expenses(month: String) async throws -> ExpenseMonth {
        try await Task.sleep(for: .milliseconds(80))
        let items = expensesByID.values
            .filter { expense in
                guard let date = try? timestampCodec.decode(expense.occurredAt) else { return false }
                return FinancialMonth(date: date).apiValue == month
            }
            .sorted {
                if $0.occurredAt == $1.occurredAt { return $0.id > $1.id }
                return $0.occurredAt > $1.occurredAt
            }
        return ExpenseMonth(month: month, items: items)
    }

    private func canonicalTimestamp(_ value: String) throws -> String {
        timestampCodec.encode(try timestampCodec.decode(value))
    }
}
#endif
