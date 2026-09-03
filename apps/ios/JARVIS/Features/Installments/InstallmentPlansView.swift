import SwiftUI

struct InstallmentPlansView: View {
    @Bindable var model: InstallmentPlansViewModel
    private let money = BRLMoneyFormatter()

    var body: some View {
        List {
            switch model.listState {
            case .idle, .loading:
                ProgressView("Carregando planos").accessibilityIdentifier("installmentPlans.loading")
            case let .failed(message):
                Section { Text(message).foregroundStyle(.secondary); Button("Tentar novamente") { Task { await model.retry() } } }
                    .accessibilityIdentifier("installmentPlans.error")
            case let .loaded(items):
                if items.isEmpty {
                    ContentUnavailableView("Nenhum plano de parcelas", systemImage: "calendar", description: Text("Compras parceladas aparecerão aqui."))
                        .accessibilityIdentifier("installmentPlans.empty")
                } else {
                    ForEach(items) { plan in
                        NavigationLink {
                            InstallmentPlanDetailView(model: model, planID: plan.id)
                        } label: {
                            VStack(alignment: .leading, spacing: 6) {
                                Text("\(plan.installmentCount) parcelas").font(.headline)
                                Text(money.string(minorUnits: plan.totalAmount.minor))
                                Text("Primeiro vencimento: \(plan.firstDueDate.displayValue) · \(plan.status == .active ? "Ativo" : "Cancelado")")
                                    .font(.subheadline).foregroundStyle(.secondary)
                            }.padding(.vertical, 4)
                        }.accessibilityIdentifier("installmentPlan.item.\(plan.id)")
                    }
                }
            }
        }
        .navigationTitle("Planos de parcelas")
        .task { await model.loadIfNeeded() }
        .refreshable { await model.load() }
        .accessibilityIdentifier("installmentPlans.list")
    }
}

private struct InstallmentPlanDetailView: View {
    @Bindable var model: InstallmentPlansViewModel
    let planID: String
    private let money = BRLMoneyFormatter()

    var body: some View {
        Group {
            switch model.detailStates[planID] ?? .idle {
            case .idle, .loading: ProgressView("Carregando plano")
            case let .failed(message): ContentUnavailableView("Plano indisponível", systemImage: "exclamationmark.triangle", description: Text(message))
            case let .loaded(plan): detail(plan)
            }
        }
        .navigationTitle("Plano de parcelas")
        .task { await model.loadDetail(id: planID) }
    }

    private func detail(_ plan: InstallmentPlan) -> some View {
        Form {
            Section("Plano") {
                LabeledContent("Total", value: money.string(minorUnits: plan.totalAmount.minor))
                LabeledContent("Parcelas", value: "\(plan.installmentCount)")
                LabeledContent("Status", value: plan.status == .active ? "Ativo" : "Cancelado")
            }
            Section("Cronograma") {
                ForEach(plan.schedule, id: \.number) { installment in
                    LabeledContent("Parcela \(installment.number)", value: "\(installment.dueDate.displayValue) · \(money.string(minorUnits: installment.amount.minor))")
                        .accessibilityIdentifier("installmentPlan.installment.\(installment.number)")
                }
            }
            if plan.status == .active {
                Section {
                    Button("Preparar cancelamento", role: .destructive) { Task { await model.previewCancellation(id: plan.id) } }
                        .frame(minHeight: 44).accessibilityIdentifier("installmentPlan.cancel.preview")
                }
            }
            if let error = model.errors[plan.id] { Text(error).foregroundStyle(.red).accessibilityIdentifier("installmentPlan.error") }
        }
        .accessibilityIdentifier("installmentPlan.detail")
        .alert("Cancelar plano?", isPresented: Binding(get: { if case .reviewing = model.cancellationStates[plan.id] { true } else { false } }, set: { isPresented in
            if !isPresented { model.dismissCancellation(id: plan.id) }
        })) {
            Button("Manter ativo", role: .cancel) { model.dismissCancellation(id: plan.id) }
            Button("Cancelar plano", role: .destructive) { model.confirmCancellation(id: plan.id) }
                .accessibilityIdentifier("installmentPlan.cancel.confirm")
        } message: {
            if case let .reviewing(preview) = model.cancellationStates[plan.id] { Text("O cancelamento em \(preview.expectedCancelledOn.displayValue) remove somente compromissos futuros.") }
        }
    }
}
