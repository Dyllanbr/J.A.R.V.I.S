package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// CardPurchaseHandler exposes the explicit, confirmed card-purchase command.
// Ownership is always taken from the server context and never from JSON.
type CardPurchaseHandler struct {
	ownerID string
	preview *application.PreviewCardPurchase
	record  *application.RecordCardPurchase
}

func NewCardPurchase(ownerID string, preview *application.PreviewCardPurchase, record *application.RecordCardPurchase) *CardPurchaseHandler {
	return &CardPurchaseHandler{ownerID: ownerID, preview: preview, record: record}
}

func (handler *CardPurchaseHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/card-purchases/preview", handler.previewPurchase)
	mux.HandleFunc("POST /v1/card-purchases", handler.recordPurchase)
	handler.registerRejectedMethods(mux, "/v1/card-purchases/preview", http.MethodPost)
	handler.registerRejectedMethods(mux, "/v1/card-purchases", http.MethodPost)
}

func (handler *CardPurchaseHandler) registerRejectedMethods(mux *http.ServeMux, path string, allowed ...string) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, method := range allowed {
		allowedSet[method] = struct{}{}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace} {
		if _, ok := allowedSet[method]; ok {
			continue
		}
		mux.HandleFunc(method+" "+path, methodNotAllowed)
	}
}

type cardPurchaseRequest struct {
	Description      *string         `json:"description"`
	Amount           *amountRequest  `json:"amount"`
	OccurredAt       *time.Time      `json:"occurredAt"`
	CategoryID       json.RawMessage `json:"categoryId"`
	CreditCardID     *string         `json:"creditCardId"`
	InstallmentCount json.RawMessage `json:"installmentCount"`
}

type cardPurchasePreviewResponse struct {
	Description        string                  `json:"description"`
	Amount             amountResponse          `json:"amount"`
	OccurredAt         time.Time               `json:"occurredAt"`
	CategoryID         *string                 `json:"categoryId,omitempty"`
	CreditCardID       string                  `json:"creditCardId"`
	PurchaseMode       string                  `json:"purchaseMode"`
	StatementClosingOn string                  `json:"statementClosingOn"`
	StatementDueOn     string                  `json:"statementDueOn"`
	InstallmentSummary *cardInstallmentSummary `json:"installmentSummary,omitempty"`
}

type cardInstallmentSummary struct {
	InstallmentCount         int            `json:"installmentCount"`
	FirstDueDate             string         `json:"firstDueDate"`
	LastDueDate              string         `json:"lastDueDate"`
	DueDayAnchor             int            `json:"dueDayAnchor"`
	RegularInstallmentAmount amountResponse `json:"regularInstallmentAmount"`
	LastInstallmentAmount    amountResponse `json:"lastInstallmentAmount"`
}

type cardPurchaseResponse struct {
	Expense         cardPurchaseExpenseResponse `json:"expense"`
	InstallmentPlan *installmentPlanResponse    `json:"installmentPlan,omitempty"`
	PurchaseMode    string                      `json:"purchaseMode"`
}

type cardPurchaseExpenseResponse struct {
	ID                string         `json:"id"`
	Type              string         `json:"type"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	PaymentMethod     string         `json:"paymentMethod"`
	CategoryID        *string        `json:"categoryId,omitempty"`
	CreditCardID      string         `json:"creditCardId"`
	StatementDueOn    string         `json:"statementDueOn"`
	OccurredAt        time.Time      `json:"occurredAt"`
	FinancialTimezone string         `json:"financialTimezone"`
	Origin            string         `json:"origin"`
	Status            string         `json:"status"`
	Version           uint64         `json:"version"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

func (handler *CardPurchaseHandler) previewPurchase(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	input, ok := handler.decodeRequest(response, request)
	if !ok {
		return
	}
	result, err := handler.preview.Execute(request.Context(), input)
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newCardPurchasePreviewResponse(result.Preview))
}

func (handler *CardPurchaseHandler) recordPurchase(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	key, ok := decodeIdempotencyKey(response, request)
	if !ok {
		return
	}
	input, ok := handler.decodeRequest(response, request)
	if !ok {
		return
	}
	result, err := handler.record.Execute(request.Context(), application.RecordCardPurchaseInput{Purchase: input, IdempotencyKey: key})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	mode := application.CardPurchaseModeOneTime
	if result.InstallmentPlan != nil {
		mode = application.CardPurchaseModeInstallment
	}
	writeJSON(response, http.StatusCreated, cardPurchaseResponse{Expense: newCardPurchaseExpenseResponse(result.Expense), InstallmentPlan: newInstallmentPlanResponse(result.InstallmentPlan), PurchaseMode: string(mode)})
}

