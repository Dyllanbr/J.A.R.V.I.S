package httpapi

import (
	"bytes"
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
	ownerID        string
	previewExpense application.PreviewExpense
	previewIncome  application.PreviewIncome
	recordExpense  *application.RecordExpense
	recordIncome   *application.RecordIncome
	list           *application.ListTransactionsByMonth
	listCategories *application.ListCategories
}

// New composes the thin HTTP adapter with server-derived ownership and the
// explicit Expense/Income use cases it dispatches to.
func New(
	ownerID string,
	previewExpense application.PreviewExpense,
	previewIncome application.PreviewIncome,
	recordExpense *application.RecordExpense,
	recordIncome *application.RecordIncome,
	list *application.ListTransactionsByMonth,
	listCategories *application.ListCategories,
) *Handler {
	return &Handler{
		ownerID:        ownerID,
		previewExpense: previewExpense,
		previewIncome:  previewIncome,
		recordExpense:  recordExpense,
		recordIncome:   recordIncome,
		list:           list,
		listCategories: listCategories,
	}
}

// Register adds the financial transaction routes to the process mux.
func (handler *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/transactions/preview", handler.previewTransaction)
	mux.HandleFunc("/v1/transactions/preview", handler.methodNotAllowed)
	mux.HandleFunc("POST /v1/transactions", handler.recordTransaction)
	mux.HandleFunc("GET /v1/transactions", handler.listTransactions)
	mux.HandleFunc("/v1/transactions", handler.methodNotAllowed)
	mux.HandleFunc("GET /v1/categories", handler.listSystemCategories)
	mux.HandleFunc("/v1/categories", handler.methodNotAllowed)
}

type amountRequest struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type optionalCategoryID struct {
	value *domain.CategoryID
}

func (field *optionalCategoryID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return domain.ErrInvalidCategoryID
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	categoryID, err := domain.NewCategoryID(value)
	if err != nil {
		return err
	}
	field.value = &categoryID
	return nil
}

func (field optionalCategoryID) domainValue() *domain.CategoryID {
	return field.value
}

type expenseRequest struct {
	Type          string             `json:"type"`
	Description   string             `json:"description"`
	Amount        amountRequest      `json:"amount"`
	PaymentMethod string             `json:"paymentMethod"`
	CategoryID    optionalCategoryID `json:"categoryId"`
	OccurredAt    time.Time          `json:"occurredAt"`
}

type incomeRequest struct {
	Type        string             `json:"type"`
	Description string             `json:"description"`
	Amount      amountRequest      `json:"amount"`
	CategoryID  optionalCategoryID `json:"categoryId"`
	OccurredAt  time.Time          `json:"occurredAt"`
}

type decodedTransaction struct {
	typeValue domain.TransactionType
	expense   application.CreateExpenseInput
	income    application.CreateIncomeInput
}

type amountResponse struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

type expensePreviewResponse struct {
	Type              string         `json:"type"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	PaymentMethod     string         `json:"paymentMethod"`
	CategoryID        *string        `json:"categoryId,omitempty"`
	OccurredAt        time.Time      `json:"occurredAt"`
	FinancialTimezone string         `json:"financialTimezone"`
	Origin            string         `json:"origin"`
}

type incomePreviewResponse struct {
	Type              string         `json:"type"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	CategoryID        *string        `json:"categoryId,omitempty"`
	OccurredAt        time.Time      `json:"occurredAt"`
	FinancialTimezone string         `json:"financialTimezone"`
	Origin            string         `json:"origin"`
}

