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

// CreditCardHandler exposes only organizational card metadata. Ownership is
// server-derived and no endpoint can execute purchases, payments or transfers.
type CreditCardHandler struct {
	ownerID string
	preview application.PreviewCreditCard
	record  *application.RecordCreditCard
	list    *application.ListCreditCards
	get     *application.GetCreditCard
	archive *application.ArchiveCreditCard
}

func NewCreditCard(
	ownerID string,
	preview application.PreviewCreditCard,
	record *application.RecordCreditCard,
	list *application.ListCreditCards,
	get *application.GetCreditCard,
	archive *application.ArchiveCreditCard,
) *CreditCardHandler {
	return &CreditCardHandler{
		ownerID: ownerID,
		preview: preview,
		record:  record,
		list:    list,
		get:     get,
		archive: archive,
	}
}

func (handler *CreditCardHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/cards/preview", handler.previewCreditCard)
	mux.HandleFunc("POST /v1/cards", handler.recordCreditCard)
	mux.HandleFunc("GET /v1/cards", handler.listCreditCards)
	handler.registerRejectedMethods(mux, "/v1/cards", http.MethodPost, http.MethodGet)
	mux.HandleFunc("POST /v1/cards/{cardId}/archive", handler.archiveCreditCard)
	handler.registerRejectedMethods(mux, "/v1/cards/{cardId}/archive", http.MethodPost)
	mux.HandleFunc("GET /v1/cards/{cardId}", handler.getCreditCard)
	handler.registerRejectedMethods(mux, "/v1/cards/{cardId}", http.MethodGet)
}

func (handler *CreditCardHandler) registerRejectedMethods(mux *http.ServeMux, path string, allowed ...string) {
	allowedMethods := make(map[string]struct{}, len(allowed))
	for _, method := range allowed {
		allowedMethods[method] = struct{}{}
	}
	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
	} {
		if _, ok := allowedMethods[method]; ok {
			continue
		}
		mux.HandleFunc(method+" "+path, handler.methodNotAllowed)
	}
}

type creditCardRequest struct {
	Name        *string         `json:"name"`
	LastFour    json.RawMessage `json:"lastFour"`
	Brand       json.RawMessage `json:"brand"`
	ClosingDay  *int            `json:"closingDay"`
	DueDay      *int            `json:"dueDay"`
	CreditLimit json.RawMessage `json:"creditLimit"`
}

type creditCardPreviewResponse struct {
	Name        string          `json:"name"`
	LastFour    *string         `json:"lastFour,omitempty"`
	Brand       *string         `json:"brand,omitempty"`
	ClosingDay  int             `json:"closingDay"`
	DueDay      int             `json:"dueDay"`
	CreditLimit *amountResponse `json:"creditLimit,omitempty"`
}

type creditCardResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	LastFour    *string         `json:"lastFour,omitempty"`
	Brand       *string         `json:"brand,omitempty"`
	ClosingDay  int             `json:"closingDay"`
	DueDay      int             `json:"dueDay"`
	CreditLimit *amountResponse `json:"creditLimit,omitempty"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"createdAt"`
	ArchivedAt  *time.Time      `json:"archivedAt,omitempty"`
}

type creditCardListResponse struct {
	Items []creditCardResponse `json:"items"`
}

func (handler *CreditCardHandler) previewCreditCard(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	input, ok := handler.decodeCreditCard(response, request)
	if !ok {
		return
	}
	result, err := handler.preview.Execute(request.Context(), input)
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newCreditCardPreviewResponse(result.Preview))
}

