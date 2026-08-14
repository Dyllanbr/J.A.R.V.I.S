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
        case let .success(expense):
            success(expense)
        }
    }

    private var navigationTitle: String {
        switch model.state {
        case .editing, .previewing:
            "Registrar"
        case .reviewing, .submitting, .retryable, .requiresEditing:
            "Revisar despesa"
        case .success:
            "Registro concluído"
        }
    }

    private var form: some View {
        Form {
            Section("Despesa") {
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
                                .accessibilityLabel("Revisando despesa")
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

    private func review(_ reviewed: ReviewedExpense) -> some View {
        Form {
            Section("Confira antes de registrar") {
                summaryRow("Tipo", value: "Despesa", identifier: "review.type")
                summaryRow("Descrição", value: reviewed.preview.description, identifier: "review.description")
                summaryRow(
                    "Valor",
                    value: moneyFormatter.string(minorUnits: reviewed.preview.amount.minor),
                    identifier: "review.amount"
                )
                summaryRow(
                    "Forma de pagamento",
                    value: reviewed.preview.paymentMethod.displayName,
                    identifier: "review.paymentMethod"
                )
                summaryRow(
                    "Data e hora",
                    value: displayFormatter.dateTime(reviewed.preview.occurredAt),
                    identifier: "review.occurredAt"
                )
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

    private func success(_ expense: Expense) -> some View {
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
