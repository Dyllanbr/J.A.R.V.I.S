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
        selectRegisterCategory("expense.food", in: app)
        fillForm(in: app, description: launched.description)

        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.screen", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("review.description", in: app).label.contains(launched.description))
        XCTAssertTrue(element("review.amount", in: app).label.contains("R$ 42,50"))
        XCTAssertTrue(element("review.category", in: app).label.contains("Alimentação"))

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
        XCTAssertTrue(expense.label.contains("Alimentação"))
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
        selectRegisterCategory("income.salary", in: app)
        fillForm(in: app, description: description)

        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.screen", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("review.type", in: app).label.contains("Receita"))
        XCTAssertTrue(element("review.description", in: app).label.contains(description))
        XCTAssertTrue(element("review.amount", in: app).label.contains("R$ 42,50"))
        XCTAssertTrue(element("review.category", in: app).label.contains("Salário"))
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
        XCTAssertTrue(income.label.contains("Salário"))
        XCTAssertFalse(income.label.contains("PIX"))
    }

    @MainActor
    func testRecurrencePreviewConfirmListAndCancel() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }
        let description = recurrenceDescription(from: launched.description)

        XCTAssertTrue(element("tab.recurrences", in: app).waitForExistence(timeout: 8))
        element("tab.recurrences", in: app).tap()
        XCTAssertTrue(element("recurrence.create", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(
            element("recurrence.list", in: app).waitForExistence(timeout: 8)
                || element("recurrence.empty", in: app).waitForExistence(timeout: 2)
        )

        element("recurrence.create", in: app).tap()
        XCTAssertTrue(element("recurrence.create.screen", in: app).waitForExistence(timeout: 5))
        fillRecurrenceForm(in: app, description: description)
        element("recurrence.review", in: app).tap()

        XCTAssertTrue(element("recurrence.review.screen", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("recurrence.review.description", in: app).label.contains(description))
        XCTAssertTrue(element("recurrence.review.amount", in: app).label.contains("R$ 42,50"))
        XCTAssertTrue(element("recurrence.review.frequency", in: app).label.contains("Mensal"))
        XCTAssertTrue(element("recurrence.review.startsOn", in: app).exists)

        element("recurrence.confirm", in: app).tap()
        XCTAssertTrue(element("recurrence.success", in: app).waitForExistence(timeout: 10))
        element("recurrence.success.return", in: app).tap()
        XCTAssertTrue(element("recurrence.list", in: app).waitForExistence(timeout: 10))

        let recurrence = app.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier BEGINSWITH %@ AND label CONTAINS %@",
                    "recurrence.item.",
                    description
                )
            )
            .firstMatch
        XCTAssertTrue(recurrence.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(recurrence.label.contains("R$ 42,50"))
        XCTAssertTrue(recurrence.label.localizedCaseInsensitiveContains("mensal"))
        XCTAssertTrue(recurrence.label.contains("Ativa"))

        let recurrenceID = String(recurrence.identifier.dropFirst("recurrence.item.".count))
        let cancel = element("recurrence.cancel.\(recurrenceID)", in: app)
        XCTAssertTrue(cancel.waitForExistence(timeout: 5))
        cancel.tap()
        let confirmationButtons = app.buttons.matching(identifier: "recurrence.cancel.confirm")
        let confirmation = confirmationButtons.firstMatch
        XCTAssertTrue(confirmation.waitForExistence(timeout: 5), app.debugDescription)
        let destructiveAction = confirmationButtons.element(boundBy: max(confirmationButtons.count - 1, 0))
        XCTAssertTrue(destructiveAction.waitForExistence(timeout: 5), app.debugDescription)
        destructiveAction.tap()

        let cancelled = NSPredicate(format: "label CONTAINS %@", "Cancelada")
        XCTAssertEqual(
            XCTWaiter().wait(
                for: [XCTNSPredicateExpectation(predicate: cancelled, object: recurrence)],
                timeout: 10
            ),
            .completed,
            app.debugDescription
        )
        XCTAssertTrue(recurrence.exists, "Cancelled recurrence must remain in the list")
        XCTAssertFalse(cancel.exists, "A known CANCELLED recurrence must not expose cancel again")
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
            "tab.recurrences",
            "register.type",
            "register.type.expense",
            "register.type.income",
            "register.description",
            "register.amount",
            "register.paymentMethod",
            "register.category",
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
            "review.category",
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
        XCTAssertTrue(element("history.filter.type", in: app).exists)
        XCTAssertTrue(element("history.filter.category", in: app).exists)

        element("tab.recurrences", in: app).tap()
        XCTAssertTrue(element("recurrence.create", in: app).waitForExistence(timeout: 8))
        element("recurrence.create", in: app).tap()
        for identifier in [
            "recurrence.description",
            "recurrence.amount",
            "recurrence.startsOn",
            "recurrence.review"
        ] {
            XCTAssertTrue(
                element(identifier, in: app).waitForExistence(timeout: 5),
                "Missing Recurrence identifier: \(identifier)\n\(app.debugDescription)"
            )
        }
    }

    @MainActor
    func testTabIdentifiersSurviveSuccessAndRepeatedNavigation() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        XCTAssertTrue(element("tab.register", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("tab.recurrences", in: app).waitForExistence(timeout: 8))
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

            let recurrencesTab = element("tab.recurrences", in: app)
            XCTAssertTrue(
                recurrencesTab.waitForExistence(timeout: 5),
                "tab.recurrences disappeared in cycle \(cycle)\n\(app.debugDescription)"
            )
            recurrencesTab.tap()
            XCTAssertTrue(element("recurrence.create", in: app).waitForExistence(timeout: 8))

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
    func testRecurrenceFailureExposesSafeRetry() {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = "real"
        app.launchEnvironment["JARVIS_IOS_API_BASE_URL"] = "http://127.0.0.1:9"
        app.launch()
        defer { app.terminate() }

        XCTAssertTrue(element("tab.recurrences", in: app).waitForExistence(timeout: 8))
        element("tab.recurrences", in: app).tap()
        XCTAssertTrue(element("recurrence.retry", in: app).waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS[c] 'sql' OR label CONTAINS[c] 'pgx'")).firstMatch.exists)
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
        selectRegisterCategory("expense.food", in: app)
        element("register.review", in: app).tap()
        XCTAssertTrue(element("review.confirm", in: app).waitForExistence(timeout: 8))
        element("review.confirm", in: app).tap()
        XCTAssertTrue(element("register.newExpense", in: app).waitForExistence(timeout: 10))
        element("register.newExpense", in: app).tap()

        selectIncome(in: app)
        selectRegisterCategory("income.salary", in: app)
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
        XCTAssertTrue(expense.label.contains("Alimentação"))
        XCTAssertTrue(income.label.contains("Entrada"))
        XCTAssertTrue(income.label.contains("Salário"))
        XCTAssertFalse(income.label.contains("PIX"))

        selectHistoryTypeFilter("expense", in: app)
        XCTAssertTrue(expense.waitForExistence(timeout: 5))
        XCTAssertFalse(income.exists)
        selectHistoryCategoryFilter("expense.food", in: app)
        XCTAssertTrue(expense.waitForExistence(timeout: 5))

        selectHistoryTypeFilter("income", in: app)
        XCTAssertTrue(income.waitForExistence(timeout: 5), "Changing type must reset an incompatible Category filter")

        selectHistoryCategoryFilter("none", in: app)
        XCTAssertTrue(element("history.filteredEmpty", in: app).waitForExistence(timeout: 5))
    }

    @MainActor
    func testExtremeDynamicTypeKeepsPrimaryJourneyReachable() throws {
        continueAfterFailure = false
        let launched = try launchApp(extremeDynamicType: true)
        let app = launched.app
        defer { app.terminate() }

        XCTAssertTrue(reveal("register.description", in: app))
        XCTAssertTrue(reveal("register.category", in: app))
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
        XCTAssertTrue(reveal("history.filter.type", in: app))
        XCTAssertTrue(reveal("history.filter.category", in: app))
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

        XCTAssertTrue(reveal("tab.recurrences", in: app))
        element("tab.recurrences", in: app).tap()
        XCTAssertTrue(reveal("recurrence.create", in: app))
        element("recurrence.create", in: app).tap()
        XCTAssertTrue(reveal("recurrence.description", in: app))
        fillRecurrenceForm(in: app, description: "Dynamic_Type_recorrencia_sintetica")
        XCTAssertTrue(reveal("recurrence.review", in: app))
        element("recurrence.review", in: app).tap()
        XCTAssertTrue(reveal("recurrence.confirm", in: app))
        XCTAssertTrue(element("recurrence.review.description", in: app).label.contains("Dynamic_Type_recorrencia_sintetica"))
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
    private func fillRecurrenceForm(in app: XCUIApplication, description: String) {
        let descriptionField = app.textFields["recurrence.description"]
        XCTAssertTrue(descriptionField.waitForExistence(timeout: 8))
        descriptionField.tap()
        descriptionField.typeText(description)

        let amountField = app.textFields["recurrence.amount"]
        XCTAssertTrue(amountField.exists)
        amountField.tap()
        amountField.typeText("42,50")
        if app.keyboards.firstMatch.exists {
            app.collectionViews["recurrence.create.screen"].swipeUp()
        }
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

    @MainActor
    private func selectRegisterCategory(_ categoryID: String, in app: XCUIApplication) {
        let picker = element("register.category", in: app)
        XCTAssertTrue(picker.waitForExistence(timeout: 8))
        XCTAssertEqual(
            XCTWaiter().wait(
                for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "enabled == true"), object: picker)],
                timeout: 8
            ),
            .completed,
            "Category catalog did not become available\n\(app.debugDescription)"
        )
        if !picker.isHittable {
            XCTAssertTrue(reveal("register.category", in: app))
        }
        picker.tap()
        let option = element("register.category.option.\(categoryID)", in: app)
        XCTAssertTrue(option.waitForExistence(timeout: 5), "Missing Category option \(categoryID)\n\(app.debugDescription)")
        option.tap()
    }

    @MainActor
    private func selectHistoryTypeFilter(_ value: String, in app: XCUIApplication) {
        let picker = element("history.filter.type", in: app)
        XCTAssertTrue(picker.waitForExistence(timeout: 5))
        picker.tap()
        let option = element("history.filter.type.\(value)", in: app)
        XCTAssertTrue(option.waitForExistence(timeout: 5))
        option.tap()
    }

    @MainActor
    private func selectHistoryCategoryFilter(_ value: String, in app: XCUIApplication) {
        let picker = element("history.filter.category", in: app)
        XCTAssertTrue(picker.waitForExistence(timeout: 5))
        picker.tap()
        let option = element("history.filter.category.option.\(value)", in: app)
        XCTAssertTrue(option.waitForExistence(timeout: 5))
        option.tap()
    }

    private func incomeDescription(from base: String) -> String {
        "\(base)_income"
    }

    private func recurrenceDescription(from base: String) -> String {
        "\(base)_recurrence"
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
