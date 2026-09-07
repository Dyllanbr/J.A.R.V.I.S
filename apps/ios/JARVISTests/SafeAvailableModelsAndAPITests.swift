import Foundation
import XCTest
@testable import JARVIS

final class SafeAvailableModelsAndAPITests: XCTestCase {
    override func tearDown() {
        SafeAvailableURLProtocol.removeHandler()
        super.tearDown()
    }

    func testDecodesSignedProjectionAndPreservesFormulaAndBreakdown() throws {
        let response = try JSONDecoder().decode(
            SafeAvailableResponse.self,
            from: Data(Self.positiveJSON.utf8)
        )

        XCTAssertEqual(response.periodStart.canonicalValue, "2026-09-01")
        XCTAssertEqual(response.periodEnd.canonicalValue, "2026-09-30")
        XCTAssertEqual(response.availableBalance.minor, 10_000)
        XCTAssertEqual(response.totalConfirmedIncome.minor, 5_000)
        XCTAssertEqual(response.totalConfirmedExpense.minor, 2_500)
        XCTAssertEqual(response.totalConfirmedCommitments.minor, 3_000)
        XCTAssertEqual(response.finalAmount.minor, 9_500)
        XCTAssertEqual(response.breakdown.map(\.kind), [.availableBalance, .income, .expense, .commitment])
        XCTAssertEqual(response.missingData, [.budget])
    }

    func testAcceptsZeroAndNegativeFinalAmounts() throws {
        let zero = try JSONDecoder().decode(
            SafeAvailableResponse.self,
            from: Data(Self.zeroJSON.utf8)
        )
        XCTAssertEqual(zero.finalAmount.minor, 0)

        let negative = try JSONDecoder().decode(
            SafeAvailableResponse.self,
            from: Data(Self.negativeJSON.utf8)
        )
        XCTAssertEqual(negative.finalAmount.minor, -2_500)
        XCTAssertEqual(negative.availableBalance.minor, 1_000)
    }

    func testRejectsUnknownKeysDuplicateLinesAndInconsistentTotals() {
        let unknown = Self.positiveJSON.replacingOccurrences(
            of: "\"missingData\":[\"BUDGET\"]",
            with: "\"unexpected\":true,\"missingData\":[\"BUDGET\"]"
        )
        XCTAssertThrowsError(try JSONDecoder().decode(SafeAvailableResponse.self, from: Data(unknown.utf8)))

        let duplicate = Self.positiveJSON.replacingOccurrences(
            of: "\"breakdown\":[",
            with: "\"breakdown\":[{\"kind\":\"AVAILABLE_BALANCE\",\"sourceId\":\"available-balance\",\"sequence\":0,\"dueOn\":\"2026-09-01\",\"amount\":{\"minor\":10000,\"currency\":\"BRL\"}},"
        )
        XCTAssertThrowsError(try JSONDecoder().decode(SafeAvailableResponse.self, from: Data(duplicate.utf8)))

        let mismatch = Self.positiveJSON.replacingOccurrences(
            of: "\"minor\":9500",
            with: "\"minor\":9501"
        )
        XCTAssertThrowsError(try JSONDecoder().decode(SafeAvailableResponse.self, from: Data(mismatch.utf8)))

        let missingBudget = Self.positiveJSON.replacingOccurrences(
            of: "[\"BUDGET\"]",
            with: "[]"
        )
        XCTAssertThrowsError(try JSONDecoder().decode(SafeAvailableResponse.self, from: Data(missingBudget.utf8)))
    }

    func testRejectsInvalidCurrencyAndOutOfPeriodBreakdown() {
        let currency = Self.positiveJSON.replacingOccurrences(of: "\"currency\":\"BRL\"", with: "\"currency\":\"USD\"")
        XCTAssertThrowsError(try JSONDecoder().decode(SafeAvailableResponse.self, from: Data(currency.utf8)))

        let date = Self.positiveJSON.replacingOccurrences(of: "\"dueOn\":\"2026-09-01\"", with: "\"dueOn\":\"2026-10-01\"")
        XCTAssertThrowsError(try JSONDecoder().decode(SafeAvailableResponse.self, from: Data(date.utf8)))
    }

