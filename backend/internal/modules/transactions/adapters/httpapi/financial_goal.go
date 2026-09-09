package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// FinancialGoalsHandler exposes the declaration-only goals and protected
// values APIs. Ownership is composed by the server and never accepted from
// the request. These declarations do not affect Safe Available in this slice.
type FinancialGoalsHandler struct {
	ownerID      string
	list         *application.ListFinancialGoals
	replaceGoal  *application.ReplaceFinancialGoal
	replaceValue *application.ReplaceProtectedValue
}

func NewFinancialGoals(
	ownerID string,
	list *application.ListFinancialGoals,
	replaceGoal *application.ReplaceFinancialGoal,
	replaceValue *application.ReplaceProtectedValue,
) *FinancialGoalsHandler {
	return &FinancialGoalsHandler{ownerID: ownerID, list: list, replaceGoal: replaceGoal, replaceValue: replaceValue}
}

func (handler *FinancialGoalsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/financial-goals", handler.listFinancialGoals)
	handler.registerRejectedMethods(mux, "/v1/financial-goals", http.MethodGet)
	mux.HandleFunc("PUT /v1/financial-goals/{goalId}", handler.replaceFinancialGoal)
	handler.registerRejectedMethods(mux, "/v1/financial-goals/{goalId}", http.MethodPut)
	mux.HandleFunc("PUT /v1/protected-values/{valueId}", handler.replaceProtectedValue)
	handler.registerRejectedMethods(mux, "/v1/protected-values/{valueId}", http.MethodPut)
	mux.HandleFunc("/v1/financial-goals/", handler.malformedCollectionPath)
	mux.HandleFunc("/v1/protected-values/", handler.malformedCollectionPath)
}

func (handler *FinancialGoalsHandler) registerRejectedMethods(mux *http.ServeMux, path string, allowed ...string) {
	allowedMethods := make(map[string]struct{}, len(allowed))
	for _, method := range allowed {
		allowedMethods[method] = struct{}{}
	}
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions,
		http.MethodTrace,
	} {
		if _, ok := allowedMethods[method]; ok {
			continue
		}
		mux.HandleFunc(method+" "+path, methodNotAllowed)
	}
}

type financialGoalsResponse struct {
	Goals           []financialGoalResponse  `json:"goals"`
	ProtectedValues []protectedValueResponse `json:"protectedValues"`
}

type financialGoalResponse struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	TargetAmount amountResponse `json:"targetAmount"`
}

type protectedValueResponse struct {
	ID     string         `json:"id"`
	Label  string         `json:"label"`
	Amount amountResponse `json:"amount"`
}

type financialGoalRequest struct {
	Title        *string        `json:"title"`
	TargetAmount *amountRequest `json:"targetAmount"`
}

type protectedValueRequest struct {
	Label  *string        `json:"label"`
	Amount *amountRequest `json:"amount"`
}

func (handler *FinancialGoalsHandler) malformedCollectionPath(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPut {
		methodNotAllowed(response, request)
		return
	}
	writeInvalidRequest(response)
}

func (handler *FinancialGoalsHandler) listFinancialGoals(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.list.Execute(request.Context(), handler.ownerID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		handler.writeError(response, err)
		return
	}
	goals := result.Goals()
	goalItems := make([]financialGoalResponse, 0, len(goals))
	for _, goal := range goals {
		goalItems = append(goalItems, financialGoalResponse{ID: goal.ID(), Title: goal.Title(), TargetAmount: newAmountResponse(goal.TargetAmount())})
	}
	values := result.ProtectedValues()
	valueItems := make([]protectedValueResponse, 0, len(values))
	for _, value := range values {
		valueItems = append(valueItems, protectedValueResponse{ID: value.ID(), Label: value.Label(), Amount: newAmountResponse(value.Amount())})
	}
	writeJSON(response, http.StatusOK, financialGoalsResponse{Goals: goalItems, ProtectedValues: valueItems})
}

func (handler *FinancialGoalsHandler) replaceFinancialGoal(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) {
		writeInvalidRequest(response)
		return
	}
	body, ok := decodeFinancialGoalBody(response, request)
	if !ok {
		return
	}
	amount, err := domain.NewMoney(body.TargetAmount.Minor, domain.Currency(body.TargetAmount.Currency))
	if err != nil {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.replaceGoal.Execute(request.Context(), application.ReplaceFinancialGoalInput{
		OwnerID: handler.ownerID, ID: request.PathValue("goalId"), Title: *body.Title, TargetAmount: amount,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		handler.writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, financialGoalResponse{ID: result.ID(), Title: result.Title(), TargetAmount: newAmountResponse(result.TargetAmount())})
}

func (handler *FinancialGoalsHandler) replaceProtectedValue(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) {
		writeInvalidRequest(response)
		return
	}
	body, ok := decodeProtectedValueBody(response, request)
	if !ok {
		return
	}
	amount, err := domain.NewMoney(body.Amount.Minor, domain.Currency(body.Amount.Currency))
	if err != nil {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.replaceValue.Execute(request.Context(), application.ReplaceProtectedValueInput{
		OwnerID: handler.ownerID, ID: request.PathValue("valueId"), Label: *body.Label, Amount: amount,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		handler.writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, protectedValueResponse{ID: result.ID(), Label: result.Label(), Amount: newAmountResponse(result.Amount())})
}

func decodeFinancialGoalBody(response http.ResponseWriter, request *http.Request) (financialGoalRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return financialGoalRequest{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := readFinancialGoalsBody(request)
	if err != nil {
		writeInvalidRequest(response)
		return financialGoalRequest{}, false
	}
	var body financialGoalRequest
	if !decodeStrict(raw, &body) || body.Title == nil || body.TargetAmount == nil || body.TargetAmount.Currency != string(domain.CurrencyBRL) || body.TargetAmount.Minor <= 0 {
		writeInvalidRequest(response)
		return financialGoalRequest{}, false
	}
	return body, true
}

func decodeProtectedValueBody(response http.ResponseWriter, request *http.Request) (protectedValueRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return protectedValueRequest{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := readFinancialGoalsBody(request)
	if err != nil {
		writeInvalidRequest(response)
		return protectedValueRequest{}, false
	}
	var body protectedValueRequest
	if !decodeStrict(raw, &body) || body.Label == nil || body.Amount == nil || body.Amount.Currency != string(domain.CurrencyBRL) || body.Amount.Minor < 0 {
		writeInvalidRequest(response)
		return protectedValueRequest{}, false
	}
	return body, true
}

func readFinancialGoalsBody(request *http.Request) ([]byte, error) {
	raw, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("invalid financial goals body")
	}
	return raw, nil
}

func (handler *FinancialGoalsHandler) writeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidFinancialGoalsOwnerID),
		errors.Is(err, application.ErrInvalidFinancialGoalInput),
		errors.Is(err, application.ErrInvalidProtectedValueInput),
		errors.Is(err, domain.ErrInvalidFinancialGoalID),
		errors.Is(err, domain.ErrInvalidFinancialGoalOwnerID),
		errors.Is(err, domain.ErrInvalidFinancialGoalTitle),
		errors.Is(err, domain.ErrInvalidFinancialGoalTarget),
		errors.Is(err, domain.ErrInvalidProtectedValueID),
		errors.Is(err, domain.ErrInvalidProtectedValueOwnerID),
		errors.Is(err, domain.ErrInvalidProtectedValueLabel),
		errors.Is(err, domain.ErrInvalidProtectedValueAmount):
		writeInvalidRequest(response)
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
