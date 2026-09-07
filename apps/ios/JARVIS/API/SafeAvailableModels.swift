import Foundation

enum SafeAvailableBreakdownKind: String, Codable, CaseIterable, Sendable {
    case availableBalance = "AVAILABLE_BALANCE"
    case income = "INCOME"
    case expense = "EXPENSE"
    case commitment = "COMMITMENT"
}

enum SafeAvailableMissingData: String, Codable, CaseIterable, Sendable {
    case budget = "BUDGET"
    case confirmedIncome = "CONFIRMED_INCOME"
    case confirmedExpense = "CONFIRMED_EXPENSE"
    case commitments = "COMMITMENTS"
}

enum SafeAvailableModelError: Error, Equatable {
    case invalid
}

struct SafeAvailableAmount: Codable, Equatable, Sendable {
    let minor: Int64
    let currency: Currency

    init(minor: Int64, currency: Currency = .brl) throws {
        guard currency == .brl else { throw SafeAvailableModelError.invalid }
        self.minor = minor
        self.currency = currency
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingSafeAvailableUnknownKeys(["minor", "currency"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            minor: container.decode(Int64.self, forKey: .minor),
            currency: container.decode(Currency.self, forKey: .currency)
        )
    }

    private enum CodingKeys: String, CodingKey { case minor, currency }
}

struct SafeAvailableBreakdown: Codable, Equatable, Sendable, Identifiable {
    let kind: SafeAvailableBreakdownKind
    let sourceID: String
    let sequence: Int
    let dueOn: RecurrenceCivilDate
    let amount: SafeAvailableAmount

    var id: String { "\(kind.rawValue):\(sourceID):\(sequence)" }

    init(
        kind: SafeAvailableBreakdownKind,
        sourceID: String,
        sequence: Int,
        dueOn: RecurrenceCivilDate,
        amount: SafeAvailableAmount
    ) throws {
        guard Self.isValidSourceID(sourceID), sequence >= 0,
              kind != .commitment || sequence > 0,
              kind != .availableBalance || (sourceID == "available-balance" && sequence == 0)
        else { throw SafeAvailableModelError.invalid }
        if kind != .availableBalance && amount.minor <= 0 {
            throw SafeAvailableModelError.invalid
        }
        self.kind = kind
        self.sourceID = sourceID
        self.sequence = sequence
        self.dueOn = dueOn
        self.amount = amount
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingSafeAvailableUnknownKeys(["kind", "sourceId", "sequence", "dueOn", "amount"])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            kind: container.decode(SafeAvailableBreakdownKind.self, forKey: .kind),
            sourceID: container.decode(String.self, forKey: .sourceID),
            sequence: container.decode(Int.self, forKey: .sequence),
            dueOn: container.decode(RecurrenceCivilDate.self, forKey: .dueOn),
            amount: container.decode(SafeAvailableAmount.self, forKey: .amount)
        )
    }

    private static func isValidSourceID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return (1...128).contains(bytes.count) && bytes.allSatisfy { (33...126).contains($0) }
    }

    private enum CodingKeys: String, CodingKey {
        case kind, sourceID = "sourceId", sequence, dueOn, amount
    }
}

struct SafeAvailableResponse: Codable, Equatable, Sendable {
    let periodStart: RecurrenceCivilDate
    let periodEnd: RecurrenceCivilDate
    let availableBalance: SafeAvailableAmount
    let totalConfirmedIncome: SafeAvailableAmount
    let totalConfirmedExpense: SafeAvailableAmount
    let totalConfirmedCommitments: SafeAvailableAmount
    let finalAmount: SafeAvailableAmount
    let breakdown: [SafeAvailableBreakdown]
    let missingData: [SafeAvailableMissingData]

    init(
        periodStart: RecurrenceCivilDate,
        periodEnd: RecurrenceCivilDate,
        availableBalance: SafeAvailableAmount,
        totalConfirmedIncome: SafeAvailableAmount,
        totalConfirmedExpense: SafeAvailableAmount,
        totalConfirmedCommitments: SafeAvailableAmount,
        finalAmount: SafeAvailableAmount,
        breakdown: [SafeAvailableBreakdown],
        missingData: [SafeAvailableMissingData]
    ) throws {
        guard !periodEnd.isBefore(periodStart),
              Self.hasUniqueMissingData(missingData),
              missingData.contains(.budget),
              Self.hasUniqueBreakdown(breakdown),
              Self.isSorted(breakdown),
              Self.isInPeriod(breakdown, start: periodStart, end: periodEnd),
              Self.matchesBreakdown(
                  breakdown,
                  availableBalance: availableBalance,
                  income: totalConfirmedIncome,
                  expense: totalConfirmedExpense,
                  commitments: totalConfirmedCommitments
              ),
              Self.matchesFormula(
                  availableBalance: availableBalance,
                  income: totalConfirmedIncome,
                  expense: totalConfirmedExpense,
                  commitments: totalConfirmedCommitments,
                  finalAmount: finalAmount
              )
        else { throw SafeAvailableModelError.invalid }
        self.periodStart = periodStart
        self.periodEnd = periodEnd
        self.availableBalance = availableBalance
        self.totalConfirmedIncome = totalConfirmedIncome
        self.totalConfirmedExpense = totalConfirmedExpense
        self.totalConfirmedCommitments = totalConfirmedCommitments
        self.finalAmount = finalAmount
        self.breakdown = breakdown
        self.missingData = missingData.sorted { $0.rawValue < $1.rawValue }
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingSafeAvailableUnknownKeys([
            "periodStart", "periodEnd", "availableBalance", "totalConfirmedIncome",
            "totalConfirmedExpense", "totalConfirmedCommitments", "finalAmount", "breakdown", "missingData"
        ])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        try self.init(
            periodStart: container.decode(RecurrenceCivilDate.self, forKey: .periodStart),
            periodEnd: container.decode(RecurrenceCivilDate.self, forKey: .periodEnd),
            availableBalance: container.decode(SafeAvailableAmount.self, forKey: .availableBalance),
            totalConfirmedIncome: container.decode(SafeAvailableAmount.self, forKey: .totalConfirmedIncome),
            totalConfirmedExpense: container.decode(SafeAvailableAmount.self, forKey: .totalConfirmedExpense),
            totalConfirmedCommitments: container.decode(SafeAvailableAmount.self, forKey: .totalConfirmedCommitments),
            finalAmount: container.decode(SafeAvailableAmount.self, forKey: .finalAmount),
            breakdown: container.decode([SafeAvailableBreakdown].self, forKey: .breakdown),
            missingData: container.decode([SafeAvailableMissingData].self, forKey: .missingData)
        )
    }

