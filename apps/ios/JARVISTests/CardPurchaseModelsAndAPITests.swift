import Foundation
import XCTest
@testable import JARVIS

@MainActor
final class CardPurchaseModelsAndAPITests: XCTestCase {
    override func tearDown() {
        CardPurchaseURLProtocolStub.removeHandler()
        super.tearDown()
    }

    func testModelsDecodeStrictlyAndEnforceCardPurchaseInvariants() throws {
        let purchase = try JSONDecoder().decode(CardPurchase.self, from: Data(Self.purchaseJSON.utf8))
        XCTAssertEqual(purchase.purchaseMode, .installment)
        XCTAssertEqual(purchase.installmentPlan?.schedule.count, 2)

        var unknown = Self.purchaseJSON
        unknown.removeLast()
        unknown += #",\"unexpected\":true}"#
        XCTAssertThrowsError(try JSONDecoder().decode(CardPurchase.self, from: Data(unknown.utf8)))

        var missing = Self.purchaseJSON
        missing = missing.replacingOccurrences(of: "\"purchaseMode\":\"INSTALLMENT\"", with: "\"purchaseMode\":\"UNKNOWN\"")
        XCTAssertThrowsError(try JSONDecoder().decode(CardPurchase.self, from: Data(missing.utf8)))

        let oneTime = CardPurchasePreviewRequest(
            description: "Compra",
            amount: FinancialMoney(minor: 100, currency: .brl),
            occurredAt: "2026-08-14T15:00:00Z",
            creditCardID: Self.cardID
        )
        let encoded = try JSONEncoder().encode(oneTime)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
        XCTAssertEqual(Set(object.keys), ["description", "amount", "occurredAt", "creditCardId"])
    }

    func testPreviewUsesCardPurchaseEndpointAndOmitsOwner() async throws {
        CardPurchaseURLProtocolStub.install { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/v1/card-purchases/preview")
            XCTAssertNil(URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.query)
            let body = try XCTUnwrap(cardPurchaseRequestBody(request))
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
            XCTAssertEqual(Set(object.keys), ["description", "amount", "occurredAt", "creditCardId", "installmentCount"])
            XCTAssertNil(object["owner"])
            return Self.response(request, status: 200, body: Self.previewJSON)
        }

        let request = CardPurchasePreviewRequest(
            description: "Compra",
            amount: FinancialMoney(minor: 12_000, currency: .brl),
            occurredAt: "2026-08-14T15:00:00Z",
            creditCardID: Self.cardID,
            installmentCount: 2
        )
        let result = try await makeClient().previewCardPurchase(request)
        XCTAssertEqual(result.purchaseMode, .installment)
        XCTAssertEqual(result.installmentSummary?.installmentCount, 2)
    }

    func testCreateSendsIdempotencyKeyAndMapsReplay() async throws {
        CardPurchaseURLProtocolStub.install { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/v1/card-purchases")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "card-purchase-key")
            var headers = Self.headers
            headers["Idempotency-Replayed"] = "true"
            return Self.response(request, status: 201, body: Self.purchaseJSON, headers: headers)
        }

        let request = CardPurchaseCreateRequest(
            description: "Compra",
            amount: FinancialMoney(minor: 12_000, currency: .brl),
            occurredAt: "2026-08-14T15:00:00Z",
            creditCardID: Self.cardID,
            installmentCount: 2
        )
        let result = try await makeClient().createCardPurchase(request, idempotencyKey: "card-purchase-key")
        XCTAssertTrue(result.replayed)
        XCTAssertEqual(result.purchase.expense.id, "exp_card_purchase_001")
    }

    func testPlanEndpointsAndCancellationBody() async throws {
        let paths = PathStorage()
        CardPurchaseURLProtocolStub.install { request in
            paths.append(request.url!.path)
            if request.url!.path.hasSuffix("/cancel") {
                let body = try XCTUnwrap(cardPurchaseRequestBody(request))
                let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
                XCTAssertEqual(object["expectedCancelledOn"] as? String, "2026-08-20")
                XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "cancel-key")
                return Self.response(request, status: 200, body: Self.cancelledPlanJSON)
            }
            if request.url!.path.hasSuffix("cancellation-preview") {
                return Self.response(request, status: 200, body: Self.cancellationPreviewJSON)
            }
            if request.url!.path == "/v1/installment-plans" {
                return Self.response(request, status: 200, body: "{\"items\":[\(Self.planJSON)]}")
            }
            return Self.response(request, status: 200, body: Self.planJSON)
        }

        let client = makeClient()
        let id = Self.planID
        _ = try await client.installmentPlans()
        _ = try await client.installmentPlan(id: id)
        _ = try await client.previewInstallmentPlanCancellation(id: id)
        let result = try await client.cancelInstallmentPlan(id: id, expectedCancelledOn: try RecurrenceCivilDate("2026-08-20"), idempotencyKey: "cancel-key")

