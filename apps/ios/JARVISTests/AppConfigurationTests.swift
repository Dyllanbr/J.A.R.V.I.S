import XCTest
@testable import JARVIS

@MainActor
final class AppConfigurationTests: XCTestCase {
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

    func testExplicitStubModeUsesTheDevelopmentStub() {
        let api = AppConfiguration.financialAPI(environment: [
            "JARVIS_IOS_API_MODE": "stub"
        ])

        XCTAssertTrue(api is StubFinancialAPI)
    }
}
