import Foundation

enum TransactionType: String, Codable, CaseIterable, Identifiable, Sendable {
    case expense = "EXPENSE"
    case income = "INCOME"

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .expense: "Despesa"
        case .income: "Receita"
        }
    }

    var directionName: String {
        switch self {
        case .expense: "Saída"
        case .income: "Entrada"
        }
    }
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

enum FinancialOrigin: String, Codable, Sendable {
    case ios = "IOS"
}

enum FinancialStatus: String, Codable, Sendable {
    case recorded = "RECORDED"
}

typealias ExpenseOrigin = FinancialOrigin
typealias ExpenseStatus = FinancialStatus

struct FinancialMoney: Codable, Equatable, Sendable {
    let minor: Int64
    let currency: Currency
}

typealias ExpenseMoney = FinancialMoney

struct ExpenseRequest: Encodable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let amount: FinancialMoney
    let paymentMethod: PaymentMethod
    let occurredAt: String

    init(
        description: String,
        amount: FinancialMoney,
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

struct IncomeRequest: Encodable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let amount: FinancialMoney
    let occurredAt: String

    init(description: String, amount: FinancialMoney, occurredAt: String) {
        type = .income
        self.description = description
        self.amount = amount
        self.occurredAt = occurredAt
    }
}

struct ExpensePreview: Decodable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let amount: FinancialMoney
    let paymentMethod: PaymentMethod
    let occurredAt: String
    let financialTimezone: String
    let origin: FinancialOrigin

    init(
        description: String,
        amount: FinancialMoney,
        paymentMethod: PaymentMethod,
        occurredAt: String,
        financialTimezone: String,
        origin: FinancialOrigin
    ) {
        type = .expense
        self.description = description
        self.amount = amount
        self.paymentMethod = paymentMethod
        self.occurredAt = occurredAt
        self.financialTimezone = financialTimezone
        self.origin = origin
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        type = try container.decode(TransactionType.self, forKey: .type)
        guard type == .expense else {
            throw DecodingError.dataCorruptedError(forKey: .type, in: container, debugDescription: "Expected EXPENSE")
        }
        description = try container.decode(String.self, forKey: .description)
        amount = try container.decode(FinancialMoney.self, forKey: .amount)
        paymentMethod = try container.decode(PaymentMethod.self, forKey: .paymentMethod)
        occurredAt = try container.decode(String.self, forKey: .occurredAt)
        financialTimezone = try container.decode(String.self, forKey: .financialTimezone)
        origin = try container.decode(FinancialOrigin.self, forKey: .origin)
    }

    private enum CodingKeys: String, CodingKey {
        case type, description, amount, paymentMethod, occurredAt, financialTimezone, origin
    }
}

struct IncomePreview: Decodable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let amount: FinancialMoney
    let occurredAt: String
    let financialTimezone: String
    let origin: FinancialOrigin

    init(
        description: String,
        amount: FinancialMoney,
        occurredAt: String,
        financialTimezone: String,
        origin: FinancialOrigin
    ) {
        type = .income
        self.description = description
        self.amount = amount
        self.occurredAt = occurredAt
        self.financialTimezone = financialTimezone
        self.origin = origin
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        type = try container.decode(TransactionType.self, forKey: .type)
        guard type == .income else {
            throw DecodingError.dataCorruptedError(forKey: .type, in: container, debugDescription: "Expected INCOME")
        }
        guard !container.contains(.paymentMethod) else {
            throw DecodingError.dataCorruptedError(
                forKey: .paymentMethod,
                in: container,
                debugDescription: "INCOME must not contain paymentMethod"
            )
        }
        description = try container.decode(String.self, forKey: .description)
        amount = try container.decode(FinancialMoney.self, forKey: .amount)
        occurredAt = try container.decode(String.self, forKey: .occurredAt)
        financialTimezone = try container.decode(String.self, forKey: .financialTimezone)
        origin = try container.decode(FinancialOrigin.self, forKey: .origin)
    }

    private enum CodingKeys: String, CodingKey {
        case type, description, amount, paymentMethod, occurredAt, financialTimezone, origin
    }
}

struct Expense: Decodable, Equatable, Identifiable, Sendable {
    let id: String
    let type: TransactionType
    let description: String
    let amount: FinancialMoney
    let paymentMethod: PaymentMethod
    let occurredAt: String
    let financialTimezone: String
    let origin: FinancialOrigin
    let status: FinancialStatus
    let version: Int64
    let createdAt: String
    let updatedAt: String