        XCTAssertEqual(paths.values, [
            "/v1/installment-plans",
            "/v1/installment-plans/\(id)",
            "/v1/installment-plans/\(id)/cancellation-preview",
            "/v1/installment-plans/\(id)/cancel"
        ])
        XCTAssertEqual(result.plan.status, .cancelled)
    }

    func testErrorMappingAndInvalidInputsFailBeforeTransport() async {
        CardPurchaseURLProtocolStub.install { _ in
            XCTFail("Invalid input must not reach URLSession")
            return Self.response(URLRequest(url: URL(string: "https://api.example.test")!), status: 200, body: Self.previewJSON)
        }
        let client = makeClient()
        let invalid = CardPurchaseCreateRequest(description: "", amount: FinancialMoney(minor: 1, currency: .brl), occurredAt: "bad", creditCardID: "bad")
        do {
            _ = try await client.createCardPurchase(invalid, idempotencyKey: "key")
            XCTFail("Expected invalid data")
        } catch let error as FinancialAPIError {
            XCTAssertEqual(error, .invalidData)
        } catch {
            XCTFail("Unexpected error: \(error)")
        }
        do {
            _ = try await client.cancelInstallmentPlan(id: Self.planID, expectedCancelledOn: try! RecurrenceCivilDate("2026-08-20"), idempotencyKey: "")
            XCTFail("Expected missing key")
        } catch let error as FinancialAPIError {
            XCTAssertEqual(error, .idempotencyKeyRequired)
        } catch {
            XCTFail("Unexpected error: \(error)")
        }
        do {
            _ = try await client.createCardPurchase(
                CardPurchaseCreateRequest(description: "Compra", amount: FinancialMoney(minor: 1, currency: .brl), occurredAt: "2026-08-14T15:00:00Z", creditCardID: Self.cardID),
                idempotencyKey: String(repeating: "x", count: 129)
            )
            XCTFail("Expected invalid key")
        } catch let error as FinancialAPIError {
            XCTAssertEqual(error, .idempotencyKeyInvalid)
        } catch {
            XCTFail("Unexpected error: \(error)")
        }
    }

    nonisolated private static let cardID = "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    nonisolated private static let planID = "ipl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    nonisolated private static let headers = ["Content-Type": "application/json", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"]

    nonisolated private static let previewJSON = """
    {"description":"Compra","amount":{"minor":12000,"currency":"BRL"},"occurredAt":"2026-08-14T15:00:00Z","creditCardId":"\(cardID)","purchaseMode":"INSTALLMENT","statementClosingOn":"2026-08-05","statementDueOn":"2026-08-12","installmentSummary":{"installmentCount":2,"firstDueDate":"2026-08-12","lastDueDate":"2026-09-12","dueDayAnchor":12,"regularInstallmentAmount":{"minor":6000,"currency":"BRL"},"lastInstallmentAmount":{"minor":6000,"currency":"BRL"}}}
    """

    nonisolated private static let expenseJSON = """
    {"id":"exp_card_purchase_001","type":"EXPENSE","description":"Compra","amount":{"minor":12000,"currency":"BRL"},"paymentMethod":"CREDIT","creditCardId":"\(cardID)","statementDueOn":"2026-08-12","occurredAt":"2026-08-14T15:00:00Z","financialTimezone":"America/Sao_Paulo","origin":"IOS","status":"RECORDED","version":1,"createdAt":"2026-08-14T15:00:00Z","updatedAt":"2026-08-14T15:00:00Z"}
    """

    nonisolated private static let planJSON = """
    {"id":"\(planID)","creditCardId":"\(cardID)","expenseId":"exp_card_purchase_001","totalAmount":{"minor":12000,"currency":"BRL"},"installmentCount":2,"firstDueDate":"2026-08-12","dueDayAnchor":12,"status":"ACTIVE","createdAt":"2026-08-14T15:00:00Z","schedule":[{"number":1,"totalCount":2,"dueDate":"2026-08-12","amount":{"minor":6000,"currency":"BRL"}},{"number":2,"totalCount":2,"dueDate":"2026-09-12","amount":{"minor":6000,"currency":"BRL"}}]}
    """

    nonisolated private static let purchaseJSON = """
    {"expense":\(expenseJSON),"installmentPlan":\(planJSON),"purchaseMode":"INSTALLMENT"}
    """

    nonisolated private static let cancellationPreviewJSON = """
    {"installmentPlanId":"\(planID)","expectedCancelledOn":"2026-08-20","plan":\(planJSON)}
    """

    nonisolated private static let cancelledPlanJSON = planJSON.replacingOccurrences(of: "\"status\":\"ACTIVE\"", with: "\"status\":\"CANCELLED\",\"cancelledOn\":\"2026-08-20\"")

    nonisolated private static func response(_ request: URLRequest, status: Int, body: String, headers: [String: String]? = nil) -> (HTTPURLResponse, Data) {
        let headers = headers ?? Self.headers
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!
        return (response, Data(body.utf8))
    }

    private func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [CardPurchaseURLProtocolStub.self]
        configuration.urlCache = nil
        return URLSessionFinancialAPIClient(baseURL: URL(string: "https://api.example.test")!, session: URLSession(configuration: configuration))
    }

}

private func cardPurchaseRequestBody(_ request: URLRequest) -> Data? {
    if let body = request.httpBody { return body }
    guard let stream = request.httpBodyStream else { return nil }
    stream.open()
    defer { stream.close() }
    var data = Data()
    var buffer = [UInt8](repeating: 0, count: 1024)
    while stream.hasBytesAvailable {
        let count = stream.read(&buffer, maxLength: buffer.count)
        if count <= 0 { break }
        data.append(buffer, count: count)
    }
    return data
}

private final class PathStorage: @unchecked Sendable {
    private let lock = NSLock()
    private var stored: [String] = []
    var values: [String] { lock.withLock { stored } }
    func append(_ value: String) { lock.withLock { stored.append(value) } }
}

private final class CardPurchaseURLProtocolStub: URLProtocol, @unchecked Sendable {
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
        guard let handler else { client?.urlProtocol(self, didFailWithError: URLError(.unknown)); return }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }

    override func stopLoading() {}
}
