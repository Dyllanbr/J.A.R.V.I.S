import XCTest
@testable import JARVIS

@MainActor
final class RegistrationViewModelTests: XCTestCase {
    private let fixedNow = Date(timeIntervalSinceReferenceDate: 808_000_000)

    func testPreviewMustCompleteBeforeCreateAndConfirmationIsExplicit() async {
        let api = FinancialAPISpy()
        let model = makeModel(api: api)
        fillValidDraft(model)

        await model.confirm()
        XCTAssertTrue(api.createRequests.isEmpty)

        await model.review()
        XCTAssertEqual(api.previewRequests.count, 1)
        XCTAssertTrue(api.createRequests.isEmpty)
        guard case .reviewing = model.state else {
            return XCTFail("Expected the reviewed state")
        }

        await model.confirm()
        XCTAssertEqual(api.createRequests.count, 1)
        guard case .success = model.state else {
            return XCTFail("Expected success after explicit confirmation")
        }
    }

    func testCatalogIsSharedAndUncategorizedRemainsTheDefault() async {
        let api = FinancialAPISpy()
        let appModel = AppModel(api: api, now: fixedNow)

        XCTAssertNil(appModel.registration.selectedCategoryID)
        async let registerLoad: Void = appModel.registration.loadCategoriesIfNeeded()
        async let historyLoad: Void = appModel.history.loadCategoriesIfNeeded()
        _ = await (registerLoad, historyLoad)

        XCTAssertEqual(api.categoryRequestCount, 1)
        XCTAssertEqual(appModel.registration.availableCategories.map(\.id), ["expense.food", "expense.other"])
    }

    func testCatalogLoadSurvivesInitiatingCallerCancellation() async {
        let api = SuspendedCategoryAPI()
        let catalog = CategoryCatalogModel(api: api)

        let initiatingCaller = Task { await catalog.loadIfNeeded() }
        await api.waitForCategoryRequest()
        XCTAssertEqual(api.categoryRequestCount, 1)

        var secondCallerCompleted = false
        let secondCallerEntered = MainActorTestSignal()
        let secondCaller = Task {
            secondCallerEntered.signal()
            await catalog.loadIfNeeded()
            secondCallerCompleted = true
        }
        await secondCallerEntered.wait()

        XCTAssertEqual(api.categoryRequestCount, 1)
        XCTAssertFalse(secondCallerCompleted, "A concurrent caller must await the shared catalog fetch")

        initiatingCaller.cancel()
        await Task.yield()

        XCTAssertEqual(api.categoryCancellationCount, 0, "A waiting View must not own or cancel the shared fetch")
        XCTAssertTrue(api.resolveCategories(syntheticCategories))
        await initiatingCaller.value
        await secondCaller.value

        XCTAssertEqual(catalog.state, .loaded(syntheticCategories))
        XCTAssertEqual(catalog.definitions, syntheticCategories)
        XCTAssertTrue(secondCallerCompleted)
        XCTAssertEqual(api.categoryRequestCount, 1)
        XCTAssertEqual(api.categoryCancellationCount, 0)
    }

    func testCatalogFailureStillAllowsSafeUncategorizedPreviewAndRetry() async {
        let api = FinancialAPISpy()
        api.categoriesResult = .failure(FinancialAPIError.serviceUnavailable)
        let model = makeModel(api: api)

        await model.loadCategoriesIfNeeded()
        guard case .failed = model.categoryCatalogState else {
            return XCTFail("Expected a visible catalog failure")
        }
        XCTAssertNil(model.selectedCategoryID)
        fillValidDraft(model)
        await model.review()

        XCTAssertNil(api.previewRequests.first?.categoryID)
        XCTAssertNotNil(model.reviewedExpense)

        model.edit()
        api.categoriesResult = .success(syntheticCategories)
        await model.retryCategories()
        XCTAssertEqual(model.availableCategories.map(\.id), ["expense.food", "expense.other"])
    }

