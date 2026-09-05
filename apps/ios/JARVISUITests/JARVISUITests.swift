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

        if try testConfiguration().mode == .real {
            element("tab.history", in: app).tap()
            element("history.scheduledCommitments.entry", in: app).tap()
            XCTAssertTrue(
                element("scheduledCommitments.screen", in: app).waitForExistence(timeout: 10),
                app.debugDescription
            )
            XCTAssertTrue(
                element("scheduledCommitments.list", in: app).waitForExistence(timeout: 10),
                app.debugDescription
            )
            let scheduledRecurrence = app.descendants(matching: .any)
                .matching(
                    NSPredicate(
                        format: "identifier BEGINSWITH %@ AND label CONTAINS %@",
                        "scheduledCommitment.RECURRENCE.",
                        "R$ 42,50"
                    )
                )
                .firstMatch
            XCTAssertTrue(scheduledRecurrence.waitForExistence(timeout: 10), app.debugDescription)
            XCTAssertTrue(scheduledRecurrence.label.contains("R$ 42,50"), app.debugDescription)
            element("tab.recurrences", in: app).tap()
            XCTAssertTrue(element("recurrence.list", in: app).waitForExistence(timeout: 10), app.debugDescription)
        }

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
    func testRecurrenceSuggestionAppearsRequiresReviewAndConfirmsThroughCanonicalFlow() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }
        let suggestionID = "rsg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

        element("tab.recurrences", in: app).tap()
        let suggestion = element("recurrence.suggestion.\(suggestionID)", in: app)
        XCTAssertTrue(suggestion.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(element("recurrence.suggestions.section", in: app).exists)
        XCTAssertTrue(suggestion.label.contains("Sugestão de possível recorrência"))
        XCTAssertTrue(suggestion.label.contains("Internet sintética"))
        XCTAssertTrue(suggestion.label.contains("R$ 99,90"))
        XCTAssertTrue(suggestion.label.contains("3 ocorrências"))
        XCTAssertTrue(element("recurrence.item.rec_ui_synthetic_active", in: app).exists)

        element("recurrence.suggestion.review.\(suggestionID)", in: app).tap()
        XCTAssertTrue(element("recurrence.review.screen", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("recurrence.suggestion.review.notice", in: app).exists)
        XCTAssertTrue(element("recurrence.review.description", in: app).label.contains("Internet sintética"))
        XCTAssertTrue(element("recurrence.review.amount", in: app).label.contains("R$ 99,90"))
        XCTAssertTrue(element("recurrence.review.startsOn", in: app).label.contains("10/09/2026"))
        XCTAssertFalse(element("recurrence.edit", in: app).exists)
        XCTAssertFalse(element("recurrence.success", in: app).exists, "Review must not create automatically")

        element("recurrence.confirm", in: app).tap()
        XCTAssertTrue(element("recurrence.success", in: app).waitForExistence(timeout: 10))
        element("recurrence.success.return", in: app).tap()
        XCTAssertTrue(suggestion.waitForNonExistence(timeout: 8))

        let confirmed = app.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier BEGINSWITH %@ AND label CONTAINS %@",
                    "recurrence.item.",
                    "Internet sintética"
                )
            )
            .firstMatch
        XCTAssertTrue(confirmed.waitForExistence(timeout: 8), app.debugDescription)
        XCTAssertTrue(confirmed.label.contains("Ativa"))
    }

    @MainActor
    func testRecurrenceSuggestionDismissRequiresConfirmationAndDoesNotAffectConfirmedItems() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }
        let suggestionID = "rsg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

        element("tab.recurrences", in: app).tap()
        let suggestion = element("recurrence.suggestion.\(suggestionID)", in: app)
        XCTAssertTrue(suggestion.waitForExistence(timeout: 10))
        element("recurrence.suggestion.dismiss.\(suggestionID)", in: app).tap()
        XCTAssertTrue(element("recurrence.suggestion.dismiss.cancel", in: app).waitForExistence(timeout: 5))
        let confirmationButtons = app.buttons.matching(identifier: "recurrence.suggestion.dismiss.confirm")
        XCTAssertTrue(confirmationButtons.firstMatch.waitForExistence(timeout: 5), app.debugDescription)
        let alertButtons = app.alerts.firstMatch.buttons.matching(
            identifier: "recurrence.suggestion.dismiss.confirm"
        )
        let destructiveAction = alertButtons.element(boundBy: max(alertButtons.count - 1, 0))
        XCTAssertTrue(destructiveAction.waitForExistence(timeout: 5), app.debugDescription)
        destructiveAction.tap()

        XCTAssertTrue(suggestion.waitForNonExistence(timeout: 8))
        XCTAssertTrue(element("recurrence.item.rec_ui_synthetic_active", in: app).exists)
        XCTAssertTrue(element("recurrence.item.rec_ui_synthetic_cancelled", in: app).exists)
        let list = element("recurrence.list", in: app)
        XCTAssertTrue(list.exists)
        list.swipeDown()
        XCTAssertFalse(suggestion.waitForExistence(timeout: 3), "Refresh resurrected the dismissed suggestion")
    }

    @MainActor
    func testStaleRecurrenceSuggestionIsRemovedWithoutInvalidReviewNavigation() {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = "stub"
        app.launchEnvironment["JARVIS_IOS_SUGGESTION_SCENARIO"] = "stale"
        app.launch()
        defer { app.terminate() }
        let suggestionID = "rsg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

        XCTAssertTrue(element("tab.recurrences", in: app).waitForExistence(timeout: 8))
        element("tab.recurrences", in: app).tap()
        let suggestion = element("recurrence.suggestion.\(suggestionID)", in: app)
        XCTAssertTrue(suggestion.waitForExistence(timeout: 10))
        element("recurrence.suggestion.review.\(suggestionID)", in: app).tap()

        XCTAssertTrue(suggestion.waitForNonExistence(timeout: 8))
        XCTAssertFalse(element("recurrence.review.screen", in: app).exists)
        XCTAssertTrue(element("recurrence.item.rec_ui_synthetic_active", in: app).exists)
    }

    @MainActor
    func testRealAPIRecurrenceSuggestionRequiresExplicitConfirmation() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        guard try testConfiguration().mode == .real else {
            throw XCTSkip("This scenario requires the official real API/PostgreSQL harness")
        }
        let fixture = try recurrenceSuggestionE2EFixture()
        let app = launched.app
        defer { app.terminate() }

        element("tab.recurrences", in: app).tap()
        let suggestion = app.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier BEGINSWITH %@ AND label CONTAINS %@",
                    "recurrence.suggestion.rsg_",
                    fixture.description
                )
            )
            .firstMatch
        XCTAssertTrue(suggestion.waitForExistence(timeout: 12), app.debugDescription)
        XCTAssertTrue(suggestion.label.contains("R$ 63,90"))
        XCTAssertTrue(suggestion.label.contains("3 ocorrências"))

        let existingBeforeConfirmation = app.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier BEGINSWITH %@ AND label CONTAINS %@",
                    "recurrence.item.",
                    fixture.description
                )
            )
            .firstMatch
        XCTAssertFalse(existingBeforeConfirmation.exists, "Suggestion created a Recurrence before confirmation")

        let suggestionID = String(suggestion.identifier.dropFirst("recurrence.suggestion.".count))
        element("recurrence.suggestion.review.\(suggestionID)", in: app).tap()
        XCTAssertTrue(element("recurrence.review.screen", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(element("recurrence.review.description", in: app).label.contains(fixture.description))
        XCTAssertTrue(element("recurrence.review.amount", in: app).label.contains("R$ 63,90"))
        XCTAssertTrue(
            element("recurrence.review.startsOn", in: app).label.contains(displayCivilDate(fixture.startsOn))
        )
        XCTAssertFalse(element("recurrence.success", in: app).exists)

        element("recurrence.confirm", in: app).tap()
        XCTAssertTrue(element("recurrence.success", in: app).waitForExistence(timeout: 12))
        element("recurrence.success.return", in: app).tap()
        XCTAssertTrue(suggestion.waitForNonExistence(timeout: 10))

        let confirmed = app.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier BEGINSWITH %@ AND label CONTAINS %@",
                    "recurrence.item.",
                    fixture.description
                )
            )
            .firstMatch
        XCTAssertTrue(confirmed.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertTrue(confirmed.label.contains("Ativa"))
    }

    @MainActor
    func testCreditCardPreviewConfirmDetailAndArchive() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }
        let name = creditCardName(from: launched.description)

        XCTAssertTrue(element("tab.cards", in: app).waitForExistence(timeout: 8))
        element("tab.cards", in: app).tap()
        XCTAssertTrue(element("card.empty", in: app).waitForExistence(timeout: 10), app.debugDescription)
        element("card.create", in: app).tap()
        fillCreditCardForm(in: app, name: name)
        element("card.review", in: app).tap()

        XCTAssertTrue(element("card.review.screen", in: app).waitForExistence(timeout: 10))
        XCTAssertFalse(element("card.success", in: app).exists, "Preview must not persist a card")
        element("card.confirm", in: app).tap()
        XCTAssertTrue(element("card.success", in: app).waitForExistence(timeout: 12))
        element("card.new", in: app).tap()
        XCTAssertTrue(element("card.list", in: app).waitForExistence(timeout: 12))

        let item = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "card.item.card_"))
            .firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 12), app.debugDescription)
        XCTAssertTrue(item.label.contains(name))
        XCTAssertTrue(item.label.contains("final 4821"))
        XCTAssertTrue(item.label.localizedCaseInsensitiveContains("ativo"))
        let cardID = String(item.identifier.dropFirst("card.item.".count))
        item.tap()

        XCTAssertTrue(element("card.detail", in: app).waitForExistence(timeout: 10))
        let archive = element("card.archive.\(cardID)", in: app)
        XCTAssertTrue(archive.waitForExistence(timeout: 8))
        archive.tap()
        XCTAssertTrue(element("card.archive.cancel", in: app).waitForExistence(timeout: 5))
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 5), app.debugDescription)
        let alertButtons = alert.buttons.matching(identifier: "card.archive.confirm")
        let confirm = alertButtons.element(boundBy: max(alertButtons.count - 1, 0))
        XCTAssertTrue(confirm.waitForExistence(timeout: 5), alert.debugDescription)
        confirm.tap()

        XCTAssertTrue(archive.waitForNonExistence(timeout: 12), app.debugDescription)
        XCTAssertTrue(element("card.detail", in: app).exists)
        app.navigationBars.buttons.firstMatch.tap()
        XCTAssertTrue(item.waitForExistence(timeout: 10))
        XCTAssertTrue(item.label.localizedCaseInsensitiveContains("arquivado"))

        if try testConfiguration().mode == .real {
            app.terminate()
            let relaunched = try launchApp().app
            element("tab.cards", in: relaunched).tap()
            let durable = relaunched.descendants(matching: .any)
                .matching(NSPredicate(format: "identifier == %@", "card.item.\(cardID)"))
                .firstMatch
            XCTAssertTrue(durable.waitForExistence(timeout: 12), relaunched.debugDescription)
            XCTAssertTrue(durable.label.localizedCaseInsensitiveContains("arquivado"))
            relaunched.terminate()
        }
    }

    @MainActor
    func testRealAPICreditCardPreviewStopsBeforeConfirmation() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        guard try testConfiguration().mode == .real else {
            throw XCTSkip("This scenario requires the official real API/PostgreSQL harness")
        }
        let app = launched.app
        defer { app.terminate() }
        element("tab.cards", in: app).tap()
        XCTAssertTrue(element("card.empty", in: app).waitForExistence(timeout: 10))
        element("card.create", in: app).tap()
        fillCreditCardForm(in: app, name: creditCardName(from: launched.description))
        element("card.review", in: app).tap()
        XCTAssertTrue(element("card.review.screen", in: app).waitForExistence(timeout: 10))
        XCTAssertFalse(element("card.success", in: app).exists)
    }

    @MainActor
    func testRealAPICardPurchaseAndInstallmentPlanLifecycle() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        guard try testConfiguration().mode == .real else {
            throw XCTSkip("This scenario requires the official real API/PostgreSQL harness")
        }
        let app = launched.app
        defer { app.terminate() }

        let cardName = "\(launched.description)_purchase_card"
        let oneTimePurchaseDescription = "\(launched.description)_purchase_one_time"
        let installmentPurchaseDescription = "\(launched.description)_purchase_installment"

        XCTAssertTrue(element("tab.cards", in: app).waitForExistence(timeout: 8))
        element("tab.cards", in: app).tap()
        let cardsContent = app.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier == %@ OR identifier == %@",
                    "card.empty",
                    "card.list"
                )
            )
            .firstMatch
        XCTAssertTrue(cardsContent.waitForExistence(timeout: 12), app.debugDescription)
        element("card.create", in: app).tap()
        fillCreditCardForm(in: app, name: cardName)
        element("card.review", in: app).tap()
        XCTAssertTrue(element("card.review.screen", in: app).waitForExistence(timeout: 10))
        element("card.confirm", in: app).tap()
        XCTAssertTrue(element("card.success", in: app).waitForExistence(timeout: 12))
        element("card.new", in: app).tap()
        XCTAssertTrue(element("card.list", in: app).waitForExistence(timeout: 12))

        let item = app.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier BEGINSWITH %@ AND label CONTAINS %@",
                    "card.item.card_",
                    cardName
                )
            )
            .firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 12), app.debugDescription)
        let cardID = String(item.identifier.dropFirst("card.item.".count))
        item.tap()
        XCTAssertTrue(element("card.detail", in: app).waitForExistence(timeout: 10))
        element("card.purchase.\(cardID)", in: app).tap()

        func createPurchase(in app: XCUIApplication, description: String, amount: String, installments: String?) {
            XCTAssertTrue(element("cardPurchase.form", in: app).waitForExistence(timeout: 10))
            app.textFields["cardPurchase.description"].tap()
            app.textFields["cardPurchase.description"].typeText(description)
            app.textFields["cardPurchase.amount"].tap()
            app.textFields["cardPurchase.amount"].typeText(amount)
            dismissKeyboard(in: app)
            if let installments {
                app.textFields["cardPurchase.installments"].tap()
                app.textFields["cardPurchase.installments"].typeText(installments)
                dismissKeyboard(in: app)
            }
            element("cardPurchase.review", in: app).tap()

            XCTAssertTrue(element("cardPurchase.review", in: app).waitForExistence(timeout: 12))
            if installments == nil {
                XCTAssertTrue(element("cardPurchase.review.mode", in: app).exists)
                XCTAssertFalse(element("cardPurchase.review.installments", in: app).exists)
            } else {
                XCTAssertTrue(element("cardPurchase.review.installments", in: app).exists)
            }
            XCTAssertFalse(element("cardPurchase.success", in: app).exists)
            element("cardPurchase.confirm", in: app).tap()
            XCTAssertTrue(element("cardPurchase.success", in: app).waitForExistence(timeout: 15))
            element("cardPurchase.done", in: app).tap()
        }

        createPurchase(in: app, description: oneTimePurchaseDescription, amount: "80,00", installments: nil)

        app.terminate()
        let secondApp = try launchApp().app
        defer { secondApp.terminate() }
        XCTAssertTrue(element("tab.cards", in: secondApp).waitForExistence(timeout: 8), secondApp.debugDescription)
        element("tab.cards", in: secondApp).tap()
        let cardAfterOneTime = secondApp.buttons["card.item.\(cardID)"].firstMatch
        XCTAssertTrue(cardAfterOneTime.waitForExistence(timeout: 12), secondApp.debugDescription)
        XCTAssertTrue(cardAfterOneTime.isHittable, secondApp.debugDescription)
        cardAfterOneTime.tap()
        XCTAssertTrue(element("card.detail", in: secondApp).waitForExistence(timeout: 10), secondApp.debugDescription)
        XCTAssertTrue(element("card.purchase.\(cardID)", in: secondApp).waitForExistence(timeout: 8))
        element("card.purchase.\(cardID)", in: secondApp).tap()
        createPurchase(in: secondApp, description: installmentPurchaseDescription, amount: "120,00", installments: "2")

        element("installmentPlans.open", in: secondApp).tap()
        XCTAssertTrue(element("installmentPlans.list", in: secondApp).waitForExistence(timeout: 12))
        let plan = secondApp.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "installmentPlan.item.ipl_"))
            .firstMatch
        XCTAssertTrue(plan.waitForExistence(timeout: 12), secondApp.debugDescription)
        let planID = String(plan.identifier.dropFirst("installmentPlan.item.".count))
        plan.tap()
        XCTAssertTrue(element("installmentPlan.detail", in: secondApp).waitForExistence(timeout: 12))

        element("tab.history", in: secondApp).tap()
        element("history.scheduledCommitments.entry", in: secondApp).tap()
        XCTAssertTrue(
            element("scheduledCommitments.screen", in: secondApp).waitForExistence(timeout: 10),
            secondApp.debugDescription
        )
        XCTAssertTrue(
            element("scheduledCommitments.list", in: secondApp).waitForExistence(timeout: 10),
            secondApp.debugDescription
        )
        let scheduledPlan = secondApp.descendants(matching: .any)
            .matching(
                NSPredicate(
                    format: "identifier BEGINSWITH %@",
                    "scheduledCommitment.INSTALLMENT_PLAN.\(planID)."
                )
            )
            .firstMatch
        XCTAssertTrue(scheduledPlan.waitForExistence(timeout: 10), secondApp.debugDescription)
        XCTAssertTrue(scheduledPlan.label.contains("Parcela"), secondApp.debugDescription)

        element("tab.cards", in: secondApp).tap()
        XCTAssertTrue(element("installmentPlan.detail", in: secondApp).waitForExistence(timeout: 10))
        element("installmentPlan.cancel.preview", in: secondApp).tap()
        let cancel = secondApp.alerts.firstMatch.buttons.matching(identifier: "installmentPlan.cancel.confirm").firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 8), secondApp.debugDescription)
        cancel.tap()
        XCTAssertTrue(element("installmentPlan.detail", in: secondApp).waitForExistence(timeout: 12))
        XCTAssertTrue(
            element("installmentPlan.cancel.preview", in: secondApp).waitForNonExistence(timeout: 10),
            secondApp.debugDescription
        )
        XCTAssertTrue(
            secondApp.staticTexts.matching(NSPredicate(format: "label CONTAINS[c] %@", "Cancelado")).firstMatch
                .waitForExistence(timeout: 8),
            secondApp.debugDescription
        )
    }

    @MainActor
    func testCreditCardFailureExposesSafeRetry() {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = "stub"
        app.launchEnvironment["JARVIS_IOS_CARD_SCENARIO"] = "listError"
        app.launch()
        defer { app.terminate() }
        element("tab.cards", in: app).tap()
        XCTAssertTrue(element("card.error", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(element("card.retry", in: app).exists)
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS[c] 'sql' OR label CONTAINS[c] 'pgx'")).firstMatch.exists)
    }

    @MainActor
    func testCardPurchaseReviewConfirmAndInstallmentPlanCancellation() throws {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = "stub"
        app.launch()
        defer { app.terminate() }

        XCTAssertTrue(element("tab.cards", in: app).waitForExistence(timeout: 8))
        element("tab.cards", in: app).tap()
        XCTAssertTrue(element("card.empty", in: app).waitForExistence(timeout: 10))
        element("card.create", in: app).tap()
        fillCreditCardForm(in: app, name: "Cartão compra 4B")
        element("card.review", in: app).tap()
        XCTAssertTrue(element("card.review.screen", in: app).waitForExistence(timeout: 10))
        element("card.confirm", in: app).tap()
        XCTAssertTrue(element("card.success", in: app).waitForExistence(timeout: 12))
        element("card.new", in: app).tap()
        XCTAssertTrue(element("card.list", in: app).waitForExistence(timeout: 12))

        let card = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "card.item.card_"))
            .firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10), app.debugDescription)
        let cardID = String(card.identifier.dropFirst("card.item.".count))
        card.tap()
        XCTAssertTrue(element("card.detail", in: app).waitForExistence(timeout: 10))
        let purchaseButton = element("card.purchase.\(cardID)", in: app)
        XCTAssertTrue(purchaseButton.waitForExistence(timeout: 8))
        purchaseButton.tap()

        XCTAssertTrue(element("cardPurchase.form", in: app).waitForExistence(timeout: 8))
        app.textFields["cardPurchase.description"].tap()
        app.textFields["cardPurchase.description"].typeText("Compra parcelada 4B")
        app.textFields["cardPurchase.amount"].tap()
        app.textFields["cardPurchase.amount"].typeText("120,00")
        dismissKeyboard(in: app)
        app.textFields["cardPurchase.installments"].tap()
        app.textFields["cardPurchase.installments"].typeText("2")
        dismissKeyboard(in: app)
        element("cardPurchase.review", in: app).tap()

        XCTAssertTrue(element("cardPurchase.review", in: app).waitForExistence(timeout: 10))
        XCTAssertFalse(element("cardPurchase.success", in: app).exists)
        XCTAssertTrue(element("cardPurchase.review.installments", in: app).exists)
        element("cardPurchase.confirm", in: app).tap()
        XCTAssertTrue(element("cardPurchase.success", in: app).waitForExistence(timeout: 12))
        element("cardPurchase.done", in: app).tap()

        element("installmentPlans.open", in: app).tap()
        XCTAssertTrue(element("installmentPlans.list", in: app).waitForExistence(timeout: 10))
        let plan = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "installmentPlan.item.ipl_"))
            .firstMatch
        XCTAssertTrue(plan.waitForExistence(timeout: 10), app.debugDescription)
        plan.tap()
        XCTAssertTrue(element("installmentPlan.detail", in: app).waitForExistence(timeout: 10))
        element("installmentPlan.cancel.preview", in: app).tap()
        let cancel = app.alerts.firstMatch.buttons.matching(identifier: "installmentPlan.cancel.confirm").firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 8), app.debugDescription)
        cancel.tap()
        XCTAssertTrue(element("installmentPlan.detail", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(
            app.staticTexts.matching(NSPredicate(format: "label CONTAINS[c] %@", "Cancelado")).firstMatch
                .waitForExistence(timeout: 8),
            app.debugDescription
        )
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
            "tab.cards",
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
        XCTAssertTrue(element("tab.cards", in: app).waitForExistence(timeout: 8))
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

            let cardsTab = element("tab.cards", in: app)
            XCTAssertTrue(cardsTab.waitForExistence(timeout: 5), "tab.cards disappeared in cycle \(cycle)")
            cardsTab.tap()
            XCTAssertTrue(element("card.create", in: app).waitForExistence(timeout: 8))

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
    func testScheduledCommitmentsListShowsInstallmentAndRecurrenceSources() throws {
        continueAfterFailure = false
        let launched = try launchApp()
        let app = launched.app
        defer { app.terminate() }

        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 8))
        element("tab.history", in: app).tap()
        let entry = element("history.scheduledCommitments.entry", in: app)
        XCTAssertTrue(entry.waitForExistence(timeout: 8), app.debugDescription)
        entry.tap()

        XCTAssertTrue(element("scheduledCommitments.screen", in: app).waitForExistence(timeout: 8), app.debugDescription)
        let installment = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "scheduledCommitment.INSTALLMENT_PLAN."))
            .firstMatch
        let recurrence = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "scheduledCommitment.RECURRENCE."))
            .firstMatch
        XCTAssertTrue(installment.waitForExistence(timeout: 8), app.debugDescription)
        XCTAssertTrue(recurrence.waitForExistence(timeout: 8), app.debugDescription)
        XCTAssertTrue(installment.label.contains("Parcela"))
        XCTAssertTrue(recurrence.label.contains("Recorrência"))
        XCTAssertTrue(installment.label.contains("R$"))
        XCTAssertTrue(recurrence.label.contains("R$"))
    }

    @MainActor
    func testScheduledCommitmentsEmptyStateIsAccessible() {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = "stub"
        app.launchEnvironment["JARVIS_IOS_SCHEDULED_COMMITMENTS_SCENARIO"] = "empty"
        app.launch()
        defer { app.terminate() }

        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 8))
        element("tab.history", in: app).tap()
        element("history.scheduledCommitments.entry", in: app).tap()
        XCTAssertTrue(element("scheduledCommitments.screen", in: app).waitForExistence(timeout: 8))
        XCTAssertTrue(element("scheduledCommitments.empty", in: app).waitForExistence(timeout: 8), app.debugDescription)
        XCTAssertFalse(element("scheduledCommitments.list", in: app).exists)
    }

    @MainActor
    func testScheduledCommitmentsFailureExposesSafeRetry() {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchEnvironment["JARVIS_IOS_API_MODE"] = "stub"
        app.launchEnvironment["JARVIS_IOS_SCHEDULED_COMMITMENTS_SCENARIO"] = "listError"
        app.launch()
        defer { app.terminate() }

        XCTAssertTrue(element("tab.history", in: app).waitForExistence(timeout: 8))
        element("tab.history", in: app).tap()
        element("history.scheduledCommitments.entry", in: app).tap()
        XCTAssertTrue(element("scheduledCommitments.error", in: app).waitForExistence(timeout: 8), app.debugDescription)
        XCTAssertTrue(element("scheduledCommitments.retry", in: app).waitForExistence(timeout: 8))
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS[c] 'sql' OR label CONTAINS[c] 'pgx'"))
            .firstMatch.exists)
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
        let suggestionID = "rsg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        XCTAssertTrue(reveal("recurrence.suggestion.\(suggestionID)", in: app))
        XCTAssertTrue(reveal("recurrence.suggestion.review.\(suggestionID)", in: app))
        XCTAssertTrue(reveal("recurrence.suggestion.dismiss.\(suggestionID)", in: app))
        XCTAssertTrue(reveal("recurrence.create", in: app))
        element("recurrence.create", in: app).tap()
        XCTAssertTrue(reveal("recurrence.description", in: app))
        fillRecurrenceForm(in: app, description: "Dynamic_Type_recorrencia_sintetica")
        XCTAssertTrue(reveal("recurrence.review", in: app))
        element("recurrence.review", in: app).tap()
        XCTAssertTrue(reveal("recurrence.confirm", in: app))
        XCTAssertTrue(element("recurrence.review.description", in: app).label.contains("Dynamic_Type_recorrencia_sintetica"))

        element("recurrence.confirm", in: app).tap()
        XCTAssertTrue(element("recurrence.success", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(reveal("recurrence.success.return", in: app))
        element("recurrence.success.return", in: app).tap()
        XCTAssertTrue(reveal("tab.cards", in: app))
        element("tab.cards", in: app).tap()
        XCTAssertTrue(reveal("card.create", in: app))
        element("card.create", in: app).tap()
        fillCreditCardForm(in: app, name: "Dynamic_Type_cartao_sintetico")
        XCTAssertTrue(reveal("card.review", in: app))
        element("card.review", in: app).tap()
        XCTAssertTrue(reveal("card.confirm", in: app))
        element("card.confirm", in: app).tap()

        XCTAssertTrue(element("card.success", in: app).waitForExistence(timeout: 10))
        XCTAssertTrue(reveal("card.new", in: app))
        element("card.new", in: app).tap()
        XCTAssertTrue(element("card.list", in: app).waitForExistence(timeout: 10))

        let cardItem = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier BEGINSWITH %@", "card.item."))
            .firstMatch
        XCTAssertTrue(cardItem.waitForExistence(timeout: 10), app.debugDescription)
        let cardItemIdentifier = cardItem.identifier
        XCTAssertTrue(reveal(cardItemIdentifier, in: app))
        let cardID = String(cardItemIdentifier.dropFirst("card.item.".count))
        element(cardItemIdentifier, in: app).tap()

        XCTAssertTrue(element("card.detail", in: app).waitForExistence(timeout: 10))
        let archiveIdentifier = "card.archive.\(cardID)"
        XCTAssertTrue(reveal(archiveIdentifier, in: app))
        element(archiveIdentifier, in: app).tap()

        let archiveAlert = app.alerts.firstMatch
        XCTAssertTrue(archiveAlert.waitForExistence(timeout: 5), app.debugDescription)
        let archiveConfirmQuery = archiveAlert.buttons.matching(identifier: "card.archive.confirm")
        let archiveCancelQuery = archiveAlert.buttons.matching(identifier: "card.archive.cancel")
        XCTAssertTrue(archiveConfirmQuery.firstMatch.waitForExistence(timeout: 5), app.debugDescription)
        XCTAssertTrue(archiveCancelQuery.firstMatch.waitForExistence(timeout: 5), app.debugDescription)
        let archiveConfirm = try XCTUnwrap(
            archiveConfirmQuery.allElementsBoundByAccessibilityElement.first(where: \.isHittable)
        )
        let archiveCancel = try XCTUnwrap(
            archiveCancelQuery.allElementsBoundByAccessibilityElement.first(where: \.isHittable)
        )
        XCTAssertTrue(archiveConfirm.isHittable)
        XCTAssertTrue(archiveCancel.isHittable)
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

    private func recurrenceSuggestionE2EFixture() throws -> (description: String, startsOn: String) {
        let info = try XCTUnwrap(Bundle(for: Self.self).infoDictionary)
        let description = try XCTUnwrap(info["JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION"] as? String)
        let startsOn = try XCTUnwrap(info["JARVIS_IOS_E2E_SUGGESTION_STARTS_ON"] as? String)
        guard !description.isEmpty, startsOn.range(of: #"^\d{4}-\d{2}-\d{2}$"#, options: .regularExpression) != nil else {
            XCTFail("The real suggestion E2E fixture was not configured")
            throw CocoaError(.fileReadCorruptFile)
        }
        return (description, startsOn)
    }

    private func creditCardName(from description: String) -> String {
        "\(description)_card"
    }

    private func displayCivilDate(_ canonical: String) -> String {
        let parts = canonical.split(separator: "-")
        guard parts.count == 3 else { return canonical }
        return "\(parts[2])/\(parts[1])/\(parts[0])"
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
    private func fillCreditCardForm(in app: XCUIApplication, name: String) {
        let nameField = app.textFields["card.name"]
        XCTAssertTrue(nameField.waitForExistence(timeout: 8))
        nameField.tap()
        nameField.typeText(name)

        let suffix = app.textFields["card.lastFour"]
        XCTAssertTrue(suffix.exists)
        suffix.tap()
        suffix.typeText("4821")
        dismissKeyboard(in: app)

        let brand = element("card.brand", in: app)
        XCTAssertTrue(brand.waitForExistence(timeout: 5))
        brand.tap()
        let visa = app.buttons["Visa"].firstMatch
        XCTAssertTrue(visa.waitForExistence(timeout: 5), app.debugDescription)
        visa.tap()

        let limit = app.textFields["card.creditLimit"]
        XCTAssertTrue(reveal("card.creditLimit", in: app))
        limit.tap()
        limit.typeText("2500,00")
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
            app.swipeUp()
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
