package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// MonthlyBudgetHandler exposes the owner-scoped monthly budget command and
// query. The owner is composed by the server and is never accepted from the
// request.
type MonthlyBudgetHandler struct {
	ownerID string
	set     *application.SetMonthlyBudget
	get     *application.GetMonthlyBudget
}

func NewMonthlyBudget(ownerID string, set *application.SetMonthlyBudget, get *application.GetMonthlyBudget) *MonthlyBudgetHandler {
	return &MonthlyBudgetHandler{ownerID: ownerID, set: set, get: get}
}

func (handler *MonthlyBudgetHandler) Register(mux *http.ServeMux) {
	const path = "/v1/monthly-budgets/{month}"
	mux.HandleFunc("PUT "+path, handler.replaceMonthlyBudget)
	mux.HandleFunc("GET "+path, handler.getMonthlyBudget)
	handler.registerRejectedMethods(mux, path, http.MethodPut, http.MethodGet)
	// Keep incomplete and extra-segment paths inside the JSON error envelope.
	mux.HandleFunc("/v1/monthly-budgets/", handler.malformedMonthlyBudgetPath)
}

func (handler *MonthlyBudgetHandler) registerRejectedMethods(mux *http.ServeMux, path string, allowed ...string) {
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
		mux.HandleFunc(method+" "+path, methodNotAllowed)
	}
}

type monthlyBudgetRequest struct {
	Amount *amountRequest `json:"amount"`
}

type monthlyBudgetResponse struct {
	Month  string         `json:"month"`
	Amount amountResponse `json:"amount"`
}

func (handler *MonthlyBudgetHandler) malformedMonthlyBudgetPath(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPut {
		methodNotAllowed(response, request)
		return
	}
	writeInvalidRequest(response)
}

func (handler *MonthlyBudgetHandler) replaceMonthlyBudget(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) {
		writeInvalidRequest(response)
		return
	}
	month, ok := monthlyBudgetMonth(request.PathValue("month"))
	if !ok {
		writeInvalidRequest(response)
		return
	}
	body, ok := decodeMonthlyBudgetBody(response, request)
	if !ok {
		return
	}
	amount, err := domain.NewMoney(body.Amount.Minor, domain.Currency(body.Amount.Currency))
	if err != nil {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.set.Execute(request.Context(), application.SetMonthlyBudgetInput{
		OwnerID: handler.ownerID,
		Month:   month,
		Amount:  amount,
	})
	if err != nil {
		handler.writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, monthlyBudgetResponse{Month: result.Budget.Month().String(), Amount: newAmountResponse(result.Budget.Amount())})
}

func (handler *MonthlyBudgetHandler) getMonthlyBudget(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	month, ok := monthlyBudgetMonth(request.PathValue("month"))
	if !ok {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.get.Execute(request.Context(), application.GetMonthlyBudgetInput{OwnerID: handler.ownerID, Month: month})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		handler.writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, monthlyBudgetResponse{Month: result.Budget.Month().String(), Amount: newAmountResponse(result.Budget.Amount())})
}

func decodeMonthlyBudgetBody(response http.ResponseWriter, request *http.Request) (monthlyBudgetRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return monthlyBudgetRequest{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(raw) {
		writeInvalidRequest(response)
		return monthlyBudgetRequest{}, false
	}
	var body monthlyBudgetRequest
	if !decodeStrict(raw, &body) || body.Amount == nil || body.Amount.Currency != string(domain.CurrencyBRL) || body.Amount.Minor < 0 {
		writeInvalidRequest(response)
		return monthlyBudgetRequest{}, false
	}
	return body, true
}

func monthlyBudgetMonth(value string) (domain.CivilMonth, bool) {
	month, err := domain.ParseCivilMonth(value)
	return month, err == nil
}

func hasIdempotencyHeader(request *http.Request) bool {
	return len(request.Header.Values("Idempotency-Key")) > 0
}

func (handler *MonthlyBudgetHandler) writeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidMonthlyBudgetOwnerID),
		errors.Is(err, application.ErrInvalidMonthlyBudgetMonth),
		errors.Is(err, application.ErrInvalidMonthlyBudgetAmount),
		errors.Is(err, domain.ErrInvalidMonthlyBudgetOwnerID),
		errors.Is(err, domain.ErrInvalidMonthlyBudgetMonth),
		errors.Is(err, domain.ErrInvalidMonthlyBudgetAmount):
		writeInvalidRequest(response)
	case errors.Is(err, application.ErrMonthlyBudgetNotFound):
		writeError(response, http.StatusNotFound, "MONTHLY_BUDGET_NOT_FOUND", "monthly budget was not found")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
