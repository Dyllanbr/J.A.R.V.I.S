import Foundation
import XCTest
@testable import JARVIS

final class RecurrenceModelsAndAPITests: XCTestCase {
    override func tearDown() {
        RecurrenceURLProtocolStub.removeHandler()
        super.tearDown()
    }

    func testCivilDateCanonicalTransportIsIndependentOfTimezone() throws {
        for value in ["2024-02-29", "2026-04-30", "2026-08-31", "2026-12-31", "2027-01-01"] {
            let civilDate = try RecurrenceCivilDate(value)
            let encoded = try JSONEncoder().encode(civilDate)
            XCTAssertEqual(String(decoding: encoded, as: UTF8.self), "\"\(value)\"")
            XCTAssertEqual(try JSONDecoder().decode(RecurrenceCivilDate.self, from: encoded), civilDate)

            for identifier in ["UTC", "America/Sao_Paulo", "Pacific/Kiritimati"] {
                let timeZone = try XCTUnwrap(TimeZone(identifier: identifier))
                var calendar = Calendar(identifier: .gregorian)
                calendar.timeZone = timeZone
                _ = calendar.dateComponents([.year, .month, .day], from: civilDate.pickerDate)
                XCTAssertEqual(civilDate.canonicalValue, value, "timezone \(identifier) changed CivilDate")
                XCTAssertEqual(civilDate.year, Int(value.prefix(4)))
            }
        }
    }

    func testCivilDateRejectsNonCanonicalAndImpossibleValues() {
        for value in [
            "2023-02-29",
            "2026-13-01",
            "2026-01-32",
            "2026-8-1",
            "2026-08-31T00:00:00Z",
            "2026-😀-"
        ] {
            XCTAssertThrowsError(try RecurrenceCivilDate(value), "accepted \(value)")
        }
    }

