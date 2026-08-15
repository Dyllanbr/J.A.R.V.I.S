import Foundation
import XCTest

final class JARVISUITests: XCTestCase {
    private enum APIMode: String {
        case stub
        case real
    }

    private struct TestConfiguration {
        let mode: APIMode
        let baseURL: String?
        let description: String
    }

    @MainActor
    func testRegisterPreviewConfirmAndHistory() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        XCTAssertTrue(element("tab.register", in: app).waitForExistence(timeout: 8))
        fillForm(in: app, description: launched.description)

        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.screen", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("review.description", in: app).label.contains(launched.description))
        XCTAssertTrue(element("review.amount", in: app).label.contains("R$ 42,50"))

        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.success", in: app).waitForExistence(timeout: 10))

        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 5))
        element("tab.history", in: app).tap()
        for identifier in [
            "history.previousMonth",
            "history.nextMonth",
            "history.month",
            "history.list"
        ] {
            XCTAssertTrue(
                element(identifier, in: app).waitForExistence(timeout: 10),
                "Missing History identifier: \(identifier)\n\(app.debugDescription)"
            )
        }
        let expense = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "history.expense."))
            .firstMatch
        XCTAssertTrue(expense.waitForExistence(timeout: 10))
        XCTAssertTrue(expense.label.contains(launched.description))
        XCTAssertTrue(expense.label.contains("R$ 42,50"))
        XCTAssertTrue(expense.label.contains("Saída"))
    }

    @MainActor
    func testRegisterIncomePreviewConfirmAndHistory() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }
        let description = incomeDescription(from: launched.description)

        XCTAssertTrue(element("tab.register", in: app).waitForExistence(timeout: 8))
        selectIncome(in: app)
        XCTAssertFalse(element("register.paymentMethod", in: app).exists)
        fillForm(in: app, description: description)

        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.screen", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("review.type", in: app).label.contains("Receita"))
        XCTAssertTrue(element("review.description", in: app).label.contains(description))
        XCTAssertTrue(element("review.amount", in: app).label.contains("R$ 42,50"))
        XCTAssertFalse(element("review.paymentMethod", in: app).exists)

        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.success", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(element("register.newIncome", in: app).exists)

        element("tab.history", in: app).tap()
        XCTAssertTrue(element("history.list", in: app).waitForExistence(timeout: 10))
        let income = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "history.income."))
            .firstMatch
        XCTAssertTrue(income.waitForExistence(timeout: 10))
        XCTAssertTrue(income.label.contains(description))
        XCTAssertTrue(income.label.contains("R$ 42,50"))
        XCTAssertTrue(income.label.contains("Entrada"))
        XCTAssertFalse(income.label.contains("PIX"))
    }

    @MainActor
    func testCriticalAccessibilityIdentifiersAreExposedAtRuntime() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        for identifier in [
            "tab.register",
            "tab.history",
            "register.type",
            "register.type.expense",
            "register.type.income",
            "register.description",
            "register.amount",
            "register.paymentMethod",
            "register.occurredAt",
            "register.review"
        ] {
            XCTAssertTrue(
                element(identifier, in: app).waitForExistence(timeout: 8),
                "Missing accessibility identifier: \(identifier)\n\(app.debugDescription)"
            )
        }

        fillForm(in: app, description: launched.description)
        element("register.review", in: app).tap()

        for identifier in [
            "review.description",
            "review.amount",
            "review.paymentMethod",
            "review.occurredAt",
            "review.edit",
            "review.confirm"
        ] {
            XCTAssertTrue(
                element(identifier, in: app).waitForExistence(timeout: 8),
                "Missing accessibility identifier: \(identifier)\n\(app.debugDescription)"
            )
        }

        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.success", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(element("register.newExpense", in: app).exists)

        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 5))
        element("tab.history", in: app).tap()
        XCTAssertTrue(element("history.list", in: app).waitForExistence(timeout: 10))
    }

    @MainActor
    func testTabIdentifiersSurviveSuccessAndRepeatedNavigation() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        XCTAssertTrue(element("tab.register", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 8))
        fillForm(in: app, description: "Tabs_sinteticas_UI")
        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.confirm", in: app).waitForExistence(timeout: 8))
        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.success", in: app).waitForExistence(timeout: 10))

        for cycle in 1...10 {
            let historyTab = element("tab.history", in: app)
            XCTAssertTrue(
                historyTab.waitForExistence(timeout: 5),
                "tab.history disappeared in cycle \(cycle)\n\(app.debugDescription)"
            )
            historyTab.tap()
            XCTAssertTrue(element("history.list", in: app).waitForExistence(timeout: 10))

            let registerTab = element("tab.register", in: app)
            XCTAssertTrue(
                registerTab.waitForExistence(timeout: 5),
                "tab.register disappeared in cycle \(cycle)\n\(app.debugDescription)"
            )
            registerTab.tap()
            XCTAssertTrue(element("register.success", in: app).waitForExistence(timeout: 5))
        }
    }

    @MainActor
    func testHistoryFailureExposesRetryAndNavigationIdentifiers() {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = "real"
        app.launchEnvironment["JARVIS_IOS_API_BASE_URL"] = "http://127.0.0.1:9"
        app.launch()
        defer { app.terminate() }

        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 8))
        element("tab.history", in: app).tap()

        for identifier in [
            "history.previousMonth",
            "history.nextMonth",
            "history.month",
            "history.retry"
        ] {
            XCTAssertTrue(
                element(identifier, in: app).waitForExistence(timeout: 10),
                "Missing failed History identifier: \(identifier)\n\(app.debugDescription)"
            )
        }
    }

    @MainActor
    func testEditRequiresANewReview() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        fillForm(in: app, description: "Transporte_sintetico_UI")
        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.edit", in: app).waitForExistence(timeout: 8))
        element("review.edit", in: app).tap()

        XCTAssertTrue(element("register.screen", in: app).waitForExistence(timeout: 5))
        XCTAssertEqual(app.textFields["register.description"].value as? String, "Transporte_sintetico_UI")
        XCTAssertFalse(element("review.confirm", in: app).exists)

        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.confirm", in: app).waitForExistence(timeout: 8))
    }

    @MainActor
    func testSwitchingReviewedExpenseToIncomeInvalidatesTheReview() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        fillForm(in: app, description: "Troca_tipo_sintetica_UI")
        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.confirm", in: app).waitForExistence(timeout: 8))
        element("review.edit", in: app).tap()

        selectIncome(in: app)
        XCTAssertFalse(element("register.paymentMethod", in: app).exists)
        XCTAssertFalse(element("review.confirm", in: app).exists)
        XCTAssertTrue(element("register.review", in: app).exists)

        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.confirm", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("review.type", in: app).label.contains("Receita"))
        XCTAssertFalse(element("review.paymentMethod", in: app).exists)
    }

    @MainActor
    func testMixedHistoryShowsExpenseAndIncomeWithDistinctSemantics() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        fillForm(in: app, description: "Despesa_mista_sintetica_UI")
        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.confirm", in: app).waitForExistence(timeout: 8))
        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.newExpense", in: app).waitForExistence(timeout: 10))
        element("register.newExpense", in: app).tap()

        selectIncome(in: app)
        fillForm(in: app, description: "Receita_mista_sintetica_UI")
        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.confirm", in: app).waitForExistence(timeout: 8))
        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.newIncome", in: app).waitForExistence(timeout: 10))

        element("tab.history", in: app).tap()
        XCTAssertTrue(element("history.list", in: app).waitForExistence(timeout: 10))
        let expense = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "history.expense."))
            .firstMatch
        let income = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "history.income."))
            .firstMatch
        XCTAssertTrue(expense.waitForExistence(timeout: 8))
        XCTAssertTrue(income.waitForExistence(timeout: 8))
        XCTAssertTrue(expense.label.contains("Saída"))
        XCTAssertTrue(expense.label.contains("PIX"))
        XCTAssertTrue(income.label.contains("Entrada"))
        XCTAssertFalse(income.label.contains("PIX"))
    }

    @MainActor
    func testExtremeDynamicTypeKeepsPrimaryJourneyReachable() throws {
        continueAfterFailure = false
        let launched = try launchApp(extremeDynamicType: true)
        let app = launched.app
        defer { app.terminate() }

        XCTAssertTrue(reveal("register.description", in: app))
        fillForm(in: app, description: "Dynamic_Type_sintetico")
        XCTAssertTrue(reveal("register.review", in: app))
        element("register.review", in: app).tap()

        XCTAssertTrue(reveal("review.edit", in: app))
        element("review.edit", in: app).tap()
        XCTAssertTrue(reveal("register.review", in: app))
        element("register.review", in: app).tap()

        XCTAssertTrue(reveal("review.confirm", in: app))
        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.success", in: app).waitForExistence(timeout: 10))

        XCTAssertTrue(reveal("register.newExpense", in: app))
        element("register.newExpense", in: app).tap()
        XCTAssertTrue(reveal("register.type.income", in: app))
        selectIncome(in: app)
        fillForm(in: app, description: "Dynamic_Type_receita_sintetica")
        XCTAssertTrue(reveal("register.review", in: app))
        element("register.review", in: app).tap()
        XCTAssertTrue(reveal("review.confirm", in: app))
        XCTAssertTrue(element("review.type", in: app).label.contains("Receita"))
        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.newIncome", in: app).waitForExistence(timeout: 10))

        XCTAssertTrue(reveal("tab.history", in: app))
        element("tab.history", in: app).tap()
        XCTAssertTrue(element("history.list", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(
            app.descendants(matching: .any)
                .matching(NSPredicate(format: "identifier BEGINSWITH %@", "history.expense."))
                .firstMatch
                .waitForExistence(timeout: 8)
        )
        XCTAssertTrue(
            app.descendants(matching: .any)
                .matching(NSPredicate(format: "identifier BEGINSWITH %@", "history.income."))
                .firstMatch
                .waitForExistence(timeout: 8)
        )
    }

    @MainActor
    private func launchApp(extremeDynamicType: Bool = false) throws -> (
        app: XCUIApplication,
        description: String
    ) {
        let configuration = try testConfiguration()
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = configuration.mode.rawValue
        if let baseURL = configuration.baseURL {
            app.launchEnvironment["JARVIS_IOS_API_BASE_URL"] = baseURL
        }
        if extremeDynamicType {
            app.launchArguments += [
                "-UIPreferredContentSizeCategoryName",
                "UICTContentSizeCategoryAccessibilityExtraExtraExtraLarge"
            ]
        }
        app.launch()
        return (app, configuration.description)
    }

    private func testConfiguration() throws -> TestConfiguration {
        let info = try XCTUnwrap(Bundle(for: Self.self).infoDictionary)
        let rawMode = try XCTUnwrap(info["JARVIS_IOS_TEST_MODE"] as? String)
        let mode = try XCTUnwrap(APIMode(rawValue: rawMode), "Unsupported iOS API test mode")
        let description = try XCTUnwrap(info["JARVIS_IOS_E2E_DESCRIPTION"] as? String)
        guard !description.isEmpty else {
            XCTFail("The synthetic iOS test description was not configured")
            throw CocoaError(.fileReadCorruptFile)
        }

        guard mode == .real else {
            return TestConfiguration(mode: .stub, baseURL: nil, description: description)
        }

        let rawBaseURL = try XCTUnwrap(
            info["JARVIS_IOS_E2E_BASE_URL"] as? String,
            "Real API mode requires a base URL in the UI test bundle"
        )
        let components = URLComponents(string: rawBaseURL)
        guard !rawBaseURL.isEmpty,
              let scheme = components?.scheme,
              ["http", "https"].contains(scheme),
              components?.host != nil,
              components?.user == nil,
              components?.password == nil
        else {
            XCTFail("Real API mode received an invalid base URL")
            throw CocoaError(.fileReadCorruptFile)
        }
        return TestConfiguration(mode: .real, baseURL: rawBaseURL, description: description)
    }

    @MainActor
    private func fillForm(in app: XCUIApplication, description: String) {
        let descriptionField = app.textFields["register.description"]
        XCTAssertTrue(descriptionField.waitForExistence(timeout: 8))
        descriptionField.tap()
        descriptionField.typeText(description)

        let amountField = app.textFields["register.amount"]
        XCTAssertTrue(amountField.exists)
        amountField.tap()
        amountField.typeText("42,50")
        dismissKeyboard(in: app)
    }

    @MainActor
    private func selectIncome(in app: XCUIApplication) {
        let selector = element("register.type.income", in: app)
        XCTAssertTrue(selector.waitForExistence(timeout: 8))
        if !selector.isHittable {
            XCTAssertTrue(reveal("register.type.income", in: app))
        }
        selector.tap()
    }

    private func incomeDescription(from base: String) -> String {
        "\(base)_income"
    }

    @MainActor
    private func dismissKeyboard(in app: XCUIApplication) {
        if app.keyboards.firstMatch.exists {
            app.collectionViews["register.screen"].swipeUp()
        }
    }

    @MainActor
    private func reveal(_ identifier: String, in app: XCUIApplication) -> Bool {
        let target = element(identifier, in: app)
        if target.waitForExistence(timeout: 3), target.isHittable {
            return true
        }
        for _ in 0..<8 {
            app.swipeUp()
            if target.exists, target.isHittable {
                return true
            }
        }
        return false
    }

    @MainActor
    private func element(_ identifier: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)[identifier]
    }
}
