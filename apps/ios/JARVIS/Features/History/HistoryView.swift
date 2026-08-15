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
        case let .loaded(transactions):
            if transactions.isEmpty {
                Spacer()
                ContentUnavailableView(
                    "Nenhuma movimentação registrada neste mês",
                    systemImage: "tray",
                    description: Text("Quando você registrar uma despesa ou receita, ela aparecerá aqui.")
                )
                .accessibilityIdentifier("history.empty")
                Spacer()
            } else {
                List(transactions) { transaction in
                    transactionRow(transaction)
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

    @ViewBuilder
    private func transactionRow(_ transaction: FinancialTransaction) -> some View {
        switch transaction {
        case let .expense(expense):
            expenseRow(expense)
        case let .income(income):
            incomeRow(income)
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
                Text("Saída · \(expense.paymentMethod.displayName)")
                Spacer()
                Text(displayFormatter.dateTime(expense.occurredAt))
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "Saída, \(expense.description), \(moneyFormatter.string(minorUnits: expense.amount.minor)), "
                + "\(expense.paymentMethod.displayName), \(displayFormatter.dateTime(expense.occurredAt))"
        )
        .accessibilityIdentifier("history.expense.\(expense.id)")
    }

    private func incomeRow(_ income: Income) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(income.description)
                    .font(.headline)
                Spacer()
                Text(moneyFormatter.string(minorUnits: income.amount.minor))
                    .font(.headline)
            }
            HStack {
                Text("Entrada")
                Spacer()
                Text(displayFormatter.dateTime(income.occurredAt))
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "Entrada, \(income.description), \(moneyFormatter.string(minorUnits: income.amount.minor)), "
                + displayFormatter.dateTime(income.occurredAt)
        )
        .accessibilityIdentifier("history.income.\(income.id)")
    }
}
