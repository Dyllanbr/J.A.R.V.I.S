import Foundation

enum CardPurchaseMode: String, Codable, Sendable {
    case oneTime = "ONE_TIME"
    case installment = "INSTALLMENT"
}

struct CardPurchasePreviewRequest: Encodable, Equatable, Sendable {
    let description: String
    let amount: FinancialMoney
    let occurredAt: String
    let categoryID: String?
    let creditCardID: String
    let installmentCount: Int?

    init(
        description: String,
        amount: FinancialMoney,
        occurredAt: String,
        categoryID: String? = nil,
        creditCardID: String,
        installmentCount: Int? = nil
    ) {
        self.description = description
        self.amount = amount
        self.occurredAt = occurredAt
        self.categoryID = categoryID
        self.creditCardID = creditCardID
        self.installmentCount = installmentCount
    }

    func isValid() -> Bool {
        CardPurchaseRequestValidation.validate(
            description: description,
            amount: amount,
            occurredAt: occurredAt,
            categoryID: categoryID,
            creditCardID: creditCardID,
            installmentCount: installmentCount
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(description, forKey: .description)
        try container.encode(StrictMoney(amount), forKey: .amount)
        try container.encode(occurredAt, forKey: .occurredAt)
        try container.encodeIfPresent(categoryID, forKey: .categoryID)
        try container.encode(creditCardID, forKey: .creditCardID)
        try container.encodeIfPresent(installmentCount, forKey: .installmentCount)
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case description, amount, occurredAt, categoryID = "categoryId", creditCardID = "creditCardId", installmentCount
    }
}

struct CardPurchaseCreateRequest: Encodable, Equatable, Sendable {
    let description: String
    let amount: FinancialMoney
    let occurredAt: String
    let categoryID: String?
    let creditCardID: String
    let installmentCount: Int?

    init(
        description: String,
        amount: FinancialMoney,
        occurredAt: String,
        categoryID: String? = nil,
        creditCardID: String,
        installmentCount: Int? = nil
    ) {
        self.description = description
        self.amount = amount
        self.occurredAt = occurredAt
        self.categoryID = categoryID
        self.creditCardID = creditCardID
        self.installmentCount = installmentCount
    }

    func isValid() -> Bool {
        CardPurchaseRequestValidation.validate(
            description: description,
            amount: amount,
            occurredAt: occurredAt,
            categoryID: categoryID,
            creditCardID: creditCardID,
            installmentCount: installmentCount
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(description, forKey: .description)
        try container.encode(StrictMoney(amount), forKey: .amount)
        try container.encode(occurredAt, forKey: .occurredAt)
        try container.encodeIfPresent(categoryID, forKey: .categoryID)
        try container.encode(creditCardID, forKey: .creditCardID)
        try container.encodeIfPresent(installmentCount, forKey: .installmentCount)
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case description, amount, occurredAt, categoryID = "categoryId", creditCardID = "creditCardId", installmentCount
    }
}

struct Installment: Decodable, Equatable, Sendable {
    let number: Int
    let totalCount: Int
    let dueDate: RecurrenceCivilDate
    let amount: FinancialMoney

    init(number: Int, totalCount: Int, dueDate: RecurrenceCivilDate, amount: FinancialMoney) throws {
        guard (1...totalCount).contains(number), (2...120).contains(totalCount), amount.isValidPositiveBRL else {
            throw CardPurchaseModelError.invalid
        }
        self.number = number
        self.totalCount = totalCount
        self.dueDate = dueDate
        self.amount = amount
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["number", "totalCount", "dueDate", "amount"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let number = try c.decode(Int.self, forKey: .number)
        let totalCount = try c.decode(Int.self, forKey: .totalCount)
        let dueDate = try c.decode(RecurrenceCivilDate.self, forKey: .dueDate)
        let amount = try c.decodeStrictMoney(forKey: .amount)
        try self.init(number: number, totalCount: totalCount, dueDate: dueDate, amount: amount)
    }

    private enum CodingKeys: String, CodingKey { case number, totalCount, dueDate, amount }
}

struct InstallmentSummary: Decodable, Equatable, Sendable {
    let installmentCount: Int
    let firstDueDate: RecurrenceCivilDate
    let lastDueDate: RecurrenceCivilDate
    let dueDayAnchor: Int
    let regularInstallmentAmount: FinancialMoney
    let lastInstallmentAmount: FinancialMoney

