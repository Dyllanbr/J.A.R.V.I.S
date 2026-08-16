#if DEBUG
import Foundation

@MainActor
final class StubFinancialAPI: FinancialAPI {
    private var transactionsByID: [String: FinancialTransaction] = [:]
    private var idempotency: [String: String] = [:]
    private var nextSequence = 1
    private let timestampCodec = RFC3339DateCodec()

    func categories() async throws -> [CategoryDefinition] {
        try await Task.sleep(for: .milliseconds(80))
        return Self.categoryFixture
    }

    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview {
        try await Task.sleep(for: .milliseconds(80))
        let description = request.description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty, request.amount.minor > 0 else {
            throw FinancialAPIError.invalidData
        }
        try validate(categoryID: request.categoryID, for: .expense)
        let occurredAt = try canonicalTimestamp(request.occurredAt)
        return ExpensePreview(
            description: description,
            amount: request.amount,
            paymentMethod: request.paymentMethod,
            categoryID: request.categoryID,
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
        try validate(categoryID: request.categoryID, for: .income)
        let occurredAt = try canonicalTimestamp(request.occurredAt)
        return IncomePreview(
            description: description,
            amount: request.amount,
            categoryID: request.categoryID,
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
            categoryID: preview.categoryID,
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
            categoryID: preview.categoryID,
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

    private func validate(categoryID: String?, for type: TransactionType) throws {
        guard let categoryID else { return }
        guard Self.categoryFixture.contains(where: { $0.id == categoryID && $0.type == type }) else {
            throw FinancialAPIError.invalidData
        }
    }

    // Debug-only fixture used by the offline stub. Production always loads GET /v1/categories.
    private static let categoryFixture: [CategoryDefinition] = [
        CategoryDefinition(id: "expense.food", type: .expense, displayName: "Alimentação"),
        CategoryDefinition(id: "expense.transport", type: .expense, displayName: "Transporte"),
        CategoryDefinition(id: "expense.housing", type: .expense, displayName: "Moradia"),
        CategoryDefinition(id: "expense.health", type: .expense, displayName: "Saúde"),
        CategoryDefinition(id: "expense.leisure", type: .expense, displayName: "Lazer"),
        CategoryDefinition(id: "expense.education", type: .expense, displayName: "Educação"),
        CategoryDefinition(id: "expense.subscriptions", type: .expense, displayName: "Assinaturas"),
        CategoryDefinition(id: "expense.shopping", type: .expense, displayName: "Compras"),
        CategoryDefinition(id: "expense.taxes_fees", type: .expense, displayName: "Impostos e taxas"),
        CategoryDefinition(id: "expense.other", type: .expense, displayName: "Outros"),
        CategoryDefinition(id: "income.salary", type: .income, displayName: "Salário"),
        CategoryDefinition(id: "income.freelance", type: .income, displayName: "Freelance"),
        CategoryDefinition(id: "income.refund", type: .income, displayName: "Reembolso"),
        CategoryDefinition(id: "income.sale", type: .income, displayName: "Venda"),
        CategoryDefinition(id: "income.investment_return", type: .income, displayName: "Rendimentos"),
        CategoryDefinition(id: "income.benefits", type: .income, displayName: "Benefícios"),
        CategoryDefinition(id: "income.other", type: .income, displayName: "Outros")
    ]
}
#endif
