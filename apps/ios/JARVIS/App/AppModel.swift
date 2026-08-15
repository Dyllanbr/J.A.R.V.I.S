import Foundation
import Observation

@MainActor
@Observable
final class AppModel {
    let registration: RegistrationViewModel
    let history: HistoryViewModel

    init(api: any FinancialAPI, now: Date = Date()) {
        let history = HistoryViewModel(api: api, now: now)
        self.history = history
        registration = RegistrationViewModel(
            api: api,
            now: now,
            onTransactionRecorded: { [weak history] in
                history?.transactionWasRecorded()
            }
        )
    }
}