    init(installmentCount: Int, firstDueDate: RecurrenceCivilDate, lastDueDate: RecurrenceCivilDate, dueDayAnchor: Int, regularInstallmentAmount: FinancialMoney, lastInstallmentAmount: FinancialMoney) throws {
        guard (2...120).contains(installmentCount), (1...31).contains(dueDayAnchor), regularInstallmentAmount.isValidPositiveBRL, lastInstallmentAmount.isValidPositiveBRL else { throw CardPurchaseModelError.invalid }
        self.installmentCount = installmentCount; self.firstDueDate = firstDueDate; self.lastDueDate = lastDueDate; self.dueDayAnchor = dueDayAnchor; self.regularInstallmentAmount = regularInstallmentAmount; self.lastInstallmentAmount = lastInstallmentAmount
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["installmentCount", "firstDueDate", "lastDueDate", "dueDayAnchor", "regularInstallmentAmount", "lastInstallmentAmount"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installmentCount = try c.decode(Int.self, forKey: .installmentCount)
        firstDueDate = try c.decode(RecurrenceCivilDate.self, forKey: .firstDueDate)
        lastDueDate = try c.decode(RecurrenceCivilDate.self, forKey: .lastDueDate)
        dueDayAnchor = try c.decode(Int.self, forKey: .dueDayAnchor)
        regularInstallmentAmount = try c.decodeStrictMoney(forKey: .regularInstallmentAmount)
        lastInstallmentAmount = try c.decodeStrictMoney(forKey: .lastInstallmentAmount)
        guard (2...120).contains(installmentCount), (1...31).contains(dueDayAnchor), regularInstallmentAmount.isValidPositiveBRL, lastInstallmentAmount.isValidPositiveBRL else {
            throw CardPurchaseModelError.invalid
        }
    }

    private enum CodingKeys: String, CodingKey { case installmentCount, firstDueDate, lastDueDate, dueDayAnchor, regularInstallmentAmount, lastInstallmentAmount }
}

struct CardPurchasePreview: Decodable, Equatable, Sendable {
    let description: String
    let amount: FinancialMoney
    let occurredAt: String
    let categoryID: String?
    let creditCardID: String
    let purchaseMode: CardPurchaseMode
    let statementClosingOn: RecurrenceCivilDate
    let statementDueOn: RecurrenceCivilDate
    let installmentSummary: InstallmentSummary?