func (handler *CardPurchaseHandler) decodeRequest(response http.ResponseWriter, request *http.Request) (application.CardPurchaseInput, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return application.CardPurchaseInput{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(raw) {
		writeInvalidRequest(response)
		return application.CardPurchaseInput{}, false
	}
	var body cardPurchaseRequest
	if !decodeStrict(raw, &body) || body.Description == nil || body.Amount == nil || body.OccurredAt == nil || body.CreditCardID == nil {
		writeInvalidRequest(response)
		return application.CardPurchaseInput{}, false
	}
	categoryID, ok := decodeCardPurchaseCategory(body.CategoryID)
	if !ok {
		writeInvalidRequest(response)
		return application.CardPurchaseInput{}, false
	}
	count, ok := decodeCardPurchaseCount(body.InstallmentCount)
	if !ok {
		writeInvalidRequest(response)
		return application.CardPurchaseInput{}, false
	}
	return application.CardPurchaseInput{UserID: handler.ownerID, Description: *body.Description, AmountMinor: body.Amount.Minor, Currency: domain.Currency(body.Amount.Currency), OccurredAt: *body.OccurredAt, CategoryID: categoryID, CreditCardID: *body.CreditCardID, InstallmentCount: count, Origin: domain.OriginIOS}, true
}

func decodeCardPurchaseCategory(raw json.RawMessage) (*domain.CategoryID, bool) {
	if raw == nil {
		return nil, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	id, err := domain.NewCategoryID(value)
	if err != nil {
		return nil, false
	}
	return &id, true
}

func decodeCardPurchaseCount(raw json.RawMessage) (*int, bool) {
	if raw == nil {
		return nil, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	return &value, true
}

func newCardPurchasePreviewResponse(preview application.CardPurchasePreview) cardPurchasePreviewResponse {
	details := preview.Expense
	cardID := ""
	if details.CreditCardID != nil {
		cardID = *details.CreditCardID
	}
	dueOn := domain.CivilDate{}
	if details.StatementDueOn != nil {
		dueOn = *details.StatementDueOn
	}
	response := cardPurchasePreviewResponse{Description: details.Description, Amount: newAmountResponse(details.Amount), OccurredAt: details.OccurredAt, CategoryID: categoryIDResponse(details.CategoryID), CreditCardID: cardID, PurchaseMode: string(preview.Mode), StatementClosingOn: preview.Cycle.StatementClosingOn().String(), StatementDueOn: dueOn.String()}
	if len(preview.Installments) != 0 {
		first := preview.Installments[0]
		last := preview.Installments[len(preview.Installments)-1]
		response.InstallmentSummary = &cardInstallmentSummary{InstallmentCount: len(preview.Installments), FirstDueDate: first.DueDate().String(), LastDueDate: last.DueDate().String(), DueDayAnchor: preview.Cycle.StatementDueOn().Day(), RegularInstallmentAmount: newAmountResponse(first.Amount()), LastInstallmentAmount: newAmountResponse(last.Amount())}
	}
	return response
}

func newCardPurchaseExpenseResponse(expense domain.Expense) cardPurchaseExpenseResponse {
	cardID, _ := expense.CreditCardID()
	dueOn, _ := expense.StatementDueOn()
	return cardPurchaseExpenseResponse{ID: expense.ID(), Type: string(expense.Type()), Description: expense.Description(), Amount: newAmountResponse(expense.Amount()), PaymentMethod: string(expense.PaymentMethod()), CategoryID: expenseCategoryIDResponse(expense), CreditCardID: cardID, StatementDueOn: dueOn.String(), OccurredAt: expense.OccurredAt(), FinancialTimezone: expense.FinancialTimezone(), Origin: string(expense.Origin()), Status: string(expense.Status()), Version: expense.Version(), CreatedAt: expense.CreatedAt(), UpdatedAt: expense.UpdatedAt()}
}

func (handler *CardPurchaseHandler) writeApplicationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCreditCardID), errors.Is(err, domain.ErrInvalidCategoryID), errors.Is(err, domain.ErrInvalidDescription), errors.Is(err, domain.ErrInvalidExpenseAmount), errors.Is(err, domain.ErrUnsupportedCurrency), errors.Is(err, domain.ErrInvalidOccurredAt), errors.Is(err, domain.ErrInvalidOrigin), errors.Is(err, domain.ErrInvalidInstallmentCount), errors.Is(err, domain.ErrInvalidCreditCardUserID):
		writeInvalidRequest(response)
	case errors.Is(err, application.ErrCardPurchaseIdempotencyKeyRequired):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	case errors.Is(err, application.ErrCardPurchaseIdempotencyKeyInvalid):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
	case errors.Is(err, application.ErrCardPurchaseIdempotencyConflict):
		writeError(response, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was reused with a different request")
	case errors.Is(err, application.ErrCardPurchaseCreditCardNotFound):
		writeError(response, http.StatusNotFound, "CREDIT_CARD_NOT_FOUND", "credit card was not found")
	case errors.Is(err, application.ErrCardPurchaseCreditCardArchived):
		writeError(response, http.StatusConflict, "CREDIT_CARD_ARCHIVED", "credit card is archived")
	case errors.Is(err, application.ErrCardPurchaseCategoryNotApplicable), errors.Is(err, application.ErrCategoryNotFound):
		writeInvalidRequest(response)
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func methodNotAllowed(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
}
