package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// ScheduledCommitmentHandler exposes the read-only future commitments
// projection. Ownership is supplied by the server composition and is never
// accepted from the request.
type ScheduledCommitmentHandler struct {
	ownerID string
	list    *application.ListScheduledCommitments
}

func NewScheduledCommitments(ownerID string, list *application.ListScheduledCommitments) *ScheduledCommitmentHandler {
	return &ScheduledCommitmentHandler{ownerID: ownerID, list: list}
}

func (handler *ScheduledCommitmentHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/scheduled-commitments", handler.listCommitments)
	handler.registerRejectedMethods(mux, "/v1/scheduled-commitments", http.MethodGet)
}

func (handler *ScheduledCommitmentHandler) registerRejectedMethods(mux *http.ServeMux, path string, allowed ...string) {
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

type scheduledCommitmentListResponse struct {
	Items []scheduledCommitmentResponse `json:"items"`
}

type scheduledCommitmentResponse struct {
	Source   string         `json:"source"`
	SourceID string         `json:"sourceId"`
	Sequence int            `json:"sequence"`
	DueOn    string         `json:"dueOn"`
	Amount   amountResponse `json:"amount"`
}

func (handler *ScheduledCommitmentHandler) listCommitments(response http.ResponseWriter, request *http.Request) {
	evaluationDate, ok := scheduledCommitmentEvaluationDate(request.URL.RawQuery)
	if !ok {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.list.Execute(request.Context(), application.ListScheduledCommitmentsInput{
		OwnerID:        handler.ownerID,
		EvaluationDate: evaluationDate,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		handler.writeError(response, err)
		return
	}
	items := make([]scheduledCommitmentResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, scheduledCommitmentResponse{
			Source:   string(item.Source()),
			SourceID: item.SourceID(),
			Sequence: item.Sequence(),
			DueOn:    item.DueOn().String(),
			Amount:   newAmountResponse(item.Amount()),
		})
	}
	writeJSON(response, http.StatusOK, scheduledCommitmentListResponse{Items: items})
}

func scheduledCommitmentEvaluationDate(rawQuery string) (domain.CivilDate, bool) {
	if rawQuery == "" {
		return domain.CivilDate{}, false
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil || len(values) != 1 {
		return domain.CivilDate{}, false
	}
	dateValues, ok := values["evaluationDate"]
	if !ok || len(dateValues) != 1 || dateValues[0] == "" {
		return domain.CivilDate{}, false
	}
	evaluationDate, err := parseCivilDate(dateValues[0])
	if err != nil {
		return domain.CivilDate{}, false
	}
	return evaluationDate, true
}

func (handler *ScheduledCommitmentHandler) writeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrScheduledCommitmentEvaluationDate),
		errors.Is(err, application.ErrScheduledCommitmentHorizon):
		writeInvalidRequest(response)
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
