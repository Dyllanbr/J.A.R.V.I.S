import SwiftUI

struct CreditCardsView: View {
    @Bindable var model: CreditCardsViewModel
    @Bindable var purchaseModel: CardPurchaseViewModel
    @Bindable var plansModel: InstallmentPlansViewModel

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Cartões")
                .navigationDestination(for: String.self) { id in
                    CreditCardDetailView(
                        model: model,
                        purchaseModel: purchaseModel,
                        cardID: id
                    )
                }
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        Button {
                            model.beginCreation()
                        } label: {
                            Label("Cadastrar cartão", systemImage: "plus")
                        }
                        .frame(minWidth: 44, minHeight: 44)
                        .accessibilityIdentifier("card.create")
                    }
                }
        }
        .task { await model.loadIfNeeded() }
        .sheet(
            isPresented: Binding(
                get: { model.isPresentingCreate },
                set: { if !$0 { model.dismissCreation() } }
            )
        ) {
            CreditCardCreateView(model: model)
                .environment(\.locale, Locale(identifier: "pt_BR"))
                .interactiveDismissDisabled(model.isCreationBusy)
        }
        .alert(
            "Arquivar cartão?",
            isPresented: Binding(
                get: { model.archiveConfirmation != nil },
                set: { if !$0 { model.dismissArchiveConfirmation() } }
            ),
            presenting: model.archiveConfirmation
        ) { card in
            Button("Manter ativo", role: .cancel) { model.dismissArchiveConfirmation() }
                .accessibilityIdentifier("card.archive.cancel")
            Button("Arquivar cartão", role: .destructive) {
                Task { await model.confirmArchive(card) }
            }
            .accessibilityIdentifier("card.archive.confirm")
        } message: { card in
            Text(
                "Arquivar \(card.name) remove o cartão das novas utilizações futuras no J.A.R.V.I.S., "
                    + "mas preserva o histórico. Isso não cancela nem bloqueia o cartão no emissor."
            )
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.listState {
        case .idle, .loading:
            ProgressView("Carregando cartões")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("card.loading")
        case let .failed(message):
            ContentUnavailableView {
                Label("Não foi possível carregar", systemImage: "exclamationmark.triangle")
                    .accessibilityIdentifier("card.error")
            } description: {
                Text(message)
            } actions: {
                Button("Tentar novamente") { Task { await model.retryList() } }
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("card.retry")
            }
        case let .loaded(items) where items.isEmpty:
            ContentUnavailableView {
                Label("Nenhum cartão cadastrado", systemImage: "creditcard")
            } description: {
                Text("Você ainda não cadastrou cartões.")
            } actions: {
                Button("Cadastrar cartão") { model.beginCreation() }
                    .buttonStyle(.borderedProminent)
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("card.empty.create")
            }
            .accessibilityIdentifier("card.empty")
        case let .loaded(items):
            List {
                Section {
                    NavigationLink {
                        InstallmentPlansView(model: plansModel)
                    } label: {
                        Label("Planos de parcelas", systemImage: "calendar")
                    }
                    .accessibilityIdentifier("installmentPlans.open")
                }
                ForEach(items) { card in
                    NavigationLink(value: card.id) {
                        CreditCardRow(card: card)
                    }
                    .accessibilityIdentifier("card.item.\(card.id)")
                }
            }
            .listStyle(.insetGrouped)
            .refreshable { await model.refresh() }
            .accessibilityIdentifier("card.list")
        }
    }
}

private struct CreditCardRow: View {
    let card: CreditCard
    private let money = BRLMoneyFormatter()

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(card.name)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                Text(card.status.displayName)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(card.status == .active ? Color.cyan : Color.secondary)
            }
            if let lastFour = card.lastFour {
                Text("•••• \(lastFour)")
                    .font(.body.monospacedDigit())
            }
            HStack {
                Text("Fecha dia \(card.closingDay) · vence dia \(card.dueDay)")
                if let limit = card.creditLimit {
                    Spacer(minLength: 8)
                    Text(money.string(minorUnits: limit.minor))
                }
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
    }

    private var accessibilityLabel: String {
        var parts = ["Cartão \(card.name)"]
        if let lastFour = card.lastFour { parts.append("final \(lastFour)") }
        parts.append(card.status.displayName.lowercased())
        return parts.joined(separator: ", ")
    }
}

