import SwiftUI

struct HistoryView: View {
    @Bindable var model: HistoryViewModel

    private let moneyFormatter = BRLMoneyFormatter()
    private let displayFormatter = FinancialDisplayFormatter()

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                monthNavigation
                filters
                categoryCatalogStatus
                content
            }
            .navigationTitle("Histórico")
        }
        .task(id: model.refreshRevision) {
            await model.load()
        }
        .task {
            await model.loadCategoriesIfNeeded()
        }
    }

    private var filters: some View {
        VStack(alignment: .leading, spacing: 8) {
            Picker(
                "Tipo",
                selection: Binding(
                    get: { model.typeFilter },
                    set: { model.selectTypeFilter($0) }
                )
            ) {
                ForEach(HistoryTypeFilter.allCases) { filter in
                    Text(filter.displayName)
                        .tag(filter)
                        .accessibilityIdentifier("history.filter.type.\(filter.rawValue)")
                }
            }
            .pickerStyle(.menu)
            .accessibilityValue(model.typeFilter.displayName)
            .accessibilityIdentifier("history.filter.type")

            Picker(
                "Categoria",
                selection: Binding(
                    get: { model.categoryFilter },
                    set: { model.selectCategoryFilter($0) }
                )
            ) {
                Text("Todas as categorias")
                    .tag(HistoryCategoryFilter.all)
                    .accessibilityIdentifier("history.filter.category.option.all")
                Text("Sem categoria")
                    .tag(HistoryCategoryFilter.uncategorized)
                    .accessibilityIdentifier("history.filter.category.option.none")
                ForEach(model.availableCategoryDefinitions) { category in
                    Text(category.displayName)
                        .tag(HistoryCategoryFilter.category(category.id))
                        .accessibilityIdentifier("history.filter.category.option.\(category.id)")
                }
            }
            .pickerStyle(.menu)
            .accessibilityValue(model.categoryFilterDisplayName)
            .accessibilityIdentifier("history.filter.category")
        }
        .padding(.horizontal)
        .padding(.bottom, 8)
    }

    @ViewBuilder
    private var categoryCatalogStatus: some View {
        switch model.categoryCatalogState {
        case .idle, .loading:
            HStack {
                ProgressView()
                Text("Carregando categorias")
            }
            .padding(.horizontal)
            .accessibilityIdentifier("history.category.loading")
        case let .failed(message):
            HStack(alignment: .firstTextBaseline) {
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                Spacer()
                Button("Tentar novamente") {
                    Task { await model.retryCategories() }
                }
                .frame(minHeight: 44)
                .accessibilityIdentifier("history.category.retry")
            }
            .padding(.horizontal)
            .accessibilityIdentifier("history.category.error")
        case .loaded:
            EmptyView()
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
            } else if model.filteredTransactions.isEmpty {
                Spacer()
                ContentUnavailableView(
                    "Nenhuma movimentação corresponde aos filtros",
                    systemImage: "line.3.horizontal.decrease.circle",
                    description: Text("Altere os filtros para ver outras movimentações deste mês.")
                )
                .accessibilityIdentifier("history.filteredEmpty")
                Spacer()
            } else {
                List(model.filteredTransactions) { transaction in
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
            Text(model.categoryDisplayName(for: .expense(expense)))
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "Saída, \(expense.description), \(moneyFormatter.string(minorUnits: expense.amount.minor)), "
                + "\(expense.paymentMethod.displayName), "
                + "\(model.categoryDisplayName(for: .expense(expense))), "
                + "\(displayFormatter.dateTime(expense.occurredAt))"
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
            Text(model.categoryDisplayName(for: .income(income)))
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel(
            "Entrada, \(income.description), \(moneyFormatter.string(minorUnits: income.amount.minor)), "
                + "\(model.categoryDisplayName(for: .income(income))), "
                + displayFormatter.dateTime(income.occurredAt)
        )
        .accessibilityIdentifier("history.income.\(income.id)")
    }
}
