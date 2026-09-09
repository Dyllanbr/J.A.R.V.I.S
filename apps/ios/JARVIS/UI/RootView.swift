import SwiftUI
import UIKit

struct RootView: View {
    @Bindable var model: AppModel

    var body: some View {
        NativeTabContainer(model: model)
            .ignoresSafeArea()
    }
}

// Shared visual language for the personal beta. It uses semantic system colors so the
// interface remains legible in light/dark mode and with accessibility settings enabled.
enum JARVISDesign {
    static let canvas = Color(uiColor: .systemGroupedBackground)
    static let surface = Color(uiColor: .secondarySystemGroupedBackground)
    static let elevated = Color(uiColor: .systemBackground)
    static let accent = Color(uiColor: .systemBlue)
    static let positive = Color(uiColor: .systemGreen)
    static let negative = Color(uiColor: .systemRed)
    static let muted = Color(uiColor: .secondaryLabel)
    static let cornerRadius: CGFloat = 20
}

struct JARVISCardModifier: ViewModifier {
    let padding: CGFloat

    func body(content: Content) -> some View {
        content
            .padding(padding)
            .background(JARVISDesign.elevated, in: RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: JARVISDesign.cornerRadius, style: .continuous)
                    .stroke(Color.primary.opacity(0.06), lineWidth: 1)
            }
            .shadow(color: Color.black.opacity(0.07), radius: 12, y: 5)
    }
}

extension View {
    func jarvisCard(padding: CGFloat = 16) -> some View {
        modifier(JARVISCardModifier(padding: padding))
    }
}

struct JARVISPrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity, minHeight: 50)
            .background(
                LinearGradient(
                    colors: [JARVISDesign.accent, JARVISDesign.accent.opacity(0.78)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                in: RoundedRectangle(cornerRadius: 15, style: .continuous)
            )
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .opacity(configuration.isPressed ? 0.88 : 1)
            .animation(.easeOut(duration: 0.16), value: configuration.isPressed)
    }
}

struct JARVISChoiceButtonStyle: ButtonStyle {
    let isSelected: Bool

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.body.weight(.semibold))
            .foregroundStyle(isSelected ? JARVISDesign.accent : Color.primary)
            .frame(maxWidth: .infinity, minHeight: 46)
            .background(
                isSelected ? JARVISDesign.accent.opacity(0.14) : JARVISDesign.surface,
                in: RoundedRectangle(cornerRadius: 13, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 13, style: .continuous)
                    .stroke(isSelected ? JARVISDesign.accent.opacity(0.45) : Color.primary.opacity(0.07), lineWidth: 1)
            }
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.easeOut(duration: 0.16), value: configuration.isPressed)
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
            configureTabBarAppearance()
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

        private func configureTabBarAppearance() {
            let appearance = UITabBarAppearance()
            appearance.configureWithOpaqueBackground()
            appearance.backgroundColor = .systemBackground
            appearance.shadowColor = UIColor.separator.withAlphaComponent(0.3)

            let itemAppearance = UITabBarItemAppearance()
            itemAppearance.normal.iconColor = .secondaryLabel
            itemAppearance.normal.titleTextAttributes = [.foregroundColor: UIColor.secondaryLabel]
            itemAppearance.selected.iconColor = .systemBlue
            itemAppearance.selected.titleTextAttributes = [.foregroundColor: UIColor.systemBlue]
            appearance.stackedLayoutAppearance = itemAppearance

            tabBar.standardAppearance = appearance
            tabBar.scrollEdgeAppearance = appearance
            tabBar.tintColor = .systemBlue
        }

        private static func registerView(model: AppModel) -> AnyView {
            AnyView(
                RegisterView(model: model.registration, purchaseModel: model.cardPurchases)
                    .environment(\.locale, Locale(identifier: "pt_BR"))
            )
        }

        private static func historyView(model: AppModel) -> AnyView {
            AnyView(
                HistorySafeAvailableEntryView(
                    model: model.history,
                    scheduledCommitments: model.scheduledCommitments,
                    safeAvailable: model.safeAvailable
                )
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
