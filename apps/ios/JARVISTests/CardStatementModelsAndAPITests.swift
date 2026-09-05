import Foundation
import XCTest
@testable import JARVIS

final class CardStatementModelsAndAPITests: XCTestCase {
    override func tearDown() {
        CardStatementURLProtocolStub.removeHandler()
        super.tearDown()
    }

    func testDecodesMixedStatementAndPreservesExactTotal() throws {
        let statement = try JSONDecoder().decode(CardStatement.self, from: Data(Self.mixedJSON.utf8))

        XCTAssertEqual(statement.creditCardID, Self.cardID)
        XCTAssertEqual(statement.statementDueOn.canonicalValue, "2026-09-10")
        XCTAssertEqual(statement.totalAmount.minor, 8_333)
        XCTAssertEqual(statement.lines.map(\.expenseID), ["exp_statement_one_time", "exp_statement_installment"])
        XCTAssertEqual(statement.lines.reduce(Int64(0)) { $0 + $1.amount.minor }, statement.totalAmount.minor)
        XCTAssertEqual(statement.lines[1].installmentNumber, 2)
        XCTAssertEqual(statement.lines[1].installmentCount, 3)
    }

    func testDecodesEmptyStatementWithZeroTotal() throws {
        let statement = try JSONDecoder().decode(
            CardStatement.self,
            from: Data(Self.emptyJSON.utf8)
        )

        XCTAssertEqual(statement.totalAmount.minor, 0)
        XCTAssertTrue(statement.lines.isEmpty)
    }

    func testRejectsUnknownKeysAndInvalidTotals() throws {
        let unknown = Self.mixedJSON.replacingOccurrences(of: "\"lines\":[", with: "\"unexpected\":true,\"lines\":[")
        XCTAssertThrowsError(try JSONDecoder().decode(CardStatement.self, from: Data(unknown.utf8)))

        let negativeTotal = Self.emptyJSON.replacingOccurrences(of: "\"minor\":0", with: "\"minor\":-1")
        XCTAssertThrowsError(try JSONDecoder().decode(CardStatement.self, from: Data(negativeTotal.utf8)))

        let mismatch = Self.mixedJSON.replacingOccurrences(of: "\"minor\":8333", with: "\"minor\":8332")
        XCTAssertThrowsError(try JSONDecoder().decode(CardStatement.self, from: Data(mismatch.utf8)))

        let duplicateID = Self.mixedJSON.replacingOccurrences(of: "exp_statement_installment", with: "exp_statement_one_time")
        XCTAssertThrowsError(try JSONDecoder().decode(CardStatement.self, from: Data(duplicateID.utf8)))
    }

    func testRejectsInvalidLineMetadataAndAmounts() throws {
        let oneTimeWithMetadata = Self.mixedJSON.replacingOccurrences(of: "\"purchaseMode\":\"ONE_TIME\"", with: "\"purchaseMode\":\"ONE_TIME\",\"installmentNumber\":1")
        XCTAssertThrowsError(try JSONDecoder().decode(CardStatement.self, from: Data(oneTimeWithMetadata.utf8)))

        let missingInstallmentCount = Self.mixedJSON.replacingOccurrences(of: ",\"installmentCount\":3", with: "")
        XCTAssertThrowsError(try JSONDecoder().decode(CardStatement.self, from: Data(missingInstallmentCount.utf8)))

        let zeroLine = Self.mixedJSON.replacingOccurrences(of: "\"minor\":3333", with: "\"minor\":0")
        XCTAssertThrowsError(try JSONDecoder().decode(CardStatement.self, from: Data(zeroLine.utf8)))
    }

