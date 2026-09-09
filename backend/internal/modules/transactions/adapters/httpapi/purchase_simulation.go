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

// PurchaseSimulationHandler exposes the read-only "can I buy?" projection.
// The owner is composed by the server and is never accepted from the client.
type PurchaseSimulationHandler struct {
	ownerID string
	useCase *application.SimulatePurchase
}

func NewPurchaseSimulation(ownerID string, useCase *application.SimulatePurchase) *PurchaseSimulationHandler {
	return &PurchaseSimulationHandler{ownerID: ownerID, useCase: useCase}
}

func (handler *PurchaseSimulationHandler) Register(mux *http.ServeMux) {
	const path = "/v1/purchase-simulations"
	mux.HandleFunc("POST "+path, handler.simulate)
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace,
	} {
		mux.HandleFunc(method+" "+path, methodNotAllowed)
	}
	mux.HandleFunc("/v1/purchase-simulations/", handler.malformedPath)
}

type purchaseSimulationRequest struct {
	CreditCardID     *string        `json:"creditCardId"`
	Amount           *amountRequest `json:"amount"`
	PurchaseOn       *string        `json:"purchaseOn"`
	PurchaseMode     *string        `json:"purchaseMode"`
	InstallmentCount *int           `json:"installmentCount"`
	PeriodStart      *string        `json:"periodStart"`
	PeriodEnd        *string        `json:"periodEnd"`
}

type purchaseSimulationResponse struct {
	CreditCardID            string                           `json:"creditCardId"`
	PurchaseOn              string                           `json:"purchaseOn"`
	PurchaseMode            string                           `json:"purchaseMode"`
	InstallmentCount        int                              `json:"installmentCount,omitempty"`
	PeriodStart             string                           `json:"periodStart"`
	PeriodEnd               string                           `json:"periodEnd"`
	Baseline                safeAvailableResponse            `json:"baseline"`
	Projected               safeAvailableResponse            `json:"projected"`
	Impact                  amountResponse                   `json:"impact"`
	HypotheticalCommitments []safeAvailableBreakdownResponse `json:"hypotheticalCommitments"`
	Assumptions             []string                         `json:"assumptions"`
}

func (handler *PurchaseSimulationHandler) malformedPath(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, request)
		return
	}
	writeInvalidRequest(response)
}

func (handler *PurchaseSimulationHandler) simulate(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || hasIdempotencyHeader(request) {
		writeInvalidRequest(response)
		return
	}
	input, ok := decodePurchaseSimulationRequest(response, request)
	if !ok {
		return
	}
	result, err := handler.useCase.Execute(request.Context(), application.SimulatePurchaseInput{
		OwnerID: handler.ownerID, Period: input.period, CreditCardID: input.creditCardID,
		AmountMinor: input.amount.Minor, Currency: domain.Currency(input.amount.Currency),
		PurchaseOn: input.purchaseOn, Mode: domain.PurchaseSimulationMode(input.purchaseMode),
		InstallmentCount: input.installmentCount,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		handler.writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newPurchaseSimulationResponse(result.Simulation))
}

type decodedPurchaseSimulationRequest struct {
	creditCardID     string
	amount           amountRequest
	purchaseOn       domain.CivilDate
	purchaseMode     string
	installmentCount int
	period           domain.SafeAvailablePeriod
}

func decodePurchaseSimulationRequest(response http.ResponseWriter, request *http.Request) (decodedPurchaseSimulationRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return decodedPurchaseSimulationRequest{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(raw) {
		writeInvalidRequest(response)
		return decodedPurchaseSimulationRequest{}, false
	}
	var body purchaseSimulationRequest
	if !decodeStrict(raw, &body) || body.CreditCardID == nil || body.Amount == nil || body.PurchaseOn == nil || body.PurchaseMode == nil || body.PeriodStart == nil || body.PeriodEnd == nil {
		writeInvalidRequest(response)
		return decodedPurchaseSimulationRequest{}, false
	}
	purchaseOn, err := parseCivilDate(*body.PurchaseOn)
	if err != nil {
		writeInvalidRequest(response)
		return decodedPurchaseSimulationRequest{}, false
	}
	periodStart, err := parseCivilDate(*body.PeriodStart)
	if err != nil {
		writeInvalidRequest(response)
		return decodedPurchaseSimulationRequest{}, false
	}
	periodEnd, err := parseCivilDate(*body.PeriodEnd)
	if err != nil {
		writeInvalidRequest(response)
		return decodedPurchaseSimulationRequest{}, false
	}
	period, err := domain.NewSafeAvailablePeriod(periodStart, periodEnd)
	if err != nil || body.Amount.Currency != string(domain.CurrencyBRL) || body.Amount.Minor <= 0 {
		writeInvalidRequest(response)
		return decodedPurchaseSimulationRequest{}, false
	}
	installmentCount := 0
	if body.InstallmentCount != nil {
		installmentCount = *body.InstallmentCount
	}
	return decodedPurchaseSimulationRequest{
		creditCardID: *body.CreditCardID, amount: *body.Amount, purchaseOn: purchaseOn,
		purchaseMode: *body.PurchaseMode, installmentCount: installmentCount, period: period,
	}, true
}

func newPurchaseSimulationResponse(result domain.PurchaseSimulationResult) purchaseSimulationResponse {
	request := result.Request()
	lines := result.HypotheticalCommitments()
	hypothetical := make([]safeAvailableBreakdownResponse, 0, len(lines))
	for _, line := range lines {
		hypothetical = append(hypothetical, safeAvailableBreakdownResponse{
			Kind: string(line.Kind()), SourceID: line.SourceID(), Sequence: line.Sequence(),
			DueOn: line.DueOn().String(), Amount: newAmountResponse(line.Amount()),
		})
	}
	assumptions := result.Assumptions()
	assumptionValues := make([]string, 0, len(assumptions))
	for _, assumption := range assumptions {
		assumptionValues = append(assumptionValues, string(assumption))
	}
	baseline := result.Baseline()
	projected := result.Projected()
	return purchaseSimulationResponse{
		CreditCardID: request.CreditCardID(), PurchaseOn: request.PurchaseOn().String(),
		PurchaseMode: string(request.Mode()), InstallmentCount: request.InstallmentCount(),
		PeriodStart: baseline.Period().StartOn().String(), PeriodEnd: baseline.Period().EndOn().String(),
		Baseline: newSafeAvailableResponse(baseline), Projected: newSafeAvailableResponse(projected),
		Impact: newAmountResponse(result.Impact()), HypotheticalCommitments: hypothetical, Assumptions: assumptionValues,
	}
}

func (handler *PurchaseSimulationHandler) writeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidPurchaseSimulationOwnerID), errors.Is(err, application.ErrInvalidPurchaseSimulationPeriod),
		errors.Is(err, domain.ErrInvalidPurchaseSimulationMode), errors.Is(err, domain.ErrInvalidPurchaseSimulationAmount),
		errors.Is(err, domain.ErrInvalidPurchaseSimulationDate), errors.Is(err, domain.ErrInvalidPurchaseSimulationCardID),
		errors.Is(err, domain.ErrInvalidPurchaseSimulationInstallments), errors.Is(err, domain.ErrInvalidCreditCardID),
		errors.Is(err, domain.ErrUnsupportedCurrency):
		writeInvalidRequest(response)
	case errors.Is(err, application.ErrPurchaseSimulationCardNotFound):
		writeError(response, http.StatusNotFound, "CREDIT_CARD_NOT_FOUND", "credit card was not found")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
