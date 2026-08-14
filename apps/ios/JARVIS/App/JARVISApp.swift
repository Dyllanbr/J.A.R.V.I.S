import SwiftUI

@main
struct JARVISApp: App {
    @State private var model: AppModel

    init() {
        let api = AppConfiguration.financialAPI()
        _model = State(initialValue: AppModel(api: api))
    }

    var body: some Scene {
        WindowGroup {
            RootView(model: model)
        }
    }
}