    func testCategoryOptionsFollowTypeAndTypeSwitchClearsWithoutMappingOther() {
        let api = FinancialAPISpy()
        let model = makeModel(api: api, categories: makeCatalog(api: api))

        XCTAssertEqual(model.availableCategories.map(\.id), ["expense.food", "expense.other"])
        model.selectCategory("expense.other")
        XCTAssertEqual(model.selectedCategoryID, "expense.other")

        model.selectTransactionType(.income)
        XCTAssertNil(model.selectedCategoryID)
        XCTAssertEqual(model.availableCategories.map(\.id), ["income.salary", "income.other"])

        model.selectCategory("income.other")
        model.selectTransactionType(.expense)
        XCTAssertNil(model.selectedCategoryID)
        XCTAssertEqual(model.selectedCategoryDisplayName, "Sem categoria")
    }

    func testPreviewFreezesBackendCategoryAndRetryKeepsCategoryAndIdempotencyKey() async {
        let api = FinancialAPISpy()
        api.previewResult = .success(syntheticPreview(categoryID: "expense.food"))
        api.createResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(
                RecordedExpense(
                    expense: syntheticExpense(categoryID: "expense.food"),
                    replayed: true
                )
            )
        ]
        let model = makeModel(
            api: api,
            categories: makeCatalog(api: api),
            makeKey: { "categorized-attempt" }
        )
        fillValidDraft(model)
        model.selectCategory("expense.food")

        await model.review()

        XCTAssertEqual(api.previewRequests.first?.categoryID, "expense.food")
        XCTAssertEqual(model.reviewedExpense?.preview.categoryID, "expense.food")
        XCTAssertEqual(model.reviewedExpense?.request.categoryID, "expense.food")
        XCTAssertEqual(model.reviewedExpense?.categoryDisplayName, "Alimentação")

