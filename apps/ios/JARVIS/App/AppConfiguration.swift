import Foundation

enum AppConfiguration {
    private static let baseURLVariable = "JARVIS_IOS_API_BASE_URL"
    private static let apiModeVariable = "JARVIS_IOS_API_MODE"

    @MainActor
    static func financialAPI(environment: [String: String] = ProcessInfo.processInfo.environment) -> any FinancialAPI {
        let mode = environment[apiModeVariable]

        #if DEBUG
        if mode == "stub" {
            return StubFinancialAPI()
        }
        #endif

        guard mode == nil || mode == "real" else {
            return UnavailableFinancialAPI()
        }

        do {
            return URLSessionFinancialAPIClient(
                baseURL: try baseURL(
                    environment: environment,
                    allowsDebugLoopbackFallback: mode == nil
                )
            )
        } catch {
            return UnavailableFinancialAPI()
        }
    }

    static func baseURL(environment: [String: String]) throws -> URL {
        try baseURL(environment: environment, allowsDebugLoopbackFallback: true)
    }

    private static func baseURL(
        environment: [String: String],
        allowsDebugLoopbackFallback: Bool
    ) throws -> URL {
        if let rawValue = environment[baseURLVariable], !rawValue.isEmpty {
            guard let url = URL(string: rawValue),
                  let scheme = url.scheme,
                  ["http", "https"].contains(scheme),
                  url.host != nil,
                  url.user == nil,
                  url.password == nil
            else {
                throw FinancialAPIError.configuration
            }
            return url
        }

        #if DEBUG
        if allowsDebugLoopbackFallback {
            return URL(string: "http://127.0.0.1:8080")!
        }
        #else
        _ = allowsDebugLoopbackFallback
        #endif
        throw FinancialAPIError.configuration
    }
}

@MainActor
private struct UnavailableFinancialAPI: FinancialAPI {
    func preview(_: ExpenseRequest) async throws -> ExpensePreview {
        throw FinancialAPIError.configuration
    }

    func create(_: ExpenseRequest, idempotencyKey _: String) async throws -> RecordedExpense {
        throw FinancialAPIError.configuration
    }

    func expenses(month _: String) async throws -> ExpenseMonth {
        throw FinancialAPIError.configuration
    }
}
