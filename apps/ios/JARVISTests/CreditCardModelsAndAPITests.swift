import Foundation
import XCTest
@testable import JARVIS

@MainActor
final class CreditCardModelsAndAPITests: XCTestCase {
    override func tearDown() {
        CreditCardURLProtocolStub.removeHandler()
        super.tearDown()
    }

    func testPreviewEncodesOnlyApprovedFieldsAndDecodesCanonicalResponse() async throws {
        CreditCardURLProtocolStub.install { request in
            XCTAssertEqual(request.url?.path, "/v1/cards/preview")
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
            let object = try XCTUnwrap(
                try JSONSerialization.jsonObject(with: try XCTUnwrap(Self.requestBody(request))) as? [String: Any]
            )
            XCTAssertEqual(Set(object.keys), ["name", "lastFour", "brand", "closingDay", "dueDay", "creditLimit"])
            XCTAssertNil(object["owner"])
            return Self.response(
                request,
                status: 200,
                body: #"{"name":"Cartão canônico","lastFour":"4821","brand":"VISA","closingDay":5,"dueDay":12,"creditLimit":{"minor":250000,"currency":"BRL"}}"#
            )
        }
        let preview = try await client().previewCreditCard(fullRequest())
        XCTAssertEqual(preview, syntheticCreditCardPreview(name: "Cartão canônico"))
    }

    func testOptionalRequestFieldMatrixPreservesAbsence() throws {
        let cases: [(CreditCardRequest, Set<String>)] = [
            (
                CreditCardRequest(
                    name: "Nenhum", lastFour: nil, brand: nil, closingDay: 1, dueDay: 10, creditLimit: nil
                ),
                []
            ),
            (
                CreditCardRequest(
                    name: "Final", lastFour: "4821", brand: nil, closingDay: 1, dueDay: 10, creditLimit: nil
                ),
                ["lastFour"]
            ),
            (
                CreditCardRequest(
                    name: "Bandeira", lastFour: nil, brand: .visa, closingDay: 1, dueDay: 10, creditLimit: nil
                ),
                ["brand"]
            ),
            (
                CreditCardRequest(
                    name: "Limite",
                    lastFour: nil,
                    brand: nil,
                    closingDay: 1,
                    dueDay: 10,
                    creditLimit: FinancialMoney(minor: 250_000, currency: .brl)
                ),
                ["creditLimit"]
            ),
            (fullRequest(), ["lastFour", "brand", "creditLimit"])
        ]

        for (request, expectedOptionalKeys) in cases {
            let object = try XCTUnwrap(
                try JSONSerialization.jsonObject(with: JSONEncoder().encode(request)) as? [String: Any]
            )
            let optionalKeys = Set(object.keys).intersection(["lastFour", "brand", "creditLimit"])
            XCTAssertEqual(optionalKeys, expectedOptionalKeys)
        }
    }

