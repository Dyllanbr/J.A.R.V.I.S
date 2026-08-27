#if DEBUG
import Foundation

@MainActor
final class StubFinancialAPI: FinancialAPI {
    private enum SuggestionScenario: String {
        case normal
        case stale
        case suppressed
        case listError
    }

    private enum CardScenario: String {
        case normal
        case listError
    }

    private struct StoredRecurrenceCreate {
        let request: RecurrenceRequest
        let recurrence: Recurrence
    }

    private struct StoredRecurrenceCancel {
        let recurrenceID: String
        let recurrence: Recurrence
    }

    private struct StoredCardCreate {
        let request: CreditCardRequest
        let card: CreditCard
    }

    private struct StoredCardArchive {
        let cardID: String
        let card: CreditCard
    }

    private var transactionsByID: [String: FinancialTransaction] = [:]
    private var idempotency: [String: String] = [:]
    private var recurrencesByID: [String: Recurrence] = [:]
    private var recurrenceCreates: [String: StoredRecurrenceCreate] = [:]
    private var recurrenceCancels: [String: StoredRecurrenceCancel] = [:]
    private var dismissedSuggestionIDs: Set<String> = []
    private var invalidatedSuggestionIDs: Set<String> = []
    private var cardsByID: [String: CreditCard] = [:]
    private var cardCreates: [String: StoredCardCreate] = [:]
    private var cardArchives: [String: StoredCardArchive] = [:]
    private var nextSequence = 1
    private let timestampCodec = RFC3339DateCodec()
    private let suggestionScenario: SuggestionScenario
    private let cardScenario: CardScenario

    init(environment: [String: String] = ProcessInfo.processInfo.environment) {
        suggestionScenario = SuggestionScenario(
            rawValue: environment["JARVIS_IOS_SUGGESTION_SCENARIO"] ?? "normal"
        ) ?? .normal
        cardScenario = CardScenario(rawValue: environment["JARVIS_IOS_CARD_SCENARIO"] ?? "normal") ?? .normal
        let active = Recurrence(
            id: "rec_ui_synthetic_active",
            description: "Academia sintética",
            expectedAmount: FinancialMoney(minor: 11_900, currency: .brl),
            startsOn: try! RecurrenceCivilDate("2026-09-10"),
            status: .active,
            createdAt: "2026-08-16T12:00:00Z"
        )
        let cancelled = Recurrence(
            id: "rec_ui_synthetic_cancelled",
            description: "Streaming sintético",
            expectedAmount: FinancialMoney(minor: 2_990, currency: .brl),
            startsOn: try! RecurrenceCivilDate("2026-08-31"),
            status: .cancelled,
            createdAt: "2026-07-01T12:00:00Z",
            cancelledAt: "2026-08-01T12:00:00Z"
        )
        recurrencesByID[active.id] = active
        recurrencesByID[cancelled.id] = cancelled
    }

    func categories() async throws -> [CategoryDefinition] {
        try await Task.sleep(for: .milliseconds(80))
        return Self.categoryFixture
    }

    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview {
        try await Task.sleep(for: .milliseconds(80))
        let description = request.description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty, request.amount.minor > 0 else {
            throw FinancialAPIError.invalidData
        }
        try validate(categoryID: request.categoryID, for: .expense)
        let occurredAt = try canonicalTimestamp(request.occurredAt)
        return ExpensePreview(
            description: description,
            amount: request.amount,
            paymentMethod: request.paymentMethod,
            categoryID: request.categoryID,
            occurredAt: occurredAt,
            financialTimezone: "America/Sao_Paulo",
            origin: .ios
        )
    }

    func preview(_ request: IncomeRequest) async throws -> IncomePreview {
        try await Task.sleep(for: .milliseconds(80))
        let description = request.description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty, request.amount.minor > 0 else {
            throw FinancialAPIError.invalidData
        }
        try validate(categoryID: request.categoryID, for: .income)
        let occurredAt = try canonicalTimestamp(request.occurredAt)
        return IncomePreview(
            description: description,
            amount: request.amount,
            categoryID: request.categoryID,
            occurredAt: occurredAt,
            financialTimezone: "America/Sao_Paulo",
            origin: .ios
        )
    }

    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense {
        try await Task.sleep(for: .milliseconds(80))
        let scopedKey = "EXPENSE:\(idempotencyKey)"
        if let existingID = idempotency[scopedKey],
           case let .expense(existing)? = transactionsByID[existingID]
        {
            return RecordedExpense(expense: existing, replayed: true)
        }

        let preview = try await preview(request)
        let id = nextID(prefix: "exp")
        let now = timestampCodec.encode(Date())
        let expense = Expense(
            id: id,
            description: preview.description,
            amount: preview.amount,
            paymentMethod: preview.paymentMethod,
            categoryID: preview.categoryID,
            occurredAt: preview.occurredAt,
            financialTimezone: preview.financialTimezone,
            origin: preview.origin,
            status: .recorded,
            version: 1,
            createdAt: now,
            updatedAt: now
        )
        transactionsByID[id] = .expense(expense)
        idempotency[scopedKey] = id
        return RecordedExpense(expense: expense, replayed: false)
    }

