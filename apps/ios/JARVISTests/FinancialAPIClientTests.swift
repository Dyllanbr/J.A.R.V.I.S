import Foundation
import XCTest
@testable import JARVIS

final class FinancialAPIClientTests: XCTestCase {
    override func tearDown() {
        URLProtocolStub.removeHandler()
        super.tearDown()
    }

    @MainActor
    func testPreviewEncodesOnlyContractFieldsAndDecodesCanonicalResponse() async throws {
        URLProtocolStub.install { request in
            let body = try XCTUnwrap(Self.requestBody(request))
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
            XCTAssertEqual(Set(object.keys), ["type", "description", "amount", "paymentMethod", "occurredAt"])
            XCTAssertNil(object["userId"])
            XCTAssertNil(object["origin"])
            XCTAssertNil(object["financialTimezone"])
            return Self.response(
                request: request,
                status: 200,
                body: """
                {"type":"EXPENSE","description":"Mercado sintético","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"2026-08-14T15:00:00.123456Z","financialTimezone":"America/Sao_Paulo","origin":"IOS"}
                """
            )
        }
        let client = makeClient()

        let preview = try await client.preview(syntheticRequest())

        XCTAssertEqual(preview, syntheticPreview(occurredAt: "2026-08-14T15:00:00.123456Z"))
    }

    @MainActor
    func testCreateSendsIdempotencyHeaderAndReadsReplayHeader() async throws {
        URLProtocolStub.install { request in
            XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "synthetic-key-001")
            var headers = Self.standardHeaders
            headers["Idempotency-Replayed"] = "true"
            return Self.response(request: request, status: 201, body: Self.expenseJSON, headers: headers)
        }
        let result = try await makeClient().create(syntheticRequest(), idempotencyKey: "synthetic-key-001")