        await model.confirm()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.request.categoryID), ["expense.food", "expense.food"])
        XCTAssertEqual(api.createRequests.map(\.key), ["categorized-attempt", "categorized-attempt"])
    }

    func testEditingCategoryAfterRetryStartsANewLogicalAttempt() async {
        let api = FinancialAPISpy()
        api.previewResult = .success(syntheticPreview(categoryID: "expense.food"))
        api.createResults = [.failure(FinancialAPIError.connectionUnavailable)]
        var keys = ["food-attempt", "other-attempt"]
        let model = makeModel(
            api: api,
            categories: makeCatalog(api: api),
            makeKey: { keys.removeFirst() }
        )
        fillValidDraft(model)
        model.selectCategory("expense.food")
        await model.review()
        await model.confirm()

        model.edit()
        model.selectCategory("expense.other")
        api.previewResult = .success(syntheticPreview(categoryID: "expense.other"))
        api.createResults = [
            .success(
                RecordedExpense(
                    expense: syntheticExpense(categoryID: "expense.other"),
                    replayed: false
                )
            )
        ]
        await model.review()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["food-attempt", "other-attempt"])
        XCTAssertEqual(api.createRequests.map(\.request.categoryID), ["expense.food", "expense.other"])
    }

    func testIncomePreviewAndConfirmUseBackendValidatedCategory() async {
        let api = FinancialAPISpy()
        api.incomePreviewResult = .success(syntheticIncomePreview(categoryID: "income.salary"))
        api.incomeCreateResults = [
            .success(
                RecordedIncome(
                    income: syntheticIncome(categoryID: "income.salary"),
                    replayed: false
                )
            )
        ]
        let model = makeModel(api: api, categories: makeCatalog(api: api))
        model.selectTransactionType(.income)
        model.selectCategory("income.salary")
        fillValidDraft(model, description: "Receita sintética", amount: "85,00")

        await model.review()
        XCTAssertEqual(model.reviewedIncome?.categoryDisplayName, "Salário")
        XCTAssertEqual(model.reviewedIncome?.request.categoryID, "income.salary")
        await model.confirm()

        XCTAssertEqual(api.incomeCreateRequests.first?.request.categoryID, "income.salary")
        XCTAssertNotNil(model.successfulIncome)
    }

    func testChangingCategoryDuringSuspendedPreviewDiscardsStaleResponse() async {
        let api = SuspendedPreviewAPI()
        let model = makeModel(api: api, categories: makeCatalog(api: api))
        fillValidDraft(model)
        model.selectCategory("expense.food")

        let previewTask = Task { await model.review() }
        await api.waitForExpensePreviewCall()
        model.selectCategory("expense.other")
        api.resolveExpensePreview(syntheticPreview(categoryID: "expense.food"))
        await previewTask.value

        XCTAssertEqual(api.expensePreviewRequests.first?.categoryID, "expense.food")
        XCTAssertEqual(model.selectedCategoryID, "expense.other")
        XCTAssertEqual(model.state, .editing)
        XCTAssertNil(model.reviewedTransaction)
        XCTAssertNil(model.errorMessage)
    }

    func testTypeSwitchDuringCategorizedPreviewClearsCategoryAndDiscardsResponse() async {
        let api = SuspendedPreviewAPI()
        let model = makeModel(api: api, categories: makeCatalog(api: api))
        fillValidDraft(model)
        model.selectCategory("expense.food")

        let previewTask = Task { await model.review() }
        await api.waitForExpensePreviewCall()
        model.selectTransactionType(.income)
        api.resolveExpensePreview(syntheticPreview(categoryID: "expense.food"))
        await previewTask.value

        XCTAssertEqual(model.transactionType, .income)
        XCTAssertNil(model.selectedCategoryID)
        XCTAssertEqual(model.state, .editing)
        XCTAssertNil(model.reviewedTransaction)
    }

    func testExpenseStalePreviewIsDiscardedWhenDraftChangesWhileSuspended() async {
        let api = SuspendedPreviewAPI()
        var generatedKeyCount = 0
        let model = makeModel(api: api, makeKey: {
            generatedKeyCount += 1
            return "unexpected-key"
        })
        fillValidDraft(model, description: "Despesa A", amount: "42,50")

        let previewTask = Task { await model.review() }
        await api.waitForExpensePreviewCall()
        model.description = "Despesa B"
        model.amountText = "99,99"
        api.resolveExpensePreview(syntheticPreview(description: "Despesa A", amount: 4_250))
        await previewTask.value

        XCTAssertEqual(model.state, .editing)
        XCTAssertEqual(model.description, "Despesa B")
        XCTAssertEqual(model.amountText, "99,99")
        XCTAssertNil(model.reviewedTransaction)
        XCTAssertNil(model.errorMessage)

        await model.confirm()
        XCTAssertTrue(api.expenseCreateRequests.isEmpty)
        XCTAssertEqual(generatedKeyCount, 0)
    }

    func testIncomeStalePreviewIsDiscardedWhenDraftChangesWhileSuspended() async {
        let api = SuspendedPreviewAPI()
        var generatedKeyCount = 0
        let model = makeModel(api: api, makeKey: {
            generatedKeyCount += 1
            return "unexpected-key"
        })
        model.selectTransactionType(.income)
        fillValidDraft(model, description: "Receita A", amount: "85,00")

        let previewTask = Task { await model.review() }
        await api.waitForIncomePreviewCall()
        model.description = "Receita B"
        model.amountText = "125,00"
        api.resolveIncomePreview(syntheticIncomePreview(description: "Receita A", amount: 8_500))
        await previewTask.value

        XCTAssertEqual(model.state, .editing)
        XCTAssertEqual(model.transactionType, .income)
        XCTAssertEqual(model.description, "Receita B")
        XCTAssertEqual(model.amountText, "125,00")
        XCTAssertNil(model.reviewedTransaction)
        XCTAssertNil(model.errorMessage)

        await model.confirm()
        XCTAssertTrue(api.incomeCreateRequests.isEmpty)
        XCTAssertEqual(generatedKeyCount, 0)
    }

    func testTypeSwitchDuringPreviewDiscardsOldResponseAndRequiresPreviewForNewType() async {
        let api = SuspendedPreviewAPI()
        let model = makeModel(api: api)
        fillValidDraft(model, description: "Despesa A", amount: "42,50")

        let expenseTask = Task { await model.review() }
        await api.waitForExpensePreviewCall()
        model.selectTransactionType(.income)
        model.description = "Receita B"
        model.amountText = "85,00"
        api.resolveExpensePreview(syntheticPreview(description: "Despesa A", amount: 4_250))
        await expenseTask.value

        XCTAssertEqual(model.state, .editing)
        XCTAssertEqual(model.transactionType, .income)
        XCTAssertEqual(model.description, "Receita B")
        XCTAssertNil(model.reviewedTransaction)

        let incomeTask = Task { await model.review() }
        await api.waitForIncomePreviewCall()
        api.resolveIncomePreview(syntheticIncomePreview(description: "Receita B", amount: 8_500))
        await incomeTask.value

        guard case let .reviewing(.income(reviewed)) = model.state else {
            return XCTFail("The new Income draft must require and receive its own Preview")
        }
        XCTAssertEqual(reviewed.request.description, "Receita B")
        XCTAssertEqual(reviewed.request.amount.minor, 8_500)
    }

    func testUnchangedSuspendedPreviewStillInstallsReview() async {
        let api = SuspendedPreviewAPI()
        let model = makeModel(api: api)
        fillValidDraft(model, description: "Despesa estável", amount: "42,50")

        let previewTask = Task { await model.review() }
        await api.waitForExpensePreviewCall()
        api.resolveExpensePreview(syntheticPreview(description: "Despesa estável", amount: 4_250))
        await previewTask.value

        guard case let .reviewing(.expense(reviewed)) = model.state else {
            return XCTFail("An unchanged draft must install its valid Review")
        }
        XCTAssertEqual(reviewed.request.description, "Despesa estável")
        XCTAssertEqual(reviewed.request.amount.minor, 4_250)
    }

    func testExpensePaymentMethodAndOccurredAtChangesEachDiscardSuspendedPreview() async {
        for mutation in ["paymentMethod", "occurredAt"] {
            let api = SuspendedPreviewAPI()
            let model = makeModel(api: api)
            fillValidDraft(model, description: "Despesa A", amount: "42,50")

            let previewTask = Task { await model.review() }
            await api.waitForExpensePreviewCall()
            switch mutation {
            case "paymentMethod":
                model.paymentMethod = .credit
            case "occurredAt":
                model.occurredAt = fixedNow.addingTimeInterval(3_600)
            default:
                XCTFail("Unsupported test mutation")
            }
            api.resolveExpensePreview(syntheticPreview(description: "Despesa A", amount: 4_250))
            await previewTask.value

            XCTAssertEqual(model.state, .editing, "\(mutation) must invalidate the suspended Preview")
            XCTAssertNil(model.reviewedTransaction)
            XCTAssertNil(model.errorMessage)
        }
    }

    func testStalePreviewFailureDoesNotReplaceCurrentDraftWithAnError() async {
        let api = SuspendedPreviewAPI()
        let model = makeModel(api: api)
        fillValidDraft(model, description: "Despesa A", amount: "42,50")

        let previewTask = Task { await model.review() }
        await api.waitForExpensePreviewCall()
        model.description = "Despesa B"
        api.rejectExpensePreview(FinancialAPIError.serviceUnavailable)
        await previewTask.value

        XCTAssertEqual(model.state, .editing)
        XCTAssertEqual(model.description, "Despesa B")
        XCTAssertNil(model.reviewedTransaction)
        XCTAssertNil(model.errorMessage)
    }

    func testPreviewFreezesServerCanonicalFieldsForConfirmation() async {
        let api = FinancialAPISpy()
        api.previewResult = .success(syntheticPreview(description: "Mercado sintético", amount: 4_250))
        let model = makeModel(api: api)
        fillValidDraft(model)
        model.description = "  Mercado sintético  "

        await model.review()
        XCTAssertEqual(model.reviewedExpense?.request.description, "Mercado sintético")
        XCTAssertEqual(model.reviewedExpense?.request.amount.minor, 4_250)
        await model.confirm()

        let request = try? XCTUnwrap(api.createRequests.first?.request)
        XCTAssertEqual(request?.description, "Mercado sintético")
        XCTAssertEqual(request?.amount.minor, 4_250)
    }

    func testEditingInvalidatesReviewAndPendingOperation() async {
        let api = FinancialAPISpy()
        api.createResults = [.failure(FinancialAPIError.connectionUnavailable)]
        var keys = ["key-first", "key-second"]
        let model = makeModel(api: api, makeKey: { keys.removeFirst() })
        fillValidDraft(model)

        await model.review()
        await model.confirm()
        guard case .retryable = model.state else {
            return XCTFail("Expected retryable state")
        }

        model.edit()
        guard case .editing = model.state else {
            return XCTFail("Expected editing state")
        }

        api.previewResult = .success(syntheticPreview(description: "Transporte teste"))
        api.createResults = [.success(RecordedExpense(expense: syntheticExpense(), replayed: false))]
        model.description = "Transporte teste"
        await model.review()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["key-first", "key-second"])
    }

    func testRetryReusesIdempotencyKeyAndSuccessClearsIt() async {
        let api = FinancialAPISpy()
        api.createResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(RecordedExpense(expense: syntheticExpense(), replayed: true)),
            .success(RecordedExpense(expense: syntheticExpense(id: "exp_test_ios_002"), replayed: false))
        ]
        var keys = ["key-first", "key-second"]
        var refreshes = 0
        let model = makeModel(
            api: api,
            makeKey: { keys.removeFirst() },
            onRecorded: { refreshes += 1 }
        )
        fillValidDraft(model)

        await model.review()
        await model.confirm()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["key-first", "key-first"])
        XCTAssertEqual(refreshes, 1)

        model.startNewExpense(now: fixedNow)
        fillValidDraft(model)
        await model.review()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["key-first", "key-first", "key-second"])
        XCTAssertEqual(refreshes, 2)
    }

    func testPreviewAndDeterministicCreateFailuresExposeSafeMessagesAndRequireEditing() async {
        let api = FinancialAPISpy()
        api.previewResult = .failure(FinancialAPIError.serviceUnavailable)
        let model = makeModel(api: api)
        fillValidDraft(model)

        await model.review()
        guard case .editing = model.state else {
            return XCTFail("Preview failure must return to editing")
        }
        XCTAssertEqual(model.errorMessage, FinancialAPIError.serviceUnavailable.userMessage)

        api.previewResult = .success(syntheticPreview())
        api.createResults = [.failure(FinancialAPIError.conflict)]
        await model.review()
        await model.confirm()
        guard case .requiresEditing = model.state else {
            return XCTFail("A deterministic conflict must require editing")
        }
        XCTAssertEqual(model.errorMessage, FinancialAPIError.conflict.userMessage)

        await model.confirm()
        XCTAssertEqual(api.createRequests.count, 1, "A deterministic error cannot retry the same operation")
    }

    func testInvalidRequestRequiresEditingInsteadOfRetrying() async {
        let api = FinancialAPISpy()
        api.createResults = [.failure(FinancialAPIError.invalidData)]
        let model = makeModel(api: api)
        fillValidDraft(model)

        await model.review()
        await model.confirm()

        guard case .requiresEditing = model.state else {
            return XCTFail("An invalid request must require editing")
        }
        await model.confirm()
        XCTAssertEqual(api.createRequests.count, 1)
    }

    func testServiceUnavailableRetryReusesTheSameIdempotencyKey() async {
        let api = FinancialAPISpy()
        api.createResults = [
            .failure(FinancialAPIError.serviceUnavailable),
            .success(RecordedExpense(expense: syntheticExpense(), replayed: true))
        ]
        let model = makeModel(api: api, makeKey: { "key-service-unavailable" })
        fillValidDraft(model)

        await model.review()
        await model.confirm()
        guard case .retryable = model.state else {
            return XCTFail("A service failure must remain retryable")
        }
        await model.confirm()

        XCTAssertEqual(
            api.createRequests.map(\.key),
            ["key-service-unavailable", "key-service-unavailable"]
        )
    }

    func testDefaultsToExpenseAndSwitchingTypeResetsPaymentMethodAndReview() async {
        let api = FinancialAPISpy()
        let model = makeModel(api: api)
        fillValidDraft(model)
        model.paymentMethod = .credit

        await model.review()
        XCTAssertNotNil(model.reviewedExpense)

        model.selectTransactionType(.income)

        XCTAssertEqual(model.transactionType, .income)
        XCTAssertEqual(model.paymentMethod, .pix)
        XCTAssertEqual(model.state, .editing)
        XCTAssertNil(model.reviewedTransaction)

        model.selectTransactionType(.expense)
        XCTAssertEqual(model.transactionType, .expense)
        XCTAssertEqual(model.paymentMethod, .pix)
    }

    func testIncomePreviewIsFrozenAndConfirmationOmitsPaymentMethodByConstruction() async {
        let api = FinancialAPISpy()
        api.incomePreviewResult = .success(
            syntheticIncomePreview(description: "Receita canônica", amount: 12_345)
        )
        let model = makeModel(api: api)
        model.selectTransactionType(.income)
        fillValidDraft(model, description: "  Receita canônica  ", amount: "123,45")

        await model.review()
        XCTAssertEqual(api.incomePreviewRequests.count, 1)
        XCTAssertTrue(api.previewRequests.isEmpty)
        XCTAssertEqual(model.reviewedIncome?.preview.description, "Receita canônica")
        XCTAssertEqual(model.reviewedIncome?.request.description, "Receita canônica")
        XCTAssertEqual(model.reviewedIncome?.request.amount.minor, 12_345)
        await model.confirm()

        let request = try? XCTUnwrap(api.incomeCreateRequests.first?.request)
        XCTAssertEqual(request?.type, .income)
        XCTAssertEqual(request?.description, "Receita canônica")
        XCTAssertEqual(request?.amount.minor, 12_345)
        XCTAssertNotNil(model.successfulIncome)
        XCTAssertNil(model.successfulExpense)
    }

    func testChangingTypeAfterRetryCreatesANewLogicalAttempt() async {
        let api = FinancialAPISpy()
        api.createResults = [.failure(FinancialAPIError.connectionUnavailable)]
        api.incomeCreateResults = [
            .success(RecordedIncome(income: syntheticIncome(), replayed: false))
        ]
        var keys = ["expense-attempt", "income-attempt"]
        let model = makeModel(api: api, makeKey: { keys.removeFirst() })
        fillValidDraft(model)

        await model.review()
        await model.confirm()
        guard case .retryable = model.state else {
            return XCTFail("Expected retryable Expense")
        }

        model.selectTransactionType(.income)
        fillValidDraft(model, description: "Receita sintética", amount: "85,00")
        await model.review()
        await model.confirm()

        XCTAssertEqual(api.createRequests.map(\.key), ["expense-attempt"])
        XCTAssertEqual(api.incomeCreateRequests.map(\.key), ["income-attempt"])
    }

    func testIncomeRetryAndDeterministicErrorsFollowExistingPolicy() async {
        let api = FinancialAPISpy()
        api.incomeCreateResults = [
            .failure(FinancialAPIError.connectionUnavailable),
            .success(RecordedIncome(income: syntheticIncome(), replayed: true))
        ]
        let model = makeModel(api: api, makeKey: { "income-retry-key" })
        model.selectTransactionType(.income)
        fillValidDraft(model, description: "Receita sintética", amount: "85,00")

        await model.review()
        await model.confirm()
        guard case .retryable = model.state else {
            return XCTFail("A network failure must remain retryable")
        }
        await model.confirm()

        XCTAssertEqual(api.incomeCreateRequests.map(\.key), ["income-retry-key", "income-retry-key"])

        model.startNewIncome(now: fixedNow)
        fillValidDraft(model, description: "Outra receita", amount: "10,00")
        api.incomeCreateResults = [.failure(FinancialAPIError.invalidData)]
        await model.review()
        await model.confirm()

        guard case .requiresEditing = model.state else {
            return XCTFail("An invalid Income must require editing")
        }
        XCTAssertEqual(model.errorMessage, FinancialAPIError.invalidData.userMessage)
    }

    func testIncomeCancellationDoesNotBecomeAVisibleFailureAndRetryKeepsTheKey() async {
        let api = FinancialAPISpy()
        let model = makeModel(api: api, makeKey: { "income-cancelled-key" })
        model.selectTransactionType(.income)
        fillValidDraft(model, description: "Receita cancelada sintética", amount: "85,00")
        api.incomePreviewResult = .failure(CancellationError())

        await model.review()

        XCTAssertEqual(model.state, .editing)
        XCTAssertNil(model.errorMessage)

        api.incomePreviewResult = .success(syntheticIncomePreview())
        api.incomeCreateResults = [
            .failure(CancellationError()),
            .success(RecordedIncome(income: syntheticIncome(), replayed: true))
        ]
        await model.review()
        await model.confirm()

        guard case .retryable = model.state else {
            return XCTFail("A cancelled confirmation must remain retryable")
        }
        XCTAssertNil(model.errorMessage)

        await model.confirm()
        XCTAssertEqual(api.incomeCreateRequests.map(\.key), ["income-cancelled-key", "income-cancelled-key"])
    }

    func testIncomeConflictRequiresEditingWithoutRetryingTheAttempt() async {
        let api = FinancialAPISpy()
        api.incomeCreateResults = [.failure(FinancialAPIError.conflict)]
        let model = makeModel(api: api)
        model.selectTransactionType(.income)
        fillValidDraft(model, description: "Receita em conflito sintética", amount: "85,00")

        await model.review()
        await model.confirm()

        guard case .requiresEditing = model.state else {
            return XCTFail("An Income conflict must require editing")
        }
        XCTAssertEqual(model.errorMessage, FinancialAPIError.conflict.userMessage)
        await model.confirm()
        XCTAssertEqual(api.incomeCreateRequests.count, 1)
    }

    private func makeModel(
        api: any FinancialAPI,
        categories: CategoryCatalogModel? = nil,
        makeKey: @escaping () -> String = { "key-synthetic" },
        onRecorded: @escaping () -> Void = {}
    ) -> RegistrationViewModel {
        RegistrationViewModel(
            api: api,
            categories: categories,
            now: fixedNow,
            makeIdempotencyKey: makeKey,
            onTransactionRecorded: onRecorded
        )
    }

    private func makeCatalog(api: any FinancialAPI) -> CategoryCatalogModel {
        CategoryCatalogModel(api: api, definitions: syntheticCategories)
    }

    private func fillValidDraft(
        _ model: RegistrationViewModel,
        description: String = "Mercado sintético",
        amount: String = "42,50"
    ) {
        model.description = description
        model.amountText = amount
        model.paymentMethod = .pix
        model.occurredAt = fixedNow
    }
}

