import SwiftUI

struct SafeAvailableView: View {
    @Bindable var model: SafeAvailableViewModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                periodSelection
                budgetSection
                content
            }
            .padding()
        }
        .background(JARVISDesign.canvas.ignoresSafeArea())
        .navigationTitle("Disponível Seguro")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("safeAvailable.screen")
        .task { await model.loadIfNeeded() }
        .task { await model.loadBudgetIfNeeded() }
    }

    private var periodSelection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Período explícito")
                .font(.headline)
            DatePicker(
                "Início",
                selection: Binding(
                    get: { model.periodStartPickerDate },
                    set: { model.setPeriodStart($0) }
                ),
                displayedComponents: .date
            )
            .accessibilityIdentifier("safeAvailable.periodStart")
            DatePicker(
                "Fim",
                selection: Binding(
                    get: { model.periodEndPickerDate },
                    set: { model.setPeriodEnd($0) }
                ),
                displayedComponents: .date
            )
            .accessibilityIdentifier("safeAvailable.periodEnd")
            Button {
                Task { await model.load(forceRefresh: true) }
            } label: {
                Label("Consultar disponibilidade", systemImage: "arrow.clockwise")
                    .frame(maxWidth: .infinity, minHeight: 44)
            }
            .buttonStyle(JARVISPrimaryButtonStyle())
            .disabled(model.state == .loading)
            .accessibilityIdentifier("safeAvailable.load")
        }
        .jarvisCard(padding: 18)
    }

    private var budgetSection: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Orçamento mensal")
                .font(.headline)
            switch model.budgetState {
            case .loading:
                ProgressView("Consultando orçamento")
                    .accessibilityIdentifier("safeAvailable.budget.loading")
            case .saving:
                ProgressView("Salvando orçamento")
                    .accessibilityIdentifier("safeAvailable.budget.saving")
            case .absent, .idle:
                Text("Nenhum orçamento definido para o mês do período.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("safeAvailable.budget.absent")
            case let .loaded(budget):
                Text("Definido: \(SafeAvailableMoneyFormatter.string(minor: budget.amount.minor))")
                    .font(.subheadline)
                    .accessibilityIdentifier("safeAvailable.budget.value")
            case let .failed(message):
                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("safeAvailable.budget.error")
            }
            TextField("Valor em reais", text: $model.budgetAmountText)
                .textFieldStyle(.roundedBorder)
                .keyboardType(.decimalPad)
                .accessibilityLabel("Valor do orçamento mensal")
                .accessibilityIdentifier("safeAvailable.budget.input")
            Button("Salvar orçamento") {
                Task { await model.saveBudget() }
            }
            .buttonStyle(.bordered)
            .frame(minHeight: 44)
            .disabled(model.budgetState == .loading || model.budgetState == .saving)
            .accessibilityIdentifier("safeAvailable.budget.save")
        }
        .jarvisCard(padding: 18)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("safeAvailable.budget")
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle:
            EmptyView()
        case .loading:
            ProgressView("Calculando disponibilidade")
                .frame(maxWidth: .infinity, minHeight: 120)
                .accessibilityIdentifier("safeAvailable.loading")
        case let .failed(message):
            VStack(spacing: 12) {
                Label("Não foi possível carregar", systemImage: "wifi.exclamationmark")
                    .font(.headline)
                Text(message)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
                Button("Tentar novamente") { Task { await model.retry() } }
                    .buttonStyle(.borderedProminent)
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("safeAvailable.retry")
            }
            .frame(maxWidth: .infinity)
            .padding()
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("safeAvailable.error")
        case let .empty(response):
            resultView(response, empty: true)
        case let .loaded(response):
            resultView(response, empty: false)
        }
    }

    private func resultView(_ response: SafeAvailableResponse, empty: Bool) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Resultado do período")
                    .font(.headline)
                Text(SafeAvailableMoneyFormatter.string(minor: response.finalAmount.minor))
                    .font(.largeTitle.weight(.semibold).monospacedDigit())
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("safeAvailable.finalAmount")
                    .accessibilityLabel(
                        "Disponível seguro "
                            + SafeAvailableMoneyFormatter.string(minor: response.finalAmount.minor)
                    )
            }
            .padding()
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                resultTint.opacity(0.13),
                in: RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous)
                    .stroke(resultTint.opacity(0.2), lineWidth: 1)
            }
            .accessibilityElement(children: .contain)

            if empty {
                Text("Nenhuma entrada confirmada neste período.")
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("safeAvailable.empty")
            }

            missingDataView(response.missingData)
            breakdownView(response.breakdown)
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("safeAvailable.content")
    }

    private var resultTint: Color {
        switch model.state {
        case let .empty(response), let .loaded(response):
            if response.finalAmount.minor < 0 { return JARVISDesign.negative }
            if response.finalAmount.minor == 0 { return JARVISDesign.muted }
            return JARVISDesign.positive
        default:
            return JARVISDesign.accent
        }
    }

    private func missingDataView(_ values: [SafeAvailableMissingData]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Dados e hipóteses")
                .font(.headline)
            ForEach(values, id: \.self) { value in
                Label(value.displayName, systemImage: "info.circle")
                    .font(.subheadline)
            }
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .jarvisCard(padding: 16)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(values.map(\.displayName).joined(separator: ", "))
        .accessibilityIdentifier("safeAvailable.missingData")
    }

    private func breakdownView(_ lines: [SafeAvailableBreakdown]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Composição")
                .font(.headline)
                .padding(.bottom, 8)
                .accessibilityIdentifier("safeAvailable.breakdown")
            ForEach(lines) { line in
                HStack(alignment: .top, spacing: 12) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(line.kind.displayName)
                            .font(.body.weight(.medium))
                        Text(line.dueOn.displayValue)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                        if line.kind == .commitment {
                            Text("Sequência \(line.sequence)")
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Spacer(minLength: 8)
                    Text(SafeAvailableMoneyFormatter.string(minor: line.amount.minor))
                        .font(.body.monospacedDigit())
                }
                .padding(.vertical, 10)
                .accessibilityElement(children: .combine)
                .accessibilityLabel(
                    "\(line.kind.displayName), \(line.dueOn.displayValue), "
                        + SafeAvailableMoneyFormatter.string(minor: line.amount.minor)
                )
                .accessibilityIdentifier("safeAvailable.breakdown.\(line.id)")
                if line.id != lines.last?.id { Divider() }
            }
        }
        .accessibilityElement(children: .contain)
        .jarvisCard(padding: 16)
    }
}

