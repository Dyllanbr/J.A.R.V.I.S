import SwiftUI

struct HistoryView: View {
    @Bindable var model: HistoryViewModel

    private let moneyFormatter = BRLMoneyFormatter()
    private let displayFormatter = FinancialDisplayFormatter()

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                monthNavigation
                content
            }
            .navigationTitle("Histórico")
        }
        .task(id: model.refreshRevision) {
            await model.load()
        }
    }

    private var monthNavigation: some View {
        HStack {
            Button {
                model.showPreviousMonth()
            } label: {
                Label("Mês anterior", systemImage: "chevron.left")
                    .labelStyle(.iconOnly)
                    .frame(width: 44, height: 44)
            }
            .accessibilityIdentifier("history.previousMonth")

            Spacer()
            Text(model.month.displayName)
                .font(.headline)
                .accessibilityIdentifier("history.month")
            Spacer()

            Button {
                model.showNextMonth()
            } label: {
                Label("Próximo mês", systemImage: "chevron.right")
                    .labelStyle(.iconOnly)
                    .frame(width: 44, height: 44)
            }
            .accessibilityIdentifier("history.nextMonth")
        }
        .padding(.horizontal)
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle, .loading:
            Spacer()
            ProgressView("Carregando histórico")
                .accessibilityIdentifier("history.loading")
            Spacer()
        case let .loaded(expenses):
            if expenses.isEmpty {
                Spacer()
                ContentUnavailableView(
                    "Nenhuma despesa registrada neste mês",
                    systemImage: "tray",
                    description: Text("Quando você registrar uma despesa, ela aparecerá aqui.")
                )
                .accessibilityIdentifier("history.empty")
                Spacer()
            } else {
                List(expenses) { expense in
                    expenseRow(expense)
                }
                .listStyle(.plain)
                .refreshable { await model.load() }
                .accessibilityIdentifier("history.list")
            }
        case let .failed(message):
            Spacer()
            ContentUnavailableView {
                Label("Não foi possível carregar", systemImage: "wifi.exclamationmark")
            } description: {
                Text(message)
            } actions: {
                Button("Tentar novamente") { model.retry() }
                    .buttonStyle(.borderedProminent)
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("history.retry")
            }
            Spacer()
        }
    }

    private func expenseRow(_ expense: Expense) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(expense.description)
                    .font(.headline)
                Spacer()
                Text(moneyFormatter.string(minorUnits: expense.amount.minor))
                    .font(.headline)
            }
            HStack {
                Text(expense.paymentMethod.displayName)
                Spacer()
                Text(displayFormatter.dateTime(expense.occurredAt))
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "\(expense.description), \(moneyFormatter.string(minorUnits: expense.amount.minor)), "
                + "\(expense.paymentMethod.displayName), \(displayFormatter.dateTime(expense.occurredAt))"
        )
        .accessibilityIdentifier("history.expense.\(expense.id)")
    }
}
