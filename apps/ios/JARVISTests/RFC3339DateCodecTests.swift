import XCTest
@testable import JARVIS

final class RFC3339DateCodecTests: XCTestCase {
    private let codec = RFC3339DateCodec()

    func testDecodesWholeSecondsAndFractionalBackendTimestamps() throws {
        let whole = try codec.decode("2026-08-14T15:00:00Z")
        let microseconds = try codec.decode("2026-08-14T15:00:00.123456Z")
        let nanoseconds = try codec.decode("2026-08-14T15:00:00.123456789Z")

        XCTAssertEqual(microseconds.timeIntervalSince(whole), 0.123456, accuracy: 0.000_001)
        XCTAssertEqual(nanoseconds.timeIntervalSince(whole), 0.123456789, accuracy: 0.000_001)
        XCTAssertNotEqual(microseconds, nanoseconds)
        XCTAssertEqual(nanoseconds.timeIntervalSince(microseconds), 0.000000789, accuracy: 0.000_0002)
    }

    func testEquivalentOffsetRepresentsSameInstant() throws {
        let utc = try codec.decode("2026-08-14T15:00:00Z")
        let offset = try codec.decode("2026-08-14T12:00:00-03:00")
        XCTAssertEqual(utc, offset)
    }

    func testEncodeUsesUTCAndRoundTrips() throws {
        let date = try codec.decode("2026-08-14T12:00:00.123-03:00")
        let encoded = codec.encode(date)
        XCTAssertTrue(encoded.hasSuffix("Z"))
        XCTAssertEqual(try codec.decode(encoded), date)
    }

    func testRejectsInvalidTimestamp() {
        for value in [
            "14/08/2026 15:00",
            "2026-08-14T15:00:00.Z",
            "2026-08-14T15:00:00.not-a-numberZ",
            "2026-08-14T15:00:00Z.123",
        ] {
            XCTAssertThrowsError(try codec.decode(value), value)
        }
    }
}
