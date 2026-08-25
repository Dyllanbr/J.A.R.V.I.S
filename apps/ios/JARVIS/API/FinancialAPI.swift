import Foundation

@MainActor
protocol FinancialAPI {
    func categories() async throws -> [CategoryDefinition]
    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview
    func preview(_ request: IncomeRequest) async throws -> IncomePreview
    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense
    func create(_ request: IncomeRequest, idempotencyKey: String) async throws -> RecordedIncome
    func transactions(month: String) async throws -> TransactionMonth
    func previewRecurrence(_ request: RecurrenceRequest) async throws -> RecurrencePreview
    func createRecurrence(_ request: RecurrenceRequest, idempotencyKey: String) async throws -> RecordedRecurrence
    func recurrences() async throws -> RecurrenceList
    func cancelRecurrence(id: String, idempotencyKey: String) async throws -> RecordedRecurrence
    func recurrenceSuggestions() async throws -> RecurrenceSuggestionList
    func dismissRecurrenceSuggestion(id: String) async throws -> DismissedRecurrenceSuggestion
    func previewRecurrenceSuggestion(id: String) async throws -> RecurrencePreview
}

enum FinancialAPIError: Error, Equatable {
    case invalidData
    case connectionUnavailable
    case serviceUnavailable
    case conflict
    case notFound
    case alreadyCancelled
    case suggestionNotFound
    case suggestionSuppressed
    case invalidResponse
    case configuration

    var userMessage: String {
        switch self {
        case .invalidData:
            "Confira os dados informados e tente novamente."
        case .connectionUnavailable:
            "Não foi possível conectar ao serviço. Verifique a conexão e tente novamente."
        case .serviceUnavailable:
            "O serviço está indisponível no momento. Tente novamente."
        case .conflict:
            "Não foi possível repetir esta confirmação. Revise os dados e tente novamente."
        case .notFound:
            "A recorrência não foi encontrada. Atualize a lista e tente novamente."
        case .alreadyCancelled:
            "Esta recorrência já está cancelada. Atualize a lista para ver o estado atual."
        case .suggestionNotFound:
            "Esta sugestão não está mais disponível. Atualizamos a lista para você."
        case .suggestionSuppressed:
            "Esta sugestão já foi descartada. Atualizamos a lista para você."
        case .invalidResponse, .configuration:
            "Não foi possível concluir a operação. Tente novamente."
        }
    }
}

@MainActor
final class URLSessionFinancialAPIClient: FinancialAPI {
    private let baseURL: URL
    private let session: URLSession
    private let timestampCodec = RFC3339DateCodec()

    init(baseURL: URL, session: URLSession? = nil) {
        self.baseURL = baseURL
        self.session = session ?? Self.makeSession()
    }