    init(
        id: String,
        description: String,
        amount: FinancialMoney,
        paymentMethod: PaymentMethod,
        occurredAt: String,
        financialTimezone: String,
        origin: FinancialOrigin,
        status: FinancialStatus,
        version: Int64,
        createdAt: String,
        updatedAt: String
    ) {
        self.id = id
        type = .expense
        self.description = description
        self.amount = amount
        self.paymentMethod = paymentMethod
        self.occurredAt = occurredAt
        self.financialTimezone = financialTimezone
        self.origin = origin
        self.status = status
        self.version = version
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        type = try container.decode(TransactionType.self, forKey: .type)
        guard type == .expense else {
            throw DecodingError.dataCorruptedError(forKey: .type, in: container, debugDescription: "Expected EXPENSE")
        }
        description = try container.decode(String.self, forKey: .description)
        amount = try container.decode(FinancialMoney.self, forKey: .amount)
        paymentMethod = try container.decode(PaymentMethod.self, forKey: .paymentMethod)
        occurredAt = try container.decode(String.self, forKey: .occurredAt)
        financialTimezone = try container.decode(String.self, forKey: .financialTimezone)
        origin = try container.decode(FinancialOrigin.self, forKey: .origin)
        status = try container.decode(FinancialStatus.self, forKey: .status)
        version = try container.decode(Int64.self, forKey: .version)
        createdAt = try container.decode(String.self, forKey: .createdAt)
        updatedAt = try container.decode(String.self, forKey: .updatedAt)
    }

    private enum CodingKeys: String, CodingKey {
        case id, type, description, amount, paymentMethod, occurredAt, financialTimezone, origin
        case status, version, createdAt, updatedAt
    }
}

struct Income: Decodable, Equatable, Identifiable, Sendable {
    let id: String
    let type: TransactionType
    let description: String
    let amount: FinancialMoney
    let occurredAt: String
    let financialTimezone: String
    let origin: FinancialOrigin
    let status: FinancialStatus
    let version: Int64
    let createdAt: String
    let updatedAt: String

    init(
        id: String,
        description: String,
        amount: FinancialMoney,
        occurredAt: String,
        financialTimezone: String,
        origin: FinancialOrigin,
        status: FinancialStatus,
        version: Int64,
        createdAt: String,
        updatedAt: String
    ) {
        self.id = id
        type = .income
        self.description = description
        self.amount = amount
        self.occurredAt = occurredAt
        self.financialTimezone = financialTimezone
        self.origin = origin
        self.status = status
        self.version = version
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        type = try container.decode(TransactionType.self, forKey: .type)
        guard type == .income else {
            throw DecodingError.dataCorruptedError(forKey: .type, in: container, debugDescription: "Expected INCOME")
        }
        guard !container.contains(.paymentMethod) else {
            throw DecodingError.dataCorruptedError(
                forKey: .paymentMethod,
                in: container,
                debugDescription: "INCOME must not contain paymentMethod"
            )
        }
        description = try container.decode(String.self, forKey: .description)
        amount = try container.decode(FinancialMoney.self, forKey: .amount)
        occurredAt = try container.decode(String.self, forKey: .occurredAt)
        financialTimezone = try container.decode(String.self, forKey: .financialTimezone)
        origin = try container.decode(FinancialOrigin.self, forKey: .origin)
        status = try container.decode(FinancialStatus.self, forKey: .status)
        version = try container.decode(Int64.self, forKey: .version)
        createdAt = try container.decode(String.self, forKey: .createdAt)
        updatedAt = try container.decode(String.self, forKey: .updatedAt)
    }

    private enum CodingKeys: String, CodingKey {
        case id, type, description, amount, paymentMethod, occurredAt, financialTimezone, origin
        case status, version, createdAt, updatedAt
    }
}

enum FinancialTransaction: Decodable, Equatable, Identifiable, Sendable {
    case expense(Expense)
    case income(Income)

    var id: String {
        switch self {
        case let .expense(expense): expense.id
        case let .income(income): income.id
        }
    }

    var type: TransactionType {
        switch self {
        case .expense: .expense
        case .income: .income
        }
    }

    var description: String {
        switch self {
        case let .expense(expense): expense.description
        case let .income(income): income.description
        }
    }

    var amount: FinancialMoney {
        switch self {
        case let .expense(expense): expense.amount
        case let .income(income): income.amount
        }
    }

    var occurredAt: String {
        switch self {
        case let .expense(expense): expense.occurredAt
        case let .income(income): income.occurredAt
        }
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: TypeCodingKeys.self)
        switch try container.decode(TransactionType.self, forKey: .type) {
        case .expense:
            self = .expense(try Expense(from: decoder))
        case .income:
            self = .income(try Income(from: decoder))
        }
    }

    private enum TypeCodingKeys: String, CodingKey {
        case type
    }
}

struct TransactionMonth: Decodable, Equatable, Sendable {
    let month: String
    let items: [FinancialTransaction]
}

struct RecordedExpense: Equatable, Sendable {
    let expense: Expense
    let replayed: Bool
}

struct RecordedIncome: Equatable, Sendable {
    let income: Income
    let replayed: Bool
}

struct APIErrorEnvelope: Decodable, Sendable {
    struct Body: Decodable, Sendable {
        let code: String
        let message: String
    }

    let error: Body
}
