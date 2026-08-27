import Foundation
import Observation

struct ReviewedExpense: Equatable, Sendable {
    let preview: ExpensePreview
    let request: ExpenseRequest
    let categoryDisplayName: String
}

struct ReviewedIncome: Equatable, Sendable {
    let preview: IncomePreview
    let request: IncomeRequest
    let categoryDisplayName: String
}

enum ReviewedTransaction: Equatable, Sendable {
    case expense(ReviewedExpense)
    case income(ReviewedIncome)

    var type: TransactionType {
        switch self {
        case .expense: .expense
        case .income: .income
        }
    }
}

enum RegistrationState: Equatable {
    case editing
    case previewing
    case reviewing(ReviewedTransaction)
    case submitting(ReviewedTransaction)
    case retryable(ReviewedTransaction)
    case requiresEditing(ReviewedTransaction)
    case success(FinancialTransaction)
}

@MainActor
@Observable
final class RegistrationViewModel {
    private(set) var transactionType: TransactionType = .expense
    private(set) var selectedCategoryID: String?
    var description = "" {
        didSet {
            guard description != oldValue else { return }
            draftDidChange()
        }
    }
    var amountText = "" {
        didSet {
            guard amountText != oldValue else { return }
            draftDidChange()
        }
    }
    var paymentMethod: PaymentMethod = .pix {
        didSet {
            guard paymentMethod != oldValue, transactionType == .expense else { return }
            draftDidChange()
        }
    }
    var occurredAt: Date {
        didSet {
            guard occurredAt != oldValue else { return }
            draftDidChange()
        }
    }
    private(set) var state: RegistrationState = .editing
    private(set) var errorMessage: String?

    private let api: any FinancialAPI
    private let categories: CategoryCatalogModel
    private let moneyParser: BRLMoneyParser
    private let timestampCodec: RFC3339DateCodec
    private let makeIdempotencyKey: () -> String
    private let onTransactionRecorded: () -> Void
    private var pendingIdempotencyKey: String?
    @ObservationIgnored private var draftGeneration: UInt64 = 0

    init(
        api: any FinancialAPI,
        categories: CategoryCatalogModel? = nil,
        now: Date = Date(),
        moneyParser: BRLMoneyParser = BRLMoneyParser(),
        timestampCodec: RFC3339DateCodec = RFC3339DateCodec(),
        makeIdempotencyKey: @escaping () -> String = { UUID().uuidString },
        onTransactionRecorded: @escaping () -> Void = {}
    ) {
        self.api = api
        self.categories = categories ?? CategoryCatalogModel(api: api)
        occurredAt = now
        self.moneyParser = moneyParser
        self.timestampCodec = timestampCodec
        self.makeIdempotencyKey = makeIdempotencyKey
        self.onTransactionRecorded = onTransactionRecorded
    }

    var reviewedTransaction: ReviewedTransaction? {
        switch state {
        case let .reviewing(reviewed),
             let .submitting(reviewed),
             let .retryable(reviewed),
             let .requiresEditing(reviewed):
            reviewed
        case .editing, .previewing, .success:
            nil
        }
    }

    var reviewedExpense: ReviewedExpense? {
        guard case let .expense(reviewed)? = reviewedTransaction else { return nil }
        return reviewed
    }

    var reviewedIncome: ReviewedIncome? {
        guard case let .income(reviewed)? = reviewedTransaction else { return nil }
        return reviewed
    }

    var successfulExpense: Expense? {
        guard case let .success(.expense(expense)) = state else { return nil }
        return expense
    }

    var successfulIncome: Income? {
        guard case let .success(.income(income)) = state else { return nil }
        return income
    }

    var isBusy: Bool {
        switch state {
        case .previewing, .submitting:
            true
        case .editing, .reviewing, .retryable, .requiresEditing, .success:
            false
        }
    }

    var categoryCatalogState: CategoryCatalogState {
        categories.state
    }

    var availableCategories: [CategoryDefinition] {
        categories.definitions(for: transactionType)
    }

    var selectedCategoryDisplayName: String {
        categories.displayName(for: selectedCategoryID)
    }

    func loadCategoriesIfNeeded() async {
        await categories.loadIfNeeded()
    }

    func retryCategories() async {
        await categories.retry()
    }

    func selectCategory(_ categoryID: String?) {
        guard categoryID != selectedCategoryID else { return }
        if let categoryID {
            guard categories.definition(for: categoryID)?.type == transactionType else { return }
        }
        if case .submitting = state { return }

        selectedCategoryID = categoryID
        draftDidChange()
    }

    func selectTransactionType(_ newType: TransactionType) {
        guard newType != transactionType else { return }
        if case .submitting = state { return }

        transactionType = newType
        selectedCategoryID = nil
        paymentMethod = .pix
        draftDidChange()
        errorMessage = nil
        state = .editing
    }

    func review() async {
        guard case .editing = state else { return }
        errorMessage = nil

        let amountMinor: Int64
        do {
            amountMinor = try moneyParser.parseMinorUnits(amountText)
        } catch {
            errorMessage = "Informe um valor maior que zero com até duas casas decimais."
            return
        }
        guard !description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            errorMessage = "Informe uma descrição para a movimentação."
            return
        }