    init(
        description: String,
        amount: FinancialMoney,
        occurredAt: String,
        categoryID: String? = nil,
        creditCardID: String,
        purchaseMode: CardPurchaseMode,
        statementClosingOn: RecurrenceCivilDate,
        statementDueOn: RecurrenceCivilDate,
        installmentSummary: InstallmentSummary? = nil
    ) throws {
        guard !description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              amount.isValidPositiveBRL,
              CreditCard.isValidID(creditCardID),
              (try? RFC3339DateCodec().decode(occurredAt)) != nil,
              (purchaseMode == .installment) == (installmentSummary != nil)
        else { throw CardPurchaseModelError.invalid }
        self.description = description
        self.amount = amount
        self.occurredAt = occurredAt
        self.categoryID = categoryID
        self.creditCardID = creditCardID
        self.purchaseMode = purchaseMode
        self.statementClosingOn = statementClosingOn
        self.statementDueOn = statementDueOn
        self.installmentSummary = installmentSummary
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["description", "amount", "occurredAt", "categoryId", "creditCardId", "purchaseMode", "statementClosingOn", "statementDueOn", "installmentSummary"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        description = try c.decode(String.self, forKey: .description)
        amount = try c.decodeStrictMoney(forKey: .amount)
        occurredAt = try c.decode(String.self, forKey: .occurredAt)
        categoryID = try c.decodeStrictOptionalString(forKey: .categoryID)
        creditCardID = try c.decode(String.self, forKey: .creditCardID)
        purchaseMode = try c.decode(CardPurchaseMode.self, forKey: .purchaseMode)
        statementClosingOn = try c.decode(RecurrenceCivilDate.self, forKey: .statementClosingOn)
        statementDueOn = try c.decode(RecurrenceCivilDate.self, forKey: .statementDueOn)
        installmentSummary = try c.decodeStrictOptional(InstallmentSummary.self, forKey: .installmentSummary)
        guard !description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, amount.isValidPositiveBRL, CreditCard.isValidID(creditCardID), (try? RFC3339DateCodec().decode(occurredAt)) != nil else {
            throw CardPurchaseModelError.invalid
        }
        switch purchaseMode {
        case .oneTime:
            guard installmentSummary == nil else { throw CardPurchaseModelError.invalid }
        case .installment:
            guard installmentSummary != nil else { throw CardPurchaseModelError.invalid }
        }
    }

    private enum CodingKeys: String, CodingKey {
        case description, amount, occurredAt, categoryID = "categoryId", creditCardID = "creditCardId", purchaseMode, statementClosingOn, statementDueOn, installmentSummary
    }
}

struct CardPurchaseExpense: Decodable, Equatable, Identifiable, Sendable {
    let id: String
    let type: TransactionType
    let description: String
    let amount: FinancialMoney
    let paymentMethod: PaymentMethod
    let categoryID: String?
    let creditCardID: String
    let statementDueOn: RecurrenceCivilDate
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
        categoryID: String? = nil,
        creditCardID: String,
        statementDueOn: RecurrenceCivilDate,
        occurredAt: String,
        createdAt: String,
        updatedAt: String
    ) throws {
        guard Self.isValidID(id), !description.isEmpty, amount.isValidPositiveBRL,
              CreditCard.isValidID(creditCardID),
              (try? RFC3339DateCodec().decode(occurredAt)) != nil,
              (try? RFC3339DateCodec().decode(createdAt)) != nil,
              (try? RFC3339DateCodec().decode(updatedAt)) != nil else { throw CardPurchaseModelError.invalid }
        self.id = id
        type = .expense
        self.description = description
        self.amount = amount
        paymentMethod = .credit
        self.categoryID = categoryID
        self.creditCardID = creditCardID
        self.statementDueOn = statementDueOn
        self.occurredAt = occurredAt
        financialTimezone = "America/Sao_Paulo"
        origin = .ios
        status = .recorded
        version = 1
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["id", "type", "description", "amount", "paymentMethod", "categoryId", "creditCardId", "statementDueOn", "occurredAt", "financialTimezone", "origin", "status", "version", "createdAt", "updatedAt"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        type = try c.decode(TransactionType.self, forKey: .type)
        description = try c.decode(String.self, forKey: .description)
        amount = try c.decodeStrictMoney(forKey: .amount)
        paymentMethod = try c.decode(PaymentMethod.self, forKey: .paymentMethod)
        categoryID = try c.decodeStrictOptionalString(forKey: .categoryID)
        creditCardID = try c.decode(String.self, forKey: .creditCardID)
        statementDueOn = try c.decode(RecurrenceCivilDate.self, forKey: .statementDueOn)
        occurredAt = try c.decode(String.self, forKey: .occurredAt)
        financialTimezone = try c.decode(String.self, forKey: .financialTimezone)
        origin = try c.decode(FinancialOrigin.self, forKey: .origin)
        status = try c.decode(FinancialStatus.self, forKey: .status)
        version = try c.decode(Int64.self, forKey: .version)
        createdAt = try c.decode(String.self, forKey: .createdAt)
        updatedAt = try c.decode(String.self, forKey: .updatedAt)
        guard Self.isValidID(id), type == .expense, paymentMethod == .credit, amount.isValidPositiveBRL, CreditCard.isValidID(creditCardID), financialTimezone == "America/Sao_Paulo", origin == .ios, status == .recorded, version > 0, (try? RFC3339DateCodec().decode(occurredAt)) != nil, (try? RFC3339DateCodec().decode(createdAt)) != nil, (try? RFC3339DateCodec().decode(updatedAt)) != nil else {
            throw CardPurchaseModelError.invalid
        }
    }

    static func isValidID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return (1...128).contains(bytes.count) && bytes.allSatisfy { (33...126).contains($0) }
    }

    private enum CodingKeys: String, CodingKey {
        case id, type, description, amount, paymentMethod, categoryID = "categoryId", creditCardID = "creditCardId", statementDueOn, occurredAt, financialTimezone, origin, status, version, createdAt, updatedAt
    }
}

struct InstallmentPlan: Decodable, Equatable, Identifiable, Sendable {
    let id: String
    let creditCardID: String
    let expenseID: String
    let totalAmount: FinancialMoney
    let installmentCount: Int
    let firstDueDate: RecurrenceCivilDate
    let dueDayAnchor: Int
    let status: InstallmentPlanStatus
    let createdAt: String
    let cancelledOn: RecurrenceCivilDate?
    let schedule: [Installment]

