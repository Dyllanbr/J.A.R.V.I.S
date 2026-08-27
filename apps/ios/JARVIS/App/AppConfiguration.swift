import Foundation

enum AppConfiguration {
    private static let baseURLVariable = "JARVIS_IOS_API_BASE_URL"
    #if DEBUG
    private static let apiModeVariable = "JARVIS_IOS_API_MODE"
    #endif

    @MainActor
    static func financialAPI(environment: [String: String] = ProcessInfo.processInfo.environment) -> any FinancialAPI {
        #if DEBUG
        let mode = environment[apiModeVariable]

        if mode == "stub" {
            return StubFinancialAPI()
        }

        guard mode == nil || mode == "real" else {
            return UnavailableFinancialAPI()
        }
        let allowsDebugLoopbackFallback = mode == nil
        #else
        let allowsDebugLoopbackFallback = false
        #endif

        do {
            return URLSessionFinancialAPIClient(
                baseURL: try baseURL(
                    environment: environment,
                    allowsDebugLoopbackFallback: allowsDebugLoopbackFallback
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
    func categories() async throws -> [CategoryDefinition] {
        throw FinancialAPIError.configuration
    }

    func preview(_: ExpenseRequest) async throws -> ExpensePreview {
        throw FinancialAPIError.configuration
    }

    func create(_: ExpenseRequest, idempotencyKey _: String) async throws -> RecordedExpense {
        throw FinancialAPIError.configuration
    }

    func preview(_: IncomeRequest) async throws -> IncomePreview {
        throw FinancialAPIError.configuration
    }

    func create(_: IncomeRequest, idempotencyKey _: String) async throws -> RecordedIncome {
        throw FinancialAPIError.configuration
    }

    func transactions(month _: String) async throws -> TransactionMonth {
        throw FinancialAPIError.configuration
    }

    func previewRecurrence(_: RecurrenceRequest) async throws -> RecurrencePreview {
        throw FinancialAPIError.configuration
    }

    func createRecurrence(_: RecurrenceRequest, idempotencyKey _: String) async throws -> RecordedRecurrence {
        throw FinancialAPIError.configuration
    }

    func recurrences() async throws -> RecurrenceList {
        throw FinancialAPIError.configuration
    }

    func cancelRecurrence(id _: String, idempotencyKey _: String) async throws -> RecordedRecurrence {
        throw FinancialAPIError.configuration
    }

    func recurrenceSuggestions() async throws -> RecurrenceSuggestionList {
        throw FinancialAPIError.configuration
    }

    func dismissRecurrenceSuggestion(id _: String) async throws -> DismissedRecurrenceSuggestion {
        throw FinancialAPIError.configuration
    }

    func previewRecurrenceSuggestion(id _: String) async throws -> RecurrencePreview {
        throw FinancialAPIError.configuration
    }

    func previewCreditCard(_: CreditCardRequest) async throws -> CreditCardPreview {
        throw FinancialAPIError.configuration
    }

    func createCreditCard(_: CreditCardRequest, idempotencyKey _: String) async throws -> RecordedCreditCard {
        throw FinancialAPIError.configuration
    }

    func creditCards() async throws -> CreditCardList {
        throw FinancialAPIError.configuration
    }

    func creditCard(id _: String) async throws -> CreditCard {
        throw FinancialAPIError.configuration
    }

    func archiveCreditCard(id _: String, idempotencyKey _: String) async throws -> RecordedCreditCard {
        throw FinancialAPIError.configuration
    }
}
