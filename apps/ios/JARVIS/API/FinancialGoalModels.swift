import Foundation

enum FinancialGoalModelError: Error, Equatable {
    case invalid
}

struct FinancialGoalAmount: Codable, Equatable, Sendable {
    let minor: Int64
    let currency: Currency

    init(minor: Int64, currency: Currency = .brl) throws {
        guard minor > 0, currency == .brl else { throw FinancialGoalModelError.invalid }
        self.minor = minor
        self.currency = currency
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingFinancialGoalUnknownKeys(["minor", "currency"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            minor: container.decode(Int64.self, forKey: .minor),
            currency: container.decode(Currency.self, forKey: .currency)
        )
    }

    private enum CodingKeys: String, CodingKey { case minor, currency }
}

struct ProtectedValueAmount: Codable, Equatable, Sendable {
    let minor: Int64
    let currency: Currency

    init(minor: Int64, currency: Currency = .brl) throws {
        guard minor >= 0, currency == .brl else { throw FinancialGoalModelError.invalid }
        self.minor = minor
        self.currency = currency
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingFinancialGoalUnknownKeys(["minor", "currency"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            minor: container.decode(Int64.self, forKey: .minor),
            currency: container.decode(Currency.self, forKey: .currency)
        )
    }

    private enum CodingKeys: String, CodingKey { case minor, currency }
}

struct FinancialGoal: Codable, Equatable, Identifiable, Sendable {
    let id: String
    let title: String
    let targetAmount: FinancialGoalAmount

    init(id: String, title: String, targetAmount: FinancialGoalAmount) throws {
        guard Self.isValidID(id), Self.isValidLabel(title) else {
            throw FinancialGoalModelError.invalid
        }
        self.id = id
        self.title = title
        self.targetAmount = targetAmount
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingFinancialGoalUnknownKeys(["id", "title", "targetAmount"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            id: container.decode(String.self, forKey: .id),
            title: container.decode(String.self, forKey: .title),
            targetAmount: container.decode(FinancialGoalAmount.self, forKey: .targetAmount)
        )
    }

    private static func isValidID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return (1...128).contains(bytes.count) && bytes.allSatisfy { (33...126).contains($0) }
    }

    private static func isValidLabel(_ value: String) -> Bool {
        !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && value.count <= 200
            && value.unicodeScalars.allSatisfy { !CharacterSet.controlCharacters.contains($0) }
    }

    private enum CodingKeys: String, CodingKey { case id, title, targetAmount }
}

struct ProtectedValue: Codable, Equatable, Identifiable, Sendable {
    let id: String
    let label: String
    let amount: ProtectedValueAmount

    init(id: String, label: String, amount: ProtectedValueAmount) throws {
        guard Self.isValidID(id), Self.isValidLabel(label) else {
            throw FinancialGoalModelError.invalid
        }
        self.id = id
        self.label = label
        self.amount = amount
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingFinancialGoalUnknownKeys(["id", "label", "amount"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            id: container.decode(String.self, forKey: .id),
            label: container.decode(String.self, forKey: .label),
            amount: container.decode(ProtectedValueAmount.self, forKey: .amount)
        )
    }

    private static func isValidID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return (1...128).contains(bytes.count) && bytes.allSatisfy { (33...126).contains($0) }
    }

    private static func isValidLabel(_ value: String) -> Bool {
        !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
            && value.count <= 200
            && value.unicodeScalars.allSatisfy { !CharacterSet.controlCharacters.contains($0) }
    }

    private enum CodingKeys: String, CodingKey { case id, label, amount }
}

struct FinancialGoalsResponse: Codable, Equatable, Sendable {
    let goals: [FinancialGoal]
    let protectedValues: [ProtectedValue]

    init(goals: [FinancialGoal], protectedValues: [ProtectedValue]) throws {
        let identifiers = goals.map(\.id) + protectedValues.map(\.id)
        guard Set(identifiers).count == identifiers.count,
              goals == goals.sorted(by: { $0.id < $1.id }),
              protectedValues == protectedValues.sorted(by: { $0.id < $1.id })
        else { throw FinancialGoalModelError.invalid }
        self.goals = goals
        self.protectedValues = protectedValues
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingFinancialGoalUnknownKeys(["goals", "protectedValues"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            goals: container.decode([FinancialGoal].self, forKey: .goals),
            protectedValues: container.decode([ProtectedValue].self, forKey: .protectedValues)
        )
    }

    private enum CodingKeys: String, CodingKey { case goals, protectedValues }
}

struct FinancialGoalRequest: Encodable, Equatable, Sendable {
    let title: String
    let targetAmount: FinancialGoalAmount
}

struct ProtectedValueRequest: Encodable, Equatable, Sendable {
    let label: String
    let amount: ProtectedValueAmount
}

private struct FinancialGoalAnyCodingKey: CodingKey {
    let stringValue: String
    let intValue: Int?

    init?(stringValue: String) { self.stringValue = stringValue; intValue = nil }
    init?(intValue: Int) { stringValue = String(intValue); self.intValue = intValue }
}

private extension Decoder {
    func rejectingFinancialGoalUnknownKeys(_ allowed: Set<String>) throws {
        let keys = try container(keyedBy: FinancialGoalAnyCodingKey.self).allKeys
        guard keys.allSatisfy({ allowed.contains($0.stringValue) }) else {
            throw FinancialGoalModelError.invalid
        }
    }
}
