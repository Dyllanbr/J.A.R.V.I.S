import Foundation
import XCTest
@testable import JARVIS

final class FinancialGoalModelsAndAPITests: XCTestCase {
    func testModelsDecodeStrictlyAndKeepZeroOnlyForProtectedValues() throws {
        let data = Data(#"{"goals":[{"id":"goal_1","title":"Viagem","targetAmount":{"minor":25000,"currency":"BRL"}}],"protectedValues":[{"id":"value_1","label":"Reserva","amount":{"minor":0,"currency":"BRL"}}]}"#.utf8)
        let response = try JSONDecoder().decode(FinancialGoalsResponse.self, from: data)
        XCTAssertEqual(response.goals[0].targetAmount.minor, 25_000)
        XCTAssertEqual(response.protectedValues[0].amount.minor, 0)
    }

    func testModelsRejectUnknownKeysInvalidAmountsAndDuplicateIDs() throws {
        let decoder = JSONDecoder()
        let unknown = Data(#"{"goals":[],"protectedValues":[],"unexpected":true}"#.utf8)
        XCTAssertThrowsError(try decoder.decode(FinancialGoalsResponse.self, from: unknown))

        let zeroGoal = Data(#"{"id":"goal_1","title":"Viagem","targetAmount":{"minor":0,"currency":"BRL"}}"#.utf8)
        XCTAssertThrowsError(try decoder.decode(FinancialGoal.self, from: zeroGoal))

        let negativeValue = Data(#"{"id":"value_1","label":"Reserva","amount":{"minor":-1,"currency":"BRL"}}"#.utf8)
        XCTAssertThrowsError(try decoder.decode(ProtectedValue.self, from: negativeValue))

        let goal = try FinancialGoal(id: "goal_1", title: "Viagem", targetAmount: FinancialGoalAmount(minor: 1))
        let value = try ProtectedValue(id: "goal_1", label: "Reserva", amount: ProtectedValueAmount(minor: 0))
        XCTAssertThrowsError(try FinancialGoalsResponse(goals: [goal], protectedValues: [value]))
    }

    @MainActor
    func testClientUsesStrictPathsHeadersAndBodiesForDeclarations() async throws {
        GoalURLProtocol.install { request in
            if request.httpMethod == "GET" {
                XCTAssertEqual(request.url?.path, "/v1/financial-goals")
                XCTAssertNil(request.url?.query)
                XCTAssertNil(request.httpBody)
                XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/json")
                XCTAssertEqual(request.value(forHTTPHeaderField: "Cache-Control"), "no-store")
                XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
                return GoalURLProtocol.response(request, status: 200, body: #"{"goals":[],"protectedValues":[]}"#)
            }

            XCTAssertEqual(request.httpMethod, "PUT")
            XCTAssertTrue(request.url?.absoluteString.contains("/v1/financial-goals/goal%2Fsafe") == true)
            XCTAssertNil(request.url?.query)
            XCTAssertNil(request.value(forHTTPHeaderField: "Idempotency-Key"))
            let body = try XCTUnwrap(Self.body(from: request))
            let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
            XCTAssertEqual(Set(object.keys), ["title", "targetAmount"])
            return GoalURLProtocol.response(
                request,
                status: 200,
                body: #"{"id":"goal/safe","title":"Viagem","targetAmount":{"minor":10000,"currency":"BRL"}}"#
            )
        }
        let client = makeClient()
        let empty = try await client.financialGoals()
        XCTAssertTrue(empty.goals.isEmpty)
        let goal = try await client.replaceFinancialGoal(
            id: "goal/safe",
            title: "Viagem",
            targetAmount: try FinancialGoalAmount(minor: 10_000)
        )
        XCTAssertEqual(goal.id, "goal/safe")
    }

    @MainActor
    func testClientMapsErrorsWithoutLeakingServerMessage() async {
        GoalURLProtocol.install { request in
            GoalURLProtocol.response(
                request,
                status: 500,
                body: #"{"error":{"code":"INTERNAL_ERROR","message":"SECRET"}}"#
            )
        }
        do {
            _ = try await makeClient().financialGoals()
            XCTFail("Expected service failure")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .serviceUnavailable)
            XCTAssertFalse(String(describing: error).contains("SECRET"))
        }
    }

    @MainActor
    private func makeClient() -> URLSessionFinancialAPIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [GoalURLProtocol.self]
        return URLSessionFinancialAPIClient(
            baseURL: URL(string: "http://127.0.0.1:18081")!,
            session: URLSession(configuration: configuration)
        )
    }

    private static func body(from request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var result = Data()
        var buffer = [UInt8](repeating: 0, count: 1_024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            guard count > 0 else { break }
            result.append(buffer, count: count)
        }
        return result
    }
}

private final class GoalURLProtocol: URLProtocol, @unchecked Sendable {
    private final class Storage: @unchecked Sendable {
        let lock = NSLock()
        var handler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))?
    }

    private static let storage = Storage()

    static func install(_ handler: @escaping @Sendable (URLRequest) throws -> (HTTPURLResponse, Data)) {
        storage.lock.withLock { storage.handler = handler }
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

    static func response(_ request: URLRequest, status: Int, body: String) -> (HTTPURLResponse, Data) {
        (
            HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!,
            Data(body.utf8)
        )
    }
}