    init(
        id: String,
        creditCardID: String,
        expenseID: String,
        totalAmount: FinancialMoney,
        installmentCount: Int,
        firstDueDate: RecurrenceCivilDate,
        dueDayAnchor: Int,
        status: InstallmentPlanStatus = .active,
        createdAt: String,
        cancelledOn: RecurrenceCivilDate? = nil,
        schedule: [Installment]
    ) throws {
        guard Self.isValidID(id), CreditCard.isValidID(creditCardID), CardPurchaseExpense.isValidID(expenseID), totalAmount.isValidPositiveBRL, (2...120).contains(installmentCount), (1...31).contains(dueDayAnchor), schedule.count == installmentCount, schedule.allSatisfy({ $0.totalCount == installmentCount }), (try? RFC3339DateCodec().decode(createdAt)) != nil else { throw CardPurchaseModelError.invalid }
        switch status {
        case .active: guard cancelledOn == nil else { throw CardPurchaseModelError.invalid }
        case .cancelled: guard cancelledOn != nil else { throw CardPurchaseModelError.invalid }
        }
        self.id = id; self.creditCardID = creditCardID; self.expenseID = expenseID; self.totalAmount = totalAmount; self.installmentCount = installmentCount; self.firstDueDate = firstDueDate; self.dueDayAnchor = dueDayAnchor; self.status = status; self.createdAt = createdAt; self.cancelledOn = cancelledOn; self.schedule = schedule
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["id", "creditCardId", "expenseId", "totalAmount", "installmentCount", "firstDueDate", "dueDayAnchor", "status", "createdAt", "cancelledOn", "schedule"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        creditCardID = try c.decode(String.self, forKey: .creditCardID)
        expenseID = try c.decode(String.self, forKey: .expenseID)
        totalAmount = try c.decodeStrictMoney(forKey: .totalAmount)
        installmentCount = try c.decode(Int.self, forKey: .installmentCount)
        firstDueDate = try c.decode(RecurrenceCivilDate.self, forKey: .firstDueDate)
        dueDayAnchor = try c.decode(Int.self, forKey: .dueDayAnchor)
        status = try c.decode(InstallmentPlanStatus.self, forKey: .status)
        createdAt = try c.decode(String.self, forKey: .createdAt)
        cancelledOn = try c.decodeStrictOptional(RecurrenceCivilDate.self, forKey: .cancelledOn)
        schedule = try c.decode([Installment].self, forKey: .schedule)
        guard Self.isValidID(id), CardPurchaseExpense.isValidID(expenseID), CreditCard.isValidID(creditCardID), totalAmount.isValidPositiveBRL, (2...120).contains(installmentCount), (1...31).contains(dueDayAnchor), schedule.count == installmentCount, schedule.allSatisfy({ $0.totalCount == installmentCount }), (try? RFC3339DateCodec().decode(createdAt)) != nil else {
            throw CardPurchaseModelError.invalid
        }
        switch status {
        case .active:
            guard cancelledOn == nil else { throw CardPurchaseModelError.invalid }
        case .cancelled:
            guard cancelledOn != nil else { throw CardPurchaseModelError.invalid }
        }
    }

    static func isValidID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        guard bytes.count == 36, bytes.starts(with: Array("ipl_".utf8)) else { return false }
        return bytes.dropFirst(4).allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }

    private enum CodingKeys: String, CodingKey { case id, creditCardID = "creditCardId", expenseID = "expenseId", totalAmount, installmentCount, firstDueDate, dueDayAnchor, status, createdAt, cancelledOn, schedule }
}

enum InstallmentPlanStatus: String, Decodable, Sendable { case active = "ACTIVE"; case cancelled = "CANCELLED" }

struct InstallmentPlanListResponse: Decodable, Equatable, Sendable {
    let items: [InstallmentPlan]
    init(items: [InstallmentPlan]) { self.items = items }
    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["items"])
        items = try decoder.container(keyedBy: CodingKeys.self).decode([InstallmentPlan].self, forKey: .items)
        guard Set(items.map(\.id)).count == items.count else { throw CardPurchaseModelError.invalid }
    }
    private enum CodingKeys: String, CodingKey { case items }
}

struct CardPurchase: Decodable, Equatable, Sendable {
    let expense: CardPurchaseExpense
    let installmentPlan: InstallmentPlan?
    let purchaseMode: CardPurchaseMode

    init(expense: CardPurchaseExpense, installmentPlan: InstallmentPlan? = nil, purchaseMode: CardPurchaseMode) throws {
        guard (purchaseMode == .installment) == (installmentPlan != nil) else { throw CardPurchaseModelError.invalid }
        self.expense = expense; self.installmentPlan = installmentPlan; self.purchaseMode = purchaseMode
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["expense", "installmentPlan", "purchaseMode"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        expense = try c.decode(CardPurchaseExpense.self, forKey: .expense)
        installmentPlan = try c.decodeStrictOptional(InstallmentPlan.self, forKey: .installmentPlan)
        purchaseMode = try c.decode(CardPurchaseMode.self, forKey: .purchaseMode)
        guard (purchaseMode == .installment) == (installmentPlan != nil) else { throw CardPurchaseModelError.invalid }
    }
    private enum CodingKeys: String, CodingKey { case expense, installmentPlan, purchaseMode }
}

struct InstallmentPlanCancellationPreview: Decodable, Equatable, Sendable {
    let installmentPlanID: String
    let expectedCancelledOn: RecurrenceCivilDate
    let plan: InstallmentPlan

