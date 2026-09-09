import Foundation
import XCTest
@testable import JARVIS

final class PurchaseSimulationModelsAndAPITests: XCTestCase {
    override func tearDown() {
        PurchaseSimulationURLProtocol.removeHandler()
        super.tearDown()
    }

    func testRequestRejectsInvalidModeAmountAndPeriod() throws {
        let date = try RecurrenceCivilDate("2026-09-10")
        let start = try RecurrenceCivilDate("2026-09-01")
        let end = try RecurrenceCivilDate("2026-09-30")
        XCTAssertNoThrow(try PurchaseSimulationRequest(
            creditCardID: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", amount: FinancialMoney(minor: 100, currency: .brl),
            purchaseOn: date, purchaseMode: .oneTime, periodStart: start, periodEnd: end
        ))
        XCTAssertThrowsError(try PurchaseSimulationRequest(
            creditCardID: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", amount: FinancialMoney(minor: 0, currency: .brl),
            purchaseOn: date, purchaseMode: .oneTime, periodStart: start, periodEnd: end
        ))
        XCTAssertThrowsError(try PurchaseSimulationRequest(
            creditCardID: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", amount: FinancialMoney(minor: 100, currency: .brl),
            purchaseOn: date, purchaseMode: .installment, installmentCount: 1,
            periodStart: start, periodEnd: end
        ))
        XCTAssertThrowsError(try PurchaseSimulationRequest(
            creditCardID: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", amount: FinancialMoney(minor: 100, currency: .brl),
            purchaseOn: date, purchaseMode: .oneTime, periodStart: end, periodEnd: start
        ))
    }

    func testResponseDecodesSignedImpactAndRejectsUnknownOrInconsistentData() throws {
        let response = try JSONDecoder().decode(
            PurchaseSimulationResponse.self,
            from: Data(Self.responseJSON.utf8)
        )
        XCTAssertEqual(response.creditCardID, "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
        XCTAssertEqual(response.impact.minor, -1_200)
        XCTAssertEqual(response.projected.finalAmount.minor, 8_800)
        XCTAssertEqual(response.hypotheticalCommitments.count, 1)
        XCTAssertEqual(response.assumptions, [.notPersisted, .noExpenseCreated])

        let unknown = Self.responseJSON.replacingOccurrences(
            of: "\"assumptions\"", with: "\"private\":true,\"assumptions\""
        )
        XCTAssertThrowsError(try JSONDecoder().decode(
            PurchaseSimulationResponse.self, from: Data(unknown.utf8)
        ))

        let mismatch = Self.responseJSON.replacingOccurrences(of: "\"minor\":8800", with: "\"minor\":8801")
        XCTAssertThrowsError(try JSONDecoder().decode(
            PurchaseSimulationResponse.self, from: Data(mismatch.utf8)
        ))
    }

    @MainActor
    func testAPIUsesPostBodyAndDoesNotSendOwnerOrIdempotencyKey() async throws {
        PurchaseSimulationURLProtocol.install { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/v1/purchase-simulations")
            XCTAssertNil(request.url?.query)
            XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/json")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Cache-Control"), "no-store")
            XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
            let body = try XCTUnwrap(Self.bodyData(request))
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
            XCTAssertEqual(Set(object.keys), ["creditCardId", "amount", "purchaseOn", "purchaseMode", "periodStart", "periodEnd"])
            XCTAssertNil(object["owner"])
            XCTAssertEqual(object["creditCardId"] as? String, "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
            XCTAssertEqual((object["amount"] as? [String: Any])?["minor"] as? Int, 1_200)
            return Self.response(request: request, status: 200, body: Self.responseJSON)
        }

        let start = try RecurrenceCivilDate("2026-09-01")
        let end = try RecurrenceCivilDate("2026-09-30")
        let request = try PurchaseSimulationRequest(
            creditCardID: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", amount: FinancialMoney(minor: 1_200, currency: .brl),
            purchaseOn: try RecurrenceCivilDate("2026-09-10"), purchaseMode: .oneTime,
            periodStart: start, periodEnd: end
        )
        let result = try await Self.makeClient().simulatePurchase(request)
        XCTAssertEqual(result.projected.finalAmount.minor, 8_800)
    }

