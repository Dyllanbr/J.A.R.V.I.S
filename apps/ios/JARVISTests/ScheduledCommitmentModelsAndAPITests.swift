import Foundation
import XCTest
@testable import JARVIS

final class ScheduledCommitmentModelsAndAPITests: XCTestCase {
    override func tearDown() {
        ScheduledCommitmentURLProtocolStub.removeHandler()
        super.tearDown()
    }

    func testDecodesValidResponseAndPreservesServerOrder() throws {
        let response = try JSONDecoder().decode(
            ScheduledCommitmentListResponse.self,
            from: Data(
                #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":2,"dueOn":"2026-11-15","amount":{"minor":2990,"currency":"BRL"}},{"source":"INSTALLMENT_PLAN","sourceId":"ipl_001","sequence":1,"dueOn":"2026-10-10","amount":{"minor":10000,"currency":"BRL"}}]}"#.utf8
            )
        )

        XCTAssertEqual(response.items.count, 2)
        XCTAssertEqual(response.items[0].source, .recurrence)
        XCTAssertEqual(response.items[0].sourceID, "rec_001")
        XCTAssertEqual(response.items[0].dueOn.canonicalValue, "2026-11-15")
        XCTAssertEqual(response.items[1].source, .installmentPlan)
    }

    func testRejectsUnknownKeysMissingFieldsAndDuplicateLines() throws {
        let payloads = [
            #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":1,"dueOn":"2026-10-10","amount":{"minor":2990,"currency":"BRL"},"owner":"spoofed"}]}"#,
            #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":1,"dueOn":"2026-10-10"}]}"#,
            #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":1,"dueOn":"2026-10-10","amount":{"minor":2990,"currency":"BRL"}},{"source":"RECURRENCE","sourceId":"rec_001","sequence":1,"dueOn":"2026-11-10","amount":{"minor":2990,"currency":"BRL"}}]}"#
        ]

        for payload in payloads {
            XCTAssertThrowsError(
                try JSONDecoder().decode(ScheduledCommitmentListResponse.self, from: Data(payload.utf8)),
                "Payload should fail closed: \(payload)"
            )
        }
    }

    func testRejectsInvalidSourceIdentitySequenceDateAndMoney() throws {
        let payloads = [
            #"{"items":[{"source":"OTHER","sourceId":"rec_001","sequence":1,"dueOn":"2026-10-10","amount":{"minor":2990,"currency":"BRL"}}]}"#,
            #"{"items":[{"source":"RECURRENCE","sourceId":"","sequence":1,"dueOn":"2026-10-10","amount":{"minor":2990,"currency":"BRL"}}]}"#,
            #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":0,"dueOn":"2026-10-10","amount":{"minor":2990,"currency":"BRL"}}]}"#,
            #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":1,"dueOn":"2026-02-30","amount":{"minor":2990,"currency":"BRL"}}]}"#,
            #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":1,"dueOn":"2026-10-10","amount":{"minor":0,"currency":"BRL"}}]}"#,
            #"{"items":[{"source":"RECURRENCE","sourceId":"rec_001","sequence":1,"dueOn":"2026-10-10","amount":{"minor":2990,"currency":"USD"}}]}"#
        ]

        for payload in payloads {
            XCTAssertThrowsError(
                try JSONDecoder().decode(ScheduledCommitmentListResponse.self, from: Data(payload.utf8)),
                "Invalid payload should fail closed: \(payload)"
            )
        }
    }

    @MainActor
    func testClientUsesExactGETQueryHeadersAndNoBodyOrOwner() async throws {
        ScheduledCommitmentURLProtocolStub.install { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/v1/scheduled-commitments")
            XCTAssertEqual(request.url?.query, "evaluationDate=2026-10-09")
            XCTAssertNil(request.httpBody)
            XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/json")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Cache-Control"), "no-store")
            XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
            XCTAssertNil(request.value(forHTTPHeaderField: "X-Owner"))
            return Self.response(request: request, status: 200, body: Self.validJSON)
        }

        let date = try RecurrenceCivilDate("2026-10-09")
        let response = try await makeClient().scheduledCommitments(evaluationDate: date)
        XCTAssertEqual(response.items.count, 1)
    }

    @MainActor
    func testClientMapsContractErrorsAndMalformedSuccessSafely() async throws {
        let cases: [(Int, String, FinancialAPIError)] = [
            (400, #"{"error":{"code":"INVALID_REQUEST","message":"private"}}"#, .invalidData),
            (405, #"{"error":{"code":"METHOD_NOT_ALLOWED","message":"private"}}"#, .invalidResponse),
            (500, #"{"error":{"code":"INTERNAL_ERROR","message":"SQL secret"}}"#, .serviceUnavailable),
            (200, "not-json", .invalidResponse)
        ]

        for (status, body, expected) in cases {
            ScheduledCommitmentURLProtocolStub.install { request in
                Self.response(request: request, status: status, body: body)
            }
            do {
                _ = try await makeClient().scheduledCommitments(evaluationDate: try RecurrenceCivilDate("2026-10-09"))
                XCTFail("Expected \(expected)")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, expected)
            }
        }
    }

    @MainActor
    func testClientPreservesCancellation() async throws {
        ScheduledCommitmentURLProtocolStub.install { _ in throw URLError(.cancelled) }
        do {
            _ = try await makeClient().scheduledCommitments(evaluationDate: try RecurrenceCivilDate("2026-10-09"))
            XCTFail("Expected cancellation")
        } catch is CancellationError {
            // Expected.
        } catch {
            XCTFail("Unexpected error: \(error)")
        }
    }

    @MainActor
    private func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ScheduledCommitmentURLProtocolStub.self]
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        return URLSessionFinancialAPIClient(
            baseURL: URL(string: "http://127.0.0.1:18081")!,
            session: URLSession(configuration: configuration)
        )
    }

    private static func response(request: URLRequest, status: Int, body: String) -> (HTTPURLResponse, Data) {
        (
            HTTPURLResponse(
                url: request.url!,
                statusCode: status,
                httpVersion: "HTTP/1.1",
                headerFields: [
                    "Content-Type": "application/json",
                    "Cache-Control": "no-store",
                    "X-Content-Type-Options": "nosniff"
                ]
            )!,
            Data(body.utf8)
        )
    }

    private static let validJSON = #"{"items":[{"source":"INSTALLMENT_PLAN","sourceId":"ipl_001","sequence":1,"dueOn":"2026-10-10","amount":{"minor":10000,"currency":"BRL"}}]}"#
}

private final class ScheduledCommitmentURLProtocolStub: URLProtocol, @unchecked Sendable {
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
        guard let handler = Self.storage.lock.withLock({ Self.storage.handler }) else {
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
