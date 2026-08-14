import Foundation

enum BRLMoneyInputError: Error, Equatable {
    case empty
    case invalid
    case nonPositive
    case tooManyFractionDigits
    case overflow
}

struct BRLMoneyParser: Sendable {
    func parseMinorUnits(_ input: String) throws -> Int64 {
        let value = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else {
            throw BRLMoneyInputError.empty
        }
        guard !value.contains("-") && !value.contains("+") else {
            throw BRLMoneyInputError.invalid
        }

        let separators = value.filter { $0 == "," || $0 == "." }
        guard separators.count <= 1 else {
            throw BRLMoneyInputError.invalid
        }

        let parts = value.split(omittingEmptySubsequences: false) { $0 == "," || $0 == "." }
        guard parts.count == 1 || parts.count == 2,
              let wholePart = parts.first,
              !wholePart.isEmpty,
              wholePart.allSatisfy({ $0.isNumber })
        else {
            throw BRLMoneyInputError.invalid
        }

        let fractionPart = parts.count == 2 ? parts[1] : Substring()
        if parts.count == 2 && fractionPart.isEmpty {
            throw BRLMoneyInputError.invalid
        }
        guard fractionPart.count <= 2 else {
            throw BRLMoneyInputError.tooManyFractionDigits
        }
        guard fractionPart.allSatisfy({ $0.isNumber }) else {
            throw BRLMoneyInputError.invalid
        }

        guard let whole = Int64(wholePart) else {
            throw BRLMoneyInputError.overflow
        }

        let fraction: Int64
        switch fractionPart.count {
        case 0:
            fraction = 0
        case 1:
            guard let digit = Int64(fractionPart) else {
                throw BRLMoneyInputError.invalid
            }
            fraction = digit * 10
        case 2:
            guard let digits = Int64(fractionPart) else {
                throw BRLMoneyInputError.invalid
            }
            fraction = digits
        default:
            throw BRLMoneyInputError.tooManyFractionDigits
        }

        guard whole <= (Int64.max - fraction) / 100 else {
            throw BRLMoneyInputError.overflow
        }
        let minorUnits = whole * 100 + fraction
        guard minorUnits > 0 else {
            throw BRLMoneyInputError.nonPositive
        }
        return minorUnits
    }
}

struct BRLMoneyFormatter: Sendable {
    func string(minorUnits: Int64) -> String {
        let major = minorUnits / 100
        let cents = minorUnits % 100
        return "R$ \(major),\(String(format: "%02lld", cents))"
    }
}