        let amount = FinancialMoney(minor: amountMinor, currency: .brl)
        let occurredAt = timestampCodec.encode(occurredAt)
        let requestedType = transactionType
        let requestedCategoryID = selectedCategoryID
        let previewGeneration = draftGeneration
        state = .previewing

        do {
            switch requestedType {
            case .expense:
                let request = ExpenseRequest(
                    description: description,
                    amount: amount,
                    paymentMethod: paymentMethod,
                    categoryID: requestedCategoryID,
                    occurredAt: occurredAt
                )
                let preview = try await api.preview(request)
                guard canInstallPreview(generation: previewGeneration) else { return }
                let frozenRequest = ExpenseRequest(
                    description: preview.description,
                    amount: preview.amount,
                    paymentMethod: preview.paymentMethod,
                    categoryID: preview.categoryID,
                    occurredAt: preview.occurredAt
                )
                pendingIdempotencyKey = nil
                state = .reviewing(
                    .expense(
                        ReviewedExpense(
                            preview: preview,
                            request: frozenRequest,
                            categoryDisplayName: categories.displayName(for: preview.categoryID)
                        )
                    )
                )
            case .income:
                let request = IncomeRequest(
                    description: description,
                    amount: amount,
                    categoryID: requestedCategoryID,
                    occurredAt: occurredAt
                )
                let preview = try await api.preview(request)
                guard canInstallPreview(generation: previewGeneration) else { return }
                let frozenRequest = IncomeRequest(
                    description: preview.description,
                    amount: preview.amount,
                    categoryID: preview.categoryID,
                    occurredAt: preview.occurredAt
                )
                pendingIdempotencyKey = nil
                state = .reviewing(
                    .income(
                        ReviewedIncome(
                            preview: preview,
                            request: frozenRequest,
                            categoryDisplayName: categories.displayName(for: preview.categoryID)
                        )
                    )
                )
            }
        } catch is CancellationError {
            guard canInstallPreview(generation: previewGeneration) else { return }
            state = .editing
        } catch {
            guard canInstallPreview(generation: previewGeneration) else { return }
            state = .editing
            errorMessage = userMessage(for: error)
        }
    }

    func confirm() async {
        let reviewed: ReviewedTransaction
        switch state {
        case let .reviewing(value), let .retryable(value):
            reviewed = value
        case .editing, .previewing, .submitting, .requiresEditing, .success:
            return
        }

        let key = pendingIdempotencyKey ?? makeIdempotencyKey()
        pendingIdempotencyKey = key
        errorMessage = nil
        state = .submitting(reviewed)

        do {
            let recorded: FinancialTransaction
            switch reviewed {
            case let .expense(expense):
                let result = try await api.create(expense.request, idempotencyKey: key)
                recorded = .expense(result.expense)
            case let .income(income):
                let result = try await api.create(income.request, idempotencyKey: key)
                recorded = .income(result.income)
            }
            pendingIdempotencyKey = nil
            clearDraft()
            state = .success(recorded)
            onTransactionRecorded()
        } catch is CancellationError {
            state = .retryable(reviewed)
        } catch {
            errorMessage = userMessage(for: error)
            if isRetryableCreateError(error) {
                state = .retryable(reviewed)
            } else {
                pendingIdempotencyKey = nil
                state = .requiresEditing(reviewed)
            }
        }
    }

    func edit() {
        switch state {
        case .reviewing, .retryable, .requiresEditing:
            pendingIdempotencyKey = nil
            errorMessage = nil
            state = .editing
        case .editing, .previewing, .submitting, .success:
            break
        }
    }

    func startNewExpense(now: Date = Date()) {
        startNew(type: .expense, now: now)
    }

    func startNewIncome(now: Date = Date()) {
        startNew(type: .income, now: now)
    }

    private func startNew(type: TransactionType, now: Date) {
        guard case .success = state else { return }
        transactionType = type
        selectedCategoryID = nil
        occurredAt = now
        pendingIdempotencyKey = nil
        errorMessage = nil
        state = .editing
    }

    private func clearDraft() {
        description = ""
        amountText = ""
        paymentMethod = .pix
        selectedCategoryID = nil
    }

    private func draftDidChange() {
        draftGeneration &+= 1
        pendingIdempotencyKey = nil

        switch state {
        case .previewing, .reviewing, .retryable, .requiresEditing:
            errorMessage = nil
            state = .editing
        case .editing, .submitting, .success:
            break
        }
    }

    private func canInstallPreview(generation: UInt64) -> Bool {
        guard generation == draftGeneration else { return false }
        guard case .previewing = state else { return false }
        return true
    }

    private func userMessage(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível concluir a operação. Tente novamente."
    }

    private func isRetryableCreateError(_ error: Error) -> Bool {
        guard let error = error as? FinancialAPIError else {
            return true
        }
        return switch error {
        case .connectionUnavailable, .serviceUnavailable, .invalidResponse:
            true
        case .invalidData, .conflict, .notFound, .alreadyCancelled,
             .suggestionNotFound, .suggestionSuppressed, .creditCardNotFound,
             .creditCardAlreadyArchived, .configuration:
            false
        }
    }
}