    func categories() async throws -> [CategoryDefinition] {
        var request = baseRequest(
            url: baseURL.appendingPathComponent("v1/categories"),
            method: "GET"
        )
        request.cachePolicy = .reloadIgnoringLocalCacheData
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 200, data: data)
        let definitions: [CategoryDefinition] = try decode(data)
        guard Set(definitions.map(\.id)).count == definitions.count else {
            throw FinancialAPIError.invalidResponse
        }
        return definitions
    }

    func preview(_ requestBody: ExpenseRequest) async throws -> ExpensePreview {
        let request = try makeRequest(path: "v1/transactions/preview", method: "POST", body: requestBody)
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 200, data: data)
        let preview: ExpensePreview = try decode(data)
        guard (try? timestampCodec.decode(preview.occurredAt)) != nil else {
            throw FinancialAPIError.invalidResponse
        }
        return preview
    }

    func preview(_ requestBody: IncomeRequest) async throws -> IncomePreview {
        let request = try makeRequest(path: "v1/transactions/preview", method: "POST", body: requestBody)
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 200, data: data)
        let preview: IncomePreview = try decode(data)
        guard (try? timestampCodec.decode(preview.occurredAt)) != nil else {
            throw FinancialAPIError.invalidResponse
        }
        return preview
    }

    func create(_ requestBody: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense {
        var request = try makeRequest(path: "v1/transactions", method: "POST", body: requestBody)
        request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key")
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 201, data: data)
        let expense: Expense = try decode(data)
        guard timestampsAreValid(expense) else {
            throw FinancialAPIError.invalidResponse
        }
        return RecordedExpense(
            expense: expense,
            replayed: response.value(forHTTPHeaderField: "Idempotency-Replayed") == "true"
        )
    }

    func create(_ requestBody: IncomeRequest, idempotencyKey: String) async throws -> RecordedIncome {
        var request = try makeRequest(path: "v1/transactions", method: "POST", body: requestBody)
        request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key")
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 201, data: data)
        let income: Income = try decode(data)
        guard timestampsAreValid(income) else {
            throw FinancialAPIError.invalidResponse
        }
        return RecordedIncome(
            income: income,
            replayed: response.value(forHTTPHeaderField: "Idempotency-Replayed") == "true"
        )
    }

    func transactions(month: String) async throws -> TransactionMonth {
        guard var components = URLComponents(
            url: baseURL.appendingPathComponent("v1/transactions"),
            resolvingAgainstBaseURL: false
        ) else {
            throw FinancialAPIError.configuration
        }
        components.queryItems = [URLQueryItem(name: "month", value: month)]
        guard let url = components.url else {
            throw FinancialAPIError.configuration
        }

        var request = baseRequest(url: url, method: "GET")
        request.cachePolicy = .reloadIgnoringLocalCacheData
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 200, data: data)
        let monthResponse: TransactionMonth = try decode(data)
        guard monthResponse.items.allSatisfy(timestampsAreValid) else {
            throw FinancialAPIError.invalidResponse
        }
        return monthResponse
    }

    func previewRecurrence(_ requestBody: RecurrenceRequest) async throws -> RecurrencePreview {
        let request = try makeRequest(path: "v1/recurrences/preview", method: "POST", body: requestBody)
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 200, data: data)
        return try decode(data)
    }

    func createRecurrence(
        _ requestBody: RecurrenceRequest,
        idempotencyKey: String
    ) async throws -> RecordedRecurrence {
        var request = try makeRequest(path: "v1/recurrences", method: "POST", body: requestBody)
        request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key")
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 201, data: data)
        let recurrence: Recurrence = try decode(data)
        guard recurrenceTimestampsAreValid(recurrence) else {
            throw FinancialAPIError.invalidResponse
        }
        return RecordedRecurrence(
            recurrence: recurrence,
            replayed: response.value(forHTTPHeaderField: "Idempotency-Replayed") == "true"
        )
    }

    func recurrences() async throws -> RecurrenceList {
        var request = baseRequest(
            url: baseURL.appendingPathComponent("v1/recurrences"),
            method: "GET"
        )
        request.cachePolicy = .reloadIgnoringLocalCacheData
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 200, data: data)
        let result: RecurrenceList = try decode(data)
        guard result.items.allSatisfy(recurrenceTimestampsAreValid) else {
            throw FinancialAPIError.invalidResponse
        }
        return result
    }

    func cancelRecurrence(id: String, idempotencyKey: String) async throws -> RecordedRecurrence {
        let url = baseURL
            .appendingPathComponent("v1")
            .appendingPathComponent("recurrences")
            .appendingPathComponent(id)
            .appendingPathComponent("cancel")
        var request = baseRequest(url: url, method: "POST")
        request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key")
        let (data, response) = try await perform(request)
        try requireStatus(response, expected: 200, data: data)
        let recurrence: Recurrence = try decode(data)
        guard recurrenceTimestampsAreValid(recurrence) else {
            throw FinancialAPIError.invalidResponse
        }
        return RecordedRecurrence(
            recurrence: recurrence,
            replayed: response.value(forHTTPHeaderField: "Idempotency-Replayed") == "true"
        )
    }

    func recurrenceSuggestions() async throws -> RecurrenceSuggestionList {
        var request = baseRequest(
            url: baseURL.appendingPathComponent("v1/recurrence-suggestions"),
            method: "GET"
        )
        request.cachePolicy = .reloadIgnoringLocalCacheData
        let (data, response) = try await perform(request)
        try requireSuggestionStatus(response, expected: 200, data: data)
        return try decode(data)
    }

    func dismissRecurrenceSuggestion(id: String) async throws -> DismissedRecurrenceSuggestion {
        let request = try recurrenceSuggestionRequest(id: id, action: "dismiss")
        let (data, response) = try await perform(request)
        try requireSuggestionStatus(response, expected: 204, data: data)
        guard data.isEmpty else { throw FinancialAPIError.invalidResponse }
        let replayed: Bool
        switch response.value(forHTTPHeaderField: "Idempotency-Replayed") {
        case nil:
            replayed = false
        case "true":
            replayed = true
        default:
            throw FinancialAPIError.invalidResponse
        }
        return DismissedRecurrenceSuggestion(replayed: replayed)
    }

    func previewRecurrenceSuggestion(id: String) async throws -> RecurrencePreview {
        let request = try recurrenceSuggestionRequest(id: id, action: "preview")
        let (data, response) = try await perform(request)
        try requireSuggestionStatus(response, expected: 200, data: data)
        return try decode(data)
    }

    private func makeRequest<Body: Encodable>(path: String, method: String, body: Body) throws -> URLRequest {
        var request = baseRequest(url: baseURL.appendingPathComponent(path), method: method)
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        do {
            request.httpBody = try JSONEncoder().encode(body)
        } catch {
            throw FinancialAPIError.invalidData
        }
        return request
    }

    private func baseRequest(url: URL, method: String) -> URLRequest {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 15
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("no-store", forHTTPHeaderField: "Cache-Control")
        return request
    }

    private func recurrenceSuggestionRequest(id: String, action: String) throws -> URLRequest {
        guard RecurrenceSuggestion.isValidID(id) else { throw FinancialAPIError.invalidData }
        let url = baseURL
            .appendingPathComponent("v1")
            .appendingPathComponent("recurrence-suggestions")
            .appendingPathComponent(id)
            .appendingPathComponent(action)
        return baseRequest(url: url, method: "POST")
    }

    private func perform(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        do {
            let (data, response) = try await session.data(for: request)
            guard let httpResponse = response as? HTTPURLResponse else {
                throw FinancialAPIError.invalidResponse
            }
            return (data, httpResponse)
        } catch is CancellationError {
            throw CancellationError()
        } catch let error as URLError where error.code == .cancelled {
            throw CancellationError()
        } catch let error as FinancialAPIError {
            throw error
        } catch {
            throw FinancialAPIError.connectionUnavailable
        }
    }

    private func requireStatus(_ response: HTTPURLResponse, expected: Int, data: Data) throws {
        guard response.statusCode == expected else {
            let code = try? JSONDecoder().decode(APIErrorEnvelope.self, from: data).error.code
            switch response.statusCode {
            case 400:
                throw FinancialAPIError.invalidData
            case 404:
                throw FinancialAPIError.notFound
            case 409:
                if code == "RECURRENCE_ALREADY_CANCELLED" {
                    throw FinancialAPIError.alreadyCancelled
                }
                throw FinancialAPIError.conflict
            case 500...599:
                throw FinancialAPIError.serviceUnavailable
            default:
                throw FinancialAPIError.invalidResponse
            }
        }
    }

    private func requireSuggestionStatus(_ response: HTTPURLResponse, expected: Int, data: Data) throws {
        guard response.statusCode == expected else {
            let code = try? JSONDecoder().decode(APIErrorEnvelope.self, from: data).error.code
            switch (response.statusCode, code) {
            case (400, "INVALID_REQUEST"):
                throw FinancialAPIError.invalidData
            case (404, "RECURRENCE_SUGGESTION_NOT_FOUND"):
                throw FinancialAPIError.suggestionNotFound
            case (409, "RECURRENCE_SUGGESTION_SUPPRESSED"):
                throw FinancialAPIError.suggestionSuppressed
            case (500...599, _):
                throw FinancialAPIError.serviceUnavailable
            default:
                throw FinancialAPIError.invalidResponse
            }
        }
    }

    private func decode<Value: Decodable>(_ data: Data) throws -> Value {
        do {
            return try JSONDecoder().decode(Value.self, from: data)
        } catch {
            throw FinancialAPIError.invalidResponse
        }
    }

    private func timestampsAreValid(_ expense: Expense) -> Bool {
        (try? timestampCodec.decode(expense.occurredAt)) != nil
            && (try? timestampCodec.decode(expense.createdAt)) != nil
            && (try? timestampCodec.decode(expense.updatedAt)) != nil
    }

    private func timestampsAreValid(_ income: Income) -> Bool {
        (try? timestampCodec.decode(income.occurredAt)) != nil
            && (try? timestampCodec.decode(income.createdAt)) != nil
            && (try? timestampCodec.decode(income.updatedAt)) != nil
    }

    private func timestampsAreValid(_ transaction: FinancialTransaction) -> Bool {
        switch transaction {
        case let .expense(expense):
            timestampsAreValid(expense)
        case let .income(income):
            timestampsAreValid(income)
        }
    }

    private func recurrenceTimestampsAreValid(_ recurrence: Recurrence) -> Bool {
        guard (try? timestampCodec.decode(recurrence.createdAt)) != nil else { return false }
        switch recurrence.status {
        case .active:
            return recurrence.cancelledAt == nil
        case .cancelled:
            guard let cancelledAt = recurrence.cancelledAt else { return false }
            return (try? timestampCodec.decode(cancelledAt)) != nil
        }
    }

    private static func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 30
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        configuration.httpShouldSetCookies = false
        return URLSession(configuration: configuration)
    }
}
