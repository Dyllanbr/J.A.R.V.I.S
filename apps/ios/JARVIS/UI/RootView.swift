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

        init(model: AppModel) {
            currentModel = model
            registerController = UIHostingController(rootView: Self.registerView(model: model))
            historyController = UIHostingController(rootView: Self.historyView(model: model))
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
            setViewControllers([registerController, historyController], animated: false)
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
        }

        private static func registerView(model: AppModel) -> AnyView {
            AnyView(
                RegisterView(model: model.registration)
                    .environment(\.locale, Locale(identifier: "pt_BR"))
            )
        }

        private static func historyView(model: AppModel) -> AnyView {
            AnyView(
                HistoryView(model: model.history)
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
