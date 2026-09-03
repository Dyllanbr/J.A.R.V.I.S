package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type InstallmentPlanHandler struct {
	ownerID       string
	list          *application.ListInstallmentPlans
	get           *application.GetInstallmentPlan
	previewCancel *application.PreviewInstallmentPlanCancellation
	cancel        *application.CancelInstallmentPlan
}

func NewInstallmentPlan(ownerID string, list *application.ListInstallmentPlans, get *application.GetInstallmentPlan, previewCancel *application.PreviewInstallmentPlanCancellation, cancel *application.CancelInstallmentPlan) *InstallmentPlanHandler {
	return &InstallmentPlanHandler{ownerID: ownerID, list: list, get: get, previewCancel: previewCancel, cancel: cancel}
}

func (handler *InstallmentPlanHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/installment-plans", handler.listPlans)
	mux.HandleFunc("GET /v1/installment-plans/{installmentPlanId}", handler.getPlan)
	mux.HandleFunc("POST /v1/installment-plans/{installmentPlanId}/cancellation-preview", handler.previewCancellation)
	mux.HandleFunc("POST /v1/installment-plans/{installmentPlanId}/cancel", handler.cancelPlan)
	handler.registerRejected(mux, "/v1/installment-plans", http.MethodGet)
	handler.registerRejected(mux, "/v1/installment-plans/{installmentPlanId}", http.MethodGet)
	handler.registerRejected(mux, "/v1/installment-plans/{installmentPlanId}/cancellation-preview", http.MethodPost)
	handler.registerRejected(mux, "/v1/installment-plans/{installmentPlanId}/cancel", http.MethodPost)
}

func (handler *InstallmentPlanHandler) registerRejected(mux *http.ServeMux, path string, allowed ...string) {
	set := make(map[string]struct{}, len(allowed))
	for _, method := range allowed {
		set[method] = struct{}{}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace} {
		if _, ok := set[method]; !ok {
			mux.HandleFunc(method+" "+path, methodNotAllowed)
		}
	}
}

type installmentPlanListResponse struct {
	Items []installmentPlanResponse `json:"items"`
}

type installmentPlanResponse struct {
	ID               string                `json:"id"`
	CreditCardID     string                `json:"creditCardId"`
	ExpenseID        string                `json:"expenseId"`
	TotalAmount      amountResponse        `json:"totalAmount"`
	InstallmentCount int                   `json:"installmentCount"`
	FirstDueDate     string                `json:"firstDueDate"`
	DueDayAnchor     int                   `json:"dueDayAnchor"`
	Status           string                `json:"status"`
	CreatedAt        time.Time             `json:"createdAt"`
	CancelledOn      *string               `json:"cancelledOn,omitempty"`
	Schedule         []installmentResponse `json:"schedule"`
}

type installmentResponse struct {
	Number     int            `json:"number"`
	TotalCount int            `json:"totalCount"`
	DueDate    string         `json:"dueDate"`
	Amount     amountResponse `json:"amount"`
}

type installmentPlanCancellationPreviewResponse struct {
	InstallmentPlanID   string                  `json:"installmentPlanId"`
	ExpectedCancelledOn string                  `json:"expectedCancelledOn"`
	Plan                installmentPlanResponse `json:"plan"`
}

type installmentPlanCancelRequest struct {
	ExpectedCancelledOn *string `json:"expectedCancelledOn"`
}

func (handler *InstallmentPlanHandler) listPlans(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" || !decodeEmptyBody(response, request) {
		if request.URL.RawQuery != "" {
			writeInvalidRequest(response)
		}
		return
	}
	result, err := handler.list.Execute(request.Context(), handler.ownerID)
	if err != nil {
		handler.writeError(response, err)
		return
	}
	items := make([]installmentPlanResponse, 0, len(result.Items))
	for _, plan := range result.Items {
		encoded, ok := newInstallmentPlanResponseValue(plan)
		if !ok {
			handler.writeError(response, application.ErrInstallmentPlanDependency)
			return
		}
		items = append(items, encoded)
	}
	writeJSON(response, http.StatusOK, installmentPlanListResponse{Items: items})
}

func (handler *InstallmentPlanHandler) getPlan(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.get.Execute(request.Context(), application.GetInstallmentPlanInput{OwnerID: handler.ownerID, PlanID: request.PathValue("installmentPlanId")})
	if err != nil {
		handler.writeError(response, err)
		return
	}
	encoded, ok := newInstallmentPlanResponseValue(result.InstallmentPlan)
	if !ok {
		handler.writeError(response, application.ErrInstallmentPlanDependency)
		return
	}
	writeJSON(response, http.StatusOK, encoded)
}