    @MainActor
    func testAPIMapsInvalidCardAndServerErrors() async throws {
        let start = try RecurrenceCivilDate("2026-09-01")
        let end = try RecurrenceCivilDate("2026-09-30")
        let request = try PurchaseSimulationRequest(
            creditCardID: "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", amount: FinancialMoney(minor: 1_200, currency: .brl),
            purchaseOn: try RecurrenceCivilDate("2026-09-10"), purchaseMode: .oneTime,
            periodStart: start, periodEnd: end
        )
        for (status, code, expected) in [
            (404, "CREDIT_CARD_NOT_FOUND", FinancialAPIError.creditCardNotFound),
            (500, "INTERNAL_ERROR", FinancialAPIError.serviceUnavailable),
            (400, "INVALID_REQUEST", FinancialAPIError.invalidData)
        ] {
            PurchaseSimulationURLProtocol.install { request in
                Self.response(request: request, status: status, body: "{\"error\":{\"code\":\"\(code)\",\"message\":\"private\"}}")
            }
            do {
                _ = try await Self.makeClient().simulatePurchase(request)
                XCTFail("Expected HTTP error")
            } catch {
                XCTAssertEqual(error as? FinancialAPIError, expected)
            }
        }
    }

    @MainActor
    private static func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [PurchaseSimulationURLProtocol.self]
        configuration.urlCache = nil
        return URLSessionFinancialAPIClient(
            baseURL: URL(string: "http://127.0.0.1:18081")!,
            session: URLSession(configuration: configuration)
        )
    }

    private static func response(request: URLRequest, status: Int, body: String) -> (HTTPURLResponse, Data) {
        (
            HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: [
                "Content-Type": "application/json", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"
            ])!,
            Data(body.utf8)
        )
    }

    private static func bodyData(_ request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var body = Data()
        var buffer = [UInt8](repeating: 0, count: 1_024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            body.append(buffer, count: count)
        }
        return body
    }

    private static let responseJSON = #"""
    {
      "creditCardId":"card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","purchaseOn":"2026-09-10","purchaseMode":"ONE_TIME",
      "periodStart":"2026-09-01","periodEnd":"2026-09-30",
      "baseline":{"periodStart":"2026-09-01","periodEnd":"2026-09-30","availableBalance":{"minor":10000,"currency":"BRL"},"totalConfirmedIncome":{"minor":0,"currency":"BRL"},"totalConfirmedExpense":{"minor":0,"currency":"BRL"},"totalConfirmedCommitments":{"minor":0,"currency":"BRL"},"finalAmount":{"minor":10000,"currency":"BRL"},"breakdown":[{"kind":"AVAILABLE_BALANCE","sourceId":"available-balance","sequence":0,"dueOn":"2026-09-01","amount":{"minor":10000,"currency":"BRL"}}],"missingData":["BUDGET"]},
      "projected":{"periodStart":"2026-09-01","periodEnd":"2026-09-30","availableBalance":{"minor":10000,"currency":"BRL"},"totalConfirmedIncome":{"minor":0,"currency":"BRL"},"totalConfirmedExpense":{"minor":0,"currency":"BRL"},"totalConfirmedCommitments":{"minor":1200,"currency":"BRL"},"finalAmount":{"minor":8800,"currency":"BRL"},"breakdown":[{"kind":"AVAILABLE_BALANCE","sourceId":"available-balance","sequence":0,"dueOn":"2026-09-01","amount":{"minor":10000,"currency":"BRL"}},{"kind":"COMMITMENT","sourceId":"sim_ui_synthetic","sequence":1,"dueOn":"2026-09-01","amount":{"minor":1200,"currency":"BRL"}}],"missingData":["BUDGET"]},
      "impact":{"minor":-1200,"currency":"BRL"},"hypotheticalCommitments":[{"kind":"COMMITMENT","sourceId":"sim_ui_synthetic","sequence":1,"dueOn":"2026-09-01","amount":{"minor":1200,"currency":"BRL"}}],"assumptions":["NOT_PERSISTED","NO_EXPENSE_CREATED"]
    }
    """#
}

private final class PurchaseSimulationURLProtocol: URLProtocol, @unchecked Sendable {
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
