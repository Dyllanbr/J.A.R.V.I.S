import Foundation

struct FinancialDisplayFormatter: Sendable {
    private let timestampCodec = RFC3339DateCodec()

    func dateTime(_ timestamp: String) -> String {
        guard let date = try? timestampCodec.decode(timestamp) else {
            return "Data indisponível"
        }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "pt_BR")
        formatter.timeZone = TimeZone(identifier: "America/Sao_Paulo")
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: date)
    }
}