    func testRequestEncodingUsesInt64BRLExpenseMonthlyAndCivilDate() throws {
        let request = RecurrenceRequest(
            description: "Academia sintética",
            expectedAmount: FinancialMoney(minor: Int64.max, currency: .brl),
            startsOn: try RecurrenceCivilDate("2026-08-31")
        )
        let object = try XCTUnwrap(
            JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any]
        )
        XCTAssertEqual(object["type"] as? String, "EXPENSE")
        XCTAssertEqual(object["frequency"] as? String, "MONTHLY")
        XCTAssertEqual(object["startsOn"] as? String, "2026-08-31")
        let amount = try XCTUnwrap(object["expectedAmount"] as? [String: Any])
        XCTAssertEqual((amount["minor"] as? NSNumber)?.int64Value, Int64.max)
        XCTAssertEqual(amount["currency"] as? String, "BRL")
    }

    func testDecodesActiveAndCancelledWithStrictCancelledAtShape() throws {
        let active = try JSONDecoder().decode(Recurrence.self, from: Data(Self.activeJSON.utf8))
        XCTAssertEqual(active.status, .active)
        XCTAssertNil(active.cancelledAt)
        XCTAssertEqual(active.startsOn.canonicalValue, "2026-08-31")

        let cancelled = try JSONDecoder().decode(Recurrence.self, from: Data(Self.cancelledJSON.utf8))
        XCTAssertEqual(cancelled.status, .cancelled)
        XCTAssertEqual(cancelled.cancelledAt, "2026-08-17T18:00:00Z")

        for invalid in [
            Self.activeJSON.replacingOccurrences(of: "\"createdAt\"", with: "\"cancelledAt\":null,\"createdAt\""),
            Self.cancelledJSON.replacingOccurrences(of: ",\"cancelledAt\":\"2026-08-17T18:00:00Z\"", with: ""),
            Self.activeJSON.replacingOccurrences(of: "\"EXPENSE\"", with: "\"INCOME\""),
            Self.activeJSON.replacingOccurrences(of: "\"MONTHLY\"", with: "\"YEARLY\"")
        ] {
            XCTAssertThrowsError(try JSONDecoder().decode(Recurrence.self, from: Data(invalid.utf8)))
        }
    }

    @MainActor
    func testClientUsesTypedEndpointsAndIdempotencyHeaders() async throws {
        RecurrenceURLProtocolStub.install { request in
            switch (request.httpMethod, request.url?.path) {
            case ("POST", "/v1/recurrences/preview"):
                XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
                return Self.response(request, status: 200, body: Self.previewJSON)
            case ("POST", "/v1/recurrences"):
                XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "create-key")
                return Self.response(
                    request,
                    status: 201,
                    body: Self.activeJSON,
                    headers: Self.standardHeaders.merging(["Idempotency-Replayed": "true"]) { _, new in new }
                )
            case ("GET", "/v1/recurrences"):
                XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
                return Self.response(request, status: 200, body: "{\"items\":[\(Self.activeJSON)]}")
            case ("POST", "/v1/recurrences/rec_test_ios_001/cancel"):
                XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "cancel-key")
                XCTAssertNil(request.httpBody)
                return Self.response(
                    request,
                    status: 200,
                    body: Self.cancelledJSON,
                    headers: Self.standardHeaders.merging(["Idempotency-Replayed": "true"]) { _, new in new }
                )
            default:
                XCTFail("unexpected request \(request.httpMethod ?? "nil") \(request.url?.path ?? "nil")")
                return Self.response(request, status: 500, body: "{}")
            }
        }

        let request = Self.request
        let client = makeClient()
        let preview = try await client.previewRecurrence(request)
        let created = try await client.createRecurrence(request, idempotencyKey: "create-key")
        let listed = try await client.recurrences()
        let cancelled = try await client.cancelRecurrence(
            id: "rec_test_ios_001",
            idempotencyKey: "cancel-key"
        )
        XCTAssertEqual(preview.startsOn, request.startsOn)
        XCTAssertTrue(created.replayed)
        XCTAssertEqual(listed.items.count, 1)
        XCTAssertTrue(cancelled.replayed)
    }

    @MainActor
    func testClientDecodesEmptyListAndMapsRecurrenceErrorsSafely() async throws {
        RecurrenceURLProtocolStub.install { request in
            Self.response(request, status: 200, body: "{\"items\":[]}")
        }
        let empty = try await makeClient().recurrences()
        XCTAssertEqual(empty, RecurrenceList(items: []))

        for (status, code, expected) in [
            (404, "RECURRENCE_NOT_FOUND", FinancialAPIError.notFound),
            (409, "RECURRENCE_ALREADY_CANCELLED", FinancialAPIError.alreadyCancelled),
            (409, "IDEMPOTENCY_KEY_REUSED", FinancialAPIError.conflict),
            (500, "INTERNAL_ERROR", FinancialAPIError.serviceUnavailable)
        ] {
            RecurrenceURLProtocolStub.install { request in
                Self.response(
                    request,
                    status: status,
                    body: "{\"error\":{\"code\":\"\(code)\",\"message\":\"secret raw response\"}}"
                )
            }
            do {
                _ = try await makeClient().cancelRecurrence(id: "rec_test_ios_001", idempotencyKey: "key")
                XCTFail("expected status \(status) to fail")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, expected)
                XCTAssertFalse(String(describing: error).contains("secret"))
            }
        }
    }

    @MainActor
    func testClientRejectsInvalidTimestampAndHistoricalActiveResponseRemainsActive() async throws {
        RecurrenceURLProtocolStub.install { request in
            Self.response(request, status: 201, body: Self.activeJSON)
        }
        let historical = try await makeClient().createRecurrence(Self.request, idempotencyKey: "historical-key")
        XCTAssertEqual(historical.recurrence.status, .active)
        XCTAssertNil(historical.recurrence.cancelledAt)

        RecurrenceURLProtocolStub.install { request in
            Self.response(
                request,
                status: 201,
                body: Self.activeJSON.replacingOccurrences(of: "2026-08-16T18:00:00Z", with: "not-a-time")
            )
        }
        do {
            _ = try await makeClient().createRecurrence(Self.request, idempotencyKey: "invalid-time")
            XCTFail("expected invalid timestamp")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .invalidResponse)
        }
    }

    func testSuggestionDecodingPreservesOnlyDeterministicEvidenceAndRejectsInconsistentPayloads() throws {
        let suggestion = try JSONDecoder().decode(
            RecurrenceSuggestion.self,
            from: Data(Self.suggestionJSON.utf8)
        )
        XCTAssertEqual(suggestion.id, Self.suggestionID)
        XCTAssertEqual(suggestion.anchorDay, 10)
        XCTAssertEqual(suggestion.evidenceCount, 3)
        XCTAssertEqual(suggestion.observedDates.map(\.canonicalValue), [
            "2026-05-10", "2026-06-10", "2026-07-10"
        ])
        XCTAssertEqual(suggestion.proposedStartsOn.canonicalValue, "2026-09-10")

        let invalidPayloads = [
            Self.suggestionJSON.replacingOccurrences(of: Self.suggestionID, with: "rsg_bad"),
            Self.suggestionJSON.replacingOccurrences(of: "\"evidenceCount\":3", with: "\"evidenceCount\":4"),
            Self.suggestionJSON.replacingOccurrences(
                of: "[\"2026-05-10\",\"2026-06-10\",\"2026-07-10\"]",
                with: "[\"2026-06-10\",\"2026-05-10\",\"2026-07-10\"]"
            ),
            Self.suggestionJSON.replacingOccurrences(of: "\"minor\":9990", with: "\"minor\":0"),
            Self.suggestionJSON.replacingOccurrences(of: "\"proposedStartsOn\":\"2026-09-10\"", with: "\"proposedStartsOn\":\"2026-07-10\"")
        ]
        for payload in invalidPayloads {
            XCTAssertThrowsError(
                try JSONDecoder().decode(RecurrenceSuggestion.self, from: Data(payload.utf8)),
                "accepted inconsistent suggestion payload: \(payload)"
            )
        }

        let duplicateList = "{\"items\":[\(Self.suggestionJSON),\(Self.suggestionJSON)]}"
        XCTAssertThrowsError(
            try JSONDecoder().decode(RecurrenceSuggestionList.self, from: Data(duplicateList.utf8))
        )
    }

    @MainActor
    func testSuggestionClientUsesExactEmptyBodyEndpointsAndReplayHeader() async throws {
        RecurrenceURLProtocolStub.install { request in
            XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
            switch (request.httpMethod, request.url?.path) {
            case ("GET", "/v1/recurrence-suggestions"):
                XCTAssertNil(request.httpBody)
                return Self.response(request, status: 200, body: "{\"items\":[\(Self.suggestionJSON)]}")
            case ("POST", "/v1/recurrence-suggestions/\(Self.suggestionID)/preview"):
                XCTAssertNil(request.httpBody)
                return Self.response(request, status: 200, body: Self.previewJSON)
            case ("POST", "/v1/recurrence-suggestions/\(Self.suggestionID)/dismiss"):
                XCTAssertNil(request.httpBody)
                return Self.response(
                    request,
                    status: 204,
                    body: "",
                    headers: Self.standardHeaders.merging(["Idempotency-Replayed": "true"]) { _, new in new }
                )
            default:
                XCTFail("unexpected suggestion request \(request.httpMethod ?? "nil") \(request.url?.path ?? "nil")")
                return Self.response(request, status: 500, body: "{}")
            }
        }

        let client = makeClient()
        let list = try await client.recurrenceSuggestions()
        let preview = try await client.previewRecurrenceSuggestion(id: Self.suggestionID)
        let dismissed = try await client.dismissRecurrenceSuggestion(id: Self.suggestionID)

        XCTAssertEqual(list.items.map(\.id), [Self.suggestionID])
        XCTAssertEqual(preview.description, "Academia sintética")
        XCTAssertTrue(dismissed.replayed)

        RecurrenceURLProtocolStub.install { request in
            Self.response(request, status: 204, body: "")
        }
        let firstDismiss = try await client.dismissRecurrenceSuggestion(id: Self.suggestionID)
        XCTAssertFalse(firstDismiss.replayed)
    }

    @MainActor
    func testSuggestionClientMapsOnlyExactPublicStaleCodesAndRejectsMalformedSuccess() async throws {
        for (status, code, expected) in [
            (400, "INVALID_REQUEST", FinancialAPIError.invalidData),
            (404, "RECURRENCE_SUGGESTION_NOT_FOUND", FinancialAPIError.suggestionNotFound),
            (409, "RECURRENCE_SUGGESTION_SUPPRESSED", FinancialAPIError.suggestionSuppressed),
            (500, "INTERNAL_ERROR", FinancialAPIError.serviceUnavailable)
        ] {
            RecurrenceURLProtocolStub.install { request in
                Self.response(
                    request,
                    status: status,
                    body: "{\"error\":{\"code\":\"\(code)\",\"message\":\"private detail\"}}"
                )
            }
            do {
                _ = try await makeClient().previewRecurrenceSuggestion(id: Self.suggestionID)
                XCTFail("expected suggestion status \(status) to fail")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, expected)
                XCTAssertFalse(String(describing: error).contains("private detail"))
            }
        }

        RecurrenceURLProtocolStub.install { request in
            Self.response(
                request,
                status: 404,
                body: "{\"error\":{\"code\":\"RECURRENCE_NOT_FOUND\",\"message\":\"wrong family\"}}"
            )
        }
        do {
            _ = try await makeClient().previewRecurrenceSuggestion(id: Self.suggestionID)
            XCTFail("expected mismatched error family to fail closed")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .invalidResponse)
        }

        for replayHeader in ["false", "TRUE"] {
            RecurrenceURLProtocolStub.install { request in
                Self.response(
                    request,
                    status: 204,
                    body: "",
                    headers: Self.standardHeaders.merging(["Idempotency-Replayed": replayHeader]) { _, new in new }
                )
            }
            do {
                _ = try await makeClient().dismissRecurrenceSuggestion(id: Self.suggestionID)
                XCTFail("accepted invalid replay header \(replayHeader)")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, .invalidResponse)
            }
        }
    }

    @MainActor
    func testSuggestionClientRejectsInvalidIDBeforeNetworkAndPreservesCancellation() async {
        RecurrenceURLProtocolStub.install { _ in
            XCTFail("Invalid suggestion ID reached the network")
            return Self.response(
                URLRequest(url: URL(string: "http://127.0.0.1")!),
                status: 500,
                body: "{}"
            )
        }
        do {
            _ = try await makeClient().previewRecurrenceSuggestion(id: "rsg_invalid")
            XCTFail("Expected invalid suggestion ID")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .invalidData)
        }

        RecurrenceURLProtocolStub.install { _ in throw URLError(.cancelled) }
        do {
            _ = try await makeClient().recurrenceSuggestions()
            XCTFail("Expected cancellation")
        } catch is CancellationError {
            // Expected: suggestion requests preserve cooperative cancellation.
        } catch {
            XCTFail("Expected CancellationError, got \(error)")
        }
    }

    @MainActor
    private func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [RecurrenceURLProtocolStub.self]
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        return URLSessionFinancialAPIClient(
            baseURL: URL(string: "http://127.0.0.1:18081")!,
            session: URLSession(configuration: configuration)
        )
    }

    private static func response(
        _ request: URLRequest,
        status: Int,
        body: String,
        headers: [String: String] = standardHeaders
    ) -> (HTTPURLResponse, Data) {
        (
            HTTPURLResponse(
                url: request.url!,
                statusCode: status,
                httpVersion: "HTTP/1.1",
                headerFields: headers
            )!,
            Data(body.utf8)
        )
    }

    private static let standardHeaders = [
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff"
    ]

    private static let request = RecurrenceRequest(
        description: "Academia sintética",
        expectedAmount: FinancialMoney(minor: 11_900, currency: .brl),
        startsOn: try! RecurrenceCivilDate("2026-08-31")
    )

    private static let previewJSON = """
    {"type":"EXPENSE","description":"Academia sintética","expectedAmount":{"minor":11900,"currency":"BRL"},"frequency":"MONTHLY","startsOn":"2026-08-31"}
    """

    private static let activeJSON = """
    {"id":"rec_test_ios_001","type":"EXPENSE","description":"Academia sintética","expectedAmount":{"minor":11900,"currency":"BRL"},"frequency":"MONTHLY","startsOn":"2026-08-31","status":"ACTIVE","createdAt":"2026-08-16T18:00:00Z"}
    """

    private static let cancelledJSON = """
    {"id":"rec_test_ios_001","type":"EXPENSE","description":"Academia sintética","expectedAmount":{"minor":11900,"currency":"BRL"},"frequency":"MONTHLY","startsOn":"2026-08-31","status":"CANCELLED","createdAt":"2026-08-16T18:00:00Z","cancelledAt":"2026-08-17T18:00:00Z"}
    """

    private static let suggestionID = "rsg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

    private static let suggestionJSON = """
    {"id":"\(suggestionID)","description":"Internet sintética","expectedAmount":{"minor":9990,"currency":"BRL"},"anchorDay":10,"proposedStartsOn":"2026-09-10","evidenceCount":3,"observedDates":["2026-05-10","2026-06-10","2026-07-10"]}
    """
}

private final class RecurrenceURLProtocolStub: URLProtocol, @unchecked Sendable {
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
