import Foundation
import Observation

enum InstallmentPlanListState: Equatable {
    case idle
    case loading
    case loaded([InstallmentPlan])
    case failed(String)
}

enum InstallmentPlanDetailState: Equatable {
    case idle
    case loading
    case loaded(InstallmentPlan)
    case failed(String)
}

enum InstallmentPlanCancellationState: Equatable {
    case idle
    case previewing
    case reviewing(InstallmentPlanCancellationPreview)
    case submitting(InstallmentPlanCancellationPreview)
    case retryable(InstallmentPlanCancellationPreview)
    case cancelled(InstallmentPlan)
}

@MainActor
@Observable
final class InstallmentPlansViewModel {
    private(set) var listState: InstallmentPlanListState = .idle
    private(set) var detailStates: [String: InstallmentPlanDetailState] = [:]
    private(set) var cancellationStates: [String: InstallmentPlanCancellationState] = [:]
    private(set) var errors: [String: String] = [:]

    private let api: any FinancialAPI
    private let makeIdempotencyKey: () -> String
    private var cancellationKeys: [String: String] = [:]
    private var listTask: Task<Void, Never>?
    private var detailTasks: [String: Task<Void, Never>] = [:]
    private var cancellationTasks: [String: Task<Void, Never>] = [:]
    private var listGeneration: UInt64 = 0
    private var detailGenerations: [String: UInt64] = [:]
    private var cancellationGenerations: [String: UInt64] = [:]

    init(api: any FinancialAPI, makeIdempotencyKey: @escaping () -> String = { UUID().uuidString }) {
        self.api = api
        self.makeIdempotencyKey = makeIdempotencyKey
    }

    var plans: [InstallmentPlan] { guard case let .loaded(items) = listState else { return [] }; return items }

    func loadIfNeeded() async {
        guard case .idle = listState else { return }
        await load()
    }

    func load(forceRefresh: Bool = false) async {
        for id in cancellationStates.keys {
            invalidateCancellationPreview(id: id)
        }
        if forceRefresh {
            listGeneration &+= 1
            listTask?.cancel()
            listTask = nil
        } else if let listTask {
            await listTask.value
            return
        }
        listGeneration &+= 1
        let generation = listGeneration
        let task = Task { @MainActor in
            let result: InstallmentPlanListState
            do { result = .loaded(try await self.api.installmentPlans().items.sorted(by: Self.sort)) }
            catch is CancellationError { result = .idle }
            catch { result = .failed(self.message(for: error)) }
            guard generation == self.listGeneration else { return }
            self.listState = result
            self.listTask = nil
        }
        listTask = task
        listState = .loading
        await task.value
    }

    func retry() async { await load() }

    func loadDetail(id: String, forceRefresh: Bool = false) async {
        guard InstallmentPlan.isValidID(id) else { detailStates[id] = .failed(FinancialAPIError.installmentPlanNotFound.userMessage); return }
        if forceRefresh {
            detailGenerations[id, default: 0] &+= 1
            detailTasks[id]?.cancel()
            detailTasks[id] = nil
        } else {
            if case .loaded = detailStates[id] { return }
            if let task = detailTasks[id] { await task.value; return }
        }
        detailGenerations[id, default: 0] &+= 1
        let generation = detailGenerations[id, default: 0]
        let task = Task { @MainActor in
            let result: InstallmentPlanDetailState
            do { result = .loaded(try await self.api.installmentPlan(id: id)) }
            catch is CancellationError { result = .idle }
            catch { result = .failed(self.message(for: error)) }
            guard generation == self.detailGenerations[id] else { return }
            self.detailStates[id] = result
            self.detailTasks[id] = nil
        }
        detailTasks[id] = task; detailStates[id] = .loading; await task.value
    }

    func refreshDetail(id: String) async {
        invalidateCancellationPreview(id: id)
        await loadDetail(id: id, forceRefresh: true)
    }

    func previewCancellation(id: String) async {
        guard cancellationTasks[id] == nil else { return }
        cancellationGenerations[id, default: 0] &+= 1
        let generation = cancellationGenerations[id, default: 0]
        cancellationStates[id] = .previewing; errors[id] = nil
        let task = Task { @MainActor in
            do {
                let preview = try await self.api.previewInstallmentPlanCancellation(id: id)
                guard generation == self.cancellationGenerations[id] else { return }
                self.cancellationStates[id] = .reviewing(preview)
            } catch is CancellationError {
                guard generation == self.cancellationGenerations[id] else { return }
                self.cancellationStates[id] = .idle
            } catch {
                guard generation == self.cancellationGenerations[id] else { return }
                await self.handleCancellationError(error, id: id, generation: generation)
            }
            guard generation == self.cancellationGenerations[id] else { return }
            self.cancellationTasks[id] = nil
        }
        cancellationTasks[id] = task; await task.value
    }

