import SwiftUI

struct FinancialGoalsView: View {
    @Bindable var model: FinancialGoalsViewModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                explanation
                content
            }
            .padding()
        }
        .background(JARVISDesign.canvas.ignoresSafeArea())
        .navigationTitle("Metas financeiras")
        .navigationBarTitleDisplayMode(.inline)
        .accessibilityIdentifier("financialGoals.view")
        .task { await model.loadIfNeeded() }
    }

    private var explanation: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Declarações financeiras", systemImage: "target")
                .font(.headline)
            Text("Registre o que você quer alcançar e o que deseja proteger. Essas declarações não movimentam dinheiro nem alteram o Disponível Seguro.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .jarvisCard(padding: 16)
        .accessibilityIdentifier("financialGoals.explanation")
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle, .loading:
            ProgressView("Carregando declarações")
                .frame(maxWidth: .infinity)
                .padding(.vertical, 32)
                .accessibilityIdentifier("financialGoals.loading")
        case .saving:
            ProgressView("Salvando declaração")
                .frame(maxWidth: .infinity)
                .padding(.vertical, 32)
                .accessibilityIdentifier("financialGoals.saving")
        case .empty:
            ContentUnavailableView(
                "Nenhuma meta declarada",
                systemImage: "target",
                description: Text("As metas e valores protegidos que você declarar aparecerão aqui.")
            )
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier("financialGoals.empty")
        case let .loaded(response):
            lists(response)
        case .failed:
            VStack(spacing: 12) {
                Label("Não foi possível carregar", systemImage: "wifi.exclamationmark")
                    .font(.headline)
                Text(model.errorMessage ?? "Tente novamente.")
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                Button("Tentar novamente") { Task { await model.retry() } }
                    .buttonStyle(JARVISPrimaryButtonStyle())
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("financialGoals.retry")
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 24)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("financialGoals.error")
        }
    }

    private func lists(_ response: FinancialGoalsResponse) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            if !response.goals.isEmpty {
                section("Metas") {
                    ForEach(response.goals) { goal in
                        declarationRow(
                            id: "financialGoals.goal.\(goal.id)",
                            title: goal.title,
                            detail: "Objetivo · \(money(goal.targetAmount.minor))"
                        )
                    }
                }
            }
            if !response.protectedValues.isEmpty {
                section("Valores protegidos") {
                    ForEach(response.protectedValues) { value in
                        declarationRow(
                            id: "financialGoals.protectedValue.\(value.id)",
                            title: value.label,
                            detail: "Proteção declarada · \(money(value.amount.minor))"
                        )
                    }
                }
            }
            Text("Nenhuma dessas declarações reserva saldo ou cria movimentações.")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("financialGoals.disclaimer")
        }
    }

    private func section<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title.uppercased())
                .font(.caption.weight(.semibold))
                .foregroundStyle(.secondary)
            VStack(spacing: 0) { content() }
                .jarvisCard(padding: 0)
        }
    }

    private func declarationRow(id: String, title: String, detail: String) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title).font(.headline)
            Text(detail).font(.subheadline).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier(id)
    }

    private func money(_ minor: Int64) -> String {
        BRLMoneyFormatter().string(minorUnits: minor)
    }
}
