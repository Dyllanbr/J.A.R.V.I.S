import Foundation

enum TransactionType: String, Codable, Sendable {
    case expense = "EXPENSE"
}

enum Currency: String, Codable, Sendable {
    case brl = "BRL"
}

enum PaymentMethod: String, Codable, CaseIterable, Identifiable, Sendable {
    case pix = "PIX"
    case debit = "DEBIT"
    case credit = "CREDIT"
    case cash = "CASH"

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .pix: "PIX"
        case .debit: "Débito"
        case .credit: "Crédito"
        case .cash: "Dinheiro"
        }
    }
}

enum ExpenseOrigin: String, Codable, Sendable {
    case ios = "IOS"
}

enum ExpenseStatus: String, Codable, Sendable {
    case recorded = "RECORDED"
}

struct ExpenseMoney: Codable, Equatable, Sendable {
    let minor: Int64
    let currency: Currency
}

struct ExpenseRequest: Encodable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let amount: ExpenseMoney
    let paymentMethod: PaymentMethod
    let occurredAt: String

    init(
        description: String,
        amount: ExpenseMoney,
        paymentMethod: PaymentMethod,
        occurredAt: String
    ) {
        type = .expense
        self.description = description
        self.amount = amount
        self.paymentMethod = paymentMethod
        self.occurredAt = occurredAt
    }
}

struct ExpensePreview: Decodable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let amount: ExpenseMoney
    let paymentMethod: PaymentMethod
    let occurredAt: String
    let financialTimezone: String
    let origin: ExpenseOrigin
}

struct Expense: Decodable, Equatable, Identifiable, Sendable {
    let id: String
    let type: TransactionType
    let description: String
    let amount: ExpenseMoney
    let paymentMethod: PaymentMethod
    let occurredAt: String
    let financialTimezone: String
    let origin: ExpenseOrigin
    let status: ExpenseStatus
    let version: Int64
    let createdAt: String
    let updatedAt: String
}

struct ExpenseMonth: Decodable, Equatable, Sendable {
    let month: String
    let items: [Expense]
}

struct RecordedExpense: Equatable, Sendable {
    let expense: Expense
    let replayed: Bool
}

struct APIErrorEnvelope: Decodable, Sendable {
    struct Body: Decodable, Sendable {
        let code: String
        let message: String
    }

    let error: Body
}
