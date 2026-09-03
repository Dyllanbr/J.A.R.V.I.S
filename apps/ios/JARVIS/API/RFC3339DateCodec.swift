import Foundation

enum RFC3339DateCodecError: Error, Equatable {
    case invalidTimestamp
}

struct RFC3339DateCodec: Sendable {
    func decode(_ value: String) throws -> Date {
        let parsed = try splitFractionalSeconds(from: value)
        let wholeSeconds = Date.ISO8601FormatStyle(includingFractionalSeconds: false)
        guard let date = try? wholeSeconds.parse(parsed.wholeValue) else {
            throw RFC3339DateCodecError.invalidTimestamp
        }
        return date.addingTimeInterval(parsed.fractionalSeconds)
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

    private func splitFractionalSeconds(from value: String) throws -> (wholeValue: String, fractionalSeconds: TimeInterval) {
        let bytes = Array(value.utf8)
        let decimalByte: UInt8 = 46
        let zeroByte: UInt8 = 48
        let nineByte: UInt8 = 57
        let utcByte: UInt8 = 90
        let plusByte: UInt8 = 43
        let minusByte: UInt8 = 45
        guard let decimalIndex = bytes.firstIndex(of: decimalByte) else {
            return (value, 0)
        }
        guard decimalIndex == 19 else {
            throw RFC3339DateCodecError.invalidTimestamp
        }

        var fractionEnd = decimalIndex + 1
        while fractionEnd < bytes.count,
              bytes[fractionEnd] >= zeroByte,
              bytes[fractionEnd] <= nineByte {
            fractionEnd += 1
        }
        guard fractionEnd > decimalIndex + 1,
              fractionEnd < bytes.count,
              bytes[fractionEnd] == utcByte ||
                bytes[fractionEnd] == plusByte ||
                bytes[fractionEnd] == minusByte
        else {
            throw RFC3339DateCodecError.invalidTimestamp
        }

        var fractionalSeconds = 0.0
        var place = 0.1
        for (index, byte) in bytes[(decimalIndex + 1)..<fractionEnd].enumerated() {
            if index < 18 {
                fractionalSeconds += Double(byte - zeroByte) * place
                place *= 0.1
            }
        }

        var wholeBytes = bytes
        wholeBytes.removeSubrange(decimalIndex..<fractionEnd)
        return (String(decoding: wholeBytes, as: UTF8.self), fractionalSeconds)
    }
}
