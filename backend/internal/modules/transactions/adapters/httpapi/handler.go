package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

const maxRequestBodyBytes = 16 * 1024

type Handler struct {
	ownerID string
	preview application.PreviewExpense
	record  *application.RecordExpense
	list    *application.ListExpensesByMonth
}

// New composes the thin HTTP adapter with server-derived ownership and the
// application use cases it invokes.
func New(
	ownerID string,
	preview application.PreviewExpense,
	record *application.RecordExpense,
	list *application.ListExpensesByMonth,
) *Handler {
	return &Handler{ownerID: ownerID, preview: preview, record: record, list: list}
}

// Register adds only the Stage 2B financial routes to the process mux.
func (handler *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/transactions/preview", handler.previewExpense)
	mux.HandleFunc("/v1/transactions/preview", handler.methodNotAllowed)
	mux.HandleFunc("POST /v1/transactions", handler.recordExpense)
	mux.HandleFunc("GET /v1/transactions", handler.listExpenses)
	mux.HandleFunc("/v1/transactions", handler.methodNotAllowed)
}

type transactionRequest struct {
	Type          string        `json:"type"`
	Description   string        `json:"description"`
	Amount        amountRequest `json:"amount"`
	PaymentMethod string        `json:"paymentMethod"`
	OccurredAt    time.Time     `json:"occurredAt"`
}

type amountRequest struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type expenseResponse struct {
	ID                string         `json:"id"`
	Type              string         `json:"type"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	PaymentMethod     string         `json:"paymentMethod"`
	OccurredAt        time.Time      `json:"occurredAt"`
	FinancialTimezone string         `json:"financialTimezone"`
	Origin            string         `json:"origin"`
	Status            string         `json:"status"`
	Version           uint64         `json:"version"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type previewResponse struct {
	Type              string         `json:"type"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	PaymentMethod     string         `json:"paymentMethod"`
	OccurredAt        time.Time      `json:"occurredAt"`
	FinancialTimezone string         `json:"financialTimezone"`
	Origin            string         `json:"origin"`
}

type amountResponse struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type monthResponse struct {
	Month string            `json:"month"`
	Items []expenseResponse `json:"items"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (handler *Handler) previewExpense(response http.ResponseWriter, request *http.Request) {
	input, ok := handler.decodeExpense(response, request)
	if !ok {
		return
	}
	result, err := handler.preview.Execute(request.Context(), input)
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	details := result.Details
	writeJSON(response, http.StatusOK, previewResponse{
		Type:              string(domain.TransactionTypeExpense),
		Description:       details.Description,
		Amount:            newAmountResponse(details.Amount),
		PaymentMethod:     string(details.PaymentMethod),
		OccurredAt:        details.OccurredAt,
		FinancialTimezone: details.FinancialTimezone,
		Origin:            string(details.Origin),
	})
}

func (handler *Handler) recordExpense(response http.ResponseWriter, request *http.Request) {
	keyValues := request.Header.Values("Idempotency-Key")
	if len(keyValues) == 0 || (len(keyValues) == 1 && keyValues[0] == "") {
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
		return
	}
	if len(keyValues) != 1 {
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
		return
	}

	input, ok := handler.decodeExpense(response, request)
	if !ok {
		return
	}
	result, err := handler.record.Execute(request.Context(), application.RecordExpenseInput{
		Expense:        input,
		IdempotencyKey: keyValues[0],
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(response, http.StatusCreated, newExpenseResponse(result.Expense))
}

func (handler *Handler) listExpenses(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		handler.methodNotAllowed(response, request)
		return
	}

	query := request.URL.Query()
	months, exists := query["month"]
	if !exists || len(query) != 1 || len(months) != 1 {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "request is invalid")
		return
	}
	result, err := handler.list.Execute(request.Context(), handler.ownerID, months[0])
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	items := make([]expenseResponse, 0, len(result.Expenses))
	for _, expense := range result.Expenses {
		items = append(items, newExpenseResponse(expense))
	}
	writeJSON(response, http.StatusOK, monthResponse{Month: result.Month, Items: items})
}

func (handler *Handler) decodeExpense(
	response http.ResponseWriter,
	request *http.Request,
) (application.CreateExpenseInput, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "request is invalid")
		return application.CreateExpenseInput{}, false
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var body transactionRequest
	if err := decoder.Decode(&body); err != nil {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "request is invalid")
		return application.CreateExpenseInput{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "request is invalid")
		return application.CreateExpenseInput{}, false
	}
	if body.Type != string(domain.TransactionTypeExpense) {
		writeError(response, http.StatusBadRequest, "UNSUPPORTED_TRANSACTION_TYPE", "transaction type is not supported")
		return application.CreateExpenseInput{}, false
	}

	return application.CreateExpenseInput{
		UserID:            handler.ownerID,
		Description:       body.Description,
		AmountMinor:       body.Amount.Minor,
		Currency:          domain.Currency(body.Amount.Currency),
		PaymentMethod:     domain.PaymentMethod(body.PaymentMethod),
		OccurredAt:        body.OccurredAt,
		FinancialTimezone: application.FinancialTimezone,
		Origin:            domain.OriginIOS,
	}, true
}

func (handler *Handler) writeApplicationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrIdempotencyKeyRequired):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	case errors.Is(err, application.ErrIdempotencyKeyInvalid):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
	case errors.Is(err, application.ErrIdempotencyConflict):
		writeError(response, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was reused with a different request")
	case errors.Is(err, application.ErrInvalidMonth),
		errors.Is(err, domain.ErrUnsupportedCurrency),
		errors.Is(err, domain.ErrInvalidUserID),
		errors.Is(err, domain.ErrInvalidDescription),
		errors.Is(err, domain.ErrInvalidExpenseAmount),
		errors.Is(err, domain.ErrInvalidPaymentMethod),
		errors.Is(err, domain.ErrInvalidOccurredAt),
		errors.Is(err, domain.ErrInvalidFinancialTimezone),
		errors.Is(err, domain.ErrInvalidOrigin):
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "request is invalid")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func (handler *Handler) methodNotAllowed(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
}

func newExpenseResponse(expense domain.Expense) expenseResponse {
	return expenseResponse{
		ID:                expense.ID(),
		Type:              string(expense.Type()),
		Description:       expense.Description(),
		Amount:            newAmountResponse(expense.Amount()),
		PaymentMethod:     string(expense.PaymentMethod()),
		OccurredAt:        expense.OccurredAt(),
		FinancialTimezone: expense.FinancialTimezone(),
		Origin:            string(expense.Origin()),
		Status:            string(expense.Status()),
		Version:           expense.Version(),
		CreatedAt:         expense.CreatedAt(),
		UpdatedAt:         expense.UpdatedAt(),
	}
}

func newAmountResponse(amount domain.Money) amountResponse {
	return amountResponse{Minor: amount.MinorUnits(), Currency: string(amount.Currency())}
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, errorEnvelope{Error: errorBody{Code: code, Message: message}})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
