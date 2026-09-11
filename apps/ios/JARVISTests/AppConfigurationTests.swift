import XCTest
@testable import JARVIS

@MainActor
final class AppConfigurationTests: XCTestCase {
    private final class InMemoryCredentialStore: SessionCredentialStore {
        var token: String?
        var readCount = 0
        var saveCount = 0
        var shouldFailRead = false
        var shouldFailSave = false

        func readBearerToken() throws -> String? {
            readCount += 1
            if shouldFailRead { throw SessionCredentialStoreError.readFailed }
            return token
        }

        func saveBearerToken(_ token: String) throws {
            saveCount += 1
            if shouldFailSave { throw SessionCredentialStoreError.saveFailed }
            self.token = token
        }
    }

    func testAcceptsExplicitLocalBaseURLWithoutCredentials() throws {
        let url = try AppConfiguration.baseURL(environment: [
            "JARVIS_IOS_API_BASE_URL": "http://127.0.0.1:18081"
        ])
        XCTAssertEqual(url.absoluteString, "http://127.0.0.1:18081")
    }

    func testRejectsInvalidOrCredentialBearingBaseURL() {
        for rawValue in ["not-a-url", "ftp://127.0.0.1", "http://user:password@127.0.0.1"] {
            XCTAssertThrowsError(try AppConfiguration.baseURL(environment: [
                "JARVIS_IOS_API_BASE_URL": rawValue
            ])) { error in
                XCTAssertEqual(error as? FinancialAPIError, .configuration)
                XCTAssertFalse(String(describing: error).contains(rawValue))
            }
        }
    }

    func testExplicitRealModeFailsClosedWithoutBaseURL() async {
        let api = AppConfiguration.financialAPI(environment: [
            "JARVIS_IOS_API_MODE": "real"
        ])

        do {
            _ = try await api.preview(
                ExpenseRequest(
                    description: "Configuração sintética",
                    amount: ExpenseMoney(minor: 100, currency: .brl),
                    paymentMethod: .pix,
                    occurredAt: "2026-08-14T15:00:00Z"
                )
            )
            XCTFail("Real mode without a base URL must fail closed")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .configuration)
        }
    }

    func testExplicitRealModeAlsoFailsClosedForIncomeWithoutBaseURL() async {
        let api = AppConfiguration.financialAPI(environment: [
            "JARVIS_IOS_API_MODE": "real"
        ])

        do {
            _ = try await api.preview(
                IncomeRequest(
                    description: "Receita de configuração sintética",
                    amount: FinancialMoney(minor: 100, currency: .brl),
                    occurredAt: "2026-08-14T15:00:00Z"
                )
            )
            XCTFail("Real mode without a base URL must fail closed for Income")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .configuration)
        }
    }

    func testExplicitStubModeUsesTheDevelopmentStub() {
        let api = AppConfiguration.financialAPI(environment: [
            "JARVIS_IOS_API_MODE": "stub"
        ])

        XCTAssertTrue(api is StubFinancialAPI)
    }

    func testInvalidBearerConfigurationFailsClosed() async {
        let api = AppConfiguration.financialAPI(environment: [
            "JARVIS_IOS_API_MODE": "real",
            "JARVIS_IOS_API_BASE_URL": "http://127.0.0.1:18081",
            "JARVIS_IOS_API_BEARER": "token with whitespace"
        ])

        do {
            _ = try await api.categories()
            XCTFail("Invalid bearer configuration must fail closed")
        } catch {
            XCTAssertEqual(error as? FinancialAPIError, .configuration)
        }
    }

    func testConfiguredBearerWinsAndIsNotPersistedWithoutExplicitOptIn() throws {
        let store = InMemoryCredentialStore()
        let token = try AppConfiguration.bearerToken(
            environment: ["JARVIS_IOS_API_BEARER": "configured-token"],
            credentialStore: store
        )

        XCTAssertEqual(token, "configured-token")
        XCTAssertEqual(store.saveCount, 0)
        XCTAssertNil(store.token)
    }

    func testConfiguredBearerCanBePersistedOnlyWithExplicitOptIn() throws {
        let store = InMemoryCredentialStore()
        let token = try AppConfiguration.bearerToken(
            environment: [
                "JARVIS_IOS_API_BEARER": "configured-token",
                "JARVIS_IOS_API_PERSIST_BEARER": "true",
            ],
            credentialStore: store
        )

        XCTAssertEqual(token, "configured-token")
        XCTAssertEqual(store.saveCount, 1)
        XCTAssertEqual(store.token, "configured-token")
    }

    func testStoredBearerIsUsedWhenEnvironmentDoesNotProvideOne() throws {
        let store = InMemoryCredentialStore()
        store.token = "stored-token"

        let token = try AppConfiguration.bearerToken(environment: [:], credentialStore: store)

        XCTAssertEqual(token, "stored-token")
        XCTAssertEqual(store.readCount, 1)
    }

    func testInvalidStoredBearerFailsClosed() {
        let store = InMemoryCredentialStore()
        store.token = "token with whitespace"

        XCTAssertThrowsError(try AppConfiguration.bearerToken(environment: [:], credentialStore: store)) { error in
            XCTAssertEqual(error as? FinancialAPIError, .configuration)
        }
    }

    func testCredentialStoreFailureFailsClosedWithoutLeakingDetails() {
        let store = InMemoryCredentialStore()
        store.shouldFailRead = true

        XCTAssertThrowsError(try AppConfiguration.bearerToken(environment: [:], credentialStore: store)) { error in
            XCTAssertEqual(error as? FinancialAPIError, .configuration)
            XCTAssertFalse(String(describing: error).contains("readFailed"))
        }

        store.shouldFailRead = false
        store.shouldFailSave = true
        XCTAssertThrowsError(try AppConfiguration.bearerToken(
            environment: [
                "JARVIS_IOS_API_BEARER": "configured-token",
                "JARVIS_IOS_API_PERSIST_BEARER": "1",
            ],
            credentialStore: store
        )) { error in
            XCTAssertEqual(error as? FinancialAPIError, .configuration)
        }
    }

    func testInvalidPersistenceFlagFailsClosed() {
        let store = InMemoryCredentialStore()

        XCTAssertThrowsError(try AppConfiguration.bearerToken(
            environment: [
                "JARVIS_IOS_API_BEARER": "configured-token",
                "JARVIS_IOS_API_PERSIST_BEARER": "sometimes",
            ],
            credentialStore: store
        )) { error in
            XCTAssertEqual(error as? FinancialAPIError, .configuration)
            XCTAssertEqual(store.saveCount, 0)
        }
    }
}