        XCTAssertTrue(result.replayed)
        XCTAssertEqual(result.expense.id, "exp_test_ios_001")
    }

    @MainActor
    func testIncomePreviewUsesCanonicalKeysAndOmitsPaymentMethod() async throws {
        URLProtocolStub.install { request in
            let body = try XCTUnwrap(Self.requestBody(request))
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
            XCTAssertEqual(Set(object.keys), ["type", "description", "amount", "occurredAt"])
            XCTAssertEqual(object["type"] as? String, "INCOME")
            XCTAssertNil(object["paymentMethod"])
            return Self.response(request: request, status: 200, body: Self.incomePreviewJSON)
        }

        let request = IncomeRequest(
            description: "Receita sintética",
            amount: FinancialMoney(minor: 8_500, currency: .brl),
            occurredAt: "2026-08-14T16:00:00Z"
        )
        let preview = try await makeClient().preview(request)

        XCTAssertEqual(preview, syntheticIncomePreview())
    }

    @MainActor
    func testIncomeCreateOmitsPaymentMethodAndReadsPersistedResponse() async throws {
        URLProtocolStub.install { request in
            XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "income-key-001")
            let body = try XCTUnwrap(Self.requestBody(request))
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
            XCTAssertNil(object["paymentMethod"])
            return Self.response(request: request, status: 201, body: Self.incomeJSON)
        }

        let result = try await makeClient().create(
            IncomeRequest(
                description: "Receita sintética",
                amount: FinancialMoney(minor: 8_500, currency: .brl),
                occurredAt: "2026-08-14T16:00:00Z"
            ),
            idempotencyKey: "income-key-001"
        )

        XCTAssertEqual(result.income, syntheticIncome())
        XCTAssertFalse(result.replayed)
    }

    @MainActor
    func testMapsHTTPAndNetworkFailuresWithoutRawServerMessages() async {
        for (status, expected) in [
            (400, FinancialAPIError.invalidData),
            (409, FinancialAPIError.conflict),
            (503, FinancialAPIError.serviceUnavailable)
        ] {
            URLProtocolStub.install { request in
                Self.response(
                    request: request,
                    status: status,
                    body: #"{"error":{"code":"INTERNAL_ERROR","message":"SUPER_SECRET_IOS_MARKER"}}"#
                )
            }
            do {
                _ = try await makeClient().preview(syntheticRequest())
                XCTFail("Expected status \(status) to fail")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, expected)
                XCTAssertFalse(String(describing: error).contains("SUPER_SECRET_IOS_MARKER"))
            }
        }

        URLProtocolStub.install { _ in throw URLError(.notConnectedToInternet) }
        do {
            _ = try await makeClient().preview(syntheticRequest())
            XCTFail("Expected network failure")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .connectionUnavailable)
        }
    }

    @MainActor
    func testMapsURLSessionCancellationToCancellationError() async {
        URLProtocolStub.install { _ in throw URLError(.cancelled) }

        do {
            _ = try await makeClient().preview(syntheticRequest())
            XCTFail("Expected cancellation")
        } catch is CancellationError {
            // Expected: cancellation must not become a visible connection failure.
        } catch {
            XCTFail("Expected CancellationError, got \(error)")
        }
    }

    @MainActor
    func testRejectsInvalidJSONAndInvalidTimestamp() async {
        for body in [
            "not-json",
            """
            {"type":"EXPENSE","description":"Mercado sintético","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"invalid","financialTimezone":"America/Sao_Paulo","origin":"IOS"}
            """
        ] {
            URLProtocolStub.install { request in
                Self.response(request: request, status: 200, body: body)
            }
            do {
                _ = try await makeClient().preview(syntheticRequest())
                XCTFail("Expected invalid response")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, .invalidResponse)
            }
        }
    }

    @MainActor
    func testHistoryUsesStrictMonthQuery() async throws {
        URLProtocolStub.install { request in
            XCTAssertEqual(request.url?.query, "month=2026-08")
            return Self.response(request: request, status: 200, body: #"{"month":"2026-08","items":[]}"#)
        }

        let response = try await makeClient().transactions(month: "2026-08")
        XCTAssertEqual(response, TransactionMonth(month: "2026-08", items: []))
    }

    @MainActor
    func testHistoryDecodesExplicitMixedTypesAndInt64Amounts() async throws {
        let maxAmount = Int64.max
        URLProtocolStub.install { request in
            Self.response(
                request: request,
                status: 200,
                body: """
                {"month":"2026-08","items":[\(Self.incomeJSON),{"id":"exp_test_ios_001","type":"EXPENSE","description":"Mercado sintético","amount":{"minor":\(maxAmount),"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"2026-08-14T15:00:00Z","financialTimezone":"America/Sao_Paulo","origin":"IOS","status":"RECORDED","version":1,"createdAt":"2026-08-14T18:00:00Z","updatedAt":"2026-08-14T18:00:00Z"}]}
                """
            )
        }

        let response = try await makeClient().transactions(month: "2026-08")

        XCTAssertEqual(response.items.map(\.type), [.income, .expense])
        XCTAssertEqual(response.items[1].amount.minor, maxAmount)
        guard case let .income(income) = response.items[0] else {
            return XCTFail("Expected Income as the first item")
        }
        XCTAssertEqual(income.id, "inc_test_ios_001")
    }

    @MainActor
    func testHistoryRejectsUnknownTypeAndMalformedIncomeShape() async {
        let invalidBodies = [
            #"{"month":"2026-08","items":[{"type":"TRANSFER"}]}"#,
            #"{"month":"2026-08","items":[{"id":"inc_test_ios_001","type":"INCOME","description":"Receita sintética","amount":{"minor":8500,"currency":"BRL"},"occurredAt":"2026-08-14T16:00:00Z"}]}"#
        ]

        for body in invalidBodies {
            URLProtocolStub.install { request in
                Self.response(request: request, status: 200, body: body)
            }
            do {
                _ = try await makeClient().transactions(month: "2026-08")
                XCTFail("Expected invalid mixed history response")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, .invalidResponse)
            }
        }
    }

    @MainActor
    func testIncomeResponsesRejectPaymentMethodEvenWhenNull() async {
        let invalidIncomePreview = """
        {"type":"INCOME","description":"Receita sintética","amount":{"minor":8500,"currency":"BRL"},"paymentMethod":null,"occurredAt":"2026-08-14T16:00:00Z","financialTimezone":"America/Sao_Paulo","origin":"IOS"}
        """
        URLProtocolStub.install { request in
            Self.response(request: request, status: 200, body: invalidIncomePreview)
        }

        do {
            _ = try await makeClient().preview(
                IncomeRequest(
                    description: "Receita sintética",
                    amount: FinancialMoney(minor: 8_500, currency: .brl),
                    occurredAt: "2026-08-14T16:00:00Z"
                )
            )
            XCTFail("Income response must not contain paymentMethod")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .invalidResponse)
        }
    }

    @MainActor
    private func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [URLProtocolStub.self]
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        let session = URLSession(configuration: configuration)
        return URLSessionFinancialAPIClient(baseURL: URL(string: "http://127.0.0.1:18081")!, session: session)
    }

    private func syntheticRequest() -> ExpenseRequest {
        ExpenseRequest(
            description: "Mercado sintético",
            amount: ExpenseMoney(minor: 4_250, currency: .brl),
            paymentMethod: .pix,
            occurredAt: "2026-08-14T15:00:00Z"
        )
    }

    private static func response(
        request: URLRequest,
        status: Int,
        body: String,
        headers: [String: String] = standardHeaders
    ) -> (HTTPURLResponse, Data) {
        let response = HTTPURLResponse(
            url: request.url!,
            statusCode: status,
            httpVersion: "HTTP/1.1",
            headerFields: headers
        )!
        return (response, Data(body.utf8))
    }

    private static func requestBody(_ request: URLRequest) -> Data? {
        if let body = request.httpBody {
            return body
        }
        guard let stream = request.httpBodyStream else {
            return nil
        }

        stream.open()
        defer { stream.close() }
        var body = Data()
        var buffer = [UInt8](repeating: 0, count: 1_024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            guard count >= 0 else { return nil }
            if count == 0 { break }
            body.append(buffer, count: count)
        }
        return body
    }

    private static let standardHeaders = [
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff"
    ]

    private static let expenseJSON = """
    {"id":"exp_test_ios_001","type":"EXPENSE","description":"Mercado sintético","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"2026-08-14T15:00:00Z","financialTimezone":"America/Sao_Paulo","origin":"IOS","status":"RECORDED","version":1,"createdAt":"2026-08-14T18:00:00Z","updatedAt":"2026-08-14T18:00:00Z"}
    """

    private static let incomePreviewJSON = """
    {"type":"INCOME","description":"Receita sintética","amount":{"minor":8500,"currency":"BRL"},"occurredAt":"2026-08-14T16:00:00Z","financialTimezone":"America/Sao_Paulo","origin":"IOS"}
    """

    private static let incomeJSON = """
    {"id":"inc_test_ios_001","type":"INCOME","description":"Receita sintética","amount":{"minor":8500,"currency":"BRL"},"occurredAt":"2026-08-14T16:00:00Z","financialTimezone":"America/Sao_Paulo","origin":"IOS","status":"RECORDED","version":1,"createdAt":"2026-08-14T18:00:00Z","updatedAt":"2026-08-14T18:00:00Z"}
    """
}

private final class URLProtocolStub: URLProtocol, @unchecked Sendable {
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