@MainActor
private final class SuspendedPreviewAPI: FinancialAPI {
    private var expensePreviewContinuation: CheckedContinuation<ExpensePreview, Error>?
    private var incomePreviewContinuation: CheckedContinuation<IncomePreview, Error>?
    private var expensePreviewCallWaiter: CheckedContinuation<Void, Never>?
    private var incomePreviewCallWaiter: CheckedContinuation<Void, Never>?

    private(set) var expenseCreateRequests: [(request: ExpenseRequest, key: String)] = []
    private(set) var incomeCreateRequests: [(request: IncomeRequest, key: String)] = []
    private(set) var expensePreviewRequests: [ExpenseRequest] = []
    private(set) var incomePreviewRequests: [IncomeRequest] = []

    func categories() async throws -> [CategoryDefinition] {
        syntheticCategories
    }

    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview {
        expensePreviewRequests.append(request)
        return try await withCheckedThrowingContinuation { continuation in
            precondition(expensePreviewContinuation == nil, "Only one suspended Expense Preview is supported")
            expensePreviewContinuation = continuation
            expensePreviewCallWaiter?.resume()
            expensePreviewCallWaiter = nil
        }
    }

    func preview(_ request: IncomeRequest) async throws -> IncomePreview {
        incomePreviewRequests.append(request)
        return try await withCheckedThrowingContinuation { continuation in
            precondition(incomePreviewContinuation == nil, "Only one suspended Income Preview is supported")
            incomePreviewContinuation = continuation
            incomePreviewCallWaiter?.resume()
            incomePreviewCallWaiter = nil
        }
    }

    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense {
        expenseCreateRequests.append((request, idempotencyKey))
        return RecordedExpense(expense: syntheticExpense(), replayed: false)
    }