    func create(_ request: IncomeRequest, idempotencyKey: String) async throws -> RecordedIncome {
        try await Task.sleep(for: .milliseconds(80))
        let scopedKey = "INCOME:\(idempotencyKey)"
        if let existingID = idempotency[scopedKey],
           case let .income(existing)? = transactionsByID[existingID]
        {
            return RecordedIncome(income: existing, replayed: true)
        }

        let preview = try await preview(request)
        let id = nextID(prefix: "inc")
        let now = timestampCodec.encode(Date())
        let income = Income(
            id: id,
            description: preview.description,
            amount: preview.amount,
            categoryID: preview.categoryID,
            occurredAt: preview.occurredAt,
            financialTimezone: preview.financialTimezone,
            origin: preview.origin,
            status: .recorded,
            version: 1,
            createdAt: now,
            updatedAt: now
        )
        transactionsByID[id] = .income(income)
        idempotency[scopedKey] = id
        return RecordedIncome(income: income, replayed: false)
    }

    func transactions(month: String) async throws -> TransactionMonth {
        try await Task.sleep(for: .milliseconds(80))
        let items = transactionsByID.values
            .filter { transaction in
                guard let date = try? timestampCodec.decode(transaction.occurredAt) else { return false }
                return FinancialMonth(date: date).apiValue == month
            }
            .sorted {
                if $0.occurredAt == $1.occurredAt { return $0.id > $1.id }
                return $0.occurredAt > $1.occurredAt
            }
        return TransactionMonth(month: month, items: items)
    }

    func previewRecurrence(_ request: RecurrenceRequest) async throws -> RecurrencePreview {
        try await Task.sleep(for: .milliseconds(80))
        let description = request.description.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !description.isEmpty,
              request.type == .expense,
              request.expectedAmount.minor > 0,
              request.expectedAmount.currency == .brl,
              request.frequency == .monthly
        else {
            throw FinancialAPIError.invalidData
        }
        return RecurrencePreview(
            description: description,
            expectedAmount: request.expectedAmount,
            startsOn: request.startsOn
        )
    }

    func createRecurrence(
        _ request: RecurrenceRequest,
        idempotencyKey: String
    ) async throws -> RecordedRecurrence {
        try await Task.sleep(for: .milliseconds(80))
        if let stored = recurrenceCreates[idempotencyKey] {
            guard stored.request == request else { throw FinancialAPIError.conflict }
            return RecordedRecurrence(recurrence: stored.recurrence, replayed: true)
        }
        let preview = try await previewRecurrence(request)
        let id = nextID(prefix: "rec")
        let recurrence = Recurrence(
            id: id,
            description: preview.description,
            expectedAmount: preview.expectedAmount,
            startsOn: preview.startsOn,
            status: .active,
            createdAt: timestampCodec.encode(Date())
        )
        recurrencesByID[id] = recurrence
        recurrenceCreates[idempotencyKey] = StoredRecurrenceCreate(request: request, recurrence: recurrence)
        return RecordedRecurrence(recurrence: recurrence, replayed: false)
    }

    func recurrences() async throws -> RecurrenceList {
        try await Task.sleep(for: .milliseconds(80))
        let items = recurrencesByID.values.sorted { lhs, rhs in
            if lhs.status != rhs.status { return lhs.status == .active }
            if lhs.startsOn != rhs.startsOn { return lhs.startsOn.canonicalValue > rhs.startsOn.canonicalValue }
            if lhs.createdAt != rhs.createdAt { return lhs.createdAt > rhs.createdAt }
            return lhs.id > rhs.id
        }
        return RecurrenceList(items: items)
    }

