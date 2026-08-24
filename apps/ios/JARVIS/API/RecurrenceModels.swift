import Foundation

enum RecurrenceFrequency: String, Codable, Sendable {
    case monthly = "MONTHLY"

    var displayName: String { "Mensal" }
}

enum RecurrenceStatus: String, Codable, Sendable {
    case active = "ACTIVE"
    case cancelled = "CANCELLED"

    var displayName: String {
        switch self {
        case .active: "Ativa"
        case .cancelled: "Cancelada"
        }
    }
}

enum RecurrenceCivilDateError: Error, Equatable {
    case invalid
}

/// A Gregorian civil date with no time-of-day or timezone semantics.
struct RecurrenceCivilDate: Codable, Equatable, Hashable, Sendable {
    let year: Int
    let month: Int
    let day: Int

    init(year: Int, month: Int, day: Int) throws {
        guard (1...9_999).contains(year), (1...12).contains(month), (1...31).contains(day) else {
            throw RecurrenceCivilDateError.invalid
        }
        var components = DateComponents()
        components.calendar = Self.pickerCalendar
        components.timeZone = Self.pickerCalendar.timeZone
        components.year = year
        components.month = month
        components.day = day
        guard let date = Self.pickerCalendar.date(from: components) else {
            throw RecurrenceCivilDateError.invalid
        }
        let validated = Self.pickerCalendar.dateComponents([.year, .month, .day], from: date)
        guard validated.year == year, validated.month == month, validated.day == day else {
            throw RecurrenceCivilDateError.invalid
        }
        self.year = year
        self.month = month
        self.day = day
    }

    init(_ canonicalValue: String) throws {
        let bytes = Array(canonicalValue.utf8)
        guard bytes.count == 10, bytes[4] == 45, bytes[7] == 45,
              bytes.enumerated().allSatisfy({ index, byte in
                  index == 4 || index == 7 || (48...57).contains(byte)
              })
        else {
            throw RecurrenceCivilDateError.invalid
        }
        let parts = canonicalValue.split(separator: "-", omittingEmptySubsequences: false)
        guard parts.count == 3, let year = Int(parts[0]),
              let month = Int(parts[1]),
              let day = Int(parts[2])
        else {
            throw RecurrenceCivilDateError.invalid
        }
        try self.init(year: year, month: month, day: day)
    }

    init(pickerDate: Date) throws {
        let components = Self.pickerCalendar.dateComponents([.year, .month, .day], from: pickerDate)
        guard let year = components.year, let month = components.month, let day = components.day else {
            throw RecurrenceCivilDateError.invalid
        }
        try self.init(year: year, month: month, day: day)
    }

    var canonicalValue: String {
        String(format: "%04d-%02d-%02d", year, month, day)
    }

    var displayValue: String {
        String(format: "%02d/%02d/%04d", day, month, year)
    }

    var pickerDate: Date {
        var components = DateComponents()
        components.calendar = Self.pickerCalendar
        components.timeZone = Self.pickerCalendar.timeZone
        components.year = year
        components.month = month
        components.day = day
        return Self.pickerCalendar.date(from: components)!
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        do {
            try self.init(container.decode(String.self))
        } catch {
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "Expected a valid YYYY-MM-DD civil date"
            )
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(canonicalValue)
    }

    static let pickerCalendar: Calendar = {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = Locale(identifier: "en_US_POSIX")
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        return calendar
    }()
}

struct RecurrenceRequest: Codable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let expectedAmount: FinancialMoney
    let frequency: RecurrenceFrequency
    let startsOn: RecurrenceCivilDate

    init(description: String, expectedAmount: FinancialMoney, startsOn: RecurrenceCivilDate) {
        type = .expense
        self.description = description
        self.expectedAmount = expectedAmount
        frequency = .monthly
        self.startsOn = startsOn
    }
}

struct RecurrencePreview: Decodable, Equatable, Sendable {
    let type: TransactionType
    let description: String
    let expectedAmount: FinancialMoney
    let frequency: RecurrenceFrequency
    let startsOn: RecurrenceCivilDate

    init(description: String, expectedAmount: FinancialMoney, startsOn: RecurrenceCivilDate) {
        type = .expense
        self.description = description
        self.expectedAmount = expectedAmount
        frequency = .monthly
        self.startsOn = startsOn
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        type = try container.decode(TransactionType.self, forKey: .type)
        guard type == .expense else {
            throw DecodingError.dataCorruptedError(forKey: .type, in: container, debugDescription: "Expected EXPENSE")
        }
        description = try container.decode(String.self, forKey: .description)
        expectedAmount = try container.decode(FinancialMoney.self, forKey: .expectedAmount)
        frequency = try container.decode(RecurrenceFrequency.self, forKey: .frequency)
        startsOn = try container.decode(RecurrenceCivilDate.self, forKey: .startsOn)
        guard !description.isEmpty, expectedAmount.minor > 0, expectedAmount.currency == .brl else {
            throw DecodingError.dataCorruptedError(forKey: .expectedAmount, in: container, debugDescription: "Invalid recurrence preview")
        }
    }

    private enum CodingKeys: String, CodingKey {
        case type, description, expectedAmount, frequency, startsOn
    }
}

struct Recurrence: Decodable, Equatable, Identifiable, Sendable {
    let id: String
    let type: TransactionType
    let description: String
    let expectedAmount: FinancialMoney
    let frequency: RecurrenceFrequency
    let startsOn: RecurrenceCivilDate
    let status: RecurrenceStatus
    let createdAt: String
    let cancelledAt: String?

    init(
        id: String,
        description: String,
        expectedAmount: FinancialMoney,
        startsOn: RecurrenceCivilDate,
        status: RecurrenceStatus,
        createdAt: String,
        cancelledAt: String? = nil
    ) {
        self.id = id
        type = .expense
        self.description = description
        self.expectedAmount = expectedAmount
        frequency = .monthly
        self.startsOn = startsOn
        self.status = status
        self.createdAt = createdAt
        self.cancelledAt = cancelledAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        type = try container.decode(TransactionType.self, forKey: .type)
        description = try container.decode(String.self, forKey: .description)
        expectedAmount = try container.decode(FinancialMoney.self, forKey: .expectedAmount)
        frequency = try container.decode(RecurrenceFrequency.self, forKey: .frequency)
        startsOn = try container.decode(RecurrenceCivilDate.self, forKey: .startsOn)
        status = try container.decode(RecurrenceStatus.self, forKey: .status)
        createdAt = try container.decode(String.self, forKey: .createdAt)

        guard type == .expense, !id.isEmpty, !description.isEmpty,
              expectedAmount.minor > 0, expectedAmount.currency == .brl
        else {
            throw DecodingError.dataCorruptedError(forKey: .id, in: container, debugDescription: "Invalid recurrence")
        }

        switch status {
        case .active:
            guard !container.contains(.cancelledAt) else {
                throw DecodingError.dataCorruptedError(
                    forKey: .cancelledAt,
                    in: container,
                    debugDescription: "ACTIVE recurrence must omit cancelledAt"
                )
            }
            cancelledAt = nil
        case .cancelled:
            cancelledAt = try container.decode(String.self, forKey: .cancelledAt)
        }
    }

    private enum CodingKeys: String, CodingKey {
        case id, type, description, expectedAmount, frequency, startsOn, status, createdAt, cancelledAt
    }
}

struct RecurrenceList: Decodable, Equatable, Sendable {
    let items: [Recurrence]
}

struct RecordedRecurrence: Equatable, Sendable {
    let recurrence: Recurrence
    let replayed: Bool
}
