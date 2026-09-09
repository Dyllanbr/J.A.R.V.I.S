import Foundation

enum PurchaseSimulationMode: String, Codable, CaseIterable, Hashable, Sendable {
    case oneTime = "ONE_TIME"
    case installment = "INSTALLMENT"
}

enum PurchaseSimulationAssumption: String, Codable, CaseIterable, Hashable, Sendable {
    case notPersisted = "NOT_PERSISTED"
    case noExpenseCreated = "NO_EXPENSE_CREATED"
}

enum PurchaseSimulationModelError: Error, Equatable {
    case invalid
}

struct PurchaseSimulationRequest: Encodable, Equatable, Sendable {
    let creditCardID: String
    let amount: FinancialMoney
    let purchaseOn: RecurrenceCivilDate
    let purchaseMode: PurchaseSimulationMode
    let installmentCount: Int?
    let periodStart: RecurrenceCivilDate
    let periodEnd: RecurrenceCivilDate

    init(
        creditCardID: String,
        amount: FinancialMoney,
        purchaseOn: RecurrenceCivilDate,
        purchaseMode: PurchaseSimulationMode,
        installmentCount: Int? = nil,
        periodStart: RecurrenceCivilDate,
        periodEnd: RecurrenceCivilDate
    ) throws {
        guard CreditCard.isValidID(creditCardID), amount.currency == .brl, amount.minor > 0,
              !(periodEnd < periodStart)
        else { throw PurchaseSimulationModelError.invalid }
        switch purchaseMode {
        case .oneTime:
            guard installmentCount == nil else { throw PurchaseSimulationModelError.invalid }
        case .installment:
            guard let installmentCount, (2...120).contains(installmentCount) else {
                throw PurchaseSimulationModelError.invalid
            }
        }
        self.creditCardID = creditCardID
        self.amount = amount
        self.purchaseOn = purchaseOn
        self.purchaseMode = purchaseMode
        self.installmentCount = installmentCount
        self.periodStart = periodStart
        self.periodEnd = periodEnd
    }

    private enum CodingKeys: String, CodingKey {
        case creditCardID = "creditCardId"
        case amount, purchaseOn, purchaseMode, installmentCount, periodStart, periodEnd
    }
}

struct PurchaseSimulationResponse: Decodable, Equatable, Sendable {
    let creditCardID: String
    let purchaseOn: RecurrenceCivilDate
    let purchaseMode: PurchaseSimulationMode
    let installmentCount: Int?
    let periodStart: RecurrenceCivilDate
    let periodEnd: RecurrenceCivilDate
    let baseline: SafeAvailableResponse
    let projected: SafeAvailableResponse
    let impact: SafeAvailableAmount
    let hypotheticalCommitments: [SafeAvailableBreakdown]
    let assumptions: [PurchaseSimulationAssumption]

    init(
        creditCardID: String,
        purchaseOn: RecurrenceCivilDate,
        purchaseMode: PurchaseSimulationMode,
        installmentCount: Int? = nil,
        periodStart: RecurrenceCivilDate,
        periodEnd: RecurrenceCivilDate,
        baseline: SafeAvailableResponse,
        projected: SafeAvailableResponse,
        impact: SafeAvailableAmount,
        hypotheticalCommitments: [SafeAvailableBreakdown],
        assumptions: [PurchaseSimulationAssumption]
    ) throws {
        guard CreditCard.isValidID(creditCardID), !(periodEnd < periodStart), impact.currency == .brl,
              baseline.periodStart == periodStart, baseline.periodEnd == periodEnd,
              projected.periodStart == periodStart, projected.periodEnd == periodEnd,
              Self.validMode(purchaseMode, count: installmentCount),
              Self.validHypotheticalCommitments(hypotheticalCommitments, start: periodStart, end: periodEnd),
              assumptions.count == Set(assumptions).count,
              Set(assumptions) == Set(PurchaseSimulationAssumption.allCases)
        else { throw PurchaseSimulationModelError.invalid }
        let (expectedProjected, overflow) = baseline.finalAmount.minor.addingReportingOverflow(impact.minor)
        guard !overflow, projected.finalAmount.minor == expectedProjected else {
            throw PurchaseSimulationModelError.invalid
        }
        self.creditCardID = creditCardID
        self.purchaseOn = purchaseOn
        self.purchaseMode = purchaseMode
        self.installmentCount = installmentCount
        self.periodStart = periodStart
        self.periodEnd = periodEnd
        self.baseline = baseline
        self.projected = projected
        self.impact = impact
        self.hypotheticalCommitments = hypotheticalCommitments
        self.assumptions = assumptions
    }

