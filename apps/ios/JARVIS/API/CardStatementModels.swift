import Foundation

struct CardStatementTotalAmount: Codable, Equatable, Sendable {
    let minor: Int64
    let currency: Currency

    init(minor: Int64, currency: Currency = .brl) throws {
        guard minor >= 0, currency == .brl else { throw CardStatementModelError.invalid }
        self.minor = minor
        self.currency = currency
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingCardStatementUnknownKeys(["minor", "currency"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            minor: container.decode(Int64.self, forKey: .minor),
            currency: container.decode(Currency.self, forKey: .currency)
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(minor, forKey: .minor)
        try container.encode(currency, forKey: .currency)
    }

    private enum CodingKeys: String, CodingKey { case minor, currency }
}

struct CardStatementLine: Codable, Equatable, Sendable {
    let expenseID: String
    let description: String
    let amount: FinancialMoney
    let occurredAt: RecurrenceCivilDate
    let purchaseMode: CardPurchaseMode
    let installmentNumber: Int?
    let installmentCount: Int?

    init(
        expenseID: String,
        description: String,
        amount: FinancialMoney,
        occurredAt: RecurrenceCivilDate,
        purchaseMode: CardPurchaseMode,
        installmentNumber: Int? = nil,
        installmentCount: Int? = nil
    ) throws {
        guard Self.isValidExpenseID(expenseID),
              !description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              description.count <= 200,
              amount.isValidCardStatementLineMoney,
              Self.validInstallmentMetadata(
                  purchaseMode: purchaseMode,
                  installmentNumber: installmentNumber,
                  installmentCount: installmentCount
              )
        else { throw CardStatementModelError.invalid }
        self.expenseID = expenseID
        self.description = description
        self.amount = amount
        self.occurredAt = occurredAt
        self.purchaseMode = purchaseMode
        self.installmentNumber = installmentNumber
        self.installmentCount = installmentCount
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingCardStatementUnknownKeys([
            "expenseId", "description", "amount", "occurredAt", "purchaseMode",
            "installmentNumber", "installmentCount"
        ])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            expenseID: container.decode(String.self, forKey: .expenseID),
            description: container.decode(String.self, forKey: .description),
            amount: container.decodeCardStatementLineMoney(forKey: .amount),
            occurredAt: container.decode(RecurrenceCivilDate.self, forKey: .occurredAt),
            purchaseMode: container.decode(CardPurchaseMode.self, forKey: .purchaseMode),
            installmentNumber: container.decodeIfPresent(Int.self, forKey: .installmentNumber),
            installmentCount: container.decodeIfPresent(Int.self, forKey: .installmentCount)
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(expenseID, forKey: .expenseID)
        try container.encode(description, forKey: .description)
        try container.encode(CardStatementStrictMoney(value: amount), forKey: .amount)
        try container.encode(occurredAt, forKey: .occurredAt)
        try container.encode(purchaseMode, forKey: .purchaseMode)
        try container.encodeIfPresent(installmentNumber, forKey: .installmentNumber)
        try container.encodeIfPresent(installmentCount, forKey: .installmentCount)
    }

    private static func isValidExpenseID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return (1...128).contains(bytes.count) && bytes.allSatisfy { (33...126).contains($0) }
    }

    private static func validInstallmentMetadata(
        purchaseMode: CardPurchaseMode,
        installmentNumber: Int?,
        installmentCount: Int?
    ) -> Bool {
        switch purchaseMode {
        case .oneTime:
            return installmentNumber == nil && installmentCount == nil
        case .installment:
            guard let installmentNumber, let installmentCount else { return false }
            return (2...120).contains(installmentCount) && (1...installmentCount).contains(installmentNumber)
        }
    }

    private enum CodingKeys: String, CodingKey {
        case expenseID = "expenseId"
        case description, amount, occurredAt, purchaseMode, installmentNumber, installmentCount
    }
}

struct CardStatement: Codable, Equatable, Sendable {
    let creditCardID: String
    let statementDueOn: RecurrenceCivilDate
    let totalAmount: CardStatementTotalAmount
    let lines: [CardStatementLine]

