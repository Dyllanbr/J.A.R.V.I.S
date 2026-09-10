import SwiftUI

struct HistoryView: View {
    @Bindable var model: HistoryViewModel
    @Bindable var safeAvailable: SafeAvailableViewModel
    let scheduledCommitments: ScheduledCommitmentsViewModel
    let financialGoals: FinancialGoalsViewModel

    private let moneyFormatter = BRLMoneyFormatter()
    private let displayFormatter = FinancialDisplayFormatter()

    var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                dashboardHeader
                monthNavigation
                scheduledCommitmentsEntry
                financialGoalsEntry
                filters
                categoryCatalogStatus
                content
            }
        }
        .background(JARVISDesign.canvas)
        .refreshable { await model.load() }
        .task(id: model.refreshRevision) {
            syncSafeAvailablePeriod()
            await model.load()
            await safeAvailable.load(forceRefresh: true)
        }
        .task {
            await model.loadCategoriesIfNeeded()
        }
    }

    private func syncSafeAvailablePeriod() {
        let calendar = Calendar.financial
        guard let start = calendar.date(from: DateComponents(year: model.month.year, month: model.month.month, day: 1)),
              let nextMonth = calendar.date(byAdding: .month, value: 1, to: start),
              let end = calendar.date(byAdding: .day, value: -1, to: nextMonth)
        else { return }
        safeAvailable.setPeriodStart(start)
        safeAvailable.setPeriodEnd(end)
    }

    private var dashboardHeader: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(alignment: .top, spacing: 12) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("JARVIS")
                        .font(.caption.weight(.bold))
                        .tracking(2)
                        .foregroundStyle(DashboardPalette.accent)
                    Text("Sua vida financeira, em foco.")
                        .font(.title2.weight(.bold))
                        .foregroundStyle(.white)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 8)
                Image(systemName: "sparkles")
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(DashboardPalette.accent)
                    .frame(width: 40, height: 40)
                    .background(DashboardPalette.accent.opacity(0.14), in: Circle())
                    .accessibilityHidden(true)
            }

            HStack(alignment: .bottom) {
                VStack(alignment: .leading, spacing: 5) {
                    Text("Disponível seguro")
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(DashboardPalette.secondaryText)
                    if let response = safeAvailable.response {
                        Text(DashboardMoneyFormatter.string(minor: response.finalAmount.minor))
                            .font(.system(size: 32, weight: .bold, design: .rounded).monospacedDigit())
                            .foregroundStyle(response.finalAmount.minor < 0 ? DashboardPalette.warning : .white)
                            .lineLimit(1)
                            .minimumScaleFactor(0.72)
                            .accessibilityIdentifier("dashboard.safeAvailable.value")
                    } else {
                        Text("Calcule seu próximo passo")
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(.white)
                            .accessibilityIdentifier("dashboard.safeAvailable.placeholder")
                    }
                }
                Spacer(minLength: 12)
                NavigationLink {
                    SafeAvailableView(model: safeAvailable)
                        .environment(\.locale, Locale(identifier: "pt_BR"))
                } label: {
                    Label("Ver análise", systemImage: "arrow.up.right")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(DashboardPalette.accent)
                        .padding(.horizontal, 12)
                        .frame(minHeight: 40)
                        .background(DashboardPalette.accent.opacity(0.14), in: Capsule())
                }
                .accessibilityIdentifier("dashboard.safeAvailable")
            }

            HStack(spacing: 10) {
                dashboardMetric(
                    title: "Entradas",
                    value: totalIncome,
                    tint: DashboardPalette.positive,
                    identifier: "dashboard.income"
                )
                dashboardMetric(
                    title: "Saídas",
                    value: totalExpense,
                    tint: DashboardPalette.warning,
                    identifier: "dashboard.expense"
                )
            }

            if !categorySpend.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Onde seu dinheiro foi")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.white)
                    ForEach(categorySpend.prefix(3), id: \.id) { item in
                        HStack(spacing: 10) {
                            Text(item.name)
                                .font(.caption)
                                .foregroundStyle(DashboardPalette.secondaryText)
                                .lineLimit(1)
                                .frame(width: 92, alignment: .leading)
                            GeometryReader { proxy in
                                Capsule()
                                    .fill(DashboardPalette.accent.opacity(0.8))
                                    .frame(width: max(8, proxy.size.width * item.share), height: 6)
                                    .frame(maxHeight: .infinity, alignment: .center)
                            }
                            .frame(height: 12)
                            Text(DashboardMoneyFormatter.string(minor: item.amount))
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(.white)
                        }
                    }
                }
                .accessibilityIdentifier("dashboard.categories")
            }

            Text(dashboardInsight)
                .font(.footnote)
                .foregroundStyle(DashboardPalette.secondaryText)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("dashboard.insight")
        }
        .padding(20)
        .background(DashboardPalette.background, in: RoundedRectangle(cornerRadius: 24, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .stroke(DashboardPalette.accent.opacity(0.28), lineWidth: 1)
        }
        .padding(.horizontal)
        .padding(.top, 10)
        .padding(.bottom, 8)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("dashboard.hero")
    }

    private func dashboardMetric(
        title: String,
        value: Int64,
        tint: Color,
        identifier: String
    ) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title)
                .font(.caption)
                .foregroundStyle(DashboardPalette.secondaryText)
            Text(DashboardMoneyFormatter.string(minor: value))
                .font(.headline.monospacedDigit())
                .foregroundStyle(tint)
                .lineLimit(1)
                .minimumScaleFactor(0.75)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(Color.white.opacity(0.07), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier(identifier)
    }

    private var totalIncome: Int64 {
        sum(model.transactions.compactMap { transaction in
            if case let .income(income) = transaction { return income.amount.minor }
            return nil
        })
    }

    private var totalExpense: Int64 {
        sum(model.transactions.compactMap { transaction in
            if case let .expense(expense) = transaction { return expense.amount.minor }
            return nil
        })
    }

    private var categorySpend: [DashboardCategorySpend] {
        var totals: [String: (name: String, amount: Int64)] = [:]
        for transaction in model.transactions {
            guard case let .expense(expense) = transaction else { continue }
            let id = expense.categoryID ?? "uncategorized"
            let name = model.categoryDisplayName(for: transaction)
            let current = totals[id]?.amount ?? 0
            let (amount, overflow) = current.addingReportingOverflow(expense.amount.minor)
            totals[id] = (name, overflow ? Int64.max : amount)
        }
        let total = max(totalExpense, 1)
        return totals
            .map { key, value in
                DashboardCategorySpend(
                    id: key,
                    name: value.name,
                    amount: value.amount,
                    share: min(1, Double(value.amount) / Double(total))
                )
            }
            .sorted { lhs, rhs in
                if lhs.amount != rhs.amount { return lhs.amount > rhs.amount }
                return lhs.name < rhs.name
            }
    }

    private var dashboardInsight: String {
        guard !model.transactions.isEmpty else {
            return "Seu resumo aparece aqui assim que houver movimentações no período."
        }
        if totalExpense > totalIncome {
            return "Resumo automático · as saídas estão acima das entradas neste mês."
        }
        return "Resumo automático · suas entradas cobrem as saídas registradas neste mês."
    }

    private func sum(_ values: [Int64]) -> Int64 {
        values.reduce(into: Int64(0)) { result, value in
            let (next, overflow) = result.addingReportingOverflow(value)
            result = overflow ? Int64.max : next
        }
    }

    private var scheduledCommitmentsEntry: some View {
        NavigationLink {
            ScheduledCommitmentsView(model: scheduledCommitments)
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "calendar.badge.clock")
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(JARVISDesign.accent)
                    .frame(width: 32, height: 32)
                    .background(JARVISDesign.accent.opacity(0.12), in: Circle())
                Text("Compromissos futuros")
                    .font(.body.weight(.semibold))
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.tertiary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .buttonStyle(.plain)
        .jarvisCard(padding: 14)
        .padding(.horizontal)
        .accessibilityIdentifier("history.scheduledCommitments.entry")
    }

    private var financialGoalsEntry: some View {
        NavigationLink {
            FinancialGoalsView(model: financialGoals)
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "target")
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(JARVISDesign.accent)
                    .frame(width: 32, height: 32)
                    .background(JARVISDesign.accent.opacity(0.12), in: Circle())
                VStack(alignment: .leading, spacing: 2) {
                    Text("Metas financeiras")
                        .font(.body.weight(.semibold))
                    Text("Declarações sem movimentação")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.tertiary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .buttonStyle(.plain)
        .jarvisCard(padding: 14)
        .padding(.horizontal)
        .accessibilityIdentifier("history.financialGoals.entry")
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
        .jarvisCard(padding: 14)
        .padding(.horizontal)
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
        .jarvisCard(padding: 8)
        .padding(.horizontal)
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .idle, .loading:
            VStack {
                ProgressView("Carregando histórico")
                    .accessibilityIdentifier("history.loading")
            }
            .frame(maxWidth: .infinity, minHeight: 260)
        case let .loaded(transactions):
            if transactions.isEmpty {
                VStack {
                    ContentUnavailableView(
                        "Nenhuma movimentação registrada neste mês",
                        systemImage: "tray",
                        description: Text("Quando você registrar uma despesa ou receita, ela aparecerá aqui.")
                    )
                    .accessibilityIdentifier("history.empty")
                }
                .frame(maxWidth: .infinity, minHeight: 360)
            } else if model.filteredTransactions.isEmpty {
                VStack {
                    ContentUnavailableView(
                        "Nenhuma movimentação corresponde aos filtros",
                        systemImage: "line.3.horizontal.decrease.circle",
                        description: Text("Altere os filtros para ver outras movimentações deste mês.")
                    )
                    .accessibilityIdentifier("history.filteredEmpty")
                }
                .frame(maxWidth: .infinity, minHeight: 360)
            } else {
                VStack(spacing: 0) {
                    ForEach(model.filteredTransactions) { transaction in
                        transactionRow(transaction)
                            .padding(.horizontal)
                    }
                }
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("history.list")
                .padding(.top, 8)
            }
        case let .failed(message):
            VStack {
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
            }
            .frame(maxWidth: .infinity, minHeight: 360)
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
                    .foregroundStyle(JARVISDesign.negative)
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
        .listRowSeparator(.hidden)
        .listRowBackground(JARVISDesign.surface)
    }

    private func incomeRow(_ income: Income) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(income.description)
                    .font(.headline)
                Spacer()
                Text(moneyFormatter.string(minorUnits: income.amount.minor))
                    .font(.headline)
                    .foregroundStyle(JARVISDesign.positive)
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
        .listRowSeparator(.hidden)
        .listRowBackground(JARVISDesign.surface)
    }
}

private struct DashboardCategorySpend {
    let id: String
    let name: String
    let amount: Int64
    let share: Double
}

private enum DashboardPalette {
    static let background = Color(red: 10 / 255, green: 13 / 255, blue: 12 / 255)
    static let accent = Color(red: 53 / 255, green: 210 / 255, blue: 138 / 255)
    static let positive = Color(red: 112 / 255, green: 230 / 255, blue: 167 / 255)
    static let warning = Color(red: 255 / 255, green: 176 / 255, blue: 92 / 255)
    static let secondaryText = Color.white.opacity(0.68)
}

private enum DashboardMoneyFormatter {
    static func string(minor: Int64) -> String {
        let negative = minor < 0
        let magnitude = minor == Int64.min ? UInt64(Int64.max) + 1 : UInt64(abs(minor))
        let whole = magnitude / 100
        let cents = magnitude % 100
        return "R$ \(negative ? "-" : "")\(whole),\(String(format: "%02llu", cents))"
    }
}