    init(from decoder: Decoder) throws {
        try decoder.rejectingPurchaseSimulationUnknownKeys([
            "creditCardId", "purchaseOn", "purchaseMode", "installmentCount", "periodStart", "periodEnd",
            "baseline", "projected", "impact", "hypotheticalCommitments", "assumptions"
        ])
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let creditCardID = try container.decode(String.self, forKey: .creditCardID)
        let purchaseOn = try container.decode(RecurrenceCivilDate.self, forKey: .purchaseOn)
        let purchaseMode = try container.decode(PurchaseSimulationMode.self, forKey: .purchaseMode)
        let installmentCount = try container.decodeIfPresent(Int.self, forKey: .installmentCount)
        let periodStart = try container.decode(RecurrenceCivilDate.self, forKey: .periodStart)
        let periodEnd = try container.decode(RecurrenceCivilDate.self, forKey: .periodEnd)
        let baseline = try container.decode(SafeAvailableResponse.self, forKey: .baseline)
        let projected = try container.decode(SafeAvailableResponse.self, forKey: .projected)
        let impact = try container.decode(SafeAvailableAmount.self, forKey: .impact)
        let hypotheticalCommitments = try container.decode([SafeAvailableBreakdown].self, forKey: .hypotheticalCommitments)
        let assumptions = try container.decode([PurchaseSimulationAssumption].self, forKey: .assumptions)
        try self.init(
            creditCardID: creditCardID, purchaseOn: purchaseOn, purchaseMode: purchaseMode,
            installmentCount: installmentCount, periodStart: periodStart, periodEnd: periodEnd,
            baseline: baseline, projected: projected, impact: impact,
            hypotheticalCommitments: hypotheticalCommitments, assumptions: assumptions
        )
    }

    private static func validMode(_ mode: PurchaseSimulationMode, count: Int?) -> Bool {
        switch mode {
        case .oneTime: count == nil
        case .installment: count.map { (2...120).contains($0) } ?? false
        }
    }

    private static func validHypotheticalCommitments(
        _ values: [SafeAvailableBreakdown],
        start: RecurrenceCivilDate,
        end: RecurrenceCivilDate
    ) -> Bool {
        let identifiers = values.map(\.id)
        guard Set(identifiers).count == identifiers.count else { return false }
        return values.allSatisfy {
            $0.kind == .commitment && $0.sequence > 0 && !$0.dueOn.isBefore(start) && !end.isBefore($0.dueOn)
        } && zip(values, values.dropFirst()).allSatisfy { lhs, rhs in
            if lhs.dueOn != rhs.dueOn { return lhs.dueOn < rhs.dueOn }
            if lhs.sourceID != rhs.sourceID { return lhs.sourceID < rhs.sourceID }
            if lhs.sequence != rhs.sequence { return lhs.sequence < rhs.sequence }
            return lhs.amount.minor <= rhs.amount.minor
        }
    }

    private enum CodingKeys: String, CodingKey {
        case creditCardID = "creditCardId"
        case purchaseOn, purchaseMode, installmentCount, periodStart, periodEnd
        case baseline, projected, impact, hypotheticalCommitments, assumptions
    }
}

private struct PurchaseSimulationAnyCodingKey: CodingKey {
    let stringValue: String
    let intValue: Int?

    init?(stringValue: String) { self.stringValue = stringValue; intValue = nil }
    init?(intValue: Int) { stringValue = String(intValue); self.intValue = intValue }
}

private extension Decoder {
    func rejectingPurchaseSimulationUnknownKeys(_ allowed: Set<String>) throws {
        let keys = try container(keyedBy: PurchaseSimulationAnyCodingKey.self).allKeys
        guard keys.allSatisfy({ allowed.contains($0.stringValue) }) else {
            throw PurchaseSimulationModelError.invalid
        }
    }
}

private extension RecurrenceCivilDate {
    func isBefore(_ other: RecurrenceCivilDate) -> Bool { self < other }
}
