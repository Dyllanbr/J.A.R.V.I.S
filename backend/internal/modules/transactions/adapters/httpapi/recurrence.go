package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

// RecurrenceHandler exposes only the explicit manual recurrence commands and
// query. Ownership remains server-derived and is never decoded from JSON.
type RecurrenceHandler struct {
	ownerID string
	preview application.PreviewRecurrence
	record  *application.RecordRecurrence
	list    *application.ListRecurrences
	cancel  *application.CancelRecurrence
}

// NewRecurrence composes the thin HTTP adapter for recurrence use cases.
func NewRecurrence(
	ownerID string,
	preview application.PreviewRecurrence,
	record *application.RecordRecurrence,
	list *application.ListRecurrences,
	cancel *application.CancelRecurrence,
) *RecurrenceHandler {
	return &RecurrenceHandler{
		ownerID: ownerID,
		preview: preview,
		record:  record,
		list:    list,
		cancel:  cancel,
	}
}

// Register adds the recurrence routes to the process mux without extending
// the writable transaction discriminator.
func (handler *RecurrenceHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/recurrences/preview", handler.previewRecurrence)
	mux.HandleFunc("/v1/recurrences/preview", handler.methodNotAllowed)
	mux.HandleFunc("POST /v1/recurrences", handler.recordRecurrence)
	mux.HandleFunc("GET /v1/recurrences", handler.listRecurrences)
	mux.HandleFunc("/v1/recurrences", handler.methodNotAllowed)
	mux.HandleFunc("POST /v1/recurrences/{recurrenceId}/cancel", handler.cancelRecurrence)
	mux.HandleFunc("/v1/recurrences/{recurrenceId}/cancel", handler.methodNotAllowed)
}

type recurrenceRequest struct {
	Type           *string        `json:"type"`
	Description    *string        `json:"description"`
	ExpectedAmount *amountRequest `json:"expectedAmount"`
	Frequency      *string        `json:"frequency"`
	StartsOn       *string        `json:"startsOn"`
}

type recurrencePreviewResponse struct {
	Type           string         `json:"type"`
	Description    string         `json:"description"`
	ExpectedAmount amountResponse `json:"expectedAmount"`
	Frequency      string         `json:"frequency"`
	StartsOn       string         `json:"startsOn"`
}

