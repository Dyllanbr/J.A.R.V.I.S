import Foundation

enum RFC3339DateCodecError: Error, Equatable {
    case invalidTimestamp
}

struct RFC3339DateCodec: Sendable {
    func decode(_ value: String) throws -> Date {
        let fractional = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
        if let date = try? fractional.parse(value) {
            return date
        }

        let wholeSeconds = Date.ISO8601FormatStyle(includingFractionalSeconds: false)
        if let date = try? wholeSeconds.parse(value) {
            return date
        }

        throw RFC3339DateCodecError.invalidTimestamp
    }

    func encode(_ date: Date) -> String {
        date.formatted(
            Date.ISO8601FormatStyle(
                dateSeparator: .dash,
                dateTimeSeparator: .standard,
                timeSeparator: .colon,
                timeZoneSeparator: .colon,
                includingFractionalSeconds: true,
                timeZone: TimeZone(secondsFromGMT: 0) ?? .gmt
            )
        )
    }
}
