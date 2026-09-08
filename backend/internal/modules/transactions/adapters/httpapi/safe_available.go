package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// SafeAvailableHandler exposes the deterministic, read-only Safe Available
// projection. The owner is supplied by server composition and is never read
// from the request.
type SafeAvailableHandler struct {
	ownerID string
	get     *application.CalculateSafeAvailable
}

func NewSafeAvailable(ownerID string, get *application.CalculateSafeAvailable) *SafeAvailableHandler {
	return &SafeAvailableHandler{ownerID: ownerID, get: get}
}

func (handler *SafeAvailableHandler) Register(mux *http.ServeMux) {
	const path = "/v1/safe-available"
	mux.HandleFunc("GET "+path, handler.getSafeAvailable)
	handler.registerRejectedMethods(mux, path, http.MethodGet)
}

func (handler *SafeAvailableHandler) registerRejectedMethods(mux *http.ServeMux, path string, allowed ...string) {
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

type safeAvailableResponse struct {
	PeriodStart              string                           `json:"periodStart"`
	PeriodEnd                string                           `json:"periodEnd"`
	AvailableBalance         amountResponse                   `json:"availableBalance"`
	TotalConfirmedIncome     amountResponse                   `json:"totalConfirmedIncome"`
	TotalConfirmedExpense    amountResponse                   `json:"totalConfirmedExpense"`
	TotalConfirmedCommitment amountResponse                   `json:"totalConfirmedCommitments"`
	FinalAmount              amountResponse                   `json:"finalAmount"`
	Budget                   *amountResponse                  `json:"budget,omitempty"`
	BudgetRemaining          *amountResponse                  `json:"budgetRemaining,omitempty"`
	Breakdown                []safeAvailableBreakdownResponse `json:"breakdown"`
	MissingData              []string                         `json:"missingData"`
}

type safeAvailableBreakdownResponse struct {
	Kind     string         `json:"kind"`
	SourceID string         `json:"sourceId"`
	Sequence int            `json:"sequence"`
	DueOn    string         `json:"dueOn"`
	Amount   amountResponse `json:"amount"`
}

func (handler *SafeAvailableHandler) getSafeAvailable(response http.ResponseWriter, request *http.Request) {
	period, ok := safeAvailablePeriodFromQuery(request.URL.RawQuery)
	if !ok {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.get.Execute(request.Context(), application.CalculateSafeAvailableInput{
		OwnerID: handler.ownerID,
		Period:  period,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		handler.writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newSafeAvailableResponse(result.Available))
}

func safeAvailablePeriodFromQuery(rawQuery string) (domain.SafeAvailablePeriod, bool) {
	if rawQuery == "" {
		return domain.SafeAvailablePeriod{}, false
	}
	for _, component := range strings.Split(rawQuery, "&") {
		if component == "" {
			return domain.SafeAvailablePeriod{}, false
		}
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil || len(values) != 2 {
		return domain.SafeAvailablePeriod{}, false
	}
	startValues, startOK := values["periodStart"]
	endValues, endOK := values["periodEnd"]
	if !startOK || !endOK || len(startValues) != 1 || len(endValues) != 1 || startValues[0] == "" || endValues[0] == "" {
		return domain.SafeAvailablePeriod{}, false
	}
	startOn, err := parseCivilDate(startValues[0])
	if err != nil {
		return domain.SafeAvailablePeriod{}, false
	}
	endOn, err := parseCivilDate(endValues[0])
	if err != nil {
		return domain.SafeAvailablePeriod{}, false
	}
	period, err := domain.NewSafeAvailablePeriod(startOn, endOn)
	if err != nil {
		return domain.SafeAvailablePeriod{}, false
	}
	return period, true
}

func newSafeAvailableResponse(result domain.SafeAvailableResult) safeAvailableResponse {
	lines := result.Breakdown()
	breakdown := make([]safeAvailableBreakdownResponse, 0, len(lines))
	for _, line := range lines {
		breakdown = append(breakdown, safeAvailableBreakdownResponse{
			Kind: string(line.Kind()), SourceID: line.SourceID(), Sequence: line.Sequence(),
			DueOn: line.DueOn().String(), Amount: newAmountResponse(line.Amount()),
		})
	}
	missing := result.MissingData()
	missingValues := make([]string, 0, len(missing))
	for _, value := range missing {
		missingValues = append(missingValues, string(value))
	}
	response := safeAvailableResponse{
		PeriodStart: result.Period().StartOn().String(), PeriodEnd: result.Period().EndOn().String(),
		AvailableBalance:         newAmountResponse(result.AvailableBalance()),
		TotalConfirmedIncome:     newAmountResponse(result.TotalConfirmedIncome()),
		TotalConfirmedExpense:    newAmountResponse(result.TotalConfirmedExpense()),
		TotalConfirmedCommitment: newAmountResponse(result.TotalConfirmedCommitments()),
		FinalAmount:              newAmountResponse(result.FinalAmount()), Breakdown: breakdown, MissingData: missingValues,
	}
	if budget, ok := result.Budget(); ok {
		value := newAmountResponse(budget.Amount())
		response.Budget = &value
	}
	if remaining, ok := result.BudgetRemaining(); ok {
		value := newAmountResponse(remaining)
		response.BudgetRemaining = &value
	}
	return response
}

func (handler *SafeAvailableHandler) writeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidSafeAvailableOwnerID), errors.Is(err, application.ErrInvalidSafeAvailablePeriod),
		errors.Is(err, domain.ErrInvalidSafeAvailablePeriod):
		writeInvalidRequest(response)
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
