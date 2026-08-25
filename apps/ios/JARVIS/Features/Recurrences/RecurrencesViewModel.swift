import Foundation
import Observation

enum RecurrenceReviewSource: Equatable, Sendable {
    case manual
    case suggestion(id: String)

    var suggestionID: String? {
        guard case let .suggestion(id) = self else { return nil }
        return id
    }
}

struct ReviewedRecurrence: Equatable, Sendable {
    let preview: RecurrencePreview
    let request: RecurrenceRequest
    let idempotencyKey: String
    let source: RecurrenceReviewSource
}

enum RecurrenceCreationState: Equatable {
    case editing
    case previewing
    case reviewing(ReviewedRecurrence)
    case submitting(ReviewedRecurrence)
    case retryable(ReviewedRecurrence)
    case requiresEditing(ReviewedRecurrence)
    case success(Recurrence)
}

enum RecurrenceListState: Equatable {
    case idle
    case loading
    case loaded([Recurrence])
    case failed(String)
}

@MainActor
@Observable
final class RecurrencesViewModel {
    private(set) var listState: RecurrenceListState = .idle
    private(set) var creationState: RecurrenceCreationState = .editing
    private(set) var creationErrorMessage: String?
    private(set) var cancellationConfirmation: Recurrence?
    private(set) var cancellingIDs: Set<String> = []
    private(set) var cancellationErrors: [String: String] = [:]
    private(set) var isPresentingCreate = false

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
    private(set) var startsOn: RecurrenceCivilDate {
        didSet {
            guard startsOn != oldValue else { return }
            draftDidChange()
        }
    }

    private let api: any FinancialAPI
    private let moneyParser: BRLMoneyParser
    private let makeIdempotencyKey: () -> String
    private let onRecurrenceConfirmed: (String?) -> Void
    private let initialStartsOn: RecurrenceCivilDate
    @ObservationIgnored private var draftGeneration: UInt64 = 0
    @ObservationIgnored private var loadGeneration: UInt64 = 0
    @ObservationIgnored private var completedLoadGeneration: UInt64 = 0
    @ObservationIgnored private var listRevision: UInt64 = 0
    @ObservationIgnored private var reconciledListRevision: UInt64 = 0
    @ObservationIgnored private var activeLoadRevision: UInt64?
    @ObservationIgnored private var needsReconciliationAfterLoad = false
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var cancelKeys: [String: String] = [:]

    init(
        api: any FinancialAPI,
        now: Date = Date(),
        calendar: Calendar = .autoupdatingCurrent,
        moneyParser: BRLMoneyParser = BRLMoneyParser(),
        makeIdempotencyKey: @escaping () -> String = { UUID().uuidString },
        onRecurrenceConfirmed: @escaping (String?) -> Void = { _ in }
    ) {
        self.api = api
        self.moneyParser = moneyParser
        self.makeIdempotencyKey = makeIdempotencyKey
        self.onRecurrenceConfirmed = onRecurrenceConfirmed
        let components = calendar.dateComponents([.year, .month, .day], from: now)
        let civilDate = try? RecurrenceCivilDate(
            year: components.year ?? 0,
            month: components.month ?? 0,
            day: components.day ?? 0
        )
        let resolvedCivilDate = civilDate
            ?? (try! RecurrenceCivilDate(year: 2001, month: 1, day: 1))
        initialStartsOn = resolvedCivilDate
        startsOn = resolvedCivilDate
    }

    var recurrences: [Recurrence] {
        guard case let .loaded(items) = listState else { return [] }
        return items
    }

    var reviewedRecurrence: ReviewedRecurrence? {
        switch creationState {
        case let .reviewing(reviewed), let .submitting(reviewed),
             let .retryable(reviewed), let .requiresEditing(reviewed):
            reviewed
        case .editing, .previewing, .success:
            nil
        }
    }

    var successfulRecurrence: Recurrence? {
        guard case let .success(recurrence) = creationState else { return nil }
        return recurrence
    }

    var isCreationBusy: Bool {
        switch creationState {
        case .previewing, .submitting: true
        case .editing, .reviewing, .retryable, .requiresEditing, .success: false
        }
    }

    var startsOnPickerDate: Date { startsOn.pickerDate }

    func setStartsOnPickerDate(_ date: Date) {
        guard let civilDate = try? RecurrenceCivilDate(pickerDate: date) else { return }
        startsOn = civilDate
    }

    func loadIfNeeded() async {
        switch listState {
        case .idle:
            await startLoad()
        case .loading:
            await waitForActiveLoad()
        case .loaded, .failed:
            return
        }
    }

    func retryList() async {
        guard case .failed = listState else { return }
        await startLoad()
    }

    func refresh() async {
        await startLoad()
    }

    func beginCreation() {
        guard !isPresentingCreate else { return }
        resetCreationDraft()
        isPresentingCreate = true
    }