    func testCreateAndArchivePreserveIdempotencyAndReplayHeaders() async throws {
        var step = 0
        CreditCardURLProtocolStub.install { request in
            step += 1
            XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), step == 1 ? "create-key" : "archive-key")
            if step == 1 {
                XCTAssertEqual(request.url?.path, "/v1/cards")
                return Self.response(request, status: 201, body: Self.activeJSON, headers: ["Idempotency-Replayed": "true"])
            }
            XCTAssertEqual(request.url?.path, "/v1/cards/card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/archive")
            return Self.response(request, status: 200, body: Self.archivedJSON, headers: ["Idempotency-Replayed": "true"])
        }
        let api = client()
        let created = try await api.createCreditCard(fullRequest(), idempotencyKey: "create-key")
        let archived = try await api.archiveCreditCard(id: created.card.id, idempotencyKey: "archive-key")
        XCTAssertTrue(created.replayed)
        XCTAssertTrue(archived.replayed)
        XCTAssertEqual(archived.card.status, .archived)
        XCTAssertNotNil(archived.card.archivedAt)
    }

    func testListDetailAndOptionalAbsence() async throws {
        var step = 0
        CreditCardURLProtocolStub.install { request in
            step += 1
            if step == 1 {
                return Self.response(request, status: 200, body: #"{"items":[]}"#)
            }
            return Self.response(
                request,
                status: 200,
                body: #"{"id":"card_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","name":"Sem opcionais","closingDay":1,"dueDay":10,"status":"ACTIVE","createdAt":"2026-08-26T12:00:00Z"}"#
            )
        }
        let api = client()
        let list = try await api.creditCards()
        XCTAssertEqual(list.items, [])
        let card = try await api.creditCard(id: "card_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
        XCTAssertNil(card.lastFour)
        XCTAssertNil(card.brand)
        XCTAssertNil(card.creditLimit)
        XCTAssertNil(card.archivedAt)
    }

    func testCardSpecificErrorsAndInvalidReplayHeaderFailClosed() async throws {
        let cases: [(Int, String, FinancialAPIError)] = [
            (404, "CREDIT_CARD_NOT_FOUND", .creditCardNotFound),
            (409, "CREDIT_CARD_ALREADY_ARCHIVED", .creditCardAlreadyArchived),
            (409, "IDEMPOTENCY_KEY_REUSED", .conflict),
            (500, "INTERNAL_ERROR", .serviceUnavailable)
        ]
        for (status, code, expected) in cases {
            CreditCardURLProtocolStub.install { request in
                Self.response(request, status: status, body: #"{"error":{"code":"\#(code)","message":"safe"}}"#)
            }
            do {
                _ = try await client().creditCard(id: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
                XCTFail("Expected error")
            } catch let error as FinancialAPIError {
                XCTAssertEqual(error, expected)
            }
        }

        CreditCardURLProtocolStub.install { request in
            Self.response(request, status: 201, body: Self.activeJSON, headers: ["Idempotency-Replayed": "false"])
        }
        do {
            _ = try await client().createCreditCard(fullRequest(), idempotencyKey: "key")
            XCTFail("Expected fail-closed replay header")
        } catch let error as FinancialAPIError {
            XCTAssertEqual(error, .invalidResponse)
        }
    }

    func testModelsRejectInvalidIDLastFourMoneyAndLifecycle() throws {
        let invalidBodies = [
            #"{"id":"bad","name":"Card","closingDay":1,"dueDay":2,"status":"ACTIVE","createdAt":"2026-08-26T12:00:00Z"}"#,
            #"{"id":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Card","lastFour":"１２３４","closingDay":1,"dueDay":2,"status":"ACTIVE","createdAt":"2026-08-26T12:00:00Z"}"#,
            #"{"id":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Card","closingDay":0,"dueDay":2,"status":"ACTIVE","createdAt":"2026-08-26T12:00:00Z"}"#,
            #"{"id":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Card","closingDay":1,"dueDay":2,"creditLimit":{"minor":0,"currency":"BRL"},"status":"ACTIVE","createdAt":"2026-08-26T12:00:00Z"}"#,
            #"{"id":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Card","closingDay":1,"dueDay":2,"status":"ACTIVE","createdAt":"2026-08-26T12:00:00Z","archivedAt":"2026-08-27T12:00:00Z"}"#,
            #"{"id":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Card","closingDay":1,"dueDay":2,"status":"ARCHIVED","createdAt":"2026-08-26T12:00:00Z"}"#
        ]
        for body in invalidBodies {
            XCTAssertThrowsError(try JSONDecoder().decode(CreditCard.self, from: Data(body.utf8)))
        }
    }

    private func client() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CreditCardURLProtocolStub.self]
        return URLSessionFinancialAPIClient(
            baseURL: URL(string: "https://example.test")!,
            session: URLSession(configuration: configuration)
        )
    }

    private func fullRequest() -> CreditCardRequest {
        CreditCardRequest(
            name: " Cartão ",
            lastFour: "4821",
            brand: .visa,
            closingDay: 5,
            dueDay: 12,
            creditLimit: FinancialMoney(minor: 250_000, currency: .brl)
        )
    }

    private static let activeJSON = #"{"id":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Cartão sintético","lastFour":"4821","brand":"VISA","closingDay":5,"dueDay":12,"creditLimit":{"minor":250000,"currency":"BRL"},"status":"ACTIVE","createdAt":"2026-08-26T12:00:00Z"}"#
    private static let archivedJSON = #"{"id":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","name":"Cartão sintético","lastFour":"4821","brand":"VISA","closingDay":5,"dueDay":12,"creditLimit":{"minor":250000,"currency":"BRL"},"status":"ARCHIVED","createdAt":"2026-08-26T12:00:00Z","archivedAt":"2026-08-27T12:00:00Z"}"#

    private static func response(
        _ request: URLRequest,
        status: Int,
        body: String,
        headers: [String: String] = [:]
    ) -> (HTTPURLResponse, Data) {
        (HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!, Data(body.utf8))
    }

    private static func requestBody(_ request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var result = Data()
        var buffer = [UInt8](repeating: 0, count: 1_024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            guard count >= 0 else { return nil }
            if count == 0 { break }
            result.append(buffer, count: count)
        }
        return result
    }
}

private final class CreditCardURLProtocolStub: URLProtocol, @unchecked Sendable {
    private static let lock = NSLock()
    private nonisolated(unsafe) static var handler: ((URLRequest) throws -> (HTTPURLResponse, Data))?

    static func install(_ handler: @escaping (URLRequest) throws -> (HTTPURLResponse, Data)) {
        lock.lock(); defer { lock.unlock() }
        self.handler = handler
    }

    static func removeHandler() {
        lock.lock(); defer { lock.unlock() }
        handler = nil
    }

    override class func canInit(with _: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        Self.lock.lock(); let handler = Self.handler; Self.lock.unlock()
        guard let handler else { preconditionFailure("Missing URLProtocol handler") }
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
