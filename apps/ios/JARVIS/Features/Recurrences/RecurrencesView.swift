import SwiftUI

struct RecurrencesView: View {
    @Bindable var model: RecurrencesViewModel
    @Bindable var suggestionsModel: RecurrenceSuggestionsViewModel

    private let moneyFormatter = BRLMoneyFormatter()

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Recorrências")
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        Button {
                            model.beginCreation()
                        } label: {
                            Label("Nova recorrência", systemImage: "plus")
                        }
                        .frame(minWidth: 44, minHeight: 44)
                        .accessibilityHint("Inicia o cadastro de um compromisso mensal esperado")
                        .accessibilityIdentifier("recurrence.create")
                    }
                }
        }
        .task {
            async let recurrences: Void = model.loadIfNeeded()
            async let suggestions: Void = suggestionsModel.loadIfNeeded()
            _ = await (recurrences, suggestions)
        }
        .sheet(
            isPresented: Binding(
                get: { model.isPresentingCreate },
                set: { if !$0 { model.dismissCreation() } }
            )
        ) {
            RecurrenceCreateView(model: model)
                .environment(\.locale, Locale(identifier: "pt_BR"))
                .interactiveDismissDisabled(model.isCreationBusy)
        }
        .alert(
            "Cancelar recorrência?",
            isPresented: Binding(
                get: { model.cancellationConfirmation != nil },
                set: { if !$0 { model.dismissCancellationConfirmation() } }
            ),
            presenting: model.cancellationConfirmation
        ) { recurrence in
            Button("Manter ativa", role: .cancel) {
                model.dismissCancellationConfirmation()
            }
            Button("Cancelar recorrência", role: .destructive) {
                Task { await model.confirmCancellation(recurrence) }
            }
            .accessibilityIdentifier("recurrence.cancel.confirm")
        } message: { recurrence in
            Text(
                "\(recurrence.description) deixará de ser considerada ativa. "
                    + "Isso não apaga despesas existentes e não executa nenhuma transação."
            )
        }
        .alert(
            "Descartar sugestão?",
            isPresented: Binding(
                get: { suggestionsModel.dismissalConfirmation != nil },
                set: { if !$0 { suggestionsModel.cancelDismissal() } }
            ),
            presenting: suggestionsModel.dismissalConfirmation
        ) { suggestion in
            Button("Manter sugestão", role: .cancel) {
                suggestionsModel.cancelDismissal()
            }
            .accessibilityIdentifier("recurrence.suggestion.dismiss.cancel")
            Button("Agora não", role: .destructive) {
                Task { await suggestionsModel.confirmDismissal(suggestion) }
            }
            .accessibilityIdentifier("recurrence.suggestion.dismiss.confirm")
        } message: { suggestion in
            Text(
                "A sugestão de possível recorrência \(suggestion.description) será ocultada para estas evidências. "
                    + "Nenhuma despesa ou recorrência será alterada. Uma nova sugestão poderá aparecer com novas evidências."
            )
        }
        .alert(
            "Sugestões atualizadas",
            isPresented: Binding(
                get: { suggestionsModel.noticeMessage != nil },
                set: { if !$0 { suggestionsModel.clearNotice() } }
            )
        ) {
            Button("OK") { suggestionsModel.clearNotice() }
        } message: {
            Text(suggestionsModel.noticeMessage ?? "")
        }
    }

    @ViewBuilder
    private var content: some View {
        if isLoadingEverything {
            ProgressView("Carregando recorrências")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("recurrence.loading")
        } else if isLoadedAndEmpty {
            ContentUnavailableView {
                Label("Nenhuma recorrência cadastrada", systemImage: "arrow.triangle.2.circlepath")
            } description: {
                Text("Sugestões determinísticas aparecerão aqui; você também pode cadastrar um compromisso manualmente.")
            }
            .accessibilityIdentifier("recurrence.empty")
        } else {
            List {
                suggestionSection
                recurrenceSection
            }
            .listStyle(.insetGrouped)
            .refreshable {
                async let recurrences: Void = model.refresh()
                async let suggestions: Void = suggestionsModel.refresh()
                _ = await (recurrences, suggestions)
            }
            .accessibilityIdentifier("recurrence.list")
        }
    }

    private var isLoadingEverything: Bool {
        let recurrencesLoading = model.listState == .idle || model.listState == .loading
        let suggestionsLoading = suggestionsModel.listState == .idle || suggestionsModel.listState == .loading
        return recurrencesLoading && suggestionsLoading
    }

    private var isLoadedAndEmpty: Bool {
        guard case let .loaded(recurrences) = model.listState,
              case let .loaded(suggestions) = suggestionsModel.listState
        else { return false }
        return recurrences.isEmpty && suggestions.isEmpty
    }

    @ViewBuilder
    private var suggestionSection: some View {
        switch suggestionsModel.listState {
        case .idle, .loading:
            Section("Sugestões") {
                ProgressView("Buscando possíveis recorrências")
                    .accessibilityIdentifier("recurrence.suggestions.loading")
            }
        case let .loaded(items):
            if !items.isEmpty {
                Section {
                    ForEach(items) { suggestion in
                        suggestionRow(suggestion)
                    }
                } header: {
                    Text("Sugestões")
                        .accessibilityIdentifier("recurrence.suggestions.section")
                } footer: {
                    Text("Baseadas em despesas semelhantes. Só viram recorrências após sua revisão e confirmação.")
                }
            }
        case let .failed(message):
            Section("Sugestões") {
                Label(message, systemImage: "wifi.exclamationmark")
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("recurrence.suggestions.error")
                Button("Tentar carregar sugestões novamente") {
                    Task { await suggestionsModel.retryList() }
                }
                .frame(minHeight: 44)
                .accessibilityIdentifier("recurrence.suggestions.retry")
            }
        }
    }

    @ViewBuilder
    private var recurrenceSection: some View {
        switch model.listState {
        case .idle, .loading:
            Section("Recorrências confirmadas") {
                ProgressView("Carregando recorrências confirmadas")
            }
        case let .loaded(items):
            if !items.isEmpty {
                Section("Recorrências confirmadas") {
                    ForEach(items) { recurrence in
                        recurrenceRow(recurrence)
                    }
                }
            }
        case let .failed(message):
            Section("Recorrências confirmadas") {
                Label(message, systemImage: "wifi.exclamationmark")
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                Button("Tentar carregar recorrências novamente") {
                    Task { await model.retryList() }
                }
                .frame(minHeight: 44)
                .accessibilityIdentifier("recurrence.retry")
            }
        }
    }

    private func suggestionRow(_ suggestion: RecurrenceSuggestion) -> some View {
        let isBusy = suggestionsModel.previewingIDs.contains(suggestion.id)
            || suggestionsModel.dismissingIDs.contains(suggestion.id)
        return VStack(alignment: .leading, spacing: 10) {
            Text("Sugestão de possível recorrência")
                .font(.caption.weight(.semibold))
                .foregroundStyle(.cyan)
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Text(suggestion.description)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                Text(moneyFormatter.string(minorUnits: suggestion.expectedAmount.minor))
                    .font(.headline)
            }
            Text("\(suggestion.evidenceCount) cobranças mensais · normalmente perto do dia \(suggestion.anchorDay)")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Text("Observadas em \(suggestion.observedDates.map(\.displayValue).joined(separator: ", "))")
                .font(.footnote)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            Text("Próxima data sugerida: \(suggestion.proposedStartsOn.displayValue)")
                .font(.subheadline.weight(.semibold))
                .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 12) {
                Button("Revisar") {
                    Task {
                        if let preview = await suggestionsModel.prepareForReview(suggestion) {
                            model.beginSuggestionReview(preview: preview, suggestionID: suggestion.id)
                        }
                    }
                }
                .buttonStyle(.borderedProminent)
                .frame(minHeight: 44)
                .disabled(isBusy)
                .accessibilityIdentifier("recurrence.suggestion.review.\(suggestion.id)")

                Button("Agora não") {
                    suggestionsModel.requestDismissal(suggestion)
                }
                .buttonStyle(.bordered)
                .frame(minHeight: 44)
                .disabled(isBusy)
                .accessibilityIdentifier("recurrence.suggestion.dismiss.\(suggestion.id)")
            }

            if isBusy {
                ProgressView("Atualizando sugestão")
                    .accessibilityIdentifier("recurrence.suggestion.action.loading.\(suggestion.id)")
            }
            if let error = suggestionsModel.actionErrors[suggestion.id] {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.red)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("recurrence.suggestion.action.error.\(suggestion.id)")
            }
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(
            "Sugestão de possível recorrência, \(suggestion.description), "
                + "\(moneyFormatter.string(minorUnits: suggestion.expectedAmount.minor)), "
                + "\(suggestion.evidenceCount) ocorrências, perto do dia \(suggestion.anchorDay), "
                + "próxima data sugerida \(suggestion.proposedStartsOn.displayValue)"
        )
        .accessibilityIdentifier("recurrence.suggestion.\(suggestion.id)")
    }

    private func recurrenceRow(_ recurrence: Recurrence) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                Text(recurrence.description)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                Text(moneyFormatter.string(minorUnits: recurrence.expectedAmount.minor))
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Label(
                recurrence.status.displayName,
                systemImage: recurrence.status == .active ? "checkmark.circle.fill" : "xmark.circle"
            )
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(recurrence.status == .active ? Color.cyan : Color.secondary)

            Text("Mensal · início em \(recurrence.startsOn.displayValue)")
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)

            if recurrence.status == .active {
                Button("Cancelar recorrência", role: .destructive) {
                    model.requestCancellation(recurrence)
                }
                .frame(minHeight: 44)
                .disabled(model.cancellingIDs.contains(recurrence.id))
                .accessibilityHint("Solicita confirmação antes de cancelar o compromisso")
                .accessibilityIdentifier("recurrence.cancel.\(recurrence.id)")
            }

            if model.cancellingIDs.contains(recurrence.id) {
                ProgressView("Cancelando recorrência")
                    .accessibilityIdentifier("recurrence.cancel.loading.\(recurrence.id)")
            }

            if let error = model.cancellationErrors[recurrence.id] {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote)
                    .foregroundStyle(.red)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("recurrence.cancel.error.\(recurrence.id)")
                if model.canRetryCancellation(id: recurrence.id) {
                    Button("Tentar cancelamento novamente") {
                        Task { await model.retryCancellation(id: recurrence.id) }
                    }
                    .frame(minHeight: 44)
                    .accessibilityIdentifier("recurrence.cancel.retry.\(recurrence.id)")
                }
            }
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(
            "\(recurrence.description), "
                + "\(moneyFormatter.string(minorUnits: recurrence.expectedAmount.minor)), "
                + "mensal, início em \(recurrence.startsOn.displayValue), "
                + "status \(recurrence.status.displayName)"
        )
        .accessibilityIdentifier("recurrence.item.\(recurrence.id)")
    }
}