    func create(_ request: IncomeRequest, idempotencyKey: String) async throws -> RecordedIncome {
        incomeCreateRequests.append((request, idempotencyKey))
        return RecordedIncome(income: syntheticIncome(), replayed: false)
    }

    func transactions(month: String) async throws -> TransactionMonth {
        TransactionMonth(month: month, items: [])
    }

    func previewRecurrence(_: RecurrenceRequest) async throws -> RecurrencePreview {
        throw FinancialAPIError.invalidResponse
    }

    func createRecurrence(_: RecurrenceRequest, idempotencyKey _: String) async throws -> RecordedRecurrence {
        throw FinancialAPIError.invalidResponse
    }

    func recurrences() async throws -> RecurrenceList {
        throw FinancialAPIError.invalidResponse
    }

    func cancelRecurrence(id _: String, idempotencyKey _: String) async throws -> RecordedRecurrence {
        throw FinancialAPIError.invalidResponse
    }

    func waitForExpensePreviewCall() async {
        guard expensePreviewContinuation == nil else { return }
        await withCheckedContinuation { expensePreviewCallWaiter = $0 }
    }

    func waitForIncomePreviewCall() async {
        guard incomePreviewContinuation == nil else { return }
        await withCheckedContinuation { incomePreviewCallWaiter = $0 }
    }

