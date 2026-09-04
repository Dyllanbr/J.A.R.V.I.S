import SwiftUI

struct ScheduledCommitmentsView: View {
    @Bindable var model: ScheduledCommitmentsViewModel

    private let moneyFormatter = BRLMoneyFormatter()

    var body: some View {
        ZStack(alignment: .topLeading) {
            content
            Color.clear
                .frame(width: 1, height: 1)
                .accessibilityElement()
                .accessibilityLabel("Compromissos futuros")
                .accessibilityIdentifier("scheduledCommitments.screen")
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .navigationTitle("Compromissos futuros")
        .navigationBarTitleDisplayMode(.inline)
            .task {
                await model.loadIfNeeded()
            }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle, .loading:
            ProgressView("Carregando compromissos")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("scheduledCommitments.loading")
        case .empty:
            VStack(spacing: 12) {
                Image(systemName: "calendar")
                    .font(.largeTitle)
                    .foregroundStyle(.secondary)
                Text("Nenhum compromisso futuro")
                    .font(.title3.weight(.semibold))
                    .accessibilityIdentifier("scheduledCommitments.empty")
                Text("Parcelas e recorrências futuras aparecerão aqui quando existirem.")
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("scheduledCommitments.empty")
        case .failed:
            VStack(spacing: 12) {
                Label("Não foi possível carregar", systemImage: "wifi.exclamationmark")
                    .font(.title3.weight(.semibold))
                    .accessibilityIdentifier("scheduledCommitments.error")
                Text(modelErrorMessage)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                Button("Tentar novamente") {
                    Task { await model.retry() }
                }
                .buttonStyle(.borderedProminent)
                .frame(minHeight: 44)
                .accessibilityIdentifier("scheduledCommitments.retry")
            }
            .padding()
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("scheduledCommitments.error")
        case .loaded:
            VStack(spacing: 0) {
                Text("Próximos compromissos")
                    .font(.headline)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal)
                    .padding(.vertical, 8)
                    .accessibilityIdentifier("scheduledCommitments.list")
                List(model.items, id: \.identity) { item in
                    commitmentRow(item)
                }
                .listStyle(.plain)
                .refreshable {
                    await model.refresh()
                }
            }
        }
    }

    private var modelErrorMessage: String {
        guard case let .failed(message) = model.state else { return "" }
        return message
    }

    private func commitmentRow(_ item: ScheduledCommitment) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(item.source.displayName)
                    .font(.headline)
                    .accessibilityIdentifier("scheduledCommitment.source")
                Spacer()
                Text(moneyFormatter.string(minorUnits: item.amount.minor))
                    .font(.headline)
                    .accessibilityIdentifier("scheduledCommitment.amount")
            }
            HStack {
                Text("\(item.source.sequenceLabel) \(item.sequence)")
                    .accessibilityIdentifier("scheduledCommitment.sequence")
                Spacer()
                Text(item.dueOn.displayValue)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("scheduledCommitment.dueOn")
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "\(item.source.accessibilityName), \(item.source.sequenceLabel) \(item.sequence), "
                + "\(item.dueOn.displayValue), \(moneyFormatter.string(minorUnits: item.amount.minor))"
        )
        .accessibilityIdentifier("scheduledCommitment.\(item.source.rawValue).\(item.sourceID).\(item.sequence)")
    }
}

private extension ScheduledCommitmentSource {
    var displayName: String {
        switch self {
        case .installmentPlan: "Parcela"
        case .recurrence: "Recorrência"
        }
    }

    var sequenceLabel: String {
        switch self {
        case .installmentPlan: "Parcela"
        case .recurrence: "Recorrência"
        }
    }

    var accessibilityName: String {
        switch self {
        case .installmentPlan: "Parcela de compra no cartão"
        case .recurrence: "Recorrência mensal"
        }
    }
}