private struct RecurrenceCreateView: View {
    @Bindable var model: RecurrencesViewModel
    @FocusState private var focusedField: Field?

    private let moneyFormatter = BRLMoneyFormatter()

    private enum Field {
        case description
        case amount
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(title)
                .toolbar {
                    if !model.isCreationBusy {
                        ToolbarItem(placement: .cancellationAction) {
                            Button("Fechar") { model.dismissCreation() }
                                .frame(minHeight: 44)
                                .accessibilityIdentifier("recurrence.close")
                        }
                    }
                }
        }
    }

    private var title: String {
        switch model.creationState {
        case .editing, .previewing: "Nova recorrência"
        case .reviewing, .submitting, .retryable, .requiresEditing: "Revisar recorrência"
        case .success: "Recorrência cadastrada"
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.creationState {
        case .editing, .previewing:
            inputForm
        case let .reviewing(reviewed), let .submitting(reviewed),
             let .retryable(reviewed), let .requiresEditing(reviewed):
            review(reviewed)
        case let .success(recurrence):
            success(recurrence)
        }
    }

    private var inputForm: some View {
        Form {
            Section {
                Text("Cadastre um compromisso mensal esperado. Nenhuma despesa ou pagamento será criado automaticamente.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Section("Compromisso mensal") {
                TextField("Descrição", text: $model.description)
                    .textContentType(.none)
                    .submitLabel(.next)
                    .focused($focusedField, equals: .description)
                    .accessibilityIdentifier("recurrence.description")

                TextField("Valor esperado", text: $model.amountText)
                    .keyboardType(.decimalPad)
                    .focused($focusedField, equals: .amount)
                    .accessibilityHint("Use vírgula ou ponto e até duas casas decimais")
                    .accessibilityIdentifier("recurrence.amount")

                DatePicker(
                    "Data de início",
                    selection: Binding(
                        get: { model.startsOnPickerDate },
                        set: { model.setStartsOnPickerDate($0) }
                    ),
                    displayedComponents: .date
                )
                .environment(\.calendar, RecurrenceCivilDate.pickerCalendar)
                .environment(\.timeZone, RecurrenceCivilDate.pickerCalendar.timeZone)
                .accessibilityHint("Seleciona apenas uma data civil, sem horário")
                .accessibilityIdentifier("recurrence.startsOn")
            }

            if let error = model.creationErrorMessage {
                Section {
                    Label(error, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityIdentifier("recurrence.error")
                }
            }

            Section {
                Button {
                    focusedField = nil
                    Task { await model.review() }
                } label: {
                    HStack {
                        Spacer()
                        if model.isCreationBusy {
                            ProgressView()
                                .accessibilityLabel("Preparando revisão da recorrência")
                        } else {
                            Text("Revisar recorrência")
                        }
                        Spacer()
                    }
                    .frame(minHeight: 44)
                }
                .disabled(model.isCreationBusy)
                .accessibilityIdentifier("recurrence.review")
            }
        }
        .scrollDismissesKeyboard(.immediately)
        .accessibilityIdentifier("recurrence.create.screen")
    }

    private func review(_ reviewed: ReviewedRecurrence) -> some View {
        Form {
            Section("Confira o compromisso esperado") {
                if reviewed.source.suggestionID != nil {
                    Text("Revise os dados preparados pelo servidor. A sugestão só vira recorrência após sua confirmação.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityIdentifier("recurrence.suggestion.review.notice")
                }
                summaryRow("Descrição", reviewed.preview.description, "recurrence.review.description")
                summaryRow(
                    "Valor esperado",
                    moneyFormatter.string(minorUnits: reviewed.preview.expectedAmount.minor),
                    "recurrence.review.amount"
                )
                summaryRow("Frequência", reviewed.preview.frequency.displayName, "recurrence.review.frequency")
                summaryRow("Data de início", reviewed.preview.startsOn.displayValue, "recurrence.review.startsOn")
                Text("Esta confirmação cadastra uma expectativa mensal. Ela não registra despesa nem executa cobrança.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }

            if let error = model.creationErrorMessage {
                Section {
                    Label(error, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .fixedSize(horizontal: false, vertical: true)
                        .accessibilityIdentifier("recurrence.review.error")
                }
            }

            Section {
                if reviewed.source == .manual {
                    Button("Editar") { model.editCreation() }
                        .frame(minHeight: 44)
                        .disabled(model.isCreationBusy)
                        .accessibilityIdentifier("recurrence.edit")
                }

                if !model.creationState.requiresEditing {
                    Button {
                        Task { await model.confirm() }
                    } label: {
                        HStack {
                            Spacer()
                            if model.isCreationBusy {
                                ProgressView()
                                    .accessibilityLabel("Confirmando recorrência")
                            } else if model.creationState.isRetryable {
                                Text("Tentar novamente")
                            } else {
                                Text("Confirmar recorrência")
                            }
                            Spacer()
                        }
                        .frame(minHeight: 44)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(model.isCreationBusy)
                    .accessibilityIdentifier("recurrence.confirm")
                }
            }
        }
        .accessibilityIdentifier("recurrence.review.screen")
    }

    private func success(_ recurrence: Recurrence) -> some View {
        ContentUnavailableView {
            Label("Recorrência cadastrada", systemImage: "checkmark.circle.fill")
                .accessibilityIdentifier("recurrence.success")
        } description: {
            Text("\(recurrence.description) agora aparece entre seus compromissos esperados.")
        } actions: {
            Button("Voltar para recorrências") {
                model.finishCreation()
            }
            .buttonStyle(.borderedProminent)
            .frame(minHeight: 44)
            .accessibilityIdentifier("recurrence.success.return")
        }
    }

    private func summaryRow(_ title: String, _ value: String, _ identifier: String) -> some View {
        LabeledContent(title) {
            Text(value)
                .multilineTextAlignment(.trailing)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier(identifier)
        }
    }
}

private extension RecurrenceCreationState {
    var isRetryable: Bool {
        guard case .retryable = self else { return false }
        return true
    }

    var requiresEditing: Bool {
        guard case .requiresEditing = self else { return false }
        return true
    }
}
