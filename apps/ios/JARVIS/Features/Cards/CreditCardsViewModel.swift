import Foundation
import Observation

struct ReviewedCreditCard: Equatable, Sendable {
    let preview: CreditCardPreview
    let request: CreditCardRequest
    let idempotencyKey: String
}

enum CreditCardCreationState: Equatable {
    case editing
    case previewing
    case reviewing(ReviewedCreditCard)
    case submitting(ReviewedCreditCard)
    case retryable(ReviewedCreditCard)
    case requiresEditing(ReviewedCreditCard)
    case success(CreditCard)
}

enum CreditCardListState: Equatable {
    case idle
    case loading
    case loaded([CreditCard])
    case failed(String)
}

enum CreditCardDetailState: Equatable {
    case idle
    case loading
    case loaded(CreditCard)
    case failed(String)
}

@MainActor
@Observable
final class CreditCardsViewModel {
    private(set) var listState: CreditCardListState = .idle
    private(set) var creationState: CreditCardCreationState = .editing
    private(set) var creationErrorMessage: String?
    private(set) var detailStates: [String: CreditCardDetailState] = [:]
    private(set) var archiveConfirmation: CreditCard?
    private(set) var archivingIDs: Set<String> = []
    private(set) var archiveErrors: [String: String] = [:]
    private(set) var isPresentingCreate = false

    var name = "" { didSet { if name != oldValue { draftDidChange() } } }
    var lastFour = "" { didSet { if lastFour != oldValue { draftDidChange() } } }
    var brand: CreditCardBrand? { didSet { if brand != oldValue { draftDidChange() } } }
    var closingDay = 1 { didSet { if closingDay != oldValue { draftDidChange() } } }
    var dueDay = 10 { didSet { if dueDay != oldValue { draftDidChange() } } }
    var creditLimitText = "" { didSet { if creditLimitText != oldValue { draftDidChange() } } }

    private let api: any FinancialAPI
    private let moneyParser: BRLMoneyParser
    private let makeIdempotencyKey: () -> String
    @ObservationIgnored private var draftGeneration: UInt64 = 0
    @ObservationIgnored private var contentRevision: UInt64 = 0
    @ObservationIgnored private var listGeneration: UInt64 = 0
    @ObservationIgnored private var completedListGeneration: UInt64 = 0
    @ObservationIgnored private var activeListRevision: UInt64?
    @ObservationIgnored private var listNeedsReconciliation = false
    @ObservationIgnored private var listTask: Task<Void, Never>?
    @ObservationIgnored private var detailGenerations: [String: UInt64] = [:]
    @ObservationIgnored private var completedDetailGenerations: [String: UInt64] = [:]
    @ObservationIgnored private var activeDetailRevisions: [String: UInt64] = [:]
    @ObservationIgnored private var detailNeedsReconciliation: Set<String> = []
    @ObservationIgnored private var detailTasks: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var archiveKeys: [String: String] = [:]

    init(
        api: any FinancialAPI,
        moneyParser: BRLMoneyParser = BRLMoneyParser(),
        makeIdempotencyKey: @escaping () -> String = { UUID().uuidString }
    ) {
        self.api = api
        self.moneyParser = moneyParser
        self.makeIdempotencyKey = makeIdempotencyKey
    }

    var cards: [CreditCard] {
        guard case let .loaded(items) = listState else { return [] }
        return items
    }

    var reviewedCard: ReviewedCreditCard? {
        switch creationState {
        case let .reviewing(value), let .submitting(value), let .retryable(value), let .requiresEditing(value): value
        case .editing, .previewing, .success: nil
        }
    }

    var successfulCard: CreditCard? {
        guard case let .success(card) = creationState else { return nil }
        return card
    }

    var isCreationBusy: Bool {
        switch creationState {
        case .previewing, .submitting: true
        case .editing, .reviewing, .retryable, .requiresEditing, .success: false
        }
    }

    func loadIfNeeded() async {
        switch listState {
        case .idle: await startListLoad()
        case .loading: await listTask?.value
        case .loaded, .failed: break
        }
    }

    func refresh() async { await startListLoad() }

    func retryList() async {
        guard case .failed = listState else { return }
        await startListLoad()
    }

    func loadDetail(id: String) async {
        guard CreditCard.isValidID(id) else {
            detailStates[id] = .failed(FinancialAPIError.creditCardNotFound.userMessage)
            return
        }
        switch detailStates[id] ?? .idle {
        case .idle: await startDetailLoad(id: id)
        case .loading: await detailTasks[id]?.value
        case .loaded, .failed: break
        }
    }

    func refreshDetail(id: String) async { await startDetailLoad(id: id) }

    func retryDetail(id: String) async {
        guard case .failed = detailStates[id] else { return }
        await startDetailLoad(id: id)
    }

    func beginCreation() {
        guard !isPresentingCreate else { return }
        resetCreationDraft()
        isPresentingCreate = true
    }

    func dismissCreation() {
        guard !isCreationBusy else { return }
        isPresentingCreate = false
        resetCreationDraft()
    }

