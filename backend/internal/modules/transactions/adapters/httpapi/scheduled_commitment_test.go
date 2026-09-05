package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type scheduledCommitmentHTTPReader struct {
	snapshot application.ScheduledCommitmentSnapshot
	err      error
	calls    int
	ownerID  string
}

func (reader *scheduledCommitmentHTTPReader) Read(_ context.Context, ownerID string) (application.ScheduledCommitmentSnapshot, error) {
	reader.calls++
	reader.ownerID = ownerID
	return reader.snapshot, reader.err
}

func TestScheduledCommitmentsHTTPReturnsClosedCombinedProjection(t *testing.T) {
	owner := "usr_http_scheduled"
	plan := scheduledHTTPPlan(t, owner)
	recurrence := scheduledHTTPRecurrence(t, owner)
	reader := &scheduledCommitmentHTTPReader{snapshot: application.ScheduledCommitmentSnapshot{
		InstallmentPlans: []domain.InstallmentPlan{plan},
		Recurrences:      []domain.Recurrence{recurrence},
	}}
	useCase, err := application.NewListScheduledCommitments(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewScheduledCommitments(owner, useCase).Register(mux)

	response := serveScheduled(t, mux, http.MethodGet, "/v1/scheduled-commitments?evaluationDate=2026-09-01", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	assertScheduledHeaders(t, response)
	var payload struct {
		Items []struct {
			Source   string `json:"source"`
			SourceID string `json:"sourceId"`
			Sequence int    `json:"sequence"`
			DueOn    string `json:"dueOn"`
			Amount   struct {
				Minor    int64  `json:"minor"`
				Currency string `json:"currency"`
			} `json:"amount"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 13 {
		t.Fatalf("items = %d, want 13: %s", len(payload.Items), response.Body.String())
	}
	if payload.Items[0].Source != "INSTALLMENT_PLAN" || payload.Items[0].SourceID != plan.ID() || payload.Items[0].Sequence != 1 || payload.Items[0].DueOn != "2026-10-10" || payload.Items[0].Amount.Minor != 100 {
		t.Fatalf("plan item = %#v", payload.Items[0])
	}
	if payload.Items[1].Source != "RECURRENCE" || payload.Items[1].SourceID != recurrence.ID() || payload.Items[1].DueOn != "2026-10-10" || payload.Items[1].Amount.Currency != "BRL" {
		t.Fatalf("recurrence item = %#v", payload.Items[1])
	}
	if reader.calls != 1 || reader.ownerID != owner {
		t.Fatalf("reader calls/owner = %d/%q", reader.calls, reader.ownerID)
	}
}

func TestScheduledCommitmentsHTTPRejectsInvalidQueriesAndBodiesBeforeApplication(t *testing.T) {
	reader := &scheduledCommitmentHTTPReader{}
	useCase, err := application.NewListScheduledCommitments(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewScheduledCommitments("usr_http_scheduled", useCase).Register(mux)
	for _, query := range []string{"", "foo=bar", "foo=", "foo;bar=baz", "foo=%ZZ", "owner=spoofed", "userId=spoofed", "evaluationDate=2026-09-01&evaluationDate=2026-09-01", "evaluationDate=2026-09-01&foo=bar", "evaluationDate=2026-02-30"} {
		response := serveScheduled(t, mux, http.MethodGet, "/v1/scheduled-commitments"+querySeparator(query), "", nil)
		assertScheduledError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
	}
	response := serveScheduled(t, mux, http.MethodGet, "/v1/scheduled-commitments?evaluationDate=2026-09-01", `{"unexpected":true}`, nil)
	assertScheduledError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
	if reader.calls != 0 {
		t.Fatalf("invalid requests reached application: %d", reader.calls)
	}
}

func TestScheduledCommitmentsHTTPRejectsUnsupportedMethodsBeforeApplication(t *testing.T) {
	reader := &scheduledCommitmentHTTPReader{}
	useCase, err := application.NewListScheduledCommitments(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewScheduledCommitments("usr_http_scheduled", useCase).Register(mux)
	for _, method := range []string{http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace, http.MethodConnect, http.MethodPost} {
		response := serveScheduled(t, mux, method, "/v1/scheduled-commitments?evaluationDate=2026-09-01", `{"unexpected":true}`, nil)
		assertScheduledError(t, response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	}
	if reader.calls != 0 {
		t.Fatalf("unsupported methods reached application: %d", reader.calls)
	}
}

func TestScheduledCommitmentsHTTPSanitizesSnapshotFailures(t *testing.T) {
	marker := "SQL_SECRET_SCHEDULED_COMMITMENTS"
	reader := &scheduledCommitmentHTTPReader{err: errors.New(marker)}
	useCase, err := application.NewListScheduledCommitments(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewScheduledCommitments("usr_http_scheduled", useCase).Register(mux)
	response := serveScheduled(t, mux, http.MethodGet, "/v1/scheduled-commitments?evaluationDate=2026-09-01", "", nil)
	assertScheduledError(t, response, http.StatusInternalServerError, "INTERNAL_ERROR")
	if strings.Contains(response.Body.String(), marker) {
		t.Fatalf("internal marker leaked: %s", response.Body.String())
	}
}

func scheduledHTTPPlan(t *testing.T, owner string) domain.InstallmentPlan {
	t.Helper()
	amount, err := domain.NewMoney(200, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	firstDue, err := domain.NewCivilDate(2026, time.October, 10)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := domain.NewDayOfMonthAnchor(10)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewInstallmentPlan(domain.InstallmentPlanParams{
		ID: "ipl_0123456789abcdef0123456789abcdef", OwnerID: owner,
		CreditCardID: "card_0123456789abcdef0123456789abcdef", ExpenseID: "expense-scheduled-http",
		TotalAmount: amount, InstallmentCount: 2, FirstDueDate: firstDue, DueDayAnchor: anchor,
		CreatedAt: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func scheduledHTTPRecurrence(t *testing.T, owner string) domain.Recurrence {
	t.Helper()
	amount, err := domain.NewMoney(300, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	startsOn, err := domain.NewCivilDate(2026, time.October, 10)
	if err != nil {
		t.Fatal(err)
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID:        "recurrence-http-scheduled-001",
		Details:   domain.RecurrenceDetails{UserID: owner, TransactionType: domain.TransactionTypeExpense, Description: "Compromisso HTTP", ExpectedAmount: amount, Frequency: domain.RecurrenceFrequencyMonthly, StartsOn: startsOn},
		CreatedAt: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return recurrence
}

func serveScheduled(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func querySeparator(query string) string {
	if query == "" {
		return ""
	}
	return "?" + query
}

func assertScheduledHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", response.Header())
	}
}

func assertScheduledError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("response = %d %s, want %d/%s", response.Code, response.Body.String(), status, code)
	}
	assertScheduledHeaders(t, response)
}
