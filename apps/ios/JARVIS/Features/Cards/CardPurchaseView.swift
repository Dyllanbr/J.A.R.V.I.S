import SwiftUI

struct CardPurchaseView: View {
    @Bindable var model: CardPurchaseViewModel
    @Environment(\.dismiss) private var dismiss
    private let money = BRLMoneyFormatter()
    private let dateFormatter = FinancialDisplayFormatter()

    var body: some View {
        NavigationStack {
            content.navigationTitle(title)
                .toolbar {
                    if !model.isBusy {
                        ToolbarItem(placement: .cancellationAction) {
                            Button("Fechar") { model.dismiss(); dismiss() }
                                .frame(minHeight: 44)
                                .accessibilityIdentifier("cardPurchase.close")
                        }
                    }
                }
        }
    }

    private var title: String {
        switch model.state {
        case .editing, .previewing: "Compra no cartão"
        case .reviewing, .submitting, .retryable, .requiresEditing: "Revisar compra"
        case .success: "Compra registrada"
        }
    }

    @ViewBuilder private var content: some View {
        switch model.state {
        case .editing, .previewing: form
        case let .reviewing(value), let .submitting(value), let .retryable(value), let .requiresEditing(value): review(value)
        case let .success(value): success(value)
        }
    }

    private var form: some View {
        Form {
            Section {
                Text("Registre uma compra já ocorrida. A compra à vista cria uma despesa; parcelas futuras permanecem apenas no plano.")
                    .font(.subheadline).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            Section("Compra") {
                TextField("Descrição", text: $model.description)
                    .accessibilityIdentifier("cardPurchase.description")
                TextField("Valor total", text: $model.amountText)
                    .keyboardType(.decimalPad)
                    .accessibilityIdentifier("cardPurchase.amount")
                Picker("Cartão", selection: Binding(get: { model.creditCardID }, set: { model.creditCardID = $0 })) {
                    Text("Selecione um cartão").tag(String?.none)
                    ForEach(model.cards.filter { $0.status == .active }) { card in
                        Text(card.name).tag(Optional(card.id))
                    }
                }
                .accessibilityIdentifier("cardPurchase.card")
                TextField("Parcelas (opcional, 2 a 120)", text: $model.installmentCountText)
                    .keyboardType(.numberPad)
                    .accessibilityIdentifier("cardPurchase.installments")
                DatePicker("Data da compra", selection: $model.occurredAt, displayedComponents: [.date, .hourAndMinute])
                    .accessibilityIdentifier("cardPurchase.occurredAt")
            }
            if case let .failed(message) = model.cardsState {
                Section { Label(message, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red) }
                    .accessibilityIdentifier("cardPurchase.cards.error")
            }
            if let error = model.errorMessage {
                Section { Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red) }
                    .accessibilityIdentifier("cardPurchase.error")
            }
            Section {
                Button {
                    Task { await model.review() }
                } label: {
                    HStack { Spacer(); if model.isBusy { ProgressView() } else { Text("Revisar compra") }; Spacer() }
                        .frame(minHeight: 44)
                }
                .disabled(model.isBusy)
                .accessibilityIdentifier("cardPurchase.review")
            }
        }
        .accessibilityIdentifier("cardPurchase.form")
    }

    private func review(_ reviewed: ReviewedCardPurchase) -> some View {
        Form {
            Section("Confira os dados recebidos do servidor") {
                row("Descrição", reviewed.preview.description, identifier: "cardPurchase.review.description")
                row("Valor", money.string(minorUnits: reviewed.preview.amount.minor), identifier: "cardPurchase.review.amount")
                row("Cartão", model.selectedCard?.name ?? reviewed.preview.creditCardID, identifier: "cardPurchase.review.card")
                row("Data", dateFormatter.dateTime(reviewed.preview.occurredAt), identifier: "cardPurchase.review.occurredAt")
                row("Vencimento", reviewed.preview.statementDueOn.displayValue, identifier: "cardPurchase.review.statementDueOn")
                if let summary = reviewed.preview.installmentSummary {
                    row("Parcelas", "\(summary.installmentCount) · \(summary.firstDueDate.displayValue) a \(summary.lastDueDate.displayValue)", identifier: "cardPurchase.review.installments")
                } else {
                    row("Modalidade", "À vista", identifier: "cardPurchase.review.mode")
                }
                Text("Nada será salvo antes de Confirmar.").font(.footnote).foregroundStyle(.secondary)
            }
            if let error = model.errorMessage {
                Section { Label(error, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red) }
                    .accessibilityIdentifier("cardPurchase.review.error")
            }
            Section {
                Button("Editar") { model.edit() }.frame(minHeight: 44).disabled(model.isBusy)
                    .accessibilityIdentifier("cardPurchase.review.edit")
                Button {
                    Task { await model.confirm() }
                } label: {
                    HStack { Spacer(); if model.isBusy { ProgressView() } else if model.isRetryable { Text("Tentar novamente") } else { Text("Confirmar compra") }; Spacer() }
                        .frame(minHeight: 44)
                }
                .buttonStyle(.borderedProminent)
                .disabled(model.isBusy)
                .accessibilityIdentifier(model.isRetryable ? "cardPurchase.retry" : "cardPurchase.confirm")
            }
        }
        .accessibilityIdentifier("cardPurchase.review")
    }

    private func success(_ purchase: CardPurchase) -> some View {
        ContentUnavailableView {
            Label("Compra registrada", systemImage: "checkmark.circle.fill")
                .accessibilityIdentifier("cardPurchase.success")
        } description: {
            Text("\(purchase.expense.description) foi adicionada ao histórico.")
        } actions: {
            Button("Voltar para cartões") { model.finish(); dismiss() }
                .buttonStyle(.borderedProminent).frame(minHeight: 44)
                .accessibilityIdentifier("cardPurchase.done")
        }
    }

    private func row(_ title: String, _ value: String, identifier: String) -> some View {
        LabeledContent(title) { Text(value).multilineTextAlignment(.trailing).fixedSize(horizontal: false, vertical: true) }
            .accessibilityIdentifier(identifier)
    }
}