private struct CreditCardDetailView: View {
    @Bindable var model: CreditCardsViewModel
    @Bindable var purchaseModel: CardPurchaseViewModel
    let cardID: String
    private let money = BRLMoneyFormatter()
    private let dateTime = FinancialDisplayFormatter()

    var body: some View {
        Group {
            switch model.detailStates[cardID] ?? .idle {
            case .idle, .loading:
                ProgressView("Carregando cartão")
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .accessibilityIdentifier("card.detail.loading")
            case let .failed(message):
                ContentUnavailableView {
                    Label("Cartão indisponível", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(message)
                } actions: {
                    Button("Atualizar") { Task { await model.retryDetail(id: cardID) } }
                        .accessibilityIdentifier("card.detail.retry")
                }
                .accessibilityIdentifier("card.detail.error")
            case let .loaded(card):
                detail(card)
            }
        }
        .navigationTitle("Detalhes do cartão")
        .task { await model.loadDetail(id: cardID) }
        .refreshable { await model.refreshDetail(id: cardID) }
    }

    private func detail(_ card: CreditCard) -> some View {
        Form {
            Section("Cartão") {
                row("Nome", card.name)
                if let lastFour = card.lastFour { row("Últimos 4 dígitos", lastFour) }
                if let brand = card.brand { row("Bandeira", brand.displayName) }
                row("Status", card.status.displayName)
            }
            Section("Datas declaradas") {
                row("Fechamento", "Dia \(card.closingDay)")
                row("Vencimento", "Dia \(card.dueDay)")
                if let limit = card.creditLimit {
                    row("Limite de crédito", money.string(minorUnits: limit.minor))
                }
            }
            Section("Registro") {
                row("Criado em", dateTime.dateTime(card.createdAt))
                if let archivedAt = card.archivedAt { row("Arquivado em", dateTime.dateTime(archivedAt)) }
            }
            if card.status == .active {
                Section {
                    NavigationLink {
                        CardPurchaseView(model: purchaseModel)
                            .environment(\.locale, Locale(identifier: "pt_BR"))
                            .onAppear { purchaseModel.begin(cardID: card.id) }
                    } label: {
                        Text("Registrar compra neste cartão")
                    }
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("card.purchase.\(card.id)")
                    Button("Arquivar cartão", role: .destructive) { model.requestArchive(card) }
                        .frame(minHeight: 44)
                        .disabled(model.archivingIDs.contains(card.id))
                        .accessibilityIdentifier("card.archive.\(card.id)")
                    if model.archivingIDs.contains(card.id) { ProgressView("Arquivando cartão") }
                    if let error = model.archiveErrors[card.id] {
                        Text(error)
                            .foregroundStyle(.red)
                            .fixedSize(horizontal: false, vertical: true)
                        if model.canRetryArchive(id: card.id) {
                            Button("Tentar novamente") { Task { await model.retryArchive(id: card.id) } }
                                .accessibilityIdentifier("card.archive.retry.\(card.id)")
                        }
                    }
                } footer: {
                    Text("Arquivar preserva o histórico e não executa nenhuma operação no emissor do cartão.")
                }
            }
        }
        .accessibilityIdentifier("card.detail")
    }

    private func row(_ title: String, _ value: String) -> some View {
        LabeledContent(title) {
            Text(value).multilineTextAlignment(.trailing).fixedSize(horizontal: false, vertical: true)
        }
    }
}

private struct CreditCardCreateView: View {
    @Bindable var model: CreditCardsViewModel
    @FocusState private var focused: Field?
    private let money = BRLMoneyFormatter()

