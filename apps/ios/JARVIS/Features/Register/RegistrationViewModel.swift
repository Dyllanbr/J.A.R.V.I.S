import Foundation
import Observation

struct ReviewedExpense: Equatable, Sendable {
    let preview: ExpensePreview
    let request: ExpenseRequest
}

enum RegistrationState: Equatable {
    case editing
    case previewing
    case reviewing(ReviewedExpense)
    case submitting(ReviewedExpense)
    case retryable(ReviewedExpense)
    case requiresEditing(ReviewedExpense)
    case success(Expense)
}

@MainActor
@Observable
final class RegistrationViewModel {
    var description = ""
    var amountText = ""
    var paymentMethod: PaymentMethod = .pix
    var occurredAt: Date
    private(set) var state: RegistrationState = .editing
    private(set) var errorMessage: String?

    private let api: any FinancialAPI
    private let moneyParser: BRLMoneyParser
    private let timestampCodec: RFC3339DateCodec
    private let makeIdempotencyKey: () -> String
    private let onExpenseRecorded: () -> Void
    private var pendingIdempotencyKey: String?

    init(
        api: any FinancialAPI,
        now: Date = Date(),
        moneyParser: BRLMoneyParser = BRLMoneyParser(),
        timestampCodec: RFC3339DateCodec = RFC3339DateCodec(),
        makeIdempotencyKey: @escaping () -> String = { UUID().uuidString },
        onExpenseRecorded: @escaping () -> Void = {}
    ) {
        self.api = api
        occurredAt = now
        self.moneyParser = moneyParser
        self.timestampCodec = timestampCodec
        self.makeIdempotencyKey = makeIdempotencyKey
        self.onExpenseRecorded = onExpenseRecorded
    }

    var reviewedExpense: ReviewedExpense? {
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

    var successfulExpense: Expense? {
        guard case let .success(expense) = state else { return nil }
        return expense
    }

    var isBusy: Bool {
        switch state {
        case .previewing, .submitting:
            true
        case .editing, .reviewing, .retryable, .requiresEditing, .success:
            false
        }
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
            errorMessage = "Informe uma descrição para a despesa."
            return
        }

        let request = ExpenseRequest(
            description: description,
            amount: ExpenseMoney(minor: amountMinor, currency: .brl),
            paymentMethod: paymentMethod,
            occurredAt: timestampCodec.encode(occurredAt)
        )
        state = .previewing

        do {
            let preview = try await api.preview(request)
            let frozenRequest = ExpenseRequest(
                description: preview.description,
                amount: preview.amount,
                paymentMethod: preview.paymentMethod,
                occurredAt: preview.occurredAt
            )
            pendingIdempotencyKey = nil
            state = .reviewing(ReviewedExpense(preview: preview, request: frozenRequest))
        } catch is CancellationError {
            state = .editing
        } catch {
            state = .editing
            errorMessage = userMessage(for: error)
        }
    }

    func confirm() async {
        let reviewed: ReviewedExpense
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
            let result = try await api.create(reviewed.request, idempotencyKey: key)
            pendingIdempotencyKey = nil
            clearDraft()
            state = .success(result.expense)
            onExpenseRecorded()
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
        guard case .success = state else { return }
        occurredAt = now
        pendingIdempotencyKey = nil
        errorMessage = nil
        state = .editing
    }

    private func clearDraft() {
        description = ""
        amountText = ""
        paymentMethod = .pix
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
        case .invalidData, .conflict, .configuration:
            false
        }
    }
}