type recurrenceResponse struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Description    string         `json:"description"`
	ExpectedAmount amountResponse `json:"expectedAmount"`
	Frequency      string         `json:"frequency"`
	StartsOn       string         `json:"startsOn"`
	Status         string         `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
	CancelledAt    *time.Time     `json:"cancelledAt,omitempty"`
}

type recurrenceListResponse struct {
	Items []recurrenceResponse `json:"items"`
}

func (handler *RecurrenceHandler) previewRecurrence(response http.ResponseWriter, request *http.Request) {
	input, ok := handler.decodeRecurrence(response, request)
	if !ok {
		return
	}
	result, err := handler.preview.Execute(request.Context(), input)
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, newRecurrencePreviewResponse(result.Details))
}

func (handler *RecurrenceHandler) recordRecurrence(response http.ResponseWriter, request *http.Request) {
	key, ok := decodeIdempotencyKey(response, request)
	if !ok {
		return
	}
	input, ok := handler.decodeRecurrence(response, request)
	if !ok {
		return
	}
	result, err := handler.record.Execute(request.Context(), application.RecordRecurrenceInput{
		Recurrence:     input,
		IdempotencyKey: key,
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(response, http.StatusCreated, newRecurrenceResponse(result.Recurrence))
}

func (handler *RecurrenceHandler) listRecurrences(response http.ResponseWriter, request *http.Request) {
	if len(request.URL.Query()) != 0 {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.list.Execute(request.Context(), handler.ownerID)
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	items := make([]recurrenceResponse, 0, len(result.Items))
	for _, recurrence := range result.Items {
		items = append(items, newRecurrenceResponse(recurrence))
	}
	writeJSON(response, http.StatusOK, recurrenceListResponse{Items: items})
}

func (handler *RecurrenceHandler) cancelRecurrence(response http.ResponseWriter, request *http.Request) {
	key, ok := decodeIdempotencyKey(response, request)
	if !ok {
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.cancel.Execute(request.Context(), application.CancelRecurrenceInput{
		UserID:         handler.ownerID,
		RecurrenceID:   request.PathValue("recurrenceId"),
		IdempotencyKey: key,
	})
	if err != nil {
		handler.writeApplicationError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(response, http.StatusOK, newRecurrenceResponse(result.Recurrence))
}

func (handler *RecurrenceHandler) decodeRecurrence(
	response http.ResponseWriter,
	request *http.Request,
) (application.RecurrenceInput, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return application.RecurrenceInput{}, false
	}

	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var body recurrenceRequest
	if err := decoder.Decode(&body); err != nil {
		writeInvalidRequest(response)
		return application.RecurrenceInput{}, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response)
		return application.RecurrenceInput{}, false
	}
	if body.Type == nil || body.Description == nil || body.ExpectedAmount == nil || body.Frequency == nil || body.StartsOn == nil {
		writeInvalidRequest(response)
		return application.RecurrenceInput{}, false
	}
	if *body.Type != string(domain.TransactionTypeExpense) {
		writeError(response, http.StatusBadRequest, "UNSUPPORTED_RECURRENCE_TYPE", "recurrence type is not supported")
		return application.RecurrenceInput{}, false
	}
	startsOn, err := parseCivilDate(*body.StartsOn)
	if err != nil {
		writeInvalidRequest(response)
		return application.RecurrenceInput{}, false
	}

	return application.RecurrenceInput{
		UserID:          handler.ownerID,
		TransactionType: domain.TransactionType(*body.Type),
		Description:     *body.Description,
		ExpectedAmount:  body.ExpectedAmount.Minor,
		Currency:        domain.Currency(body.ExpectedAmount.Currency),
		Frequency:       domain.RecurrenceFrequency(*body.Frequency),
		StartsOn:        startsOn,
	}, true
}

func parseCivilDate(value string) (domain.CivilDate, error) {
	if len(value) != len("0000-00-00") || value[4] != '-' || value[7] != '-' {
		return domain.CivilDate{}, domain.ErrInvalidCivilDate
	}
	for index, character := range []byte(value) {
		if index == 4 || index == 7 {
			continue
		}
		if character < '0' || character > '9' {
			return domain.CivilDate{}, domain.ErrInvalidCivilDate
		}
	}
	year, _ := strconv.Atoi(value[0:4])
	month, _ := strconv.Atoi(value[5:7])
	day, _ := strconv.Atoi(value[8:10])
	return domain.NewCivilDate(year, time.Month(month), day)
}

func decodeEmptyBody(response http.ResponseWriter, request *http.Request) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil || len(bytes.TrimSpace(body)) != 0 {
		writeInvalidRequest(response)
		return false
	}
	return true
}

func (handler *RecurrenceHandler) writeApplicationError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrRecurrenceIdempotencyKeyRequired):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	case errors.Is(err, application.ErrRecurrenceIdempotencyKeyInvalid):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
	case errors.Is(err, application.ErrRecurrenceIdempotencyConflict):
		writeError(response, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was reused with a different request")
	case errors.Is(err, application.ErrRecurrenceNotFound):
		writeError(response, http.StatusNotFound, "RECURRENCE_NOT_FOUND", "recurrence was not found")
	case errors.Is(err, domain.ErrRecurrenceAlreadyCancelled):
		writeError(response, http.StatusConflict, "RECURRENCE_ALREADY_CANCELLED", "recurrence is already cancelled")
	case errors.Is(err, domain.ErrInvalidCivilDate),
		errors.Is(err, domain.ErrUnsupportedCurrency),
		errors.Is(err, domain.ErrInvalidRecurrenceID),
		errors.Is(err, domain.ErrInvalidRecurrenceUserID),
		errors.Is(err, domain.ErrInvalidRecurrenceType),
		errors.Is(err, domain.ErrInvalidRecurrenceDescription),
		errors.Is(err, domain.ErrInvalidRecurrenceExpectedAmount),
		errors.Is(err, domain.ErrInvalidRecurrenceFrequency),
		errors.Is(err, domain.ErrInvalidRecurrenceStartsOn),
		errors.Is(err, domain.ErrInvalidRecurrenceCreatedAt),
		errors.Is(err, domain.ErrInvalidRecurrenceCancelledAt),
		errors.Is(err, domain.ErrInvalidRecurrenceState):
		writeInvalidRequest(response)
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}

func (handler *RecurrenceHandler) methodNotAllowed(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
}

func newRecurrencePreviewResponse(details domain.RecurrenceDetails) recurrencePreviewResponse {
	return recurrencePreviewResponse{
		Type:           string(details.TransactionType),
		Description:    details.Description,
		ExpectedAmount: newAmountResponse(details.ExpectedAmount),
		Frequency:      string(details.Frequency),
		StartsOn:       details.StartsOn.String(),
	}
}

func newRecurrenceResponse(recurrence domain.Recurrence) recurrenceResponse {
	result := recurrenceResponse{
		ID:             recurrence.ID(),
		Type:           string(recurrence.Type()),
		Description:    recurrence.Description(),
		ExpectedAmount: newAmountResponse(recurrence.ExpectedAmount()),
		Frequency:      string(recurrence.Frequency()),
		StartsOn:       recurrence.StartsOn().String(),
		Status:         string(recurrence.Status()),
		CreatedAt:      recurrence.CreatedAt(),
	}
	if cancelledAt, ok := recurrence.CancelledAt(); ok {
		result.CancelledAt = &cancelledAt
	}
	return result
}
