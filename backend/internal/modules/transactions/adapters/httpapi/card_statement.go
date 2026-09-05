package httpapi

import (
	"errors"
	"net/http"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// CardStatementHandler exposes the read-only statement projection for one
// server-owned credit-card context. The request never supplies an owner.
type CardStatementHandler struct {
	ownerID string
	get     *application.GetCardStatement
}

// NewCardStatement composes the statement HTTP adapter with its server-owned
// owner and the existing application use case.
func NewCardStatement(ownerID string, get *application.GetCardStatement) *CardStatementHandler {
	return &CardStatementHandler{ownerID: ownerID, get: get}
}

// Register adds the strict read-only card statement route.
func (handler *CardStatementHandler) Register(mux *http.ServeMux) {
	const path = "/v1/credit-cards/{creditCardId}/statements/{statementDueOn}"
	const malformedPath = "/v1/credit-cards/"
	mux.HandleFunc("GET "+path, handler.getStatement)
	// Keep malformed or incomplete statement paths inside the financial error
	// envelope instead of allowing ServeMux to emit its plain-text 404.
	mux.HandleFunc(malformedPath, handler.malformedStatementPath)
	handler.registerRejectedMethods(mux, path, http.MethodGet)
}

func (handler *CardStatementHandler) malformedStatementPath(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, request)
		return
	}
	handler.getStatement(response, request)
}

func (handler *CardStatementHandler) registerRejectedMethods(mux *http.ServeMux, path string, allowed ...string) {
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

type cardStatementResponse struct {
	CreditCardID   string                      `json:"creditCardId"`
	StatementDueOn string                      `json:"statementDueOn"`
	TotalAmount    amountResponse              `json:"totalAmount"`
	Lines          []cardStatementLineResponse `json:"lines"`
}

type cardStatementLineResponse struct {
	ExpenseID         string         `json:"expenseId"`
	Description       string         `json:"description"`
	Amount            amountResponse `json:"amount"`
	OccurredAt        string         `json:"occurredAt"`
	PurchaseMode      string         `json:"purchaseMode"`
	InstallmentNumber *int           `json:"installmentNumber,omitempty"`
	InstallmentCount  *int           `json:"installmentCount,omitempty"`
}

func (handler *CardStatementHandler) getStatement(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	statementDueOn, err := parseCivilDate(request.PathValue("statementDueOn"))
	if err != nil {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.get.Execute(request.Context(), application.GetCardStatementInput{
		OwnerID: handler.ownerID, CreditCardID: request.PathValue("creditCardId"), StatementDueOn: statementDueOn,
	})
	if err != nil {
		handler.writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newCardStatementResponse(result.Statement))
}

func newCardStatementResponse(statement domain.CardStatement) cardStatementResponse {
	lines := statement.Lines()
	items := make([]cardStatementLineResponse, 0, len(lines))
	for _, line := range lines {
		var installmentNumber, installmentCount *int
		if number, ok := line.InstallmentNumber(); ok {
			value := number
			installmentNumber = &value
		}
		if count, ok := line.InstallmentCount(); ok {
			value := count
			installmentCount = &value
		}
		items = append(items, cardStatementLineResponse{
			ExpenseID: line.ExpenseID(), Description: line.Description(), Amount: newAmountResponse(line.Amount()),
			OccurredAt: line.OccurredAt().UTC().Format("2006-01-02"), PurchaseMode: string(line.PurchaseMode()),
			InstallmentNumber: installmentNumber, InstallmentCount: installmentCount,
		})
	}
	return cardStatementResponse{
		CreditCardID: statement.CreditCardID(), StatementDueOn: statement.StatementDueOn().String(),
		TotalAmount: newAmountResponse(statement.TotalAmount()), Lines: items,
	}
}

func (handler *CardStatementHandler) writeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidCardStatementOwnerID),
		errors.Is(err, domain.ErrInvalidCreditCardID),
		errors.Is(err, domain.ErrInvalidCardStatementDueDate):
		writeInvalidRequest(response)
	case errors.Is(err, application.ErrCardStatementCreditCardNotFound):
		writeError(response, http.StatusNotFound, "CREDIT_CARD_NOT_FOUND", "credit card was not found")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
