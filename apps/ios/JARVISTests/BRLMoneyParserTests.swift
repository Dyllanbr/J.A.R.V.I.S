import XCTest
@testable import JARVIS

final class BRLMoneyParserTests: XCTestCase {
    private let parser = BRLMoneyParser()

    func testParsesAcceptedBrazilianInputsWithoutFloatingPoint() throws {
        XCTAssertEqual(try parser.parseMinorUnits("42"), 4_200)
        XCTAssertEqual(try parser.parseMinorUnits("42,5"), 4_250)
        XCTAssertEqual(try parser.parseMinorUnits("42,50"), 4_250)
        XCTAssertEqual(try parser.parseMinorUnits("42.50"), 4_250)
    }

    func testRejectsEmptyZeroNegativeInvalidAndExcessPrecision() {
        assertError("", .empty)
        assertError("0", .nonPositive)
        assertError("-1", .invalid)
        assertError("abc", .invalid)
        assertError("42,501", .tooManyFractionDigits)
        assertError("1.000,00", .invalid)
    }

    func testRejectsOverflow() {
        assertError("92233720368547758,08", .overflow)
        assertError("999999999999999999999999", .overflow)
    }

    private func assertError(
        _ input: String,
        _ expected: BRLMoneyInputError,
        file: StaticString = #filePath,
        line: UInt = #line
    ) {
        XCTAssertThrowsError(try parser.parseMinorUnits(input), file: file, line: line) { error in
            XCTAssertEqual(error as? BRLMoneyInputError, expected, file: file, line: line)
        }
    }
}