    func cancelRecurrence(id: String, idempotencyKey: String) async throws -> RecordedRecurrence {
        try await Task.sleep(for: .milliseconds(80))
        if let stored = recurrenceCancels[idempotencyKey] {
            guard stored.recurrenceID == id else { throw FinancialAPIError.conflict }
            return RecordedRecurrence(recurrence: stored.recurrence, replayed: true)
        }
        guard let recurrence = recurrencesByID[id] else { throw FinancialAPIError.notFound }
        guard recurrence.status == .active else { throw FinancialAPIError.alreadyCancelled }
        let cancelled = Recurrence(
            id: recurrence.id,
            description: recurrence.description,
            expectedAmount: recurrence.expectedAmount,
            startsOn: recurrence.startsOn,
            status: .cancelled,
            createdAt: recurrence.createdAt,
            cancelledAt: timestampCodec.encode(Date())
        )
        recurrencesByID[id] = cancelled
        recurrenceCancels[idempotencyKey] = StoredRecurrenceCancel(
            recurrenceID: id,
            recurrence: cancelled
        )
        return RecordedRecurrence(recurrence: cancelled, replayed: false)
    }

    func recurrenceSuggestions() async throws -> RecurrenceSuggestionList {
        try await Task.sleep(for: .milliseconds(80))
        if suggestionScenario == .listError {
            throw FinancialAPIError.serviceUnavailable
        }
        guard !dismissedSuggestionIDs.contains(Self.suggestionFixture.id),
              !invalidatedSuggestionIDs.contains(Self.suggestionFixture.id),
              !recurrencesByID.values.contains(where: {
                  $0.status == .active
                      && $0.description == Self.suggestionFixture.description
                      && $0.expectedAmount == Self.suggestionFixture.expectedAmount
              })
        else {
            return RecurrenceSuggestionList(items: [])
        }
        return RecurrenceSuggestionList(items: [Self.suggestionFixture])
    }

    func dismissRecurrenceSuggestion(id: String) async throws -> DismissedRecurrenceSuggestion {
        try await Task.sleep(for: .milliseconds(80))
        guard id == Self.suggestionFixture.id else { throw FinancialAPIError.suggestionNotFound }
        let inserted = dismissedSuggestionIDs.insert(id).inserted
        return DismissedRecurrenceSuggestion(replayed: !inserted)
    }

    func previewRecurrenceSuggestion(id: String) async throws -> RecurrencePreview {
        try await Task.sleep(for: .milliseconds(80))
        guard id == Self.suggestionFixture.id else { throw FinancialAPIError.suggestionNotFound }
        guard !dismissedSuggestionIDs.contains(id) else { throw FinancialAPIError.suggestionSuppressed }
        switch suggestionScenario {
        case .stale:
            invalidatedSuggestionIDs.insert(id)
            throw FinancialAPIError.suggestionNotFound
        case .suppressed:
            dismissedSuggestionIDs.insert(id)
            throw FinancialAPIError.suggestionSuppressed
        case .normal, .listError:
            break
        }
        return RecurrencePreview(
            description: Self.suggestionFixture.description,
            expectedAmount: Self.suggestionFixture.expectedAmount,
            startsOn: Self.suggestionFixture.proposedStartsOn
        )
    }

    func previewCreditCard(_ request: CreditCardRequest) async throws -> CreditCardPreview {
        try await Task.sleep(for: .milliseconds(80))
        let name = request.name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty,
              request.lastFour.map(CreditCardPreview.isValidLastFour) ?? true,
              (1...31).contains(request.closingDay),
              (1...31).contains(request.dueDay),
              request.creditLimit.map({ $0.minor > 0 && $0.currency == .brl }) ?? true
        else { throw FinancialAPIError.invalidData }
        return CreditCardPreview(
            name: name,
            lastFour: request.lastFour,
            brand: request.brand,
            closingDay: request.closingDay,
            dueDay: request.dueDay,
            creditLimit: request.creditLimit
        )
    }

    func createCreditCard(
        _ request: CreditCardRequest,
        idempotencyKey: String
    ) async throws -> RecordedCreditCard {
        try await Task.sleep(for: .milliseconds(80))
        if let stored = cardCreates[idempotencyKey] {
            guard stored.request == request else { throw FinancialAPIError.conflict }
            return RecordedCreditCard(card: stored.card, replayed: true)
        }
        let preview = try await previewCreditCard(request)
        let id = nextCardID()
        let card = CreditCard(
            id: id,
            name: preview.name,
            lastFour: preview.lastFour,
            brand: preview.brand,
            closingDay: preview.closingDay,
            dueDay: preview.dueDay,
            creditLimit: preview.creditLimit,
            createdAt: timestampCodec.encode(Date())
        )
        cardsByID[id] = card
        cardCreates[idempotencyKey] = StoredCardCreate(request: request, card: card)
        return RecordedCreditCard(card: card, replayed: false)
    }