type expenseResponse struct {
	ID                string         `json:"id"`
	Type              string         `json:"type"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	PaymentMethod     string         `json:"paymentMethod"`
	CategoryID        *string        `json:"categoryId,omitempty"`
	OccurredAt        time.Time      `json:"occurredAt"`
	FinancialTimezone string         `json:"financialTimezone"`
	Origin            string         `json:"origin"`
	Status            string         `json:"status"`
	Version           uint64         `json:"version"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type incomeResponse struct {
	ID                string         `json:"id"`
	Type              string         `json:"type"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	CategoryID        *string        `json:"categoryId,omitempty"`
	OccurredAt        time.Time      `json:"occurredAt"`
	FinancialTimezone string         `json:"financialTimezone"`
	Origin            string         `json:"origin"`
	Status            string         `json:"status"`
	Version           uint64         `json:"version"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

type monthResponse struct {
	Month string `json:"month"`
	Items []any  `json:"items"`
}

type categoryResponse struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"displayName"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (handler *Handler) previewTransaction(response http.ResponseWriter, request *http.Request) {
	input, ok := handler.decodeTransaction(response, request)
	if !ok {
		return
	}

	switch input.typeValue {
	case domain.TransactionTypeExpense:
		result, err := handler.previewExpense.Execute(request.Context(), input.expense)
		if err != nil {
			handler.writeApplicationError(response, err)
			return
		}
		details := result.Details
		writeJSON(response, http.StatusOK, expensePreviewResponse{
			Type:              string(domain.TransactionTypeExpense),
			Description:       details.Description,
			Amount:            newAmountResponse(details.Amount),
			PaymentMethod:     string(details.PaymentMethod),
			CategoryID:        categoryIDResponse(details.CategoryID),
			OccurredAt:        details.OccurredAt,
			FinancialTimezone: details.FinancialTimezone,
			Origin:            string(details.Origin),
		})
	case domain.TransactionTypeIncome:
		result, err := handler.previewIncome.Execute(request.Context(), input.income)
		if err != nil {
			handler.writeApplicationError(response, err)
			return
		}
		details := result.Details
		writeJSON(response, http.StatusOK, incomePreviewResponse{
			Type:              string(domain.TransactionTypeIncome),
			Description:       details.Description,
			Amount:            newAmountResponse(details.Amount),
			CategoryID:        categoryIDResponse(details.CategoryID),
			OccurredAt:        details.OccurredAt,
			FinancialTimezone: details.FinancialTimezone,
			Origin:            string(details.Origin),
		})
	}
}

func (handler *Handler) recordTransaction(response http.ResponseWriter, request *http.Request) {
	key, ok := decodeIdempotencyKey(response, request)
	if !ok {
		return
	}
	input, ok := handler.decodeTransaction(response, request)
	if !ok {
		return
	}

	switch input.typeValue {
	case domain.TransactionTypeExpense:
		result, err := handler.recordExpense.Execute(request.Context(), application.RecordExpenseInput{
			Expense: input.expense, IdempotencyKey: key,
		})
		if err != nil {
			handler.writeApplicationError(response, err)
			return
		}
		if result.Replayed {
			response.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(response, http.StatusCreated, newExpenseResponse(result.Expense))
	case domain.TransactionTypeIncome:
		result, err := handler.recordIncome.Execute(request.Context(), application.RecordIncomeInput{
			Income: input.income, IdempotencyKey: key,
		})
		if err != nil {
			handler.writeApplicationError(response, err)
			return
		}
		if result.Replayed {
			response.Header().Set("Idempotency-Replayed", "true")
		}
		writeJSON(response, http.StatusCreated, newIncomeResponse(result.Income))
	}
}

func (handler *Handler) listTransactions(response http.ResponseWriter, request *http.Request) {
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
	items := make([]any, 0, len(result.Items))
	for _, item := range result.Items {
		encoded, valid := newMonthlyTransactionResponse(item)
		if !valid {
			writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
			return
		}
		items = append(items, encoded)
	}
	writeJSON(response, http.StatusOK, monthResponse{Month: result.Month, Items: items})
}

func (handler *Handler) listSystemCategories(response http.ResponseWriter, request *http.Request) {
	result, err := handler.listCategories.Execute(request.Context())
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	items := make([]categoryResponse, 0, len(result.Items))
	for _, definition := range result.Items {
		items = append(items, categoryResponse{
			ID:          definition.ID().String(),
			Type:        string(definition.TransactionType()),
			DisplayName: definition.DisplayName(),
		})
	}
	writeJSON(response, http.StatusOK, items)
}

func (handler *Handler) decodeTransaction(
	response http.ResponseWriter,
	request *http.Request,
) (decodedTransaction, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return decodedTransaction{}, false
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		writeInvalidRequest(response)
		return decodedTransaction{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response)
		return decodedTransaction{}, false
	}

	var discriminator struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(raw, &discriminator); err != nil || discriminator.Type == nil {
		writeInvalidRequest(response)
		return decodedTransaction{}, false
	}

	switch domain.TransactionType(*discriminator.Type) {
	case domain.TransactionTypeExpense:
		var body expenseRequest
		if !decodeStrict(raw, &body) || body.Type != string(domain.TransactionTypeExpense) {
			writeInvalidRequest(response)
			return decodedTransaction{}, false
		}
		return decodedTransaction{
			typeValue: domain.TransactionTypeExpense,
			expense: application.CreateExpenseInput{
				UserID:            handler.ownerID,
				Description:       body.Description,
				AmountMinor:       body.Amount.Minor,
				Currency:          domain.Currency(body.Amount.Currency),
				PaymentMethod:     domain.PaymentMethod(body.PaymentMethod),
				CategoryID:        body.CategoryID.domainValue(),
				OccurredAt:        body.OccurredAt,
				FinancialTimezone: application.FinancialTimezone,
				Origin:            domain.OriginIOS,
			},
		}, true
	case domain.TransactionTypeIncome:
		var body incomeRequest
		if !decodeStrict(raw, &body) || body.Type != string(domain.TransactionTypeIncome) {
			writeInvalidRequest(response)
			return decodedTransaction{}, false
		}
		return decodedTransaction{
			typeValue: domain.TransactionTypeIncome,
			income: application.CreateIncomeInput{
				UserID:            handler.ownerID,
				Description:       body.Description,
				AmountMinor:       body.Amount.Minor,
				Currency:          domain.Currency(body.Amount.Currency),
				CategoryID:        body.CategoryID.domainValue(),
				OccurredAt:        body.OccurredAt,
				FinancialTimezone: application.FinancialTimezone,
				Origin:            domain.OriginIOS,
			},
		}, true
	default:
		writeError(response, http.StatusBadRequest, "UNSUPPORTED_TRANSACTION_TYPE", "transaction type is not supported")
		return decodedTransaction{}, false
	}
}

func decodeStrict(raw []byte, target any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func decodeIdempotencyKey(response http.ResponseWriter, request *http.Request) (string, bool) {
	values := request.Header.Values("Idempotency-Key")
	if len(values) == 0 || (len(values) == 1 && values[0] == "") {
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
		return "", false
	}
	if len(values) != 1 {
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
		return "", false
	}
	return values[0], true
}

func (handler *Handler) writeApplicationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrIdempotencyKeyRequired),
		errors.Is(err, application.ErrIncomeIdempotencyKeyRequired):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	case errors.Is(err, application.ErrIdempotencyKeyInvalid),
		errors.Is(err, application.ErrIncomeIdempotencyKeyInvalid):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
	case errors.Is(err, application.ErrIdempotencyConflict),
		errors.Is(err, application.ErrIncomeIdempotencyConflict):
		writeError(response, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was reused with a different request")
	case errors.Is(err, application.ErrInvalidMonth),
		errors.Is(err, domain.ErrUnsupportedCurrency),
		errors.Is(err, domain.ErrInvalidUserID),
		errors.Is(err, domain.ErrInvalidDescription),
		errors.Is(err, domain.ErrInvalidExpenseAmount),
		errors.Is(err, domain.ErrInvalidPaymentMethod),
		errors.Is(err, domain.ErrInvalidOccurredAt),
		errors.Is(err, domain.ErrInvalidFinancialTimezone),
		errors.Is(err, domain.ErrInvalidOrigin),
		errors.Is(err, domain.ErrInvalidIncomeUserID),
		errors.Is(err, domain.ErrInvalidIncomeDescription),
		errors.Is(err, domain.ErrInvalidIncomeAmount),
		errors.Is(err, domain.ErrInvalidIncomeOccurredAt),
		errors.Is(err, domain.ErrInvalidIncomeFinancialTimezone),
		errors.Is(err, domain.ErrInvalidIncomeOrigin),
		errors.Is(err, domain.ErrInvalidCategoryID),
		errors.Is(err, application.ErrCategoryNotFound),
		errors.Is(err, application.ErrCategoryNotApplicable):
		writeInvalidRequest(response)
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func (handler *Handler) methodNotAllowed(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
}

func newExpenseResponse(expense domain.Expense) expenseResponse {
	return expenseResponse{
		ID: expense.ID(), Type: string(expense.Type()), Description: expense.Description(),
		Amount: newAmountResponse(expense.Amount()), PaymentMethod: string(expense.PaymentMethod()),
		CategoryID: expenseCategoryIDResponse(expense),
		OccurredAt: expense.OccurredAt(), FinancialTimezone: expense.FinancialTimezone(),
		Origin: string(expense.Origin()), Status: string(expense.Status()), Version: expense.Version(),
		CreatedAt: expense.CreatedAt(), UpdatedAt: expense.UpdatedAt(),
	}
}

func newIncomeResponse(income domain.Income) incomeResponse {
	return incomeResponse{
		ID: income.ID(), Type: string(income.Type()), Description: income.Description(),
		Amount: newAmountResponse(income.Amount()), CategoryID: incomeCategoryIDResponse(income), OccurredAt: income.OccurredAt(),
		FinancialTimezone: income.FinancialTimezone(), Origin: string(income.Origin()),
		Status: string(income.Status()), Version: income.Version(),
		CreatedAt: income.CreatedAt(), UpdatedAt: income.UpdatedAt(),
	}
}

func newMonthlyTransactionResponse(item application.MonthlyTransaction) (any, bool) {
	commonIncome := incomeResponse{
		ID: item.ID, Type: string(item.Type), Description: item.Description,
		Amount: newAmountResponse(item.Amount), CategoryID: categoryIDResponse(item.CategoryID), OccurredAt: item.OccurredAt,
		FinancialTimezone: item.FinancialTimezone, Origin: string(item.Origin),
		Status: item.Status, Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
	switch item.Type {
	case domain.TransactionTypeExpense:
		if item.PaymentMethod == nil {
			return nil, false
		}
		return expenseResponse{
			ID: commonIncome.ID, Type: commonIncome.Type, Description: commonIncome.Description,
			Amount: commonIncome.Amount, PaymentMethod: string(*item.PaymentMethod),
			CategoryID: commonIncome.CategoryID,
			OccurredAt: commonIncome.OccurredAt, FinancialTimezone: commonIncome.FinancialTimezone,
			Origin: commonIncome.Origin, Status: commonIncome.Status, Version: commonIncome.Version,
			CreatedAt: commonIncome.CreatedAt, UpdatedAt: commonIncome.UpdatedAt,
		}, true
	case domain.TransactionTypeIncome:
		if item.PaymentMethod != nil {
			return nil, false
		}
		return commonIncome, true
	default:
		return nil, false
	}
}

func newAmountResponse(amount domain.Money) amountResponse {
	return amountResponse{Minor: amount.MinorUnits(), Currency: string(amount.Currency())}
}

func expenseCategoryIDResponse(expense domain.Expense) *string {
	categoryID, ok := expense.CategoryID()
	if !ok {
		return nil
	}
	return categoryIDResponse(&categoryID)
}

func incomeCategoryIDResponse(income domain.Income) *string {
	categoryID, ok := income.CategoryID()
	if !ok {
		return nil
	}
	return categoryIDResponse(&categoryID)
}

func categoryIDResponse(categoryID *domain.CategoryID) *string {
	if categoryID == nil {
		return nil
	}
	value := categoryID.String()
	return &value
}

func writeInvalidRequest(response http.ResponseWriter) {
	writeError(response, http.StatusBadRequest, "INVALID_REQUEST", "request is invalid")
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
