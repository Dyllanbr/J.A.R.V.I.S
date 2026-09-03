import Foundation
import Observation

struct ReviewedCardPurchase: Equatable, Sendable {
    let preview: CardPurchasePreview
    let request: CardPurchaseCreateRequest
}

enum CardPurchaseState: Equatable {
    case editing
    case previewing
    case reviewing(ReviewedCardPurchase)
    case submitting(ReviewedCardPurchase)
    case retryable(ReviewedCardPurchase)
    case requiresEditing(ReviewedCardPurchase)
    case success(CardPurchase)
}

@MainActor
@Observable
final class CardPurchaseViewModel {
    private(set) var state: CardPurchaseState = .editing
    private(set) var errorMessage: String?
    private(set) var isPresenting = false
    private(set) var cards: [CreditCard] = []
    private(set) var cardsState: CreditCardListState = .idle

    var description = ""
    var amountText = ""
    var occurredAt: Date
    var creditCardID: String?
    var installmentCountText = ""
    var categoryID: String?

    private let api: any FinancialAPI
    private let moneyParser: BRLMoneyParser
    private let timestampCodec: RFC3339DateCodec
    private let makeIdempotencyKey: () -> String
    private let onPurchaseRecorded: () -> Void
    private var pendingIdempotencyKey: String?
    private var previewGeneration: UInt64 = 0

    init(
        api: any FinancialAPI,
        now: Date = Date(),
        moneyParser: BRLMoneyParser = BRLMoneyParser(),
        timestampCodec: RFC3339DateCodec = RFC3339DateCodec(),
        makeIdempotencyKey: @escaping () -> String = { UUID().uuidString },
        onPurchaseRecorded: @escaping () -> Void = {}
    ) {
        self.api = api
        occurredAt = now
        self.moneyParser = moneyParser
        self.timestampCodec = timestampCodec
        self.makeIdempotencyKey = makeIdempotencyKey
        self.onPurchaseRecorded = onPurchaseRecorded
    }

    var isBusy: Bool {
        switch state { case .previewing, .submitting: true; default: false }
    }

    var isRetryable: Bool { if case .retryable = state { true } else { false } }
    var reviewed: ReviewedCardPurchase? {
        switch state {
        case let .reviewing(value), let .submitting(value), let .retryable(value), let .requiresEditing(value): value
        default: nil
        }
    }

    var selectedCard: CreditCard? { cards.first { $0.id == creditCardID } }
    var installmentCount: Int? {
        let trimmed = installmentCountText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return Int(trimmed)
    }

    func begin(cardID: String? = nil, description: String = "", amountText: String = "", occurredAt: Date? = nil, categoryID: String? = nil) {
        guard !isPresenting else { return }
        resetDraft()
        creditCardID = cardID
        self.description = description
        self.amountText = amountText
        if let occurredAt { self.occurredAt = occurredAt }
        self.categoryID = categoryID
        isPresenting = true
        Task { await loadCardsIfNeeded() }
    }

    func dismiss() {
        guard !isBusy else { return }
        isPresenting = false
        resetDraft()
    }

    func loadCardsIfNeeded() async {
        guard case .idle = cardsState else { return }
        await loadCards()
    }

    func loadCards() async {
        cardsState = .loading
        do {
            cards = try await api.creditCards().items
            cardsState = .loaded(cards)
            if creditCardID == nil { creditCardID = cards.first(where: { $0.status == .active })?.id }
        } catch is CancellationError {
            cardsState = .idle
        } catch {
            cardsState = .failed(message(for: error))
        }
    }

    func retryCards() async { await loadCards() }

    func review() async {
        guard case .editing = state else { return }
        errorMessage = nil
        guard let cardID = creditCardID, CreditCard.isValidID(cardID) else {
            errorMessage = "Selecione um cartão ativo."
            return
        }
        guard let card = selectedCard, card.status == .active else {
            errorMessage = FinancialAPIError.creditCardAlreadyArchived.userMessage
            return
        }
        let amount: FinancialMoney
        do { amount = FinancialMoney(minor: try moneyParser.parseMinorUnits(amountText), currency: .brl) }
        catch { errorMessage = "Informe um valor maior que zero com até duas casas decimais."; return }
        let count = installmentCount
        guard installmentCountText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || (count != nil && (2...120).contains(count!)) else {
            errorMessage = "Informe 2 a 120 parcelas, ou deixe em branco para compra à vista."
            return
        }
        let request = CardPurchasePreviewRequest(description: description, amount: amount, occurredAt: timestampCodec.encode(occurredAt), categoryID: categoryID, creditCardID: cardID, installmentCount: count)
        previewGeneration &+= 1
        let generation = previewGeneration
        state = .previewing
        do {
            let preview = try await api.previewCardPurchase(request)
            guard generation == previewGeneration, case .previewing = state else { return }
            let frozen = CardPurchaseCreateRequest(
                description: preview.description,
                amount: preview.amount,
                occurredAt: preview.occurredAt,
                categoryID: preview.categoryID,
                creditCardID: preview.creditCardID,
                installmentCount: preview.installmentSummary?.installmentCount
            )
            pendingIdempotencyKey = nil
            state = .reviewing(ReviewedCardPurchase(preview: preview, request: frozen))
        } catch is CancellationError {
            guard generation == previewGeneration else { return }; state = .editing
        } catch {
            guard generation == previewGeneration else { return }; state = .editing; errorMessage = message(for: error)
        }
    }

    func edit() {
        switch state {
        case .reviewing, .retryable, .requiresEditing:
            pendingIdempotencyKey = nil; errorMessage = nil; previewGeneration &+= 1; state = .editing
        default: break
        }
    }

    func confirm() async {
        let reviewed: ReviewedCardPurchase
        switch state { case let .reviewing(value), let .retryable(value): reviewed = value; default: return }
        let key = pendingIdempotencyKey ?? makeIdempotencyKey()
        pendingIdempotencyKey = key
        state = .submitting(reviewed); errorMessage = nil
        do {
            let result = try await api.createCardPurchase(reviewed.request, idempotencyKey: key)
            pendingIdempotencyKey = nil
            state = .success(result.purchase)
            onPurchaseRecorded()
        } catch is CancellationError {
            state = .retryable(reviewed)
        } catch {
            errorMessage = message(for: error)
            if isRetryable(error) { state = .retryable(reviewed) } else { pendingIdempotencyKey = nil; state = .requiresEditing(reviewed) }
        }
    }

    func finish() { guard case .success = state else { return }; isPresenting = false; resetDraft() }

    private func resetDraft() {
        description = ""; amountText = ""; occurredAt = Date(); creditCardID = nil; installmentCountText = ""; categoryID = nil; errorMessage = nil; pendingIdempotencyKey = nil; state = .editing
    }

    private func message(for error: Error) -> String { (error as? FinancialAPIError)?.userMessage ?? "Não foi possível concluir a compra. Tente novamente." }
    private func isRetryable(_ error: Error) -> Bool {
        guard let error = error as? FinancialAPIError else { return true }
        return switch error {
        case .connectionUnavailable, .serviceUnavailable, .invalidResponse: true
        default: false
        }
    }
}