    @MainActor
    func testAPIUsesExplicitCivilQueryAndReadOnlyHeaders() async throws {
        SafeAvailableURLProtocol.install { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/safe-available")
            XCTAssertEqual(request.url?.query, "periodStart=2026-09-01&periodEnd=2026-09-30")
            XCTAssertNil(request.httpBody)
            XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/json")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Cache-Control"), "no-store")
            XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
            XCTAssertFalse(request.url?.query?.contains("owner") ?? false)
            return Self.response(request: request, status: 200, body: Self.positiveJSON)
        }

        let result = try await Self.makeClient().safeAvailable(
            periodStart: try RecurrenceCivilDate("2026-09-01"),
            periodEnd: try RecurrenceCivilDate("2026-09-30")
        )
        XCTAssertEqual(result.finalAmount.minor, 9_500)
    }

    @MainActor
    func testAPIFailsBeforeNetworkForInvertedPeriodAndMapsErrors() async throws {
        SafeAvailableURLProtocol.install { _ in
            XCTFail("Invalid periods must be rejected before the network")
            throw URLError(.badURL)
        }
        do {
            _ = try await Self.makeClient().safeAvailable(
                periodStart: try RecurrenceCivilDate("2026-09-30"),
                periodEnd: try RecurrenceCivilDate("2026-09-01")
            )
            XCTFail("Expected invalid data")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .invalidData)
        }

        for (status, expected) in [
            (400, FinancialAPIError.invalidData),
            (500, FinancialAPIError.serviceUnavailable),
            (405, FinancialAPIError.invalidResponse)
        ] {
            SafeAvailableURLProtocol.install { request in
                Self.response(
                    request: request,
                    status: status,
                    body: #"{"error":{"code":"PRIVATE","message":"SQL secret"}}"#
                )
            }
            do {
                _ = try await Self.makeClient().safeAvailable(
                    periodStart: try RecurrenceCivilDate("2026-09-01"),
                    periodEnd: try RecurrenceCivilDate("2026-09-30")
                )
                XCTFail("Expected HTTP failure")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, expected)
            }
        }
    }

    @MainActor
    func testAPIPreservesCancellation() async throws {
        SafeAvailableURLProtocol.install { _ in throw URLError(.cancelled) }
        do {
            _ = try await Self.makeClient().safeAvailable(
                periodStart: try RecurrenceCivilDate("2026-09-01"),
                periodEnd: try RecurrenceCivilDate("2026-09-30")
            )
            XCTFail("Expected cancellation")
        } catch is CancellationError {
            // The client must not translate cancellation into a generic connection error.
        } catch {
            XCTFail("Expected CancellationError, got \(error)")
        }
    }

    @MainActor
    private static func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [SafeAvailableURLProtocol.self]
        configuration.urlCache = nil
        let session = URLSession(configuration: configuration)
        return URLSessionFinancialAPIClient(baseURL: URL(string: "http://127.0.0.1:18081")!, session: session)
    }

    private static func response(
        request: URLRequest,
        status: Int,
        body: String
    ) -> (HTTPURLResponse, Data) {
        let response = HTTPURLResponse(
            url: request.url!,
            statusCode: status,
            httpVersion: "HTTP/1.1",
            headerFields: ["Content-Type": "application/json"]
        )!
        return (response, Data(body.utf8))
    }

    private static let positiveJSON = #"""
    {
      "periodStart":"2026-09-01","periodEnd":"2026-09-30",
      "availableBalance":{"minor":10000,"currency":"BRL"},
      "totalConfirmedIncome":{"minor":5000,"currency":"BRL"},
      "totalConfirmedExpense":{"minor":2500,"currency":"BRL"},
      "totalConfirmedCommitments":{"minor":3000,"currency":"BRL"},
      "finalAmount":{"minor":9500,"currency":"BRL"},
      "breakdown":[
        {"kind":"AVAILABLE_BALANCE","sourceId":"available-balance","sequence":0,"dueOn":"2026-09-01","amount":{"minor":10000,"currency":"BRL"}},
        {"kind":"INCOME","sourceId":"inc_safe_001","sequence":0,"dueOn":"2026-09-01","amount":{"minor":5000,"currency":"BRL"}},
        {"kind":"EXPENSE","sourceId":"exp_safe_001","sequence":0,"dueOn":"2026-09-01","amount":{"minor":2500,"currency":"BRL"}},
        {"kind":"COMMITMENT","sourceId":"ipl_safe_001","sequence":1,"dueOn":"2026-09-01","amount":{"minor":3000,"currency":"BRL"}}
      ],
      "missingData":["BUDGET"]
    }
    """#

    private static let zeroJSON = #"""
    {
      "periodStart":"2026-09-01","periodEnd":"2026-09-30",
      "availableBalance":{"minor":1000,"currency":"BRL"},
      "totalConfirmedIncome":{"minor":0,"currency":"BRL"},
      "totalConfirmedExpense":{"minor":1000,"currency":"BRL"},
      "totalConfirmedCommitments":{"minor":0,"currency":"BRL"},
      "finalAmount":{"minor":0,"currency":"BRL"},
      "breakdown":[
        {"kind":"AVAILABLE_BALANCE","sourceId":"available-balance","sequence":0,"dueOn":"2026-09-01","amount":{"minor":1000,"currency":"BRL"}},
        {"kind":"EXPENSE","sourceId":"exp_safe_001","sequence":0,"dueOn":"2026-09-01","amount":{"minor":1000,"currency":"BRL"}}
      ],
      "missingData":["BUDGET"]
    }
    """#

    private static let negativeJSON = #"""
    {
      "periodStart":"2026-09-01","periodEnd":"2026-09-30",
      "availableBalance":{"minor":1000,"currency":"BRL"},
      "totalConfirmedIncome":{"minor":0,"currency":"BRL"},
      "totalConfirmedExpense":{"minor":2500,"currency":"BRL"},
      "totalConfirmedCommitments":{"minor":1000,"currency":"BRL"},
      "finalAmount":{"minor":-2500,"currency":"BRL"},
      "breakdown":[
        {"kind":"AVAILABLE_BALANCE","sourceId":"available-balance","sequence":0,"dueOn":"2026-09-01","amount":{"minor":1000,"currency":"BRL"}},
        {"kind":"EXPENSE","sourceId":"exp_safe_001","sequence":0,"dueOn":"2026-09-01","amount":{"minor":2500,"currency":"BRL"}},
        {"kind":"COMMITMENT","sourceId":"ipl_safe_001","sequence":1,"dueOn":"2026-09-01","amount":{"minor":1000,"currency":"BRL"}}
      ],
      "missingData":["BUDGET"]
    }
    """#
}

private final class SafeAvailableURLProtocol: URLProtocol, @unchecked Sendable {
    private final class Storage: @unchecked Sendable {
        let lock = NSLock()
        var handler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))?
    }

    private static let storage = Storage()

    static func install(_ handler: @escaping @Sendable (URLRequest) throws -> (HTTPURLResponse, Data)) {
        storage.lock.withLock { storage.handler = handler }
    }

    static func removeHandler() {
        storage.lock.withLock { storage.handler = nil }
    }

    override class func canInit(with _: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let handler = Self.storage.lock.withLock { Self.storage.handler }
        guard let handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.unknown))
            return
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}