    func beginSuggestionReview(preview: RecurrencePreview, suggestionID: String) {
        guard !isPresentingCreate, RecurrenceSuggestion.isValidID(suggestionID) else { return }
        resetCreationDraft()
        let frozen = RecurrenceRequest(
            description: preview.description,
            expectedAmount: preview.expectedAmount,
            startsOn: preview.startsOn
        )
        creationState = .reviewing(
            ReviewedRecurrence(
                preview: preview,
                request: frozen,
                idempotencyKey: makeIdempotencyKey(),
                source: .suggestion(id: suggestionID)
            )
        )
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

        let amountMinor: Int64
        do {
            amountMinor = try moneyParser.parseMinorUnits(amountText)
        } catch {
            creationErrorMessage = "Informe um valor esperado maior que zero com até duas casas decimais."
            return
        }
        guard !description.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            creationErrorMessage = "Informe uma descrição para a recorrência."
            return
        }

        let request = RecurrenceRequest(
            description: description,
            expectedAmount: FinancialMoney(minor: amountMinor, currency: .brl),
            startsOn: startsOn
        )
        let previewGeneration = draftGeneration
        creationState = .previewing

        do {
            let preview = try await api.previewRecurrence(request)
            guard canInstallPreview(generation: previewGeneration) else { return }
            let frozen = RecurrenceRequest(
                description: preview.description,
                expectedAmount: preview.expectedAmount,
                startsOn: preview.startsOn
            )
            creationState = .reviewing(
                ReviewedRecurrence(
                    preview: preview,
                    request: frozen,
                    idempotencyKey: makeIdempotencyKey(),
                    source: .manual
                )
            )
        } catch is CancellationError {
            guard canInstallPreview(generation: previewGeneration) else { return }
            creationState = .editing
        } catch {
            guard canInstallPreview(generation: previewGeneration) else { return }
            creationState = .editing
            creationErrorMessage = userMessage(for: error)
        }
    }

    func confirm() async {
        let reviewed: ReviewedRecurrence
        switch creationState {
        case let .reviewing(value), let .retryable(value):
            reviewed = value
        case .editing, .previewing, .submitting, .requiresEditing, .success:
            return
        }

        creationErrorMessage = nil
        creationState = .submitting(reviewed)
        do {
            let result = try await api.createRecurrence(
                reviewed.request,
                idempotencyKey: reviewed.idempotencyKey
            )
            install(result.recurrence)
            creationState = .success(result.recurrence)
            onRecurrenceConfirmed(reviewed.source.suggestionID)
        } catch is CancellationError {
            creationState = .retryable(reviewed)
        } catch {
            creationErrorMessage = userMessage(for: error)
            if isRetryable(error) {
                creationState = .retryable(reviewed)
            } else {
                creationState = .requiresEditing(reviewed)
            }
        }
    }

    func editCreation() {
        guard reviewedRecurrence?.source == .manual else { return }
        switch creationState {
        case .reviewing, .retryable, .requiresEditing:
            creationErrorMessage = nil
            creationState = .editing
        case .editing, .previewing, .submitting, .success:
            break
        }
    }

    func finishCreation() {
        guard case .success = creationState else { return }
        isPresentingCreate = false
        resetCreationDraft()
        Task { await reconcileCurrentMutation() }
    }

    func requestCancellation(_ recurrence: Recurrence) {
        guard recurrence.status == .active, !cancellingIDs.contains(recurrence.id) else { return }
        cancellationConfirmation = recurrence
    }

    func dismissCancellationConfirmation() {
        cancellationConfirmation = nil
    }

    func confirmCancellation() async {
        guard let recurrence = cancellationConfirmation else { return }
        await confirmCancellation(recurrence)
    }

    func confirmCancellation(_ recurrence: Recurrence) async {
        cancellationConfirmation = nil
        let key = cancelKeys[recurrence.id] ?? makeIdempotencyKey()
        cancelKeys[recurrence.id] = key
        await performCancellation(recurrenceID: recurrence.id, key: key)
    }

    func retryCancellation(id: String) async {
        guard let key = cancelKeys[id], !cancellingIDs.contains(id) else { return }
        await performCancellation(recurrenceID: id, key: key)
    }

    func canRetryCancellation(id: String) -> Bool {
        cancelKeys[id] != nil && !cancellingIDs.contains(id)
    }

    private func startLoad() async {
        let task = beginLoadIfNeeded()
        await task.value
    }

    private func beginLoadIfNeeded() -> Task<Void, Never> {
        if let loadTask { return loadTask }

        loadGeneration &+= 1
        let generation = loadGeneration
        let revision = listRevision
        let task = Task { @MainActor [api] in
            let result: RecurrenceListState
            do {
                result = .loaded(try await api.recurrences().items)
            } catch {
                result = .failed(
                    (error as? FinancialAPIError)?.userMessage
                        ?? "Não foi possível carregar as recorrências. Tente novamente."
                )
            }
            self.completeLoad(result, generation: generation, revision: revision)
        }
        loadTask = task
        activeLoadRevision = revision
        if case .loaded = listState {
            // Keep the latest visible content while a refresh is in flight.
        } else {
            listState = .loading
        }
        return task
    }

    private func waitForActiveLoad() async {
        guard let loadTask else {
            listState = .failed("Não foi possível carregar as recorrências. Tente novamente.")
            return
        }
        await loadTask.value
    }

    private func completeLoad(
        _ result: RecurrenceListState,
        generation: UInt64,
        revision: UInt64
    ) {
        guard generation == loadGeneration else { return }

        let needsReconciliation = needsReconciliationAfterLoad
        needsReconciliationAfterLoad = false
        if revision == listRevision {
            listState = result
            if case .loaded = result {
                reconciledListRevision = revision
            }
        }
        loadTask = nil
        activeLoadRevision = nil
        completedLoadGeneration = generation

        if needsReconciliation {
            _ = beginLoadIfNeeded()
        }
    }

    private func reconcileAfterCurrentLoad() async {
        let generationBeforeRequest = loadGeneration
        if loadTask != nil {
            needsReconciliationAfterLoad = true
        } else {
            _ = beginLoadIfNeeded()
        }

        while completedLoadGeneration <= generationBeforeRequest {
            guard let loadTask else {
                _ = beginLoadIfNeeded()
                continue
            }
            await loadTask.value
        }
    }

    private func reconcileCurrentMutation() async {
        guard reconciledListRevision != listRevision else { return }
        if let loadTask {
            if activeLoadRevision != listRevision {
                needsReconciliationAfterLoad = true
            }
            await loadTask.value
            return
        }
        await startLoad()
    }

    private func performCancellation(recurrenceID: String, key: String) async {
        guard !cancellingIDs.contains(recurrenceID) else { return }
        cancellingIDs.insert(recurrenceID)
        cancellationErrors[recurrenceID] = nil
        defer { cancellingIDs.remove(recurrenceID) }

        do {
            let result = try await api.cancelRecurrence(id: recurrenceID, idempotencyKey: key)
            install(result.recurrence)
            cancelKeys[recurrenceID] = nil
        } catch is CancellationError {
            cancellationErrors[recurrenceID] = "O cancelamento foi interrompido. Tente novamente."
        } catch let error as FinancialAPIError where error == .alreadyCancelled || error == .notFound {
            cancelKeys[recurrenceID] = nil
            cancellationErrors[recurrenceID] = error.userMessage
            await reconcileAfterCurrentLoad()
            if case let .loaded(items) = listState,
               !items.contains(where: { $0.id == recurrenceID && $0.status == .active })
            {
                cancellationErrors[recurrenceID] = nil
            }
        } catch {
            cancellationErrors[recurrenceID] = userMessage(for: error)
            if !isRetryable(error) {
                cancelKeys[recurrenceID] = nil
            }
        }
    }

    private func install(_ recurrence: Recurrence) {
        listRevision &+= 1
        var items = recurrences
        if let index = items.firstIndex(where: { $0.id == recurrence.id }) {
            items[index] = recurrence
        } else {
            items.append(recurrence)
        }
        listState = .loaded(Self.sorted(items))
        cancellationErrors[recurrence.id] = nil
        if loadTask != nil {
            needsReconciliationAfterLoad = true
        }
    }

    private static func sorted(_ items: [Recurrence]) -> [Recurrence] {
        items.sorted { lhs, rhs in
            if lhs.status != rhs.status { return lhs.status == .active }
            if lhs.startsOn != rhs.startsOn { return lhs.startsOn.canonicalValue > rhs.startsOn.canonicalValue }
            if lhs.createdAt != rhs.createdAt { return lhs.createdAt > rhs.createdAt }
            return lhs.id > rhs.id
        }
    }

    private func resetCreationDraft() {
        description = ""
        amountText = ""
        startsOn = initialStartsOn
        creationErrorMessage = nil
        creationState = .editing
    }

    private func draftDidChange() {
        draftGeneration &+= 1
        switch creationState {
        case .previewing, .reviewing, .retryable, .requiresEditing:
            creationErrorMessage = nil
            creationState = .editing
        case .editing, .submitting, .success:
            break
        }
    }

    private func canInstallPreview(generation: UInt64) -> Bool {
        guard generation == draftGeneration else { return false }
        guard case .previewing = creationState else { return false }
        return true
    }

    private func userMessage(for error: Error) -> String {
        (error as? FinancialAPIError)?.userMessage
            ?? "Não foi possível concluir a operação. Tente novamente."
    }

    private func isRetryable(_ error: Error) -> Bool {
        guard let error = error as? FinancialAPIError else { return true }
        return switch error {
        case .connectionUnavailable, .serviceUnavailable, .invalidResponse:
            true
        case .invalidData, .conflict, .notFound, .alreadyCancelled,
             .suggestionNotFound, .suggestionSuppressed, .configuration:
            false
        }
    }
}
