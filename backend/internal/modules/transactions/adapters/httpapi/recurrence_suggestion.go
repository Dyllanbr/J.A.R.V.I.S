package httpapi

import (
	"errors"
	"net/http"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// RecurrenceSuggestionHandler exposes derived recurrence suggestions without
// persisting suggestions or creating financial facts and commitments.
type RecurrenceSuggestionHandler struct {
	ownerID string
	list    *application.ListRecurrenceSuggestions
	dismiss *application.DismissRecurrenceSuggestion
	prepare *application.PrepareSuggestedRecurrence
}

func NewRecurrenceSuggestion(
	ownerID string,
	list *application.ListRecurrenceSuggestions,
	dismiss *application.DismissRecurrenceSuggestion,
	prepare *application.PrepareSuggestedRecurrence,
) *RecurrenceSuggestionHandler {
	return &RecurrenceSuggestionHandler{ownerID: ownerID, list: list, dismiss: dismiss, prepare: prepare}
}

func (handler *RecurrenceSuggestionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/recurrence-suggestions", handler.listSuggestions)
	mux.HandleFunc("HEAD /v1/recurrence-suggestions", handler.methodNotAllowed)
	mux.HandleFunc("/v1/recurrence-suggestions", handler.methodNotAllowed)
	mux.HandleFunc("POST /v1/recurrence-suggestions/{suggestionId}/dismiss", handler.dismissSuggestion)
	mux.HandleFunc("/v1/recurrence-suggestions/{suggestionId}/dismiss", handler.methodNotAllowed)
	mux.HandleFunc("POST /v1/recurrence-suggestions/{suggestionId}/preview", handler.previewSuggestion)
	mux.HandleFunc("/v1/recurrence-suggestions/{suggestionId}/preview", handler.methodNotAllowed)
}

type recurrenceSuggestionResponse struct {
	ID               string         `json:"id"`
	Description      string         `json:"description"`
	ExpectedAmount   amountResponse `json:"expectedAmount"`
	AnchorDay        int            `json:"anchorDay"`
	ProposedStartsOn string         `json:"proposedStartsOn"`
	EvidenceCount    int            `json:"evidenceCount"`
	ObservedDates    []string       `json:"observedDates"`
}

type recurrenceSuggestionListResponse struct {
	Items []recurrenceSuggestionResponse `json:"items"`
}

func (handler *RecurrenceSuggestionHandler) listSuggestions(response http.ResponseWriter, request *http.Request) {
	if len(request.URL.Query()) != 0 {
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
	items := make([]recurrenceSuggestionResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newRecurrenceSuggestionResponse(item))
	}
	writeJSON(response, http.StatusOK, recurrenceSuggestionListResponse{Items: items})
}

func (handler *RecurrenceSuggestionHandler) dismissSuggestion(response http.ResponseWriter, request *http.Request) {
	if len(request.URL.Query()) != 0 {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	id := application.RecurrenceSuggestionID(request.PathValue("suggestionId"))
	if err := application.ValidateRecurrenceSuggestionID(id); err != nil {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.dismiss.Execute(request.Context(), application.DismissRecurrenceSuggestionInput{
		UserID: handler.ownerID, SuggestionID: id,
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeNoContent(response)
}

func (handler *RecurrenceSuggestionHandler) previewSuggestion(response http.ResponseWriter, request *http.Request) {
	if len(request.URL.Query()) != 0 {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	id := application.RecurrenceSuggestionID(request.PathValue("suggestionId"))
	if err := application.ValidateRecurrenceSuggestionID(id); err != nil {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.prepare.Execute(request.Context(), application.PrepareSuggestedRecurrenceInput{
		UserID: handler.ownerID, SuggestionID: id,
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newRecurrencePreviewResponse(result.Preview.Details))
}

func (handler *RecurrenceSuggestionHandler) writeApplicationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidRecurrenceSuggestionID),
		errors.Is(err, domain.ErrInvalidUserID):
		writeInvalidRequest(response)
	case errors.Is(err, application.ErrRecurrenceSuggestionNotFound):
		writeError(response, http.StatusNotFound, "RECURRENCE_SUGGESTION_NOT_FOUND", "recurrence suggestion was not found")
	case errors.Is(err, application.ErrRecurrenceSuggestionSuppressed):
		writeError(response, http.StatusConflict, "RECURRENCE_SUGGESTION_SUPPRESSED", "recurrence suggestion is suppressed")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func (handler *RecurrenceSuggestionHandler) methodNotAllowed(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
}

func newRecurrenceSuggestionResponse(item application.RecurrenceSuggestion) recurrenceSuggestionResponse {
	suggestion := item.Suggestion
	evidence := suggestion.Evidence()
	observedDates := make([]string, 0, len(evidence))
	for _, observation := range evidence {
		observedDates = append(observedDates, observation.OccurredOn().String())
	}
	return recurrenceSuggestionResponse{
		ID:               string(item.ID),
		Description:      suggestion.Description(),
		ExpectedAmount:   newAmountResponse(suggestion.ExpectedAmount()),
		AnchorDay:        suggestion.AnchorDay(),
		ProposedStartsOn: suggestion.ProposedStartsOn().String(),
		EvidenceCount:    suggestion.EvidenceCount(),
		ObservedDates:    observedDates,
	}
}

func writeNoContent(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(http.StatusNoContent)
}
