import Foundation

struct FinancialMonth: Equatable, Sendable {
    let year: Int
    let month: Int

    init(date: Date, calendar: Calendar = .financial) {
        let components = calendar.dateComponents([.year, .month], from: date)
        year = components.year ?? 1
        month = components.month ?? 1
    }

    init(year: Int, month: Int) {
        self.year = year
        self.month = month
    }

    var apiValue: String {
        String(format: "%04d-%02d", year, month)
    }

    var displayName: String {
        guard let date = Calendar.financial.date(from: DateComponents(year: year, month: month, day: 1)) else {
            return apiValue
        }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "pt_BR")
        formatter.timeZone = Calendar.financial.timeZone
        formatter.dateFormat = "LLLL 'de' yyyy"
        return formatter.string(from: date).capitalized(with: formatter.locale)
    }

    func adding(months: Int) -> FinancialMonth {
        let calendar = Calendar.financial
        guard let date = calendar.date(from: DateComponents(year: year, month: month, day: 1)),
              let changed = calendar.date(byAdding: .month, value: months, to: date)
        else {
            return self
        }
        return FinancialMonth(date: changed, calendar: calendar)
    }
}

extension Calendar {
    static var financial: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.locale = Locale(identifier: "pt_BR")
        calendar.timeZone = TimeZone(identifier: "America/Sao_Paulo") ?? TimeZone(secondsFromGMT: 0)!
        return calendar
    }
}