    init(installmentPlanID: String, expectedCancelledOn: RecurrenceCivilDate, plan: InstallmentPlan) throws {
        guard InstallmentPlan.isValidID(installmentPlanID), plan.id == installmentPlanID else { throw CardPurchaseModelError.invalid }
        self.installmentPlanID = installmentPlanID; self.expectedCancelledOn = expectedCancelledOn; self.plan = plan
    }
    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["installmentPlanId", "expectedCancelledOn", "plan"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        installmentPlanID = try c.decode(String.self, forKey: .installmentPlanID)
        expectedCancelledOn = try c.decode(RecurrenceCivilDate.self, forKey: .expectedCancelledOn)
        plan = try c.decode(InstallmentPlan.self, forKey: .plan)
        guard InstallmentPlan.isValidID(installmentPlanID), plan.id == installmentPlanID else { throw CardPurchaseModelError.invalid }
    }
    private enum CodingKeys: String, CodingKey { case installmentPlanID = "installmentPlanId", expectedCancelledOn, plan }
}

struct InstallmentPlanCancelRequest: Encodable, Equatable, Sendable {
    let expectedCancelledOn: RecurrenceCivilDate
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(expectedCancelledOn, forKey: .expectedCancelledOn)
    }
    private enum CodingKeys: String, CodingKey { case expectedCancelledOn }
}

struct RecordedCardPurchase: Equatable, Sendable {
    let purchase: CardPurchase
    let replayed: Bool
}

struct RecordedInstallmentPlan: Equatable, Sendable {
    let plan: InstallmentPlan
    let replayed: Bool
}

private enum CardPurchaseModelError: Error { case invalid }

private enum CardPurchaseRequestValidation {
    static func validate(description: String, amount: FinancialMoney, occurredAt: String, categoryID: String?, creditCardID: String, installmentCount: Int?) -> Bool {
        !description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && description.count <= 200
            && amount.isValidPositiveBRL
            && (try? RFC3339DateCodec().decode(occurredAt)) != nil
            && categoryID.map { !$0.isEmpty && $0.count <= 128 } ?? true
            && CreditCard.isValidID(creditCardID)
            && (installmentCount == nil || (2...120).contains(installmentCount!))
    }
}

private struct StrictMoney: Codable {
    let value: FinancialMoney
    init(_ value: FinancialMoney) { self.value = value }
    init(from decoder: Decoder) throws {
        try decoder.rejectingUnknownKeys(["minor", "currency"])
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let value = FinancialMoney(minor: try c.decode(Int64.self, forKey: .minor), currency: try c.decode(Currency.self, forKey: .currency))
        guard value.isValidPositiveBRL else { throw CardPurchaseModelError.invalid }
        self.value = value
    }
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(value.minor, forKey: .minor)
        try c.encode(value.currency, forKey: .currency)
    }
    private enum CodingKeys: String, CodingKey { case minor, currency }
}

private extension FinancialMoney {
    var isValidPositiveBRL: Bool { minor > 0 && currency == .brl }
}

private struct AnyCodingKey: CodingKey {
    let stringValue: String
    let intValue: Int?
    init?(stringValue: String) { self.stringValue = stringValue; intValue = nil }
    init?(intValue: Int) { stringValue = String(intValue); self.intValue = intValue }
}

private extension Decoder {
    func rejectingUnknownKeys(_ allowed: Set<String>) throws {
        let keys = try container(keyedBy: AnyCodingKey.self).allKeys.map(\.stringValue)
        guard Set(keys).isSubset(of: allowed) else { throw CardPurchaseModelError.invalid }
    }
}

private extension KeyedDecodingContainer {
    func decodeStrictOptional<T: Decodable>(_ type: T.Type, forKey key: Key) throws -> T? {
        guard contains(key) else { return nil }
        return try decode(T.self, forKey: key)
    }

    func decodeStrictOptionalString(forKey key: Key) throws -> String? {
        guard contains(key) else { return nil }
        return try decode(String.self, forKey: key)
    }

    func decodeStrictMoney(forKey key: Key) throws -> FinancialMoney {
        try decode(StrictMoney.self, forKey: key).value
    }
}