func (handler *CreditCardHandler) recordCreditCard(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	key, ok := decodeIdempotencyKey(response, request)
	if !ok {
		return
	}
	input, ok := handler.decodeCreditCard(response, request)
	if !ok {
		return
	}
	result, err := handler.record.Execute(request.Context(), application.RecordCreditCardInput{
		CreditCard: input, IdempotencyKey: key,
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(response, http.StatusCreated, newCreditCardResponse(result.CreditCard))
}

func (handler *CreditCardHandler) listCreditCards(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.list.Execute(request.Context(), handler.ownerID)
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	items := make([]creditCardResponse, 0, len(result.Items))
	for _, card := range result.Items {
		items = append(items, newCreditCardResponse(card))
	}
	writeJSON(response, http.StatusOK, creditCardListResponse{Items: items})
}

func (handler *CreditCardHandler) getCreditCard(response http.ResponseWriter, request *http.Request) {
	if request.PathValue("cardId") == "preview" {
		handler.methodNotAllowed(response, request)
		return
	}
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.get.Execute(request.Context(), application.GetCreditCardInput{
		UserID: handler.ownerID, CreditCardID: request.PathValue("cardId"),
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newCreditCardResponse(result.CreditCard))
}

func (handler *CreditCardHandler) archiveCreditCard(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	key, ok := decodeIdempotencyKey(response, request)
	if !ok {
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.archive.Execute(request.Context(), application.ArchiveCreditCardInput{
		UserID: handler.ownerID, CreditCardID: request.PathValue("cardId"), IdempotencyKey: key,
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(response, http.StatusOK, newCreditCardResponse(result.CreditCard))
}

func (handler *CreditCardHandler) decodeCreditCard(
	response http.ResponseWriter,
	request *http.Request,
) (application.CreditCardInput, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return application.CreditCardInput{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(raw) {
		writeInvalidRequest(response)
		return application.CreditCardInput{}, false
	}
	var body creditCardRequest
	if !decodeStrict(raw, &body) || body.Name == nil || body.ClosingDay == nil || body.DueDay == nil {
		writeInvalidRequest(response)
		return application.CreditCardInput{}, false
	}

	lastFour, ok := decodeOptionalCreditCardString(body.LastFour)
	if !ok {
		writeInvalidRequest(response)
		return application.CreditCardInput{}, false
	}
	brandValue, ok := decodeOptionalCreditCardString(body.Brand)
	if !ok {
		writeInvalidRequest(response)
		return application.CreditCardInput{}, false
	}
	creditLimit, ok := decodeOptionalCreditCardLimit(body.CreditLimit)
	if !ok {
		writeInvalidRequest(response)
		return application.CreditCardInput{}, false
	}

	input := application.CreditCardInput{
		UserID: handler.ownerID, Name: *body.Name, LastFour: lastFour,
		ClosingDayAnchor: *body.ClosingDay, DueDayAnchor: *body.DueDay, CreditLimit: creditLimit,
	}
	if brandValue != nil {
		brand := domain.CreditCardBrand(*brandValue)
		input.Brand = &brand
	}
	return input, true
}

func decodeOptionalCreditCardString(raw json.RawMessage) (*string, bool) {
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
	return &value, true
}

func decodeOptionalCreditCardLimit(raw json.RawMessage) (*application.CreditCardLimitInput, bool) {
	if raw == nil {
		return nil, true
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var value amountRequest
	if !decodeStrict(raw, &value) {
		return nil, false
	}
	return &application.CreditCardLimitInput{
		MinorUnits: value.Minor, Currency: domain.Currency(value.Currency),
	}, true
}

func (handler *CreditCardHandler) writeApplicationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrCreditCardIdempotencyKeyRequired):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	case errors.Is(err, application.ErrCreditCardIdempotencyKeyInvalid):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
	case errors.Is(err, application.ErrCreditCardIdempotencyConflict):
		writeError(response, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was reused with a different request")
	case errors.Is(err, application.ErrCreditCardNotFound):
		writeError(response, http.StatusNotFound, "CREDIT_CARD_NOT_FOUND", "credit card was not found")
	case errors.Is(err, domain.ErrCreditCardAlreadyArchived):
		writeError(response, http.StatusConflict, "CREDIT_CARD_ALREADY_ARCHIVED", "credit card is already archived")
	case errors.Is(err, domain.ErrInvalidCreditCardID),
		errors.Is(err, domain.ErrInvalidCreditCardUserID),
		errors.Is(err, domain.ErrInvalidCreditCardName),
		errors.Is(err, domain.ErrInvalidCreditCardLastFour),
		errors.Is(err, domain.ErrInvalidCreditCardBrand),
		errors.Is(err, domain.ErrInvalidDayOfMonthAnchor),
		errors.Is(err, domain.ErrInvalidCreditCardCreditLimit),
		errors.Is(err, domain.ErrUnsupportedCurrency):
		writeInvalidRequest(response)
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func (handler *CreditCardHandler) methodNotAllowed(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
}

func newCreditCardPreviewResponse(preview application.CreditCardPreview) creditCardPreviewResponse {
	result := creditCardPreviewResponse{
		Name: preview.Name, LastFour: preview.LastFour,
		ClosingDay: preview.ClosingDayAnchor, DueDay: preview.DueDayAnchor,
	}
	if preview.Brand != nil {
		brand := string(*preview.Brand)
		result.Brand = &brand
	}
	if preview.CreditLimit != nil {
		limit := newAmountResponse(*preview.CreditLimit)
		result.CreditLimit = &limit
	}
	return result
}

func newCreditCardResponse(card domain.CreditCard) creditCardResponse {
	result := creditCardResponse{
		ID: card.ID(), Name: card.Name().String(),
		ClosingDay: card.ClosingDayAnchor().Day(), DueDay: card.DueDayAnchor().Day(),
		Status: string(card.Status()), CreatedAt: card.CreatedAt(),
	}
	if lastFour, ok := card.LastFour(); ok {
		value := lastFour.String()
		result.LastFour = &value
	}
	if brand, ok := card.Brand(); ok {
		value := string(brand)
		result.Brand = &value
	}
	if creditLimit, ok := card.CreditLimit(); ok {
		value := newAmountResponse(creditLimit)
		result.CreditLimit = &value
	}
	if archivedAt, ok := card.ArchivedAt(); ok {
		result.ArchivedAt = &archivedAt
	}
	return result
}
