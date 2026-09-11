import Foundation
import Security

protocol SessionCredentialStore {
    func readBearerToken() throws -> String?
    func saveBearerToken(_ token: String) throws
}

enum SessionCredentialStoreError: Error, Equatable {
    case readFailed
    case saveFailed
}

struct KeychainSessionCredentialStore: SessionCredentialStore, Sendable {
    static let shared = KeychainSessionCredentialStore()

    private let service = "dev.jarvis.JARVIS.session"
    private let account = "financial-api-bearer"

    func readBearerToken() throws -> String? {
        var query = baseQuery
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data,
                  let token = String(data: data, encoding: .utf8)
            else { throw SessionCredentialStoreError.readFailed }
            return token
        case errSecItemNotFound:
            return nil
        default:
            throw SessionCredentialStoreError.readFailed
        }
    }

    func saveBearerToken(_ token: String) throws {
        var query = baseQuery
        query[kSecValueData as String] = Data(token.utf8)
        query[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly

        let status = SecItemUpdate(baseQuery as CFDictionary, query as CFDictionary)
        if status == errSecSuccess { return }
        guard status == errSecItemNotFound else {
            throw SessionCredentialStoreError.saveFailed
        }

        let addStatus = SecItemAdd(query as CFDictionary, nil)
        guard addStatus == errSecSuccess else {
            throw SessionCredentialStoreError.saveFailed
        }
    }

    private var baseQuery: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }
}

enum AppConfiguration {
    private static let baseURLVariable = "JARVIS_IOS_API_BASE_URL"
    private static let bearerTokenVariable = "JARVIS_IOS_API_BEARER"
    private static let persistBearerVariable = "JARVIS_IOS_API_PERSIST_BEARER"
    #if DEBUG
    private static let apiModeVariable = "JARVIS_IOS_API_MODE"
    #endif

    @MainActor
    static func financialAPI(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        credentialStore: any SessionCredentialStore = KeychainSessionCredentialStore.shared
    ) -> any FinancialAPI {
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
            let bearerToken = try bearerToken(environment: environment, credentialStore: credentialStore)
            return URLSessionFinancialAPIClient(
                baseURL: try baseURL(
                    environment: environment,
                    allowsDebugLoopbackFallback: allowsDebugLoopbackFallback
                ),
                bearerToken: bearerToken
            )
        } catch {
            return UnavailableFinancialAPI()
        }
    }

    static func baseURL(environment: [String: String]) throws -> URL {
        try baseURL(environment: environment, allowsDebugLoopbackFallback: true)
    }

    static func bearerToken(
        environment: [String: String],
        credentialStore: any SessionCredentialStore
    ) throws -> String? {
        if let value = environment[bearerTokenVariable] {
            try validateBearerToken(value)
            if try persistenceIsEnabled(environment: environment) {
                do {
                    try credentialStore.saveBearerToken(value)
                } catch {
                    throw FinancialAPIError.configuration
                }
            }
            return value
        }

        do {
            guard let stored = try credentialStore.readBearerToken() else { return nil }
            try validateBearerToken(stored)
            return stored
        } catch let error as FinancialAPIError {
            throw error
        } catch {
            throw FinancialAPIError.configuration
        }
    }

    private static func persistenceIsEnabled(environment: [String: String]) throws -> Bool {
        guard let rawValue = environment[persistBearerVariable] else { return false }
        switch rawValue.lowercased() {
        case "1", "true", "yes":
            return true
        case "0", "false", "no":
            return false
        default:
            throw FinancialAPIError.configuration
        }
    }

    private static func validateBearerToken(_ value: String) throws {
        let bytes = Array(value.utf8)
        guard (1...4096).contains(bytes.count), bytes.allSatisfy({ (33...126).contains($0) }) else {
            throw FinancialAPIError.configuration
        }
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

    func previewCardPurchase(_: CardPurchasePreviewRequest) async throws -> CardPurchasePreview {
        throw FinancialAPIError.configuration
    }

    func createCardPurchase(_: CardPurchaseCreateRequest, idempotencyKey _: String) async throws -> RecordedCardPurchase {
        throw FinancialAPIError.configuration
    }

    func installmentPlans() async throws -> InstallmentPlanListResponse {
        throw FinancialAPIError.configuration
    }

    func installmentPlan(id _: String) async throws -> InstallmentPlan {
        throw FinancialAPIError.configuration
    }

    func previewInstallmentPlanCancellation(id _: String) async throws -> InstallmentPlanCancellationPreview {
        throw FinancialAPIError.configuration
    }

    func cancelInstallmentPlan(id _: String, expectedCancelledOn _: RecurrenceCivilDate, idempotencyKey _: String) async throws -> RecordedInstallmentPlan {
        throw FinancialAPIError.configuration
    }

    func scheduledCommitments(evaluationDate _: RecurrenceCivilDate) async throws -> ScheduledCommitmentListResponse {
        throw FinancialAPIError.configuration
    }

    func monthlyBudget(month _: String) async throws -> MonthlyBudget {
        throw FinancialAPIError.configuration
    }

    func replaceMonthlyBudget(month _: String, amount _: MonthlyBudgetAmount) async throws -> MonthlyBudget {
        throw FinancialAPIError.configuration
    }
}
