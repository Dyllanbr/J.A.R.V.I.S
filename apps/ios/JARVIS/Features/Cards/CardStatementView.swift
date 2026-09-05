import SwiftUI

struct CardStatementView: View {
    @Bindable var model: CardStatementViewModel
    let card: CreditCard

    private let moneyFormatter = BRLMoneyFormatter()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                selection
                content
            }
            .padding()
        }
        .navigationTitle("Fatura do cartão")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("cardStatement.screen")
        .task { await model.loadIfNeeded() }
    }

    private var selection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(card.name)
                .font(.headline)
                .accessibilityIdentifier("cardStatement.card")
            DatePicker(
                "Vencimento",
                selection: Binding(
                    get: { model.statementDueOn.pickerDate },
                    set: { newDate in
                        guard let civilDate = try? RecurrenceCivilDate(pickerDate: newDate) else { return }
                        model.statementDueOn = civilDate
                    }
                ),
                displayedComponents: .date
            )
            .accessibilityIdentifier("cardStatement.statementDueOn")
            Button {
                Task { await model.load(forceRefresh: true) }
            } label: {
                HStack {
                    Image(systemName: "arrow.clockwise")
                    Text("Consultar fatura")
                    Spacer()
                }
                .frame(minHeight: 44)
            }
            .buttonStyle(.borderedProminent)
            .disabled(model.state == .loading)
            .accessibilityIdentifier("cardStatement.load")
        }
        .padding()
        .background(Color.secondary.opacity(0.08), in: RoundedRectangle(cornerRadius: 12))
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle:
            EmptyView()
        case .loading:
            ProgressView("Carregando fatura")
                .frame(maxWidth: .infinity, minHeight: 120)
                .accessibilityIdentifier("cardStatement.loading")
        case let .failed(message):
            VStack(spacing: 12) {
                Label("Não foi possível carregar", systemImage: "wifi.exclamationmark")
                    .font(.headline)
                    .accessibilityIdentifier("cardStatement.error")
                Text(message)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                Button("Tentar novamente") {
                    Task { await model.retry() }
                }
                .frame(minHeight: 44)
                .accessibilityIdentifier("cardStatement.retry")
            }
            .frame(maxWidth: .infinity)
            .padding()
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("cardStatement.errorState")
        case let .empty(statement):
            totalView(statement.totalAmount)
            VStack(spacing: 8) {
                Image(systemName: "doc.text")
                    .font(.largeTitle)
                    .foregroundStyle(.secondary)
                Text("Nenhum lançamento nesta fatura")
                    .font(.headline)
                Text("Não há compras para este ciclo.")
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 24)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("cardStatement.empty")
        case let .loaded(statement):
            totalView(statement.totalAmount)
            VStack(alignment: .leading, spacing: 0) {
                Text("Lançamentos")
                    .font(.headline)
                    .padding(.bottom, 8)
                    .accessibilityIdentifier("cardStatement.lines")
                ForEach(statement.lines, id: \.expenseID) { line in
                    lineView(line)
                    if line.expenseID != statement.lines.last?.expenseID {
                        Divider()
                    }
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("cardStatement.list")
        }
    }

    private func totalView(_ total: CardStatementTotalAmount) -> some View {
        HStack(alignment: .firstTextBaseline) {
            Text("Total")
                .font(.headline)
            Spacer()
            Text(moneyFormatter.string(minorUnits: total.minor))
                .font(.title2.weight(.semibold).monospacedDigit())
        }
        .padding()
        .background(Color.accentColor.opacity(0.12), in: RoundedRectangle(cornerRadius: 12))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Total \(moneyFormatter.string(minorUnits: total.minor))")
        .accessibilityIdentifier("cardStatement.total")
    }

    private func lineView(_ line: CardStatementLine) -> some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(line.description)
                    .font(.body.weight(.medium))
                    .fixedSize(horizontal: false, vertical: true)
                Text(line.purchaseLabel)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text(line.occurredAt.displayValue)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            Text(moneyFormatter.string(minorUnits: line.amount.minor))
                .font(.body.monospacedDigit())
        }
        .padding(.vertical, 10)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("cardStatement.line.\(line.expenseID)")
        .accessibilityLabel(
            "\(line.description), \(line.purchaseLabel), \(line.occurredAt.displayValue), "
                + moneyFormatter.string(minorUnits: line.amount.minor)
        )
    }
}

private extension CardStatementLine {
    var purchaseLabel: String {
        switch purchaseMode {
        case .oneTime:
            "Compra à vista"
        case .installment:
            "Parcela \(installmentNumber ?? 0) de \(installmentCount ?? 0)"
        }
    }
}
