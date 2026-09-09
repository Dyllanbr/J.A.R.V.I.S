import SwiftUI

struct PurchaseSimulationView: View {
    @Bindable var model: PurchaseSimulationViewModel
    let card: CreditCard

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                form
                content
            }
            .padding()
        }
        .navigationTitle("Posso comprar?")
        .navigationBarTitleDisplayMode(.inline)
        .tint(JARVISDesign.accent)
        .background(JARVISDesign.canvas.ignoresSafeArea())
        .accessibilityIdentifier("purchaseSimulation.screen")
        .onAppear { model.configure(card: card) }
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Simule o impacto sem registrar uma compra.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Text("Cartão: \(card.name)")
                .font(.headline)
                .accessibilityIdentifier("purchaseSimulation.card")
            TextField("Valor da compra", text: $model.amountText)
                .keyboardType(.decimalPad)
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("purchaseSimulation.amount")
            Picker("Modalidade", selection: $model.purchaseMode) {
                ForEach(PurchaseSimulationMode.allCases, id: \.self) { mode in
                    Text(mode.displayName).tag(mode)
                }
            }
            .accessibilityIdentifier("purchaseSimulation.mode")
            if model.purchaseMode == .installment {
                TextField("Número de parcelas", text: $model.installmentCountText)
                    .keyboardType(.numberPad)
                    .textFieldStyle(.roundedBorder)
                    .accessibilityIdentifier("purchaseSimulation.installments")
            }
            DatePicker(
                "Data da compra",
                selection: Binding(
                    get: { model.purchaseOn.pickerDate },
                    set: { value in model.setPurchaseOn(value) }
                ),
                displayedComponents: .date
            )
            .accessibilityIdentifier("purchaseSimulation.purchaseOn")
            DatePicker(
                "Início do período",
                selection: Binding(
                    get: { model.periodStart.pickerDate },
                    set: { value in model.setPeriodStart(value) }
                ),
                displayedComponents: .date
            )
            .accessibilityIdentifier("purchaseSimulation.periodStart")
            DatePicker(
                "Fim do período",
                selection: Binding(
                    get: { model.periodEnd.pickerDate },
                    set: { value in model.setPeriodEnd(value) }
                ),
                displayedComponents: .date
            )
            .accessibilityIdentifier("purchaseSimulation.periodEnd")
            Button {
                Task { await model.simulate() }
            } label: {
                HStack {
                    if model.isBusy { ProgressView() }
                    Text(model.isBusy ? "Calculando…" : "Simular compra")
                }
                .frame(maxWidth: .infinity, minHeight: 44)
            }
            .buttonStyle(JARVISPrimaryButtonStyle())
            .disabled(model.isBusy)
            .accessibilityIdentifier("purchaseSimulation.simulate")
        }
        .jarvisCard(padding: 18)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("purchaseSimulation.form")
    }

    @ViewBuilder private var content: some View {
        switch model.state {
        case .idle:
            EmptyView()
        case .loading:
            ProgressView("Calculando impacto")
                .frame(maxWidth: .infinity, minHeight: 100)
                .accessibilityIdentifier("purchaseSimulation.loading")
        case let .failed(message):
            VStack(spacing: 12) {
                Label("Não foi possível simular", systemImage: "exclamationmark.triangle")
                    .font(.headline)
                Text(message)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                Button("Tentar novamente") { Task { await model.retry() } }
                    .frame(minHeight: 44)
                    .buttonStyle(.borderedProminent)
                    .accessibilityIdentifier("purchaseSimulation.retry")
            }
            .frame(maxWidth: .infinity)
            .padding()
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("purchaseSimulation.error")
        case let .loaded(response):
            result(response)
        }
    }

    private func result(_ response: PurchaseSimulationResponse) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            VStack(alignment: .leading, spacing: 6) {
                Text("Impacto estimado")
                    .font(.headline)
                Text(money(response.projected.finalAmount.minor))
                    .font(.largeTitle.weight(.semibold).monospacedDigit())
                    .accessibilityIdentifier("purchaseSimulation.projected")
                Text("Variação: \(money(response.impact.minor))")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("purchaseSimulation.impact")
            }
            .padding()
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(JARVISDesign.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous))
            .accessibilityElement(children: .combine)

            VStack(alignment: .leading, spacing: 8) {
                Text("Antes da compra")
                    .font(.headline)
                Text(money(response.baseline.finalAmount.minor))
                    .accessibilityIdentifier("purchaseSimulation.baseline")
                Text("Depois da compra")
                    .font(.headline)
                Text(money(response.projected.finalAmount.minor))
            }
            .jarvisCard(padding: 16)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("purchaseSimulation.comparison")

            VStack(alignment: .leading, spacing: 8) {
                Text("Compromissos adicionados")
                    .font(.headline)
                if response.hypotheticalCommitments.isEmpty {
                    Text("Esta compra não gera compromisso dentro do período selecionado.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                } else {
                    ForEach(response.hypotheticalCommitments) { line in
                        HStack(alignment: .firstTextBaseline) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(line.kind == .commitment ? "Compromisso" : line.kind.rawValue)
                                    .font(.subheadline.weight(.semibold))
                                Text("Parcela \(line.sequence) · \(line.dueOn.canonicalValue)")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer(minLength: 8)
                            Text(money(line.amount.minor))
                                .font(.subheadline.monospacedDigit())
                        }
                        .accessibilityElement(children: .combine)
                        .accessibilityIdentifier("purchaseSimulation.commitment.\(line.sourceID).\(line.sequence)")
                    }
                }
            }
            .jarvisCard(padding: 16)
            .accessibilityIdentifier("purchaseSimulation.commitments")

            VStack(alignment: .leading, spacing: 8) {
                Text("Hipóteses")
                    .font(.headline)
                Text("Nenhuma compra foi registrada ou persistida.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                ForEach(response.assumptions, id: \.self) { assumption in
                    Text(assumption.displayName)
                        .font(.subheadline)
                }
            }
            .jarvisCard(padding: 16)
            .accessibilityIdentifier("purchaseSimulation.assumptions")
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("purchaseSimulation.result")
    }

    private func money(_ minor: Int64) -> String {
        let negative = minor < 0
        let magnitude = minor == Int64.min ? UInt64(Int64.max) + 1 : UInt64(abs(minor))
        let whole = magnitude / 100
        let cents = magnitude % 100
        return "R$ \(negative ? "-" : "")\(whole),\(String(format: "%02llu", cents))"
    }
}

private extension PurchaseSimulationMode {
    var displayName: String {
        switch self {
        case .oneTime: "À vista"
        case .installment: "Parcelada"
        }
    }
}

private extension PurchaseSimulationAssumption {
    var displayName: String {
        switch self {
        case .notPersisted: "Simulação não persistida"
        case .noExpenseCreated: "Nenhuma despesa criada"
        }
    }
}
