import Foundation
import XCTest
@testable import JARVIS

@MainActor
final class CardPurchaseViewModelTests: XCTestCase {
    func testPreviewFreezesServerPreviewWithoutCreatingOrGeneratingAnID() async throws {
        let spy = FinancialAPISpy()
        spy.creditCardListResult = .success(CreditCardList(items: [syntheticCreditCard()]))
        spy.cardPurchasePreviewResult = .success(try preview())
        let model = CardPurchaseViewModel(api: spy, now: Date(timeIntervalSince1970: 1_700_000_000))

        model.begin()
        await model.loadCards()
        model.description = "Compra"
        model.amountText = "120,00"
        model.installmentCountText = "2"
        await model.review()

        guard case let .reviewing(reviewed) = model.state else {
            return XCTFail("Expected review state")
        }
        XCTAssertEqual(reviewed.preview, try preview())
        XCTAssertEqual(spy.cardPurchasePreviewRequests.count, 1)
        XCTAssertTrue(spy.cardPurchaseCreateRequests.isEmpty)
        XCTAssertTrue(spy.creditCardListRequestCount >= 1)
    }

    func testCountOneIsRejectedBeforePreview() async {
        let spy = FinancialAPISpy()
        spy.creditCardListResult = .success(CreditCardList(items: [syntheticCreditCard()]))
        let model = CardPurchaseViewModel(api: spy)
        model.begin()
        await model.loadCards()
        model.description = "Compra"
        model.amountText = "120,00"
        model.installmentCountText = "1"

        await model.review()

        XCTAssertEqual(model.state, .editing)
        XCTAssertTrue(spy.cardPurchasePreviewRequests.isEmpty)
        XCTAssertTrue(spy.cardPurchaseCreateRequests.isEmpty)
    }

    func testConfirmRetryReusesTheSameIdempotencyKey() async throws {
        let spy = FinancialAPISpy()
        spy.creditCardListResult = .success(CreditCardList(items: [syntheticCreditCard()]))
        spy.cardPurchasePreviewResult = .success(try preview())
        let purchase = try cardPurchase()
        spy.cardPurchaseCreateResults = [
            .failure(FinancialAPIError.serviceUnavailable),
            .success(RecordedCardPurchase(purchase: purchase, replayed: true))
        ]
        var keyCounter = 0
        let model = CardPurchaseViewModel(api: spy, makeIdempotencyKey: {
            keyCounter += 1
            return "purchase-key-\(keyCounter)"
        })
        model.begin()
        await model.loadCards()
        model.description = "Compra"
        model.amountText = "120,00"
        await model.review()
        await model.confirm()

        XCTAssertTrue(model.isRetryable)
        await model.confirm()

        guard case let .success(result) = model.state else {
            return XCTFail("Expected successful retry")
        }
        XCTAssertEqual(result, purchase)
        XCTAssertEqual(keyCounter, 1)
        XCTAssertEqual(spy.cardPurchaseCreateRequests.map(\.key), ["purchase-key-1", "purchase-key-1"])
    }

    func testOneTimePreviewOmitsInstallmentCount() async throws {
        let spy = FinancialAPISpy()
        spy.creditCardListResult = .success(CreditCardList(items: [syntheticCreditCard()]))
        spy.cardPurchasePreviewResult = .success(try oneTimePreview())
        let model = CardPurchaseViewModel(api: spy)
        model.begin()
        await model.loadCards()
        model.description = "Compra à vista"
        model.amountText = "120,00"

        await model.review()

        XCTAssertNil(spy.cardPurchasePreviewRequests.first?.installmentCount)
        guard case let .reviewing(reviewed) = model.state else { return XCTFail("Expected review") }
        XCTAssertEqual(reviewed.preview.purchaseMode, .oneTime)
    }

    private func preview() throws -> CardPurchasePreview {
        try JSONDecoder().decode(CardPurchasePreview.self, from: Data(Self.previewJSON.utf8))
    }

    private func oneTimePreview() throws -> CardPurchasePreview {
        try JSONDecoder().decode(CardPurchasePreview.self, from: Data(Self.oneTimePreviewJSON.utf8))
    }

    private func cardPurchase() throws -> CardPurchase {
        try JSONDecoder().decode(CardPurchase.self, from: Data(Self.purchaseJSON.utf8))
    }

    private static let cardID = "card_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    private static let planID = "ipl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

    private static let previewJSON = """
    {"description":"Compra","amount":{"minor":12000,"currency":"BRL"},"occurredAt":"2026-08-14T15:00:00Z","creditCardId":"\(cardID)","purchaseMode":"INSTALLMENT","statementClosingOn":"2026-08-05","statementDueOn":"2026-08-12","installmentSummary":{"installmentCount":2,"firstDueDate":"2026-08-12","lastDueDate":"2026-09-12","dueDayAnchor":12,"regularInstallmentAmount":{"minor":6000,"currency":"BRL"},"lastInstallmentAmount":{"minor":6000,"currency":"BRL"}}}
    """

    private static let oneTimePreviewJSON = """
    {"description":"Compra à vista","amount":{"minor":12000,"currency":"BRL"},"occurredAt":"2026-08-14T15:00:00Z","creditCardId":"\(cardID)","purchaseMode":"ONE_TIME","statementClosingOn":"2026-08-05","statementDueOn":"2026-08-12"}
    """

    private static let purchaseJSON = """
    {"expense":{"id":"exp_card_purchase_001","type":"EXPENSE","description":"Compra","amount":{"minor":12000,"currency":"BRL"},"paymentMethod":"CREDIT","creditCardId":"\(cardID)","statementDueOn":"2026-08-12","occurredAt":"2026-08-14T15:00:00Z","financialTimezone":"America/Sao_Paulo","origin":"IOS","status":"RECORDED","version":1,"createdAt":"2026-08-14T15:00:00Z","updatedAt":"2026-08-14T15:00:00Z"},"installmentPlan":{"id":"\(planID)","creditCardId":"\(cardID)","expenseId":"exp_card_purchase_001","totalAmount":{"minor":12000,"currency":"BRL"},"installmentCount":2,"firstDueDate":"2026-08-12","dueDayAnchor":12,"status":"ACTIVE","createdAt":"2026-08-14T15:00:00Z","schedule":[{"number":1,"totalCount":2,"dueDate":"2026-08-12","amount":{"minor":6000,"currency":"BRL"}},{"number":2,"totalCount":2,"dueDate":"2026-09-12","amount":{"minor":6000,"currency":"BRL"}}]},"purchaseMode":"INSTALLMENT"}
    """
}
