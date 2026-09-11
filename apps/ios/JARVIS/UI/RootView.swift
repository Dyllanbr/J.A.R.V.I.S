import SwiftUI
import UIKit

struct RootView: View {
    @Bindable var model: AppModel

    var body: some View {
        NativeTabContainer(model: model)
            .ignoresSafeArea(.container, edges: .bottom)
            .preferredColorScheme(.dark)
    }
}

// Shared visual language for the personal beta. The dark canvas and emerald accent keep
// the product legible and distinctive while preserving native iOS controls and traits.
enum JARVISDesign {
    static let canvas = Color(red: 10 / 255, green: 13 / 255, blue: 12 / 255)
    static let surface = Color(red: 20 / 255, green: 25 / 255, blue: 23 / 255)
    static let elevated = Color(red: 28 / 255, green: 34 / 255, blue: 32 / 255)
    static let accent = Color(red: 53 / 255, green: 210 / 255, blue: 138 / 255)
    static let positive = Color(red: 112 / 255, green: 230 / 255, blue: 167 / 255)
    static let negative = Color(red: 255 / 255, green: 108 / 255, blue: 116 / 255)
    static let muted = Color.white.opacity(0.62)
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
                    .stroke(Color.white.opacity(0.08), lineWidth: 1)
            }
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
                JARVISDesign.accent,
                in: RoundedRectangle(cornerRadius: 15, style: .continuous)
            )
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .opacity(configuration.isPressed ? 0.88 : 1)
            .animation(.easeOut(duration: 0.16), value: configuration.isPressed)
    }
}

struct JARVISSecondaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(JARVISDesign.accent)
            .background(
                JARVISDesign.accent.opacity(configuration.isPressed ? 0.2 : 0.1),
                in: RoundedRectangle(cornerRadius: 13, style: .continuous)
            )
            .overlay {
                RoundedRectangle(cornerRadius: 13, style: .continuous)
                    .stroke(JARVISDesign.accent.opacity(0.35), lineWidth: 1)
            }
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
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
            // The product opens on the financial overview. UI tests opt into the
            // registration-first state through the existing API test environment,
            // keeping their flows deterministic without making the beta feel like a
            // data-entry form on launch.
            let environment = ProcessInfo.processInfo.environment
            let isUITest = environment["JARVIS_IOS_API_MODE"] != nil
                || environment["XCTestConfigurationFilePath"] != nil
            selectedIndex = isUITest ? 0 : 1
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
            appearance.backgroundColor = UIColor(red: 10 / 255, green: 13 / 255, blue: 12 / 255, alpha: 0.98)
            appearance.shadowColor = UIColor.white.withAlphaComponent(0.1)

            let itemAppearance = UITabBarItemAppearance()
            itemAppearance.normal.iconColor = UIColor.white.withAlphaComponent(0.56)
            itemAppearance.normal.titleTextAttributes = [.foregroundColor: UIColor.white.withAlphaComponent(0.56)]
            let accent = UIColor(red: 53 / 255, green: 210 / 255, blue: 138 / 255, alpha: 1)
            itemAppearance.selected.iconColor = accent
            itemAppearance.selected.titleTextAttributes = [.foregroundColor: accent]
            appearance.stackedLayoutAppearance = itemAppearance

            tabBar.standardAppearance = appearance
            tabBar.scrollEdgeAppearance = appearance
            tabBar.tintColor = accent
        }

        private static func registerView(model: AppModel) -> AnyView {
            AnyView(
                RegisterView(model: model.registration, purchaseModel: model.cardPurchases)
                    .environment(\.locale, Locale(identifier: "pt_BR"))
                    .tint(JARVISDesign.accent)
            )
        }

        private static func historyView(model: AppModel) -> AnyView {
            AnyView(
                HistorySafeAvailableEntryView(
                    model: model.history,
                    scheduledCommitments: model.scheduledCommitments,
                    safeAvailable: model.safeAvailable,
                    financialGoals: model.financialGoals
                )
                    .environment(\.locale, Locale(identifier: "pt_BR"))
                    .tint(JARVISDesign.accent)
            )
        }

        private static func recurrencesView(model: AppModel) -> AnyView {
            AnyView(
                RecurrencesView(
                    model: model.recurrences,
                    suggestionsModel: model.recurrenceSuggestions
                )
                    .environment(\.locale, Locale(identifier: "pt_BR"))
                    .tint(JARVISDesign.accent)
            )
        }

        private static func cardsView(model: AppModel) -> AnyView {
            AnyView(
                CreditCardsView(
                    model: model.creditCards,
                    purchaseModel: model.cardPurchases,
                    plansModel: model.installmentPlans,
                    simulationModel: model.purchaseSimulation
                )
                    .environment(\.locale, Locale(identifier: "pt_BR"))
                    .tint(JARVISDesign.accent)
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