    @MainActor
    func testCardStatementRequestUsesSafePathHeadersAndNoBody() async throws {
        CardStatementURLProtocolStub.install { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/credit-cards/\(Self.cardID)/statements/2026-09-10")
            XCTAssertNil(request.url?.query)
            XCTAssertNil(request.httpBody)
            XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/json")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Cache-Control"), "no-store")
            return Self.response(request: request, status: 200, body: Self.mixedJSON)
        }

        let result = try await Self.makeClient().cardStatement(
            creditCardID: Self.cardID,
            statementDueOn: try RecurrenceCivilDate("2026-09-10")
        )
        XCTAssertEqual(result.totalAmount.minor, 8_333)
    }

    @MainActor
    func testCardStatementMapsNotFoundAndServerErrorsSafely() async throws {
        for (status, body, expected) in [
            (400, #"{"error":{"code":"INVALID_REQUEST","message":"invalid"}}"#, FinancialAPIError.invalidData),
            (404, #"{"error":{"code":"CREDIT_CARD_NOT_FOUND","message":"not found"}}"#, FinancialAPIError.creditCardNotFound),
            (405, #"{"error":{"code":"METHOD_NOT_ALLOWED","message":"method"}}"#, FinancialAPIError.invalidResponse),
            (500, #"{"error":{"code":"PRIVATE_INTERNAL","message":"private"}}"#, FinancialAPIError.serviceUnavailable)
        ] {
            CardStatementURLProtocolStub.install { request in
                return Self.response(
                    request: request,
                    status: status,
                    body: body
                )
            }

            do {
                _ = try await Self.makeClient().cardStatement(
                    creditCardID: Self.cardID,
                    statementDueOn: try RecurrenceCivilDate("2026-09-10")
                )
                XCTFail("Expected the response to fail")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, expected)
            }
        }
    }

    @MainActor
    func testCardStatementRejectsInvalidIDBeforeNetwork() async throws {
        CardStatementURLProtocolStub.install { _ in
            XCTFail("Invalid IDs must be rejected before the network")
            throw URLError(.badURL)
        }
        do {
            _ = try await Self.makeClient().cardStatement(
                creditCardID: "not-a-card",
                statementDueOn: try RecurrenceCivilDate("2026-09-10")
            )
            XCTFail("Expected invalid data")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .invalidData)
        }
    }

    @MainActor
    private static func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CardStatementURLProtocolStub.self]
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        return URLSessionFinancialAPIClient(
            baseURL: URL(string: "http://127.0.0.1:18081")!,
            session: URLSession(configuration: configuration)
        )
    }

    private static func response(request: URLRequest, status: Int, body: String) -> (HTTPURLResponse, Data) {
        let response = HTTPURLResponse(
            url: request.url!,
            statusCode: status,
            httpVersion: "HTTP/1.1",
            headerFields: [
                "Content-Type": "application/json",
                "Cache-Control": "no-store",
                "X-Content-Type-Options": "nosniff"
            ]
        )!
        return (response, Data(body.utf8))
    }

    private static let cardID = "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

    private static let mixedJSON = """
    {
      "creditCardId":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "statementDueOn":"2026-09-10",
      "totalAmount":{"minor":8333,"currency":"BRL"},
      "lines":[
        {"expenseId":"exp_statement_installment","description":"Compra parcelada","amount":{"minor":3333,"currency":"BRL"},"occurredAt":"2026-08-13","purchaseMode":"INSTALLMENT","installmentNumber":2,"installmentCount":3},
        {"expenseId":"exp_statement_one_time","description":"Compra à vista","amount":{"minor":5000,"currency":"BRL"},"occurredAt":"2026-08-12","purchaseMode":"ONE_TIME"}
      ]
    }
    """

    private static let emptyJSON = """
    {"creditCardId":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","statementDueOn":"2026-09-10","totalAmount":{"minor":0,"currency":"BRL"},"lines":[]}
    """
}

private final class CardStatementURLProtocolStub: URLProtocol, @unchecked Sendable {
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
        let current = Self.storage.lock.withLock { Self.storage.handler }
        guard let current else {
            client?.urlProtocol(self, didFailWithError: URLError(.unknown))
            return
        }
        do {
            let (response, data) = try current(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}
