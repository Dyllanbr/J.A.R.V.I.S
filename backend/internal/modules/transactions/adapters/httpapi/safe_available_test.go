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

type safeAvailableHTTPReader struct {
	snapshot domain.SafeAvailableSnapshot
	err      error
	calls    int
	query    application.SafeAvailableSnapshotQuery
}

func (reader *safeAvailableHTTPReader) Read(_ context.Context, query application.SafeAvailableSnapshotQuery) (domain.SafeAvailableSnapshot, error) {
	reader.calls++
	reader.query = query
	return reader.snapshot, reader.err
}

func TestSafeAvailableHTTPReturnsDeterministicProjectionAndServerOwner(t *testing.T) {
	const owner = "usr_safe_available_http"
	period := safeHTTPPeriod(t)
	snapshot := safeHTTPSnapshot(t, owner, period)
	reader := &safeAvailableHTTPReader{snapshot: snapshot}
	useCase, err := application.NewCalculateSafeAvailable(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewSafeAvailable(owner, useCase).Register(mux)

	response := serveSafeAvailable(t, mux, http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	assertSafeAvailableHeaders(t, response)
	var payload struct {
		PeriodStart              string        `json:"periodStart"`
		PeriodEnd                string        `json:"periodEnd"`
		AvailableBalance         amountPayload `json:"availableBalance"`
		TotalConfirmedIncome     amountPayload `json:"totalConfirmedIncome"`
		TotalConfirmedExpense    amountPayload `json:"totalConfirmedExpense"`
		TotalConfirmedCommitment amountPayload `json:"totalConfirmedCommitments"`
		FinalAmount              amountPayload `json:"finalAmount"`
		Breakdown                []struct {
			Kind     string        `json:"kind"`
			SourceID string        `json:"sourceId"`
			Sequence int           `json:"sequence"`
			DueOn    string        `json:"dueOn"`
			Amount   amountPayload `json:"amount"`
		} `json:"breakdown"`
		MissingData []string `json:"missingData"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PeriodStart != "2026-09-01" || payload.PeriodEnd != "2026-09-30" || payload.AvailableBalance.Minor != 100 || payload.TotalConfirmedIncome.Minor != 30 || payload.TotalConfirmedExpense.Minor != 20 || payload.TotalConfirmedCommitment.Minor != 10 || payload.FinalAmount.Minor != 100 || len(payload.Breakdown) != 4 {
		t.Fatalf("safe available payload = %#v", payload)
	}
	if payload.Breakdown[0].Kind != "AVAILABLE_BALANCE" || payload.Breakdown[1].Kind != "INCOME" || payload.Breakdown[2].Kind != "EXPENSE" || payload.Breakdown[3].Kind != "COMMITMENT" {
		t.Fatalf("breakdown order = %#v", payload.Breakdown)
	}
	if len(payload.MissingData) != 1 || payload.MissingData[0] != "BUDGET" {
		t.Fatalf("missing data = %#v", payload.MissingData)
	}
	if reader.calls != 1 || reader.query.OwnerID != owner || !reader.query.Period.StartOn().Equal(period.StartOn()) || !reader.query.Period.EndOn().Equal(period.EndOn()) {
		t.Fatalf("reader calls/query = %d/%#v", reader.calls, reader.query)
	}
}

func TestSafeAvailableHTTPReturnsEmptyAndNegativeResults(t *testing.T) {
	owner := "usr_safe_available_http_empty"
	period := safeHTTPPeriod(t)
	zero, err := domain.NewMoney(0, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	zeroSnapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: owner, Period: period, AvailableBalance: zero})
	if err != nil {
		t.Fatal(err)
	}
	reader := &safeAvailableHTTPReader{snapshot: zeroSnapshot}
	useCase := safeHTTPUseCase(t, reader)
	mux := http.NewServeMux()
	httpapi.NewSafeAvailable(owner, useCase).Register(mux)
	response := serveSafeAvailable(t, mux, http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"finalAmount":{"minor":0,"currency":"BRL"}`) {
		t.Fatalf("empty response = %d %s", response.Code, response.Body.String())
	}

	negativeMoney, err := domain.NewMoney(-50, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	negativeSnapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{OwnerID: owner, Period: period, AvailableBalance: negativeMoney})
	if err != nil {
		t.Fatal(err)
	}
	reader.snapshot = negativeSnapshot
	response = serveSafeAvailable(t, mux, http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30", "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"finalAmount":{"minor":-50,"currency":"BRL"}`) {
		t.Fatalf("negative response = %d %s", response.Code, response.Body.String())
	}
}

func TestSafeAvailableHTTPRejectsInvalidQueriesAndBodiesBeforeApplication(t *testing.T) {
	reader := &safeAvailableHTTPReader{}
	useCase := safeHTTPUseCase(t, reader)
	mux := http.NewServeMux()
	httpapi.NewSafeAvailable("usr_safe_available_http", useCase).Register(mux)
	queries := []string{
		"", "periodStart=2026-09-01", "periodEnd=2026-09-30", "periodStart=2026-09-01&periodEnd=",
		"periodStart=2026-09-01&periodEnd=2026-08-31", "periodStart=2026-02-30&periodEnd=2026-09-30",
		"periodStart=2026-09-01&periodEnd=2026-09-30&owner=spoofed", "periodStart=2026-09-01&periodEnd=2026-09-30&userId=spoofed",
		"periodStart=2026-09-01&periodStart=2026-09-01&periodEnd=2026-09-30", "periodStart=2026-09-01&periodEnd=2026-09-30&foo=bar", "periodStart=2026-09-01&periodEnd=2026-09-30&", "periodStart=2026-09-01;periodEnd=2026-09-30", "periodStart=%ZZ&periodEnd=2026-09-30",
	}
	for _, query := range queries {
		response := serveSafeAvailable(t, mux, http.MethodGet, "/v1/safe-available"+safeQuerySeparator(query), "", nil)
		assertSafeAvailableError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
	}
	response := serveSafeAvailable(t, mux, http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30", `{"unexpected":true}`, nil)
	assertSafeAvailableError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
	if reader.calls != 0 {
		t.Fatalf("invalid requests reached application: %d", reader.calls)
	}
}

func TestSafeAvailableHTTPRejectsUnsupportedMethodsBeforeApplication(t *testing.T) {
	reader := &safeAvailableHTTPReader{}
	useCase := safeHTTPUseCase(t, reader)
	mux := http.NewServeMux()
	httpapi.NewSafeAvailable("usr_safe_available_http", useCase).Register(mux)
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace, http.MethodConnect} {
		response := serveSafeAvailable(t, mux, method, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30", `{"unexpected":true}`, nil)
		assertSafeAvailableError(t, response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	}
	if reader.calls != 0 {
		t.Fatalf("unsupported methods reached application: %d", reader.calls)
	}
}

func TestSafeAvailableHTTPSanitizesDependencyErrors(t *testing.T) {
	reader := &safeAvailableHTTPReader{err: errors.New("SQL DSN owner secret")}
	useCase := safeHTTPUseCase(t, reader)
	mux := http.NewServeMux()
	httpapi.NewSafeAvailable("usr_safe_available_http", useCase).Register(mux)
	response := serveSafeAvailable(t, mux, http.MethodGet, "/v1/safe-available?periodStart=2026-09-01&periodEnd=2026-09-30", "", nil)
	assertSafeAvailableError(t, response, http.StatusInternalServerError, "INTERNAL_ERROR")
	if strings.Contains(response.Body.String(), "SQL DSN owner secret") {
		t.Fatal("dependency detail leaked")
	}
}

type amountPayload struct {
	Minor    int64  `json:"minor"`
	Currency string `json:"currency"`
}

func safeHTTPUseCase(t *testing.T, reader application.SafeAvailableSnapshotReader) *application.CalculateSafeAvailable {
	t.Helper()
	useCase, err := application.NewCalculateSafeAvailable(reader)
	if err != nil {
		t.Fatal(err)
	}
	return useCase
}

func safeHTTPPeriod(t *testing.T) domain.SafeAvailablePeriod {
	t.Helper()
	start, err := domain.NewCivilDate(2026, time.September, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := domain.NewCivilDate(2026, time.September, 30)
	if err != nil {
		t.Fatal(err)
	}
	period, err := domain.NewSafeAvailablePeriod(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return period
}

func safeHTTPSnapshot(t *testing.T, owner string, period domain.SafeAvailablePeriod) domain.SafeAvailableSnapshot {
	t.Helper()
	date := period.StartOn()
	entry := func(kind domain.SafeAvailableEntryKind, id string, sequence int, minor int64) domain.SafeAvailableEntry {
		amount, err := domain.NewMoney(minor, domain.CurrencyBRL)
		if err != nil {
			t.Fatal(err)
		}
		value, err := domain.NewSafeAvailableEntry(domain.SafeAvailableEntryParams{Kind: kind, OwnerID: owner, SourceID: id, Sequence: sequence, DueOn: date, Amount: amount})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	balance, err := domain.NewMoney(100, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewSafeAvailableSnapshot(domain.SafeAvailableSnapshotParams{
		OwnerID: owner, Period: period, AvailableBalance: balance,
		Incomes:     []domain.SafeAvailableEntry{entry(domain.SafeAvailableEntryKindIncome, "income-http", 0, 30)},
		Expenses:    []domain.SafeAvailableEntry{entry(domain.SafeAvailableEntryKindExpense, "expense-http", 0, 20)},
		Commitments: []domain.SafeAvailableEntry{entry(domain.SafeAvailableEntryKindCommitment, "ipl_0123456789abcdef0123456789abcdef", 1, 10)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func serveSafeAvailable(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func safeQuerySeparator(query string) string {
	if query == "" {
		return ""
	}
	return "?" + query
}

func assertSafeAvailableHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", response.Header())
	}
}

func assertSafeAvailableError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("response = %d %s, want %d/%s", response.Code, response.Body.String(), status, code)
	}
	assertSafeAvailableHeaders(t, response)
}
