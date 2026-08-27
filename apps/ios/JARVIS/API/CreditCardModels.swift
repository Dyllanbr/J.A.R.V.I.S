import Foundation

enum CreditCardBrand: String, Codable, CaseIterable, Identifiable, Sendable {
    case visa = "VISA"
    case mastercard = "MASTERCARD"
    case elo = "ELO"
    case americanExpress = "AMERICAN_EXPRESS"
    case hipercard = "HIPERCARD"
    case other = "OTHER"

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .visa: "Visa"
        case .mastercard: "Mastercard"
        case .elo: "Elo"
        case .americanExpress: "American Express"
        case .hipercard: "Hipercard"
        case .other: "Outra"
        }
    }
}

enum CreditCardStatus: String, Codable, Sendable {
    case active = "ACTIVE"
    case archived = "ARCHIVED"

    var displayName: String {
        switch self {
        case .active: "Ativo"
        case .archived: "Arquivado"
        }
    }
}

struct CreditCardRequest: Encodable, Equatable, Sendable {
    let name: String
    let lastFour: String?
    let brand: CreditCardBrand?
    let closingDay: Int
    let dueDay: Int
    let creditLimit: FinancialMoney?
}

struct CreditCardPreview: Decodable, Equatable, Sendable {
    let name: String
    let lastFour: String?
    let brand: CreditCardBrand?
    let closingDay: Int
    let dueDay: Int
    let creditLimit: FinancialMoney?

    init(
        name: String,
        lastFour: String? = nil,
        brand: CreditCardBrand? = nil,
        closingDay: Int,
        dueDay: Int,
        creditLimit: FinancialMoney? = nil
    ) {
        self.name = name
        self.lastFour = lastFour
        self.brand = brand
        self.closingDay = closingDay
        self.dueDay = dueDay
        self.creditLimit = creditLimit
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decode(String.self, forKey: .name)
        lastFour = try container.decodeIfPresent(String.self, forKey: .lastFour)
        brand = try container.decodeIfPresent(CreditCardBrand.self, forKey: .brand)
        closingDay = try container.decode(Int.self, forKey: .closingDay)
        dueDay = try container.decode(Int.self, forKey: .dueDay)
        creditLimit = try container.decodeIfPresent(FinancialMoney.self, forKey: .creditLimit)
        guard Self.isValid(
            name: name,
            lastFour: lastFour,
            closingDay: closingDay,
            dueDay: dueDay,
            creditLimit: creditLimit
        ) else {
            throw DecodingError.dataCorruptedError(
                forKey: .name,
                in: container,
                debugDescription: "Invalid credit card representation"
            )
        }
    }

    fileprivate static func isValid(
        name: String,
        lastFour: String?,
        closingDay: Int,
        dueDay: Int,
        creditLimit: FinancialMoney?
    ) -> Bool {
        !name.isEmpty && name.count <= 200
            && (lastFour.map(isValidLastFour) ?? true)
            && (1...31).contains(closingDay) && (1...31).contains(dueDay)
            && (creditLimit.map({ $0.minor > 0 && $0.currency == .brl }) ?? true)
    }

    static func isValidLastFour(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        return bytes.count == 4 && bytes.allSatisfy { (48...57).contains($0) }
    }

    fileprivate enum CodingKeys: String, CodingKey {
        case name, lastFour, brand, closingDay, dueDay, creditLimit
    }
}

struct CreditCard: Decodable, Equatable, Identifiable, Sendable {
    let id: String
    let name: String
    let lastFour: String?
    let brand: CreditCardBrand?
    let closingDay: Int
    let dueDay: Int
    let creditLimit: FinancialMoney?
    let status: CreditCardStatus
    let createdAt: String
    let archivedAt: String?

    init(
        id: String,
        name: String,
        lastFour: String? = nil,
        brand: CreditCardBrand? = nil,
        closingDay: Int,
        dueDay: Int,
        creditLimit: FinancialMoney? = nil,
        status: CreditCardStatus = .active,
        createdAt: String = "2026-08-26T12:00:00Z",
        archivedAt: String? = nil
    ) {
        self.id = id
        self.name = name
        self.lastFour = lastFour
        self.brand = brand
        self.closingDay = closingDay
        self.dueDay = dueDay
        self.creditLimit = creditLimit
        self.status = status
        self.createdAt = createdAt
        self.archivedAt = archivedAt
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        name = try container.decode(String.self, forKey: .name)
        lastFour = try container.decodeIfPresent(String.self, forKey: .lastFour)
        brand = try container.decodeIfPresent(CreditCardBrand.self, forKey: .brand)
        closingDay = try container.decode(Int.self, forKey: .closingDay)
        dueDay = try container.decode(Int.self, forKey: .dueDay)
        creditLimit = try container.decodeIfPresent(FinancialMoney.self, forKey: .creditLimit)
        status = try container.decode(CreditCardStatus.self, forKey: .status)
        createdAt = try container.decode(String.self, forKey: .createdAt)

        guard Self.isValidID(id) else {
            throw DecodingError.dataCorruptedError(forKey: .id, in: container, debugDescription: "Invalid card ID")
        }
        guard CreditCardPreview.isValid(
            name: name,
            lastFour: lastFour,
            closingDay: closingDay,
            dueDay: dueDay,
            creditLimit: creditLimit
        ) else {
            throw DecodingError.dataCorruptedError(
                forKey: .name,
                in: container,
                debugDescription: "Invalid credit card representation"
            )
        }
        switch status {
        case .active:
            guard !container.contains(.archivedAt) else {
                throw DecodingError.dataCorruptedError(
                    forKey: .archivedAt,
                    in: container,
                    debugDescription: "ACTIVE card must omit archivedAt"
                )
            }
            archivedAt = nil
        case .archived:
            archivedAt = try container.decode(String.self, forKey: .archivedAt)
        }
    }

    static func isValidID(_ value: String) -> Bool {
        let bytes = Array(value.utf8)
        guard bytes.count == 37, bytes.starts(with: Array("card_".utf8)) else { return false }
        return bytes.dropFirst(5).allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, lastFour, brand, closingDay, dueDay, creditLimit, status, createdAt, archivedAt
    }
}

struct CreditCardList: Decodable, Equatable, Sendable {
    let items: [CreditCard]

    init(items: [CreditCard]) {
        self.items = items
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        items = try container.decode([CreditCard].self, forKey: .items)
        guard Set(items.map(\.id)).count == items.count else {
            throw DecodingError.dataCorruptedError(forKey: .items, in: container, debugDescription: "Duplicate card ID")
        }
    }

    private enum CodingKeys: String, CodingKey { case items }
}

struct RecordedCreditCard: Equatable, Sendable {
    let card: CreditCard
    let replayed: Bool
}
