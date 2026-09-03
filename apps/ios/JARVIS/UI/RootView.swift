import SwiftUI
import UIKit

struct RootView: View {
    @Bindable var model: AppModel

    var body: some View {
        NativeTabContainer(model: model)
            .ignoresSafeArea()
    }
}

private struct NativeTabContainer: UIViewControllerRepresentable {
    let model: AppModel

    func makeUIViewController(context _: Context) -> Controller {
        Controller(model: model)
    }

    func updateUIViewController(_ controller: Controller, context _: Context) {
        controller.update(model: model)
    }

    final class Controller: UITabBarController {
        private var currentModel: AppModel
        private let registerController: UIHostingController<AnyView>
        private let historyController: UIHostingController<AnyView>
        private let recurrencesController: UIHostingController<AnyView>
        private let cardsController: UIHostingController<AnyView>

        init(model: AppModel) {
            currentModel = model
            registerController = UIHostingController(rootView: Self.registerView(model: model))
            historyController = UIHostingController(rootView: Self.historyView(model: model))
            recurrencesController = UIHostingController(rootView: Self.recurrencesView(model: model))
            cardsController = UIHostingController(rootView: Self.cardsView(model: model))
            super.init(nibName: nil, bundle: nil)

            registerController.tabBarItem = Self.tabBarItem(
                title: "Registrar",
                image: "plus.circle",
                selectedImage: "plus.circle.fill",
                identifier: "tab.register"
            )
            historyController.tabBarItem = Self.tabBarItem(
                title: "Histórico",
                image: "clock",
                selectedImage: "clock.fill",
                identifier: "tab.history"
            )
            recurrencesController.tabBarItem = Self.tabBarItem(
                title: "Recorrências",
                image: "arrow.triangle.2.circlepath",
                selectedImage: "arrow.triangle.2.circlepath.circle.fill",
                identifier: "tab.recurrences"
            )
            cardsController.tabBarItem = Self.tabBarItem(
                title: "Cartões",
                image: "creditcard",
                selectedImage: "creditcard.fill",
                identifier: "tab.cards"
            )
            setViewControllers(
                [registerController, historyController, recurrencesController, cardsController],
                animated: false
            )
        }

        @available(*, unavailable)
        required init?(coder _: NSCoder) {
            fatalError("init(coder:) is unavailable")
        }

        func update(model: AppModel) {
            guard currentModel !== model else {
                return
            }
            currentModel = model
            registerController.rootView = Self.registerView(model: model)
            historyController.rootView = Self.historyView(model: model)
            recurrencesController.rootView = Self.recurrencesView(model: model)
            cardsController.rootView = Self.cardsView(model: model)
        }

        private static func registerView(model: AppModel) -> AnyView {
            AnyView(
                RegisterView(model: model.registration, purchaseModel: model.cardPurchases)
                    .environment(\.locale, Locale(identifier: "pt_BR"))
            )
        }

        private static func historyView(model: AppModel) -> AnyView {
            AnyView(
                HistoryView(model: model.history)
                    .environment(\.locale, Locale(identifier: "pt_BR"))
            )
        }

        private static func recurrencesView(model: AppModel) -> AnyView {
            AnyView(
                RecurrencesView(
                    model: model.recurrences,
                    suggestionsModel: model.recurrenceSuggestions
                )
                    .environment(\.locale, Locale(identifier: "pt_BR"))
            )
        }

        private static func cardsView(model: AppModel) -> AnyView {
            AnyView(
                CreditCardsView(
                    model: model.creditCards,
                    purchaseModel: model.cardPurchases,
                    plansModel: model.installmentPlans
                )
                    .environment(\.locale, Locale(identifier: "pt_BR"))
            )
        }

        private static func tabBarItem(
            title: String,
            image: String,
            selectedImage: String,
            identifier: String
        ) -> UITabBarItem {
            let item = UITabBarItem(
                title: title,
                image: UIImage(systemName: image),
                selectedImage: UIImage(systemName: selectedImage)
            )
            item.accessibilityIdentifier = identifier
            return item
        }
    }
}