    private enum Field { case name, lastFour, creditLimit }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(title)
                .toolbar {
                    if !model.isCreationBusy {
                        ToolbarItem(placement: .cancellationAction) {
                            Button("Fechar") { model.dismissCreation() }
                                .accessibilityIdentifier("card.close")
                        }
                    }
                }
        }
    }

    private var title: String {
        switch model.creationState {
        case .editing, .previewing: "Cadastrar cartão"
        case .reviewing, .submitting, .retryable, .requiresEditing: "Revisar cartão"
        case .success: "Cartão cadastrado"
        }
    }

    @ViewBuilder private var content: some View {
        switch model.creationState {
        case .editing, .previewing: form
        case let .reviewing(value), let .submitting(value), let .retryable(value), let .requiresEditing(value):
            review(value)
        case let .success(card): success(card)
        }
    }

    private var form: some View {
        Form {
            Section {
                Text("Cadastre somente uma referência organizacional. Não informe número completo, CVV, PIN ou senha.")
                    .font(.subheadline).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            Section("Identificação") {
                TextField("Nome", text: $model.name)
                    .focused($focused, equals: .name)
                    .accessibilityIdentifier("card.name")
                TextField("Últimos 4 dígitos (opcional)", text: $model.lastFour)
                    .keyboardType(.numberPad)
                    .focused($focused, equals: .lastFour)
                    .accessibilityIdentifier("card.lastFour")
                Picker("Bandeira", selection: $model.brand) {
                    Text("Não informar").tag(CreditCardBrand?.none)
                    ForEach(CreditCardBrand.allCases) { brand in
                        Text(brand.displayName).tag(Optional(brand))
                    }
                }
                .accessibilityIdentifier("card.brand")
            }
            Section("Datas declaradas") {
                Picker("Dia de fechamento", selection: $model.closingDay) {
                    ForEach(1...31, id: \.self) { Text("\($0)").tag($0) }
                }
                .accessibilityIdentifier("card.closingDay")
                Picker("Dia de vencimento", selection: $model.dueDay) {
                    ForEach(1...31, id: \.self) { Text("\($0)").tag($0) }
                }
                .accessibilityIdentifier("card.dueDay")
                TextField("Limite de crédito (opcional)", text: $model.creditLimitText)
                    .keyboardType(.decimalPad)
                    .focused($focused, equals: .creditLimit)
                    .accessibilityIdentifier("card.creditLimit")
            }
            if let error = model.creationErrorMessage {
                Section { Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red) }
            }
            Section {
                Button {
                    focused = nil
                    Task { await model.review() }
                } label: {
                    HStack {
                        Spacer()
                        if model.isCreationBusy { ProgressView() } else { Text("Revisar cartão") }
                        Spacer()
                    }.frame(minHeight: 44)
                }
                .disabled(model.isCreationBusy)
                .accessibilityIdentifier("card.review")
            }
        }
        .scrollDismissesKeyboard(.immediately)
        .accessibilityIdentifier("card.create.screen")
    }

    private func review(_ reviewed: ReviewedCreditCard) -> some View {
        Form {
            Section("Confira os dados recebidos do servidor") {
                row("Nome", reviewed.preview.name)
                if let lastFour = reviewed.preview.lastFour { row("Últimos 4 dígitos", lastFour) }
                if let brand = reviewed.preview.brand { row("Bandeira", brand.displayName) }
                row("Fechamento", "Dia \(reviewed.preview.closingDay)")
                row("Vencimento", "Dia \(reviewed.preview.dueDay)")
                if let limit = reviewed.preview.creditLimit {
                    row("Limite de crédito", money.string(minorUnits: limit.minor))
                }
                Text("Nada será salvo antes de Confirmar. O cartão é apenas uma referência organizacional.")
                    .font(.footnote).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            if let error = model.creationErrorMessage {
                Section { Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red) }
            }
            Section {
                Button("Voltar") { model.editCreation() }
                    .disabled(model.isCreationBusy)
                    .accessibilityIdentifier("card.review.cancel")
                if !model.creationState.requiresEditing {
                    Button {
                        Task { await model.confirmCreation() }
                    } label: {
                        HStack {
                            Spacer()
                            if model.isCreationBusy { ProgressView() }
                            else if model.creationState.isRetryable { Text("Tentar novamente") }
                            else { Text("Confirmar cartão") }
                            Spacer()
                        }.frame(minHeight: 44)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(model.isCreationBusy)
                    .accessibilityIdentifier("card.confirm")
                }
            }
        }
        .accessibilityIdentifier("card.review.screen")
    }

    private func success(_ card: CreditCard) -> some View {
        ContentUnavailableView {
            Label("Cartão cadastrado", systemImage: "checkmark.circle.fill")
                .accessibilityIdentifier("card.success")
        } description: {
            Text("\(card.name) agora aparece em Cartões.")
        } actions: {
            Button("Voltar para cartões") { model.finishCreation() }
                .buttonStyle(.borderedProminent)
                .accessibilityIdentifier("card.new")
        }
    }

    private func row(_ title: String, _ value: String) -> some View {
        LabeledContent(title) { Text(value).multilineTextAlignment(.trailing).fixedSize(horizontal: false, vertical: true) }
    }
}

private extension CreditCardCreationState {
    var isRetryable: Bool { if case .retryable = self { true } else { false } }
    var requiresEditing: Bool { if case .requiresEditing = self { true } else { false } }
}