func (handler *InstallmentPlanHandler) previewCancellation(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	if !decodeEmptyBody(response, request) {
		return
	}
	result, err := handler.previewCancel.Execute(request.Context(), application.PreviewInstallmentPlanCancellationInput{OwnerID: handler.ownerID, PlanID: request.PathValue("installmentPlanId")})
	if err != nil {
		handler.writeError(response, err)
		return
	}
	planResult, err := handler.get.Execute(request.Context(), application.GetInstallmentPlanInput{OwnerID: handler.ownerID, PlanID: result.PlanID})
	if err != nil {
		handler.writeError(response, err)
		return
	}
	plan, ok := newInstallmentPlanResponseValue(planResult.InstallmentPlan)
	if !ok {
		handler.writeError(response, application.ErrInstallmentPlanDependency)
		return
	}
	writeJSON(response, http.StatusOK, installmentPlanCancellationPreviewResponse{InstallmentPlanID: result.PlanID, ExpectedCancelledOn: result.CancelledOn.String(), Plan: plan})
}

func (handler *InstallmentPlanHandler) cancelPlan(response http.ResponseWriter, request *http.Request) {
	if request.URL.RawQuery != "" {
		writeInvalidRequest(response)
		return
	}
	key, ok := decodeIdempotencyKey(response, request)
	if !ok {
		return
	}
	body, ok := decodeCancellationBody(response, request)
	if !ok {
		return
	}
	expected, err := parseCivilDate(*body.ExpectedCancelledOn)
	if err != nil {
		writeInvalidRequest(response)
		return
	}
	result, err := handler.cancel.Execute(request.Context(), application.CancelInstallmentPlanInput{OwnerID: handler.ownerID, PlanID: request.PathValue("installmentPlanId"), ExpectedCancelledOn: expected, IdempotencyKey: key})
	if err != nil {
		handler.writeError(response, err)
		return
	}
	if result.Replayed {
		response.Header().Set("Idempotency-Replayed", "true")
	}
	encoded, ok := newInstallmentPlanResponseValue(result.InstallmentPlan)
	if !ok {
		handler.writeError(response, application.ErrInstallmentPlanDependency)
		return
	}
	writeJSON(response, http.StatusOK, encoded)
}

func decodeCancellationBody(response http.ResponseWriter, request *http.Request) (installmentPlanCancelRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInvalidRequest(response)
		return installmentPlanCancelRequest{}, false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil || !utf8.Valid(raw) {
		writeInvalidRequest(response)
		return installmentPlanCancelRequest{}, false
	}
	var body installmentPlanCancelRequest
	if !decodeStrict(raw, &body) || body.ExpectedCancelledOn == nil {
		writeInvalidRequest(response)
		return installmentPlanCancelRequest{}, false
	}
	return body, true
}

func newInstallmentPlanResponse(plan *domain.InstallmentPlan) *installmentPlanResponse {
	if plan == nil {
		return nil
	}
	value, ok := newInstallmentPlanResponseValue(*plan)
	if !ok {
		return nil
	}
	return &value
}

func newInstallmentPlanResponseValue(plan domain.InstallmentPlan) (installmentPlanResponse, bool) {
	schedule, err := plan.Schedule()
	if err != nil {
		return installmentPlanResponse{}, false
	}
	items := make([]installmentResponse, 0, len(schedule))
	for _, installment := range schedule {
		items = append(items, installmentResponse{Number: installment.Number(), TotalCount: installment.TotalCount(), DueDate: installment.DueDate().String(), Amount: newAmountResponse(installment.Amount())})
	}
	var cancelledOn *string
	if value, ok := plan.CancelledOn(); ok {
		text := value.String()
		cancelledOn = &text
	}
	return installmentPlanResponse{ID: plan.ID(), CreditCardID: plan.CreditCardID(), ExpenseID: plan.ExpenseID(), TotalAmount: newAmountResponse(plan.TotalAmount()), InstallmentCount: plan.InstallmentCount(), FirstDueDate: plan.FirstDueDate().String(), DueDayAnchor: plan.DueDayAnchor().Day(), Status: string(plan.Status()), CreatedAt: plan.CreatedAt(), CancelledOn: cancelledOn, Schedule: items}, true
}

func (handler *InstallmentPlanHandler) writeError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInstallmentPlanID), errors.Is(err, domain.ErrInvalidInstallmentPlanOwnerID), errors.Is(err, domain.ErrInvalidInstallmentCancellationDate):
		writeInvalidRequest(response)
	case errors.Is(err, application.ErrInstallmentPlanNotFound):
		writeError(response, http.StatusNotFound, "INSTALLMENT_PLAN_NOT_FOUND", "installment plan was not found")
	case errors.Is(err, domain.ErrInstallmentPlanAlreadyCancelled):
		writeError(response, http.StatusConflict, "INSTALLMENT_PLAN_ALREADY_CANCELLED", "installment plan is already cancelled")
	case errors.Is(err, application.ErrInstallmentCancellationDateStale):
		writeError(response, http.StatusConflict, "INSTALLMENT_CANCELLATION_DATE_STALE", "cancellation date is stale")
	case errors.Is(err, application.ErrInstallmentCancelKeyRequired):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "idempotency key is required")
	case errors.Is(err, application.ErrInstallmentCancelKeyInvalid):
		writeError(response, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "idempotency key is invalid")
	case errors.Is(err, application.ErrInstallmentCancelConflict):
		writeError(response, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was reused with a different request")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