    private static func hasUniqueMissingData(_ values: [SafeAvailableMissingData]) -> Bool {
        Set(values).count == values.count
    }

    private static func hasUniqueBreakdown(_ values: [SafeAvailableBreakdown]) -> Bool {
        Set(values.map(\.id)).count == values.count
    }

    private static func isInPeriod(
        _ values: [SafeAvailableBreakdown],
        start: RecurrenceCivilDate,
        end: RecurrenceCivilDate
    ) -> Bool {
        values.allSatisfy { !$0.dueOn.isBefore(start) && !end.isBefore($0.dueOn) }
    }

    private static func isSorted(_ values: [SafeAvailableBreakdown]) -> Bool {
        zip(values, values.dropFirst()).allSatisfy { isBeforeOrEqual($0, $1) }
    }

    private static func isBeforeOrEqual(_ lhs: SafeAvailableBreakdown, _ rhs: SafeAvailableBreakdown) -> Bool {
        if lhs.dueOn != rhs.dueOn { return lhs.dueOn < rhs.dueOn }
        if lhs.kind != rhs.kind { return kindRank(lhs.kind) < kindRank(rhs.kind) }
        if lhs.sourceID != rhs.sourceID { return lhs.sourceID < rhs.sourceID }
        if lhs.sequence != rhs.sequence { return lhs.sequence < rhs.sequence }
        return lhs.amount.minor <= rhs.amount.minor
    }

    private static func kindRank(_ kind: SafeAvailableBreakdownKind) -> Int {
        switch kind {
        case .availableBalance: 0
        case .income: 1
        case .expense: 2
        case .commitment: 3
        }
    }

    private static func matchesBreakdown(
        _ lines: [SafeAvailableBreakdown],
        availableBalance: SafeAvailableAmount,
        income: SafeAvailableAmount,
        expense: SafeAvailableAmount,
        commitments: SafeAvailableAmount
    ) -> Bool {
        var balanceLines = lines.filter { $0.kind == .availableBalance }
        guard balanceLines.count == 1, balanceLines.removeFirst().amount == availableBalance else { return false }
        guard sum(lines.filter { $0.kind == .income }) == Optional(income.minor) else { return false }
        guard sum(lines.filter { $0.kind == .expense }) == Optional(expense.minor) else { return false }
        return sum(lines.filter { $0.kind == .commitment }) == Optional(commitments.minor)
    }

    private static func sum(_ values: [SafeAvailableBreakdown]) -> Int64? {
        var total: Int64 = 0
        for value in values {
            let (next, overflow) = total.addingReportingOverflow(value.amount.minor)
            if overflow { return nil }
            total = next
        }
        return total
    }

    private static func matchesFormula(
        availableBalance: SafeAvailableAmount,
        income: SafeAvailableAmount,
        expense: SafeAvailableAmount,
        commitments: SafeAvailableAmount,
        finalAmount: SafeAvailableAmount
    ) -> Bool {
        let (withIncome, incomeOverflow) = availableBalance.minor.addingReportingOverflow(income.minor)
        guard !incomeOverflow else { return false }
        let (afterExpense, expenseOverflow) = withIncome.subtractingReportingOverflow(expense.minor)
        guard !expenseOverflow else { return false }
        let (expected, commitmentOverflow) = afterExpense.subtractingReportingOverflow(commitments.minor)
        return !commitmentOverflow && expected == finalAmount.minor
    }

    private enum CodingKeys: String, CodingKey {
        case periodStart, periodEnd, availableBalance, totalConfirmedIncome, totalConfirmedExpense
        case totalConfirmedCommitments, finalAmount, breakdown, missingData
    }
}

private struct SafeAvailableAnyCodingKey: CodingKey {
    let stringValue: String
    let intValue: Int?

    init?(stringValue: String) { self.stringValue = stringValue; intValue = nil }
    init?(intValue: Int) { stringValue = String(intValue); self.intValue = intValue }
}

private extension Decoder {
    func rejectingSafeAvailableUnknownKeys(_ allowed: Set<String>) throws {
        let keys = try container(keyedBy: SafeAvailableAnyCodingKey.self).allKeys
        guard keys.allSatisfy({ allowed.contains($0.stringValue) }) else {
            throw SafeAvailableModelError.invalid
        }
    }
}

private extension RecurrenceCivilDate {
    func isBefore(_ other: RecurrenceCivilDate) -> Bool { self < other }
}
