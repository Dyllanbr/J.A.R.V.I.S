import SwiftUI

struct RecurrencesView: View {
    @Bindable var model: RecurrencesViewModel

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
            await model.loadIfNeeded()
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
    }

    @ViewBuilder
    private var content: some View {
        switch model.listState {
        case .idle, .loading:
            ProgressView("Carregando recorrências")
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("recurrence.loading")
        case let .loaded(items):
            if items.isEmpty {
                ContentUnavailableView {
                    Label("Nenhuma recorrência cadastrada", systemImage: "arrow.triangle.2.circlepath")
                } description: {
                    Text("Cadastre manualmente compromissos mensais que você espera acompanhar.")
                }
                .accessibilityIdentifier("recurrence.empty")
            } else {
                List(items) { recurrence in
                    recurrenceRow(recurrence)
                }
                .listStyle(.plain)
                .refreshable { await model.refresh() }
                .accessibilityIdentifier("recurrence.list")
            }
        case let .failed(message):
            ContentUnavailableView {
                Label("Não foi possível carregar", systemImage: "wifi.exclamationmark")
            } description: {
                Text(message)
            } actions: {
                Button("Tentar novamente") {
                    Task { await model.retryList() }
                }
                .buttonStyle(.borderedProminent)
                .frame(minHeight: 44)
                .accessibilityIdentifier("recurrence.retry")
            }
        }
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
                Button("Editar") { model.editCreation() }
                    .frame(minHeight: 44)
                    .disabled(model.isCreationBusy)
                    .accessibilityIdentifier("recurrence.edit")

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