    func cancel(id: String) async {
        guard let preview = cancellationStates[id].flatMap(Self.reviewValue) else { return }
        guard cancellationTasks[id] == nil else { return }
        let task = beginCancellation(id: id, preview: preview)
        await task.value
    }

    @discardableResult
    func confirmCancellation(id: String) -> Task<Void, Never>? {
        guard case let .reviewing(preview) = cancellationStates[id] else { return nil }
        guard cancellationTasks[id] == nil else { return nil }
        return beginCancellation(id: id, preview: preview)
    }

    private func beginCancellation(id: String, preview: InstallmentPlanCancellationPreview) -> Task<Void, Never> {
        cancellationGenerations[id, default: 0] &+= 1
        let generation = cancellationGenerations[id, default: 0]
        let key = cancellationKeys[id] ?? makeIdempotencyKey(); cancellationKeys[id] = key
        cancellationStates[id] = .submitting(preview); errors[id] = nil
        let task = Task { @MainActor in
            do {
                let result = try await self.api.cancelInstallmentPlan(id: id, expectedCancelledOn: preview.expectedCancelledOn, idempotencyKey: key)
                guard generation == self.cancellationGenerations[id] else { return }
                self.cancellationStates[id] = .cancelled(result.plan)
                self.cancellationKeys[id] = nil
                self.detailStates[id] = .loaded(result.plan)
                await self.load()
            } catch is CancellationError {
                guard generation == self.cancellationGenerations[id] else { return }
                self.cancellationStates[id] = .retryable(preview)
            } catch {
                guard generation == self.cancellationGenerations[id] else { return }
                await self.handleCancellationError(error, id: id, generation: generation, preview: preview)
            }
            guard generation == self.cancellationGenerations[id] else { return }
            self.cancellationTasks[id] = nil
        }
        cancellationTasks[id] = task
        return task
    }

    func retryCancellation(id: String) async { await cancel(id: id) }

    func dismissCancellation(id: String) {
        guard case .reviewing = cancellationStates[id] else { return }
        cancellationStates[id] = .idle
    }

    private func handleCancellationError(
        _ error: Error,
        id: String,
        generation: UInt64,
        preview: InstallmentPlanCancellationPreview? = nil
    ) async {
        guard generation == cancellationGenerations[id] else { return }
        switch error as? FinancialAPIError {
        case .installmentPlanNotFound:
            cancellationKeys[id] = nil
            cancellationStates[id] = .idle
            errors[id] = FinancialAPIError.installmentPlanNotFound.userMessage
            await reconcilePlan(id: id, generation: generation)
        case .installmentPlanAlreadyCancelled:
            cancellationKeys[id] = nil
            cancellationStates[id] = .idle
            errors[id] = FinancialAPIError.installmentPlanAlreadyCancelled.userMessage
            await reconcilePlan(id: id, generation: generation)
        case .installmentCancellationDateStale:
            cancellationKeys[id] = nil
            cancellationStates[id] = .idle
            errors[id] = FinancialAPIError.installmentCancellationDateStale.userMessage
            await reconcilePlan(id: id, generation: generation)
        default:
            errors[id] = self.message(for: error)
            if let preview { cancellationStates[id] = .retryable(preview) }
            else { cancellationStates[id] = .idle }
        }
    }

    private func reconcilePlan(id: String, generation: UInt64) async {
        guard generation == cancellationGenerations[id] else { return }
        await loadDetail(id: id, forceRefresh: true)
        guard generation == cancellationGenerations[id] else { return }
        await load(forceRefresh: true)
    }

    private func invalidateCancellationPreview(id: String) {
        switch cancellationStates[id] {
        case .previewing, .reviewing, .retryable:
            break
        default:
            return
        }
        cancellationGenerations[id, default: 0] &+= 1
        cancellationTasks[id]?.cancel()
        cancellationTasks[id] = nil
        cancellationKeys[id] = nil
        cancellationStates[id] = .idle
        errors[id] = nil
    }

    private static func reviewValue(_ state: InstallmentPlanCancellationState) -> InstallmentPlanCancellationPreview? {
        switch state { case let .reviewing(value), let .retryable(value): value; default: nil }
    }

    private static func sort(_ lhs: InstallmentPlan, _ rhs: InstallmentPlan) -> Bool {
        if lhs.status != rhs.status { return lhs.status == .active }
        if lhs.firstDueDate != rhs.firstDueDate { return lhs.firstDueDate < rhs.firstDueDate }
        return lhs.id < rhs.id
    }

    private func message(for error: Error) -> String { (error as? FinancialAPIError)?.userMessage ?? "Não foi possível concluir a operação. Tente novamente." }
}
