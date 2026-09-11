import SwiftUI

struct RegisterView: View {
    @Bindable var model: RegistrationViewModel
    @Bindable var purchaseModel: CardPurchaseViewModel
    @FocusState private var focusedField: FormField?
    @State private var quickCaptureText = ""
    @State private var quickCaptureError: String?
    @FocusState private var quickCaptureFocused: Bool

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
                .tint(JARVISDesign.accent)
        }
        .task {
            await model.loadCategoriesIfNeeded()
        }
        .sheet(
            isPresented: Binding(
                get: { purchaseModel.isPresenting },
                set: { if !$0 { purchaseModel.dismiss() } }
            )
        ) {
            CardPurchaseView(model: purchaseModel)
                .environment(\.locale, Locale(identifier: "pt_BR"))
                .interactiveDismissDisabled(purchaseModel.isBusy)
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
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                quickCaptureCard
                registerIntro
                transactionTypePicker
                movementFields
                categoryCatalogStatus
                registerError
                reviewButton
            }
            .padding(.horizontal)
            .padding(.top, 12)
            .padding(.bottom, 28)
        }
        .accessibilityIdentifier("register.screen")
        .scrollDismissesKeyboard(.immediately)
        .background(JARVISDesign.canvas)
        .toolbar {
            ToolbarItemGroup(placement: .keyboard) {
                Spacer()
                Button("Concluído") {
                    focusedField = nil
                }
                .accessibilityIdentifier("keyboard.done")
            }
        }
    }

    private var quickCaptureCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .center, spacing: 10) {
                Image(systemName: "wand.and.stars")
                    .font(.headline.weight(.bold))
                    .foregroundStyle(JARVISDesign.canvas)
                    .frame(width: 34, height: 34)
                    .background(JARVISDesign.accent, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text("ENTRADA RÁPIDA")
                        .font(.caption2.weight(.bold))
                        .tracking(1.2)
                        .foregroundStyle(JARVISDesign.accent)
                    Text("Fale como você falaria com o J.A.R.V.I.S.")
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(.white)
                }
            }

            Text("O app preenche um rascunho para você revisar. Nada é salvo sem confirmação.")
                .font(.footnote)
                .foregroundStyle(JARVISDesign.muted)
                .fixedSize(horizontal: false, vertical: true)

            TextField("Ex.: comprei pão por R$ 12,50", text: $quickCaptureText, axis: .vertical)
                .lineLimit(1...3)
                .textContentType(.none)
                .submitLabel(.done)
                .focused($quickCaptureFocused)
                .font(.body.weight(.medium))
                .foregroundStyle(.white)
                .tint(JARVISDesign.accent)
                .padding(.horizontal, 13)
                .padding(.vertical, 11)
                .background(JARVISDesign.canvas, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 13, style: .continuous)
                        .stroke(Color.white.opacity(0.1), lineWidth: 1)
                }
                .accessibilityLabel("Entrada rápida")
                .accessibilityHint("Digite uma despesa ou receita em linguagem natural")
                .accessibilityIdentifier("register.quickCapture")

            if let quickCaptureError {
                Label(quickCaptureError, systemImage: "info.circle")
                    .font(.footnote)
                    .foregroundStyle(JARVISDesign.negative)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("register.quickCapture.error")
            }

            Button {
                applyQuickCapture()
            } label: {
                Label("Preencher rascunho", systemImage: "arrow.down.to.line.compact")
                    .frame(maxWidth: .infinity, minHeight: 42)
            }
            .buttonStyle(JARVISSecondaryButtonStyle())
            .disabled(quickCaptureText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.isBusy)
            .accessibilityIdentifier("register.quickCapture.apply")
        }
        .padding(15)
        .background(JARVISDesign.surface, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .stroke(JARVISDesign.accent.opacity(0.28), lineWidth: 1)
        }
    }

    private func applyQuickCapture() {
        do {
            let draft = try QuickCaptureParser.parse(quickCaptureText)
            if let installmentCountText = draft.installmentCountText {
                purchaseModel.begin(
                    description: draft.description,
                    amountText: draft.amountText,
                    installmentCountText: installmentCountText
                )
            } else {
                model.applyQuickCapture(draft)
            }
            quickCaptureText = ""
            quickCaptureError = nil
            quickCaptureFocused = false
        } catch let error as QuickCaptureParserError {
            quickCaptureError = error.message
        } catch {
            quickCaptureError = QuickCaptureParserError.invalidAmount.message
        }
    }

    private var registerIntro: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                Image(systemName: model.transactionType == .expense ? "arrow.down.right" : "arrow.up.right")
                    .font(.headline.weight(.bold))
                    .foregroundStyle(JARVISDesign.canvas)
                    .frame(width: 40, height: 40)
                    .background(
                        model.transactionType == .expense ? JARVISDesign.negative : JARVISDesign.positive,
                        in: RoundedRectangle(cornerRadius: 13, style: .continuous)
                    )
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text("NOVA MOVIMENTAÇÃO")
                        .font(.caption2.weight(.bold))
                        .tracking(1.3)
                        .foregroundStyle(JARVISDesign.accent)
                    Text("Registre sem perder o contexto.")
                        .font(.title3.weight(.bold))
                        .foregroundStyle(.white)
                }
            }
            Text("Confirme os detalhes antes de salvar no seu histórico.")
                .font(.subheadline)
                .foregroundStyle(JARVISDesign.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityIdentifier("register.intro")
    }

    private var transactionTypePicker: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("TIPO DE MOVIMENTAÇÃO")
                .font(.caption.weight(.bold))
                .tracking(1.1)
                .foregroundStyle(JARVISDesign.muted)
            HStack(spacing: 10) {
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
                    .buttonStyle(JARVISChoiceButtonStyle(isSelected: model.transactionType == type))
                    .accessibilityAddTraits(model.transactionType == type ? .isSelected : [])
                    .accessibilityHint(
                        model.transactionType == type
                            ? "Selecionado"
                            : "Seleciona \(type.displayName.lowercased())"
                    )
                    .accessibilityIdentifier("register.type.\(type.rawValue.lowercased())")
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("register.type")
    }

    private var movementFields: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(model.transactionType.displayName.uppercased())
                .font(.caption.weight(.bold))
                .tracking(1.1)
                .foregroundStyle(JARVISDesign.accent)
                .padding(.bottom, 12)

            registerTextField(
                title: "Descrição",
                placeholder: "Ex.: supermercado, salário...",
                text: $model.description,
                field: .description,
                identifier: "register.description"
            )
            registerDivider
            registerTextField(
                title: "Valor",
                placeholder: "0,00",
                text: $model.amountText,
                field: .amount,
                identifier: "register.amount",
                keyboard: .decimalPad,
                accessibilityLabel: "Valor em reais",
                accessibilityHint: "Use vírgula ou ponto e até duas casas decimais"
            )

            if model.transactionType == .expense {
                registerDivider
                Picker("Forma de pagamento", selection: $model.paymentMethod) {
                    ForEach(PaymentMethod.allCases) { method in
                        Text(method.displayName)
                            .tag(method)
                            .accessibilityIdentifier(
                                "register.paymentMethod.\(method.rawValue.lowercased())"
                            )
                    }
                }
                .pickerStyle(.menu)
                .tint(JARVISDesign.accent)
                .padding(.vertical, 7)
                .accessibilityIdentifier("register.paymentMethod")
            }

            registerDivider
            Picker(
                "Categoria",
                selection: Binding(
                    get: { model.selectedCategoryID },
                    set: { model.selectCategory($0) }
                )
            ) {
                Text("Sem categoria")
                    .tag(String?.none)
                    .accessibilityIdentifier("register.category.option.none")
                ForEach(model.availableCategories) { category in
                    Text(category.displayName)
                        .tag(Optional(category.id))
                        .accessibilityIdentifier("register.category.option.\(category.id)")
                }
            }
            .pickerStyle(.menu)
            .tint(JARVISDesign.accent)
            .disabled(!model.categoryCatalogState.isLoaded)
            .accessibilityValue(model.selectedCategoryDisplayName)
            .accessibilityHint(categoryAccessibilityHint)
            .accessibilityIdentifier("register.category")

            registerDivider
            DatePicker(
                "Data e hora",
                selection: $model.occurredAt,
                displayedComponents: [.date, .hourAndMinute]
            )
            .tint(JARVISDesign.accent)
            .padding(.vertical, 6)
            .accessibilityIdentifier("register.occurredAt")
        }
        .padding(16)
        .background(JARVISDesign.elevated, in: RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous)
                .stroke(Color.white.opacity(0.08), lineWidth: 1)
        }
    }

    private func registerTextField(
        title: String,
        placeholder: String,
        text: Binding<String>,
        field: FormField,
        identifier: String,
        keyboard: UIKeyboardType = .default,
        accessibilityLabel: String? = nil,
        accessibilityHint: String? = nil
    ) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(title)
                .font(.caption)
                .foregroundStyle(JARVISDesign.muted)
            TextField(placeholder, text: text)
                .textContentType(.none)
                .submitLabel(.next)
                .keyboardType(keyboard)
                .focused($focusedField, equals: field)
                .font(.body.weight(.medium))
                .foregroundStyle(.white)
                .tint(JARVISDesign.accent)
                .accessibilityLabel(accessibilityLabel ?? title)
                .accessibilityHint(accessibilityHint ?? "")
                .accessibilityIdentifier(identifier)
        }
        .padding(.vertical, 4)
    }

    private var registerDivider: some View {
        Divider().overlay(Color.white.opacity(0.08))
    }

    @ViewBuilder
    private var registerError: some View {
        if let errorMessage = model.errorMessage {
            Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                .font(.subheadline)
                .foregroundStyle(JARVISDesign.negative)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(14)
                .background(JARVISDesign.negative.opacity(0.1), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .accessibilityIdentifier("register.error")
        }
    }

    private var reviewButton: some View {
        Button {
            focusedField = nil
            if model.transactionType == .expense && model.paymentMethod == .credit {
                purchaseModel.begin(
                    description: model.description,
                    amountText: model.amountText,
                    occurredAt: model.occurredAt,
                    categoryID: model.selectedCategoryID
                )
            } else {
                Task { await model.review() }
            }
        } label: {
            HStack {
                if model.isBusy {
                    ProgressView()
                        .tint(.white)
                        .accessibilityLabel("Revisando movimentação")
                } else {
                    Text(model.transactionType == .expense && model.paymentMethod == .credit ? "Continuar com cartão" : "Revisar")
                }
            }
            .frame(maxWidth: .infinity, minHeight: 50)
        }
        .buttonStyle(JARVISPrimaryButtonStyle())
        .disabled(model.isBusy)
        .accessibilityIdentifier(model.transactionType == .expense && model.paymentMethod == .credit ? "register.cardPurchase" : "register.review")
    }

    private func review(_ reviewed: ReviewedTransaction) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("REVISÃO")
                        .font(.caption.weight(.bold))
                        .tracking(1.3)
                        .foregroundStyle(JARVISDesign.accent)
                    Text("Confira antes de registrar")
                        .font(.title2.weight(.bold))
                        .foregroundStyle(.white)
                    Text("Nada é salvo até você confirmar.")
                        .font(.subheadline)
                        .foregroundStyle(JARVISDesign.muted)
                }

                VStack(alignment: .leading, spacing: 0) {
                    switch reviewed {
                    case let .expense(expense):
                        reviewCommonRows(
                            type: .expense,
                            description: expense.preview.description,
                            amount: expense.preview.amount,
                            occurredAt: expense.preview.occurredAt
                        )
                        reviewDivider
                        summaryRow(
                            "Forma de pagamento",
                            value: expense.preview.paymentMethod.displayName,
                            identifier: "review.paymentMethod"
                        )
                        reviewDivider
                        summaryRow(
                            "Categoria",
                            value: expense.categoryDisplayName,
                            identifier: "review.category"
                        )
                    case let .income(income):
                        reviewCommonRows(
                            type: .income,
                            description: income.preview.description,
                            amount: income.preview.amount,
                            occurredAt: income.preview.occurredAt
                        )
                        reviewDivider
                        summaryRow(
                            "Categoria",
                            value: income.categoryDisplayName,
                            identifier: "review.category"
                        )
                    }
                }
                .padding(16)
                .background(JARVISDesign.elevated, in: RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous)
                        .stroke(Color.white.opacity(0.08), lineWidth: 1)
                }

                if let errorMessage = model.errorMessage {
                    Label(errorMessage, systemImage: "exclamationmark.triangle.fill")
                        .font(.subheadline)
                        .foregroundStyle(JARVISDesign.negative)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(14)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(JARVISDesign.negative.opacity(0.1), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                        .accessibilityIdentifier("review.error")
                }

                VStack(spacing: 10) {
                    Button("Editar") { model.edit() }
                        .frame(maxWidth: .infinity, minHeight: 48)
                        .background(JARVISDesign.surface, in: RoundedRectangle(cornerRadius: 15, style: .continuous))
                        .overlay {
                            RoundedRectangle(cornerRadius: 15, style: .continuous)
                                .stroke(Color.white.opacity(0.1), lineWidth: 1)
                        }
                        .disabled(model.isBusy)
                        .accessibilityIdentifier("review.edit")

                    if !model.state.requiresEditing {
                        Button {
                            Task { await model.confirm() }
                        } label: {
                            HStack {
                                if model.isBusy {
                                    ProgressView()
                                        .tint(.white)
                                        .accessibilityLabel("Confirmando registro")
                                } else if case .retryable = model.state {
                                    Text("Tentar novamente")
                                } else {
                                    Text("Confirmar registro")
                                }
                            }
                            .frame(maxWidth: .infinity, minHeight: 50)
                        }
                        .buttonStyle(JARVISPrimaryButtonStyle())
                        .disabled(model.isBusy)
                        .accessibilityIdentifier(
                            model.state.isRetryable ? "review.retry" : "review.confirm"
                        )
                    }
                }
            }
            .padding(.horizontal)
            .padding(.top, 18)
            .padding(.bottom, 28)
        }
        .accessibilityIdentifier("review.screen")
        .background(JARVISDesign.canvas)
    }

    private var reviewDivider: some View {
        Divider().overlay(Color.white.opacity(0.08)).padding(.vertical, 10)
    }

    @ViewBuilder
    private var categoryCatalogStatus: some View {
        switch model.categoryCatalogState {
        case .idle, .loading:
            Section {
                HStack {
                    ProgressView()
                    Text("Carregando categorias")
                }
                .accessibilityIdentifier("register.category.loading")
            }
        case let .failed(message):
            Section {
                Label(message, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.secondary)
                    .accessibilityIdentifier("register.category.error")
                Button("Tentar carregar categorias novamente") {
                    Task { await model.retryCategories() }
                }
                .frame(minHeight: 44)
                .accessibilityIdentifier("register.category.retry")
            }
        case .loaded:
            EmptyView()
        }
    }

    private var categoryAccessibilityHint: String {
        switch model.categoryCatalogState {
        case .loaded:
            "Seleciona uma categoria opcional para a movimentação"
        case .idle, .loading:
            "O catálogo de categorias está sendo carregado"
        case .failed:
            "Categorias indisponíveis; o registro sem categoria continua permitido"
        }
    }

    private func success(_ transaction: FinancialTransaction) -> some View {
        let title: String
        let description: String
        let newButtonTitle: String
        let newButtonIdentifier: String
        switch transaction {
        case let .expense(expense):
            title = "Despesa registrada"
            description = "\(expense.description) foi adicionada ao histórico."
            newButtonTitle = "Registrar nova despesa"
            newButtonIdentifier = "register.newExpense"
        case let .income(income):
            title = "Receita registrada"
            description = "\(income.description) foi adicionada ao histórico."
            newButtonTitle = "Registrar nova receita"
            newButtonIdentifier = "register.newIncome"
        }

        return VStack(spacing: 18) {
            Spacer(minLength: 28)
            VStack(spacing: 12) {
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: 54, weight: .semibold))
                    .foregroundStyle(JARVISDesign.positive)
                    .accessibilityHidden(true)
                Text(title)
                    .font(.title2.weight(.bold))
                    .foregroundStyle(.white)
                    .accessibilityIdentifier("register.success")
                Text(description)
                    .font(.body)
                    .foregroundStyle(JARVISDesign.muted)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity)
            .padding(24)
            .background(JARVISDesign.elevated, in: RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous)
                    .stroke(JARVISDesign.positive.opacity(0.25), lineWidth: 1)
            }
            Button(newButtonTitle) {
                switch transaction {
                case .expense: model.startNewExpense()
                case .income: model.startNewIncome()
                }
            }
            .buttonStyle(JARVISPrimaryButtonStyle())
            .accessibilityIdentifier(newButtonIdentifier)
            Spacer()
        }
        .padding(.horizontal)
        .padding(.top, 20)
        .background(JARVISDesign.canvas)
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

private extension CategoryCatalogState {
    var isLoaded: Bool {
        guard case .loaded = self else { return false }
        return true
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