    func resolveExpensePreview(_ preview: ExpensePreview) {
        guard let continuation = expensePreviewContinuation else {
            preconditionFailure("Expense Preview was not suspended")
        }
        expensePreviewContinuation = nil
        continuation.resume(returning: preview)
    }

    func resolveIncomePreview(_ preview: IncomePreview) {
        guard let continuation = incomePreviewContinuation else {
            preconditionFailure("Income Preview was not suspended")
        }
        incomePreviewContinuation = nil
        continuation.resume(returning: preview)
    }

    func rejectExpensePreview(_ error: Error) {
        guard let continuation = expensePreviewContinuation else {
            preconditionFailure("Expense Preview was not suspended")
        }
        expensePreviewContinuation = nil
        continuation.resume(throwing: error)
    }
}

@MainActor
private final class SuspendedCategoryAPI: FinancialAPI {
    private var categoryContinuation: CheckedContinuation<[CategoryDefinition], Error>?
    private var categoryRequestWaiter: CheckedContinuation<Void, Never>?

    private(set) var categoryRequestCount = 0
    private(set) var categoryCancellationCount = 0

    func categories() async throws -> [CategoryDefinition] {
        categoryRequestCount += 1
        return try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                precondition(categoryContinuation == nil, "Only one suspended Category request is supported")
                categoryContinuation = continuation
                categoryRequestWaiter?.resume()
                categoryRequestWaiter = nil
            }
        } onCancel: { [weak self] in
            Task { @MainActor in
                self?.cancelCategoryRequest()
            }
        }
    }

    func preview(_: ExpenseRequest) async throws -> ExpensePreview {
        throw FinancialAPIError.invalidResponse
    }

    func preview(_: IncomeRequest) async throws -> IncomePreview {
        throw FinancialAPIError.invalidResponse
    }

    func create(_: ExpenseRequest, idempotencyKey _: String) async throws -> RecordedExpense {
        throw FinancialAPIError.invalidResponse
    }

    func create(_: IncomeRequest, idempotencyKey _: String) async throws -> RecordedIncome {
        throw FinancialAPIError.invalidResponse
    }

    func transactions(month _: String) async throws -> TransactionMonth {
        throw FinancialAPIError.invalidResponse
    }

    func previewRecurrence(_: RecurrenceRequest) async throws -> RecurrencePreview {
        throw FinancialAPIError.invalidResponse
    }

    func createRecurrence(_: RecurrenceRequest, idempotencyKey _: String) async throws -> RecordedRecurrence {
        throw FinancialAPIError.invalidResponse
    }

    func recurrences() async throws -> RecurrenceList {
        throw FinancialAPIError.invalidResponse
    }

    func cancelRecurrence(id _: String, idempotencyKey _: String) async throws -> RecordedRecurrence {
        throw FinancialAPIError.invalidResponse
    }

    func waitForCategoryRequest() async {
        guard categoryContinuation == nil else { return }
        await withCheckedContinuation { categoryRequestWaiter = $0 }
    }

    func resolveCategories(_ definitions: [CategoryDefinition]) -> Bool {
        guard let continuation = categoryContinuation else { return false }
        categoryContinuation = nil
        continuation.resume(returning: definitions)
        return true
    }

    private func cancelCategoryRequest() {
        categoryCancellationCount += 1
        guard let continuation = categoryContinuation else { return }
        categoryContinuation = nil
        continuation.resume(throwing: CancellationError())
    }
}

@MainActor
private final class MainActorTestSignal {
    private var isSignaled = false
    private var waiter: CheckedContinuation<Void, Never>?

    func signal() {
        precondition(!isSignaled, "The test signal may only be delivered once")
        isSignaled = true
        waiter?.resume()
        waiter = nil
    }

    func wait() async {
        guard !isSignaled else { return }
        await withCheckedContinuation { continuation in
            precondition(waiter == nil, "The test signal supports only one waiter")
            waiter = continuation
        }
    }
}