    func review() async {
        guard case .editing = creationState else { return }
        creationErrorMessage = nil
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmedName.isEmpty else {
            creationErrorMessage = "Informe um nome para o cartão."
            return
        }
        let suffix: String? = lastFour.isEmpty ? nil : lastFour
        guard suffix.map(CreditCardPreview.isValidLastFour) ?? true else {
            creationErrorMessage = "Informe exatamente os 4 últimos dígitos do cartão."
            return
        }
        let limit: FinancialMoney?
        if creditLimitText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            limit = nil
        } else {
            do {
                limit = FinancialMoney(minor: try moneyParser.parseMinorUnits(creditLimitText), currency: .brl)
            } catch {
                creationErrorMessage = "Informe um limite positivo com até duas casas decimais."
                return
            }
        }
        let request = CreditCardRequest(
            name: trimmedName,
            lastFour: suffix,
            brand: brand,
            closingDay: closingDay,
            dueDay: dueDay,
            creditLimit: limit
        )
        let generation = draftGeneration
        creationState = .previewing
        do {
            let preview = try await api.previewCreditCard(request)
            guard generation == draftGeneration, case .previewing = creationState else { return }
            let frozen = CreditCardRequest(
                name: preview.name,
                lastFour: preview.lastFour,
                brand: preview.brand,
                closingDay: preview.closingDay,
                dueDay: preview.dueDay,
                creditLimit: preview.creditLimit
            )
            creationState = .reviewing(
                ReviewedCreditCard(preview: preview, request: frozen, idempotencyKey: makeIdempotencyKey())
            )
        } catch is CancellationError {
            guard generation == draftGeneration else { return }
            creationState = .editing
        } catch {
            guard generation == draftGeneration else { return }
            creationState = .editing
            creationErrorMessage = message(for: error)
        }
    }

    func editCreation() {
        switch creationState {
        case .reviewing, .retryable, .requiresEditing:
            creationErrorMessage = nil
            creationState = .editing
        case .editing, .previewing, .submitting, .success:
            break
        }
    }

    func confirmCreation() async {
        let reviewed: ReviewedCreditCard
        switch creationState {
        case let .reviewing(value), let .retryable(value): reviewed = value
        case .editing, .previewing, .submitting, .requiresEditing, .success: return
        }
        creationState = .submitting(reviewed)
        creationErrorMessage = nil
        do {
            let result = try await api.createCreditCard(reviewed.request, idempotencyKey: reviewed.idempotencyKey)
            install(result.card)
            creationState = .success(result.card)
        } catch is CancellationError {
            creationState = .retryable(reviewed)
        } catch {
            creationErrorMessage = message(for: error)
            creationState = isRetryable(error) ? .retryable(reviewed) : .requiresEditing(reviewed)
        }
    }

    func finishCreation() {
        guard case .success = creationState else { return }
        isPresentingCreate = false
        resetCreationDraft()
        requestListReconciliation()
    }

    func requestArchive(_ card: CreditCard) {
        guard card.status == .active, !archivingIDs.contains(card.id) else { return }
        archiveConfirmation = card
    }

    func dismissArchiveConfirmation() { archiveConfirmation = nil }

    func confirmArchive(_ card: CreditCard) async {
        archiveConfirmation = nil
        guard card.status == .active else { return }
        let key = archiveKeys[card.id] ?? makeIdempotencyKey()
        archiveKeys[card.id] = key
        await performArchive(id: card.id, key: key)
    }

    func retryArchive(id: String) async {
        guard let key = archiveKeys[id], !archivingIDs.contains(id) else { return }
        await performArchive(id: id, key: key)
    }

    func canRetryArchive(id: String) -> Bool { archiveKeys[id] != nil && !archivingIDs.contains(id) }

    private func startListLoad() async {
        let task = beginListLoadIfNeeded()
        await task.value
    }

    private func beginListLoadIfNeeded() -> Task<Void, Never> {
        if let listTask { return listTask }
        listGeneration &+= 1
        let generation = listGeneration
        let revision = contentRevision
        let task = Task { @MainActor [api] in
            let result: CreditCardListState
            do { result = .loaded(try await api.creditCards().items) }
            catch { result = .failed(self.message(for: error)) }
            self.completeListLoad(result, generation: generation, revision: revision)
        }
        listTask = task
        activeListRevision = revision
        if case .loaded = listState {} else { listState = .loading }
        return task
    }

    private func completeListLoad(_ result: CreditCardListState, generation: UInt64, revision: UInt64) {
        guard generation == listGeneration else { return }
        let reconcile = listNeedsReconciliation
        listNeedsReconciliation = false
        if revision == contentRevision { listState = result }
        listTask = nil
        activeListRevision = nil
        completedListGeneration = generation
        if reconcile { _ = beginListLoadIfNeeded() }
    }

    private func startDetailLoad(id: String) async {
        let task = beginDetailLoadIfNeeded(id: id)
        await task.value
    }

    private func beginDetailLoadIfNeeded(id: String) -> Task<Void, Never> {
        if let task = detailTasks[id] { return task }
        let generation = (detailGenerations[id] ?? 0) &+ 1
        detailGenerations[id] = generation
        let revision = contentRevision
        let task = Task { @MainActor [api] in
            let result: CreditCardDetailState
            do { result = .loaded(try await api.creditCard(id: id)) }
            catch { result = .failed(self.message(for: error)) }
            self.completeDetailLoad(result, id: id, generation: generation, revision: revision)
        }
        detailTasks[id] = task
        activeDetailRevisions[id] = revision
        if case .loaded = detailStates[id] {} else { detailStates[id] = .loading }
        return task
    }

    private func completeDetailLoad(
        _ result: CreditCardDetailState,
        id: String,
        generation: UInt64,
        revision: UInt64
    ) {
        guard detailGenerations[id] == generation else { return }
        let reconcile = detailNeedsReconciliation.remove(id) != nil
        if revision == contentRevision { detailStates[id] = result }
        detailTasks[id] = nil
        activeDetailRevisions[id] = nil
        completedDetailGenerations[id] = generation
        if reconcile { _ = beginDetailLoadIfNeeded(id: id) }
    }

    private func performArchive(id: String, key: String) async {
        guard !archivingIDs.contains(id) else { return }
        archivingIDs.insert(id)
        archiveErrors[id] = nil
        defer { archivingIDs.remove(id) }
        do {
            let result = try await api.archiveCreditCard(id: id, idempotencyKey: key)
            install(result.card)
            archiveKeys[id] = nil
            requestListReconciliation()
            requestDetailReconciliation(id: id)
        } catch is CancellationError {
            archiveErrors[id] = "O arquivamento foi interrompido. Tente novamente."
        } catch let error as FinancialAPIError
            where error == .creditCardAlreadyArchived || error == .creditCardNotFound
        {
            archiveKeys[id] = nil
            archiveErrors[id] = error.userMessage
            await reconcileListAfterCurrentLoad()
            await reconcileDetailAfterCurrentLoad(id: id)
            if case let .loaded(items) = listState,
               !items.contains(where: { $0.id == id && $0.status == .active })
            {
                archiveErrors[id] = nil
            }
        } catch {
            archiveErrors[id] = message(for: error)
            if !isRetryable(error) { archiveKeys[id] = nil }
        }
    }

    private func install(_ card: CreditCard) {
        contentRevision &+= 1
        var items = cards
        if let index = items.firstIndex(where: { $0.id == card.id }) { items[index] = card }
        else { items.insert(card, at: 0) }
        listState = .loaded(items)
        detailStates[card.id] = .loaded(card)
        archiveErrors[card.id] = nil
        if listTask != nil { listNeedsReconciliation = true }
        for id in detailTasks.keys where activeDetailRevisions[id] != contentRevision {
            detailNeedsReconciliation.insert(id)
        }
    }

    private func requestListReconciliation() {
        if listTask != nil { listNeedsReconciliation = true }
        else { _ = beginListLoadIfNeeded() }
    }

    private func requestDetailReconciliation(id: String) {
        if detailTasks[id] != nil { detailNeedsReconciliation.insert(id) }
        else { _ = beginDetailLoadIfNeeded(id: id) }
    }

    private func reconcileListAfterCurrentLoad() async {
        let generationBeforeRequest = listGeneration
        requestListReconciliation()
        while completedListGeneration <= generationBeforeRequest {
            guard let listTask else {
                _ = beginListLoadIfNeeded()
                continue
            }
            await listTask.value
        }
    }

    private func reconcileDetailAfterCurrentLoad(id: String) async {
        let generationBeforeRequest = detailGenerations[id] ?? 0
        requestDetailReconciliation(id: id)
        while (completedDetailGenerations[id] ?? 0) <= generationBeforeRequest {
            guard let task = detailTasks[id] else {
                _ = beginDetailLoadIfNeeded(id: id)
                continue
            }
            await task.value
        }
    }

    private func resetCreationDraft() {
        name = ""
        lastFour = ""
        brand = nil
        closingDay = 1
        dueDay = 10
        creditLimitText = ""
        creationErrorMessage = nil
        creationState = .editing
    }

    private func draftDidChange() {
        draftGeneration &+= 1
        switch creationState {
        case .previewing, .reviewing, .retryable, .requiresEditing:
            creationErrorMessage = nil
            creationState = .editing
        case .editing, .submitting, .success: break
        }
    }

    private func message(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível concluir a operação. Tente novamente."
    }

    private func isRetryable(_ error: Error) -> Bool {
        guard let error = error as? FinancialAPIError else { return true }
        return switch error {
        case .connectionUnavailable, .serviceUnavailable, .invalidResponse: true
        case .invalidData, .conflict, .notFound, .alreadyCancelled, .suggestionNotFound,
             .suggestionSuppressed, .creditCardNotFound, .creditCardAlreadyArchived, .configuration: false
        }
    }
}