struct HistorySafeAvailableEntryView: View {
    @Bindable var model: HistoryViewModel
    let scheduledCommitments: ScheduledCommitmentsViewModel
    @Bindable var safeAvailable: SafeAvailableViewModel
    let financialGoals: FinancialGoalsViewModel
    @State private var isPresentingSafeAvailable = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                Button {
                    isPresentingSafeAvailable = true
                } label: {
                    HStack(spacing: 12) {
                        Image(systemName: "chart.line.uptrend.xyaxis")
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(JARVISDesign.positive)
                            .frame(width: 32, height: 32)
                            .background(JARVISDesign.positive.opacity(0.12), in: Circle())
                        Text("Disponível Seguro")
                            .font(.body.weight(.semibold))
                        Spacer()
                        Image(systemName: "chevron.right")
                            .font(.caption.weight(.bold))
                            .foregroundStyle(.tertiary)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(minHeight: 48)
                .buttonStyle(.plain)
                .jarvisCard(padding: 14)
                .padding(.horizontal)
                .padding(.top, 8)
                .accessibilityIdentifier("history.safeAvailable.entry")
                HistoryView(
                    model: model,
                    safeAvailable: safeAvailable,
                    scheduledCommitments: scheduledCommitments,
                    financialGoals: financialGoals
                )
            }
            .background(JARVISDesign.canvas)
            .padding(.top, 36)
            .navigationTitle("Visão geral")
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(JARVISDesign.canvas, for: .navigationBar)
        }
        .sheet(isPresented: $isPresentingSafeAvailable) {
            NavigationStack {
                SafeAvailableView(model: safeAvailable)
                    .environment(\.locale, Locale(identifier: "pt_BR"))
            }
        }
    }
}

private enum SafeAvailableMoneyFormatter {
    static func string(minor: Int64) -> String {
        let negative = minor < 0
        let magnitude = minor == Int64.min ? UInt64(Int64.max) + 1 : UInt64(abs(minor))
        let whole = magnitude / 100
        let cents = magnitude % 100
        return "R$ \(negative ? "-" : "")\(whole),\(String(format: "%02llu", cents))"
    }
}

private extension SafeAvailableBreakdownKind {
    var displayName: String {
        switch self {
        case .availableBalance: "Saldo de entrada"
        case .income: "Receita confirmada"
        case .expense: "Despesa confirmada"
        case .commitment: "Compromisso confirmado"
        }
    }
}

private extension SafeAvailableMissingData {
    var displayName: String {
        switch self {
        case .budget: "Orçamento mensal ausente (não tratado como zero)"
        case .confirmedIncome: "Receitas confirmadas ausentes"
        case .confirmedExpense: "Despesas confirmadas ausentes"
        case .commitments: "Compromissos confirmados ausentes"
        }
    }
}
