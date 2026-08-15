import SwiftUI

struct RegisterView: View {
    @Bindable var model: RegistrationViewModel
    @FocusState private var focusedField: FormField?

    private let moneyFormatter = BRLMoneyFormatter()
    private let displayFormatter = FinancialDisplayFormatter()

    private enum FormField {
        case description
        case amount
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(navigationTitle)
        }
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .editing, .previewing:
            form
        case let .reviewing(reviewed),
             let .submitting(reviewed),
             let .retryable(reviewed),
             let .requiresEditing(reviewed):
            review(reviewed)
        case let .success(transaction):
            success(transaction)
        }
    }

    private var navigationTitle: String {
        switch model.state {
        case .editing, .previewing:
            "Registrar"
        case .reviewing, .submitting, .retryable, .requiresEditing:
            model.reviewedTransaction.map { "Revisar \($0.type.displayName.lowercased())" }
                ?? "Revisar registro"
        case .success:
            "Registro concluído"
        }
    }

    private var form: some View {
        Form {
            Section("Tipo de movimentação") {
                HStack {
                    ForEach(TransactionType.allCases) { type in
                        Button {
                            model.selectTransactionType(type)
                        } label: {
                            Label(
                                type.displayName,
                                systemImage: model.transactionType == type
                                    ? "checkmark.circle.fill"
                                    : "circle"
                            )
                            .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                        .accessibilityAddTraits(model.transactionType == type ? .isSelected : [])
                        .accessibilityHint(
                            model.transactionType == type
                                ? "Selecionado"
                                : "Seleciona \(type.displayName.lowercased())"
                        )
                        .accessibilityIdentifier("register.type.\(type.rawValue.lowercased())")
                    }
                }
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("register.type")
            }

            Section(model.transactionType.displayName) {
                TextField("Descrição", text: $model.description)
                    .textContentType(.none)
                    .submitLabel(.next)
                    .focused($focusedField, equals: .description)
                    .accessibilityIdentifier("register.description")

                TextField("Valor", text: $model.amountText)
                    .keyboardType(.decimalPad)
                    .focused($focusedField, equals: .amount)
                    .accessibilityLabel("Valor em reais")
                    .accessibilityHint("Use vírgula ou ponto e até duas casas decimais")
                    .accessibilityIdentifier("register.amount")

                if model.transactionType == .expense {
                    Picker("Forma de pagamento", selection: $model.paymentMethod) {
                        ForEach(PaymentMethod.allCases) { method in
                            Text(method.displayName)
                                .tag(method)
                                .accessibilityIdentifier(
                                    "register.paymentMethod.\(method.rawValue.lowercased())"
                                )
                        }
                    }
                    .accessibilityIdentifier("register.paymentMethod")
                }

                DatePicker(
                    "Data e hora",
                    selection: $model.occurredAt,
                    displayedComponents: [.date, .hourAndMinute]
                )
                .accessibilityIdentifier("register.occurredAt")
            }

            if let errorMessage = model.errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .accessibilityIdentifier("register.error")
                }
            }

            Section {
                Button {
                    focusedField = nil
                    Task { await model.review() }
                } label: {
                    HStack {
                        Spacer()
                        if model.isBusy {
                            ProgressView()
                                .accessibilityLabel("Revisando movimentação")
                        } else {
                            Text("Revisar")
                        }
                        Spacer()
                    }
                    .frame(minHeight: 44)
                }
                .disabled(model.isBusy)
                .accessibilityIdentifier("register.review")
            }
        }
        .accessibilityIdentifier("register.screen")
        .scrollDismissesKeyboard(.immediately)
    }

    private func review(_ reviewed: ReviewedTransaction) -> some View {
        Form {
            Section("Confira antes de registrar") {
                switch reviewed {
                case let .expense(expense):
                    reviewCommonRows(
                        type: .expense,
                        description: expense.preview.description,
                        amount: expense.preview.amount,
                        occurredAt: expense.preview.occurredAt
                    )
                    summaryRow(
                        "Forma de pagamento",
                        value: expense.preview.paymentMethod.displayName,
                        identifier: "review.paymentMethod"
                    )
                case let .income(income):
                    reviewCommonRows(
                        type: .income,
                        description: income.preview.description,
                        amount: income.preview.amount,
                        occurredAt: income.preview.occurredAt
                    )
                }
            }

            if let errorMessage = model.errorMessage {
                Section {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(.red)
                        .accessibilityIdentifier("review.error")
                }
            }

            Section {
                Button("Editar") { model.edit() }
                    .frame(minHeight: 44)
                    .disabled(model.isBusy)
                    .accessibilityIdentifier("review.edit")

                if !model.state.requiresEditing {
                    Button {
                        Task { await model.confirm() }
                    } label: {
                        HStack {
                            Spacer()
                            if model.isBusy {
                                ProgressView()
                                    .accessibilityLabel("Confirmando registro")
                            } else if case .retryable = model.state {
                                Text("Tentar novamente")
                            } else {
                                Text("Confirmar registro")
                            }
                            Spacer()
                        }
                        .frame(minHeight: 44)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(model.isBusy)
                    .accessibilityIdentifier(
                        model.state.isRetryable ? "review.retry" : "review.confirm"
                    )
                }
            }
        }
        .accessibilityIdentifier("review.screen")
    }

    private func success(_ transaction: FinancialTransaction) -> some View {
        switch transaction {
        case let .expense(expense):
            ContentUnavailableView {
                Label("Despesa registrada", systemImage: "checkmark.circle.fill")
                    .accessibilityIdentifier("register.success")
            } description: {
                Text("\(expense.description) foi adicionada ao histórico.")
            } actions: {
                Button("Registrar nova despesa") {
                    model.startNewExpense()
                }
                .buttonStyle(.borderedProminent)
                .frame(minHeight: 44)
                .accessibilityIdentifier("register.newExpense")
            }
        case let .income(income):
            ContentUnavailableView {
                Label("Receita registrada", systemImage: "checkmark.circle.fill")
                    .accessibilityIdentifier("register.success")
            } description: {
                Text("\(income.description) foi adicionada ao histórico.")
            } actions: {
                Button("Registrar nova receita") {
                    model.startNewIncome()
                }
                .buttonStyle(.borderedProminent)
                .frame(minHeight: 44)
                .accessibilityIdentifier("register.newIncome")
            }
        }
    }

    @ViewBuilder
    private func reviewCommonRows(
        type: TransactionType,
        description: String,
        amount: FinancialMoney,
        occurredAt: String
    ) -> some View {
        summaryRow("Tipo", value: type.displayName, identifier: "review.type")
        summaryRow("Descrição", value: description, identifier: "review.description")
        summaryRow(
            "Valor",
            value: moneyFormatter.string(minorUnits: amount.minor),
            identifier: "review.amount"
        )
        summaryRow(
            "Data e hora",
            value: displayFormatter.dateTime(occurredAt),
            identifier: "review.occurredAt"
        )
    }

    private func summaryRow(_ title: String, value: String, identifier: String) -> some View {
        LabeledContent(title) {
            Text(value)
                .multilineTextAlignment(.trailing)
                .accessibilityIdentifier(identifier)
        }
    }
}

private extension RegistrationState {
    var isRetryable: Bool {
        guard case .retryable = self else { return false }
        return true
    }

    var requiresEditing: Bool {
        guard case .requiresEditing = self else { return false }
        return true
    }
}