    init(
        creditCardID: String,
        statementDueOn: RecurrenceCivilDate,
        totalAmount: CardStatementTotalAmount,
        lines: [CardStatementLine]
    ) throws {
        guard CreditCard.isValidID(creditCardID),
              Self.hasUniqueExpenses(lines),
              Self.hasExactTotal(totalAmount.minor, lines: lines),
              lines.isEmpty == (totalAmount.minor == 0)
        else { throw CardStatementModelError.invalid }
        self.creditCardID = creditCardID
        self.statementDueOn = statementDueOn
        self.totalAmount = totalAmount
        self.lines = lines.sorted(by: Self.sortLines)
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingCardStatementUnknownKeys([
            "creditCardId", "statementDueOn", "totalAmount", "lines"
        ])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            creditCardID: container.decode(String.self, forKey: .creditCardID),
            statementDueOn: container.decode(RecurrenceCivilDate.self, forKey: .statementDueOn),
            totalAmount: container.decode(CardStatementTotalAmount.self, forKey: .totalAmount),
            lines: container.decode([CardStatementLine].self, forKey: .lines)
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(creditCardID, forKey: .creditCardID)
        try container.encode(statementDueOn, forKey: .statementDueOn)
        try container.encode(totalAmount, forKey: .totalAmount)
        try container.encode(lines, forKey: .lines)
    }

    private static func hasUniqueExpenses(_ lines: [CardStatementLine]) -> Bool {
        Set(lines.map(\.expenseID)).count == lines.count
    }

    private static func hasExactTotal(_ total: Int64, lines: [CardStatementLine]) -> Bool {
        var sum: Int64 = 0
        for line in lines {
            let (next, overflow) = sum.addingReportingOverflow(line.amount.minor)
            guard !overflow else { return false }
            sum = next
        }
        return sum == total
    }

    private static func sortLines(_ lhs: CardStatementLine, _ rhs: CardStatementLine) -> Bool {
        if lhs.occurredAt != rhs.occurredAt { return lhs.occurredAt < rhs.occurredAt }
        if lhs.expenseID != rhs.expenseID { return lhs.expenseID < rhs.expenseID }
        return (lhs.installmentNumber ?? 0) < (rhs.installmentNumber ?? 0)
    }

    private enum CodingKeys: String, CodingKey {
        case creditCardID = "creditCardId", statementDueOn, totalAmount, lines
    }
}

private enum CardStatementModelError: Error, Equatable {
    case invalid
}

private struct CardStatementStrictMoney: Codable {
    let value: FinancialMoney

    init(value: FinancialMoney) {
        self.value = value
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingCardStatementUnknownKeys(["minor", "currency"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let value = FinancialMoney(
            minor: try container.decode(Int64.self, forKey: .minor),
            currency: try container.decode(Currency.self, forKey: .currency)
        )
        guard value.isValidCardStatementLineMoney else { throw CardStatementModelError.invalid }
        self.value = value
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(value.minor, forKey: .minor)
        try container.encode(value.currency, forKey: .currency)
    }

    private enum CodingKeys: String, CodingKey { case minor, currency }
}

private extension FinancialMoney {
    var isValidCardStatementLineMoney: Bool { minor > 0 && currency == .brl }
}

private struct CardStatementAnyCodingKey: CodingKey {
    let stringValue: String
    let intValue: Int?

    init?(stringValue: String) {
        self.stringValue = stringValue
        intValue = nil
    }

    init?(intValue: Int) {
        stringValue = String(intValue)
        self.intValue = intValue
    }
}

private extension Decoder {
    func rejectingCardStatementUnknownKeys(_ allowed: Set<String>) throws {
        let keys = try container(keyedBy: CardStatementAnyCodingKey.self).allKeys
        guard keys.allSatisfy({ allowed.contains($0.stringValue) }) else {
            throw CardStatementModelError.invalid
        }
    }
}

private extension KeyedDecodingContainer {
    func decodeCardStatementLineMoney(forKey key: Key) throws -> FinancialMoney {
        try decode(CardStatementStrictMoney.self, forKey: key).value
    }
}
