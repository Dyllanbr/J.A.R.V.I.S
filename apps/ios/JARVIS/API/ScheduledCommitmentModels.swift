import Foundation

enum ScheduledCommitmentSource: String, Decodable, Sendable {
    case installmentPlan = "INSTALLMENT_PLAN"
    case recurrence = "RECURRENCE"
}

struct ScheduledCommitment: Decodable, Equatable, Sendable {
    let source: ScheduledCommitmentSource
    let sourceID: String
    let sequence: Int
    let dueOn: RecurrenceCivilDate
    let amount: FinancialMoney

    init(
        source: ScheduledCommitmentSource,
        sourceID: String,
        sequence: Int,
        dueOn: RecurrenceCivilDate,
        amount: FinancialMoney
    ) throws {
        guard Self.isValidSourceID(sourceID), sequence >= 1, amount.isValidScheduledCommitmentMoney else {
            throw ScheduledCommitmentModelError.invalid
        }
        self.source = source
        self.sourceID = sourceID
        self.sequence = sequence
        self.dueOn = dueOn
        self.amount = amount
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingScheduledCommitmentUnknownKeys([
            "source", "sourceId", "sequence", "dueOn", "amount"
        ])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let source = try container.decode(ScheduledCommitmentSource.self, forKey: .source)
        let sourceID = try container.decode(String.self, forKey: .sourceID)
        let sequence = try container.decode(Int.self, forKey: .sequence)
        let dueOn = try container.decode(RecurrenceCivilDate.self, forKey: .dueOn)
        let amount = try container.decode(ScheduledCommitmentStrictMoney.self, forKey: .amount).value
        guard Self.isValidSourceID(sourceID), sequence >= 1 else {
            throw ScheduledCommitmentModelError.invalid
        }
        self.source = source
        self.sourceID = sourceID
        self.sequence = sequence
        self.dueOn = dueOn
        self.amount = amount
    }

    /// A stable in-memory identity for SwiftUI. It is not serialized or persisted.
    var identity: String {
        "\(source.rawValue):\(sourceID):\(sequence)"
    }

    private static func isValidSourceID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return (1...128).contains(bytes.count) && bytes.allSatisfy { (33...126).contains($0) }
    }

    private enum CodingKeys: String, CodingKey {
        case source
        case sourceID = "sourceId"
        case sequence
        case dueOn
        case amount
    }
}

struct ScheduledCommitmentListResponse: Decodable, Equatable, Sendable {
    let items: [ScheduledCommitment]

    init(items: [ScheduledCommitment]) throws {
        guard Self.hasUniqueIdentities(items) else { throw ScheduledCommitmentModelError.invalid }
        self.items = items
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingScheduledCommitmentUnknownKeys(["items"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let items = try container.decode([ScheduledCommitment].self, forKey: .items)
        guard Self.hasUniqueIdentities(items) else { throw ScheduledCommitmentModelError.invalid }
        self.items = items
    }

    private static func hasUniqueIdentities(_ items: [ScheduledCommitment]) -> Bool {
        Set(items.map(\.identity)).count == items.count
    }

    private enum CodingKeys: String, CodingKey { case items }
}

private enum ScheduledCommitmentModelError: Error {
    case invalid
}

private struct ScheduledCommitmentStrictMoney: Decodable {
    let value: FinancialMoney

    init(from decoder: Decoder) throws {
        try decoder.rejectingScheduledCommitmentUnknownKeys(["minor", "currency"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let value = FinancialMoney(
            minor: try container.decode(Int64.self, forKey: .minor),
            currency: try container.decode(Currency.self, forKey: .currency)
        )
        guard value.isValidScheduledCommitmentMoney else { throw ScheduledCommitmentModelError.invalid }
        self.value = value
    }

    private enum CodingKeys: String, CodingKey { case minor, currency }
}

private extension FinancialMoney {
    var isValidScheduledCommitmentMoney: Bool {
        minor > 0 && currency == .brl
    }
}

private struct ScheduledCommitmentAnyCodingKey: CodingKey {
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
    func rejectingScheduledCommitmentUnknownKeys(_ allowed: Set<String>) throws {
        let keys = try container(keyedBy: ScheduledCommitmentAnyCodingKey.self).allKeys
        guard keys.allSatisfy({ allowed.contains($0.stringValue) }) else {
            throw ScheduledCommitmentModelError.invalid
        }
    }
}
