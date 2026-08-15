import Foundation

@MainActor
protocol FinancialAPI {
    func preview(_ request: ExpenseRequest) async throws -> ExpensePreview
    func preview(_ request: IncomeRequest) async throws -> IncomePreview
    func create(_ request: ExpenseRequest, idempotencyKey: String) async throws -> RecordedExpense
    func create(_ request: IncomeRequest, idempotencyKey: String) async throws -> RecordedIncome
    func transactions(month: String) async throws -> TransactionMonth
}

enum FinancialAPIError: Error, Equatable {
    case invalidData
    case connectionUnavailable
    case serviceUnavailable
    case conflict
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
            _ = try? JSONDecoder().decode(APIErrorEnvelope.self, from: data)
            switch response.statusCode {
            case 400:
                throw FinancialAPIError.invalidData
            case 409:
                throw FinancialAPIError.conflict
            case 500...599:
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