    func creditCards() async throws -> CreditCardList {
        try await Task.sleep(for: .milliseconds(80))
        if cardScenario == .listError { throw FinancialAPIError.serviceUnavailable }
        return CreditCardList(items: cardsByID.values.sorted { lhs, rhs in
            if lhs.status != rhs.status { return lhs.status == .active }
            if lhs.name != rhs.name { return lhs.name < rhs.name }
            if lhs.createdAt != rhs.createdAt { return lhs.createdAt > rhs.createdAt }
            return lhs.id < rhs.id
        })
    }

    func creditCard(id: String) async throws -> CreditCard {
        try await Task.sleep(for: .milliseconds(80))
        guard let card = cardsByID[id] else { throw FinancialAPIError.creditCardNotFound }
        return card
    }

    func archiveCreditCard(id: String, idempotencyKey: String) async throws -> RecordedCreditCard {
        try await Task.sleep(for: .milliseconds(80))
        if let stored = cardArchives[idempotencyKey] {
            guard stored.cardID == id else { throw FinancialAPIError.conflict }
            return RecordedCreditCard(card: stored.card, replayed: true)
        }
        guard let card = cardsByID[id] else { throw FinancialAPIError.creditCardNotFound }
        guard card.status == .active else { throw FinancialAPIError.creditCardAlreadyArchived }
        let archived = CreditCard(
            id: card.id,
            name: card.name,
            lastFour: card.lastFour,
            brand: card.brand,
            closingDay: card.closingDay,
            dueDay: card.dueDay,
            creditLimit: card.creditLimit,
            status: .archived,
            createdAt: card.createdAt,
            archivedAt: timestampCodec.encode(Date())
        )
        cardsByID[id] = archived
        cardArchives[idempotencyKey] = StoredCardArchive(cardID: id, card: archived)
        return RecordedCreditCard(card: archived, replayed: false)
    }

    private func canonicalTimestamp(_ value: String) throws -> String {
        timestampCodec.encode(try timestampCodec.decode(value))
    }

    private func nextID(prefix: String) -> String {
        defer { nextSequence += 1 }
        return "\(prefix)_ui_synthetic_\(String(format: "%03d", nextSequence))"
    }

    private func nextCardID() -> String {
        defer { nextSequence += 1 }
        return "card_\(String(format: "%032x", nextSequence))"
    }

    private func validate(categoryID: String?, for type: TransactionType) throws {
        guard let categoryID else { return }
        guard Self.categoryFixture.contains(where: { $0.id == categoryID && $0.type == type }) else {
            throw FinancialAPIError.invalidData
        }
    }

    // Debug-only fixture used by the offline stub. Production always loads GET /v1/categories.
    private static let categoryFixture: [CategoryDefinition] = [
        CategoryDefinition(id: "expense.food", type: .expense, displayName: "Alimentação"),
        CategoryDefinition(id: "expense.transport", type: .expense, displayName: "Transporte"),
        CategoryDefinition(id: "expense.housing", type: .expense, displayName: "Moradia"),
        CategoryDefinition(id: "expense.health", type: .expense, displayName: "Saúde"),
        CategoryDefinition(id: "expense.leisure", type: .expense, displayName: "Lazer"),
        CategoryDefinition(id: "expense.education", type: .expense, displayName: "Educação"),
        CategoryDefinition(id: "expense.subscriptions", type: .expense, displayName: "Assinaturas"),
        CategoryDefinition(id: "expense.shopping", type: .expense, displayName: "Compras"),
        CategoryDefinition(id: "expense.taxes_fees", type: .expense, displayName: "Impostos e taxas"),
        CategoryDefinition(id: "expense.other", type: .expense, displayName: "Outros"),
        CategoryDefinition(id: "income.salary", type: .income, displayName: "Salário"),
        CategoryDefinition(id: "income.freelance", type: .income, displayName: "Freelance"),
        CategoryDefinition(id: "income.refund", type: .income, displayName: "Reembolso"),
        CategoryDefinition(id: "income.sale", type: .income, displayName: "Venda"),
        CategoryDefinition(id: "income.investment_return", type: .income, displayName: "Rendimentos"),
        CategoryDefinition(id: "income.benefits", type: .income, displayName: "Benefícios"),
        CategoryDefinition(id: "income.other", type: .income, displayName: "Outros")
    ]

    private static let suggestionFixture = try! RecurrenceSuggestion(
        id: "rsg_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        description: "Internet sintética",
        expectedAmount: FinancialMoney(minor: 9_990, currency: .brl),
        anchorDay: 10,
        proposedStartsOn: RecurrenceCivilDate("2026-09-10"),
        observedDates: [
            RecurrenceCivilDate("2026-05-10"),
            RecurrenceCivilDate("2026-06-10"),
            RecurrenceCivilDate("2026-07-10")
        ]
    )
}
#endif
