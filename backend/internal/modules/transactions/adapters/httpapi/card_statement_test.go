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

type cardStatementHTTPReader struct {
	snapshot application.CardStatementSnapshot
	err      error
	calls    int
	query    application.CardStatementSnapshotQuery
}

func (reader *cardStatementHTTPReader) Read(_ context.Context, query application.CardStatementSnapshotQuery) (application.CardStatementSnapshot, error) {
	reader.calls++
	reader.query = query
	return reader.snapshot, reader.err
}

func TestCardStatementHTTPReturnsDeterministicMixedStatementAndServerOwner(t *testing.T) {
	const owner = "usr_statement_http"
	card := httpStatementCard(t, owner, false)
	dueOn := httpStatementDate(t, 2026, time.June, 10)
	oneTime := httpStatementExpense(t, owner, card.ID(), "expense-http-one-time", "Mercado", 500, dueOn, time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC))
	installmentExpense := httpStatementExpense(t, owner, card.ID(), "expense-http-installment", "Notebook", 3000, httpStatementDate(t, 2026, time.May, 10), time.Date(2026, time.May, 3, 10, 0, 0, 0, time.UTC))
	plan := httpStatementPlan(t, owner, card.ID(), installmentExpense.ID(), "ipl_0123456789abcdef0123456789abcdef", 3000, 3, httpStatementDate(t, 2026, time.May, 10))
	reader := &cardStatementHTTPReader{snapshot: application.CardStatementSnapshot{
		CreditCard: card, Expenses: []domain.Expense{installmentExpense, oneTime}, InstallmentPlans: []domain.InstallmentPlan{plan}, Found: true,
	}}
	useCase, err := application.NewGetCardStatement(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewCardStatement(owner, useCase).Register(mux)

	request := httptest.NewRequest(http.MethodGet, "/v1/credit-cards/"+card.ID()+"/statements/2026-06-10", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	assertStatementHTTPHeaders(t, response)
	var payload struct {
		CreditCardID   string `json:"creditCardId"`
		StatementDueOn string `json:"statementDueOn"`
		TotalAmount    struct {
			Minor    int64  `json:"minor"`
			Currency string `json:"currency"`
		} `json:"totalAmount"`
		Lines []struct {
			ExpenseID   string `json:"expenseId"`
			Description string `json:"description"`
			Amount      struct {
				Minor    int64  `json:"minor"`
				Currency string `json:"currency"`
			} `json:"amount"`
			OccurredAt        string `json:"occurredAt"`
			PurchaseMode      string `json:"purchaseMode"`
			InstallmentNumber *int   `json:"installmentNumber"`
			InstallmentCount  *int   `json:"installmentCount"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CreditCardID != card.ID() || payload.StatementDueOn != "2026-06-10" || payload.TotalAmount.Minor != 1500 || payload.TotalAmount.Currency != "BRL" || len(payload.Lines) != 2 {
		t.Fatalf("statement payload = %#v", payload)
	}
	if payload.Lines[0].ExpenseID != oneTime.ID() || payload.Lines[0].PurchaseMode != "ONE_TIME" || payload.Lines[0].InstallmentNumber != nil || payload.Lines[0].InstallmentCount != nil || payload.Lines[0].OccurredAt != "2026-05-02" {
		t.Fatalf("one-time line = %#v", payload.Lines[0])
	}
	if payload.Lines[1].ExpenseID != installmentExpense.ID() || payload.Lines[1].PurchaseMode != "INSTALLMENT" || payload.Lines[1].Amount.Minor != 1000 || payload.Lines[1].OccurredAt != "2026-05-03" || payload.Lines[1].InstallmentNumber == nil || *payload.Lines[1].InstallmentNumber != 2 || payload.Lines[1].InstallmentCount == nil || *payload.Lines[1].InstallmentCount != 3 {
		t.Fatalf("installment line = %#v", payload.Lines[1])
	}
	if reader.calls != 1 || reader.query.OwnerID != owner || reader.query.CreditCardID != card.ID() || !reader.query.StatementDueOn.Equal(dueOn) {
		t.Fatalf("reader calls/query = %d/%#v", reader.calls, reader.query)
	}
}

func TestCardStatementHTTPReturnsEmptyStatementAndAllowsArchivedCard(t *testing.T) {
	const owner = "usr_statement_http_empty"
	card := httpStatementCard(t, owner, true)
	reader := &cardStatementHTTPReader{snapshot: application.CardStatementSnapshot{CreditCard: card, Expenses: []domain.Expense{}, InstallmentPlans: []domain.InstallmentPlan{}, Found: true}}
	useCase, err := application.NewGetCardStatement(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewCardStatement(owner, useCase).Register(mux)
	response := statementHTTPServe(t, mux, http.MethodGet, "/v1/credit-cards/"+card.ID()+"/statements/2026-09-10", "", nil)
	if response.Code != http.StatusOK || response.Body.String() != `{"creditCardId":"`+card.ID()+`","statementDueOn":"2026-09-10","totalAmount":{"minor":0,"currency":"BRL"},"lines":[]}`+"\n" {
		t.Fatalf("empty statement = %d %s", response.Code, response.Body.String())
	}
	assertStatementHTTPHeaders(t, response)
}

func TestCardStatementHTTPRejectsInvalidBoundaryBeforeApplication(t *testing.T) {
	reader := &cardStatementHTTPReader{}
	useCase, err := application.NewGetCardStatement(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewCardStatement("usr_statement_http", useCase).Register(mux)
	validPath := "/v1/credit-cards/card_0123456789abcdef0123456789abcdef/statements/2026-09-10"
	for _, test := range []struct {
		name, method, path, body string
	}{
		{"query", http.MethodGet, validPath + "?owner=spoofed", ""},
		{"body", http.MethodGet, validPath, `{"unexpected":true}`},
		{"invalid card", http.MethodGet, "/v1/credit-cards/not-a-card/statements/2026-09-10", ""},
		{"invalid date", http.MethodGet, "/v1/credit-cards/card_0123456789abcdef0123456789abcdef/statements/2026-02-30", ""},
		{"missing date", http.MethodGet, "/v1/credit-cards/card_0123456789abcdef0123456789abcdef/statements/", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := statementHTTPServe(t, mux, test.method, test.path, test.body, nil)
			assertStatementError(t, response, http.StatusBadRequest, "INVALID_REQUEST")
		})
	}
	if reader.calls != 0 {
		t.Fatalf("invalid requests reached application: %d", reader.calls)
	}
}

func TestCardStatementHTTPRejectsUnsupportedMethodsBeforeApplication(t *testing.T) {
	reader := &cardStatementHTTPReader{}
	useCase, err := application.NewGetCardStatement(reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	httpapi.NewCardStatement("usr_statement_http", useCase).Register(mux)
	path := "/v1/credit-cards/card_0123456789abcdef0123456789abcdef/statements/2026-09-10"
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodTrace, http.MethodConnect} {
		response := statementHTTPServe(t, mux, method, path, `{"unexpected":true}`, nil)
		assertStatementError(t, response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
	}
	if reader.calls != 0 {
		t.Fatalf("unsupported methods reached application: %d", reader.calls)
	}
}

func TestCardStatementHTTPSanitizesNotFoundAndDependencyErrors(t *testing.T) {
	const owner = "usr_statement_http_errors"
	card := httpStatementCard(t, owner, false)
	tests := []struct {
		name       string
		snapshot   application.CardStatementSnapshot
		readerErr  error
		statusCode int
		errorCode  string
	}{
		{"not found", application.CardStatementSnapshot{Expenses: []domain.Expense{}, InstallmentPlans: []domain.InstallmentPlan{}}, nil, http.StatusNotFound, "CREDIT_CARD_NOT_FOUND"},
		{"internal", application.CardStatementSnapshot{}, errors.New("SQL secret statement"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &cardStatementHTTPReader{snapshot: test.snapshot, err: test.readerErr}
			useCase, err := application.NewGetCardStatement(reader)
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			httpapi.NewCardStatement(owner, useCase).Register(mux)
			response := statementHTTPServe(t, mux, http.MethodGet, "/v1/credit-cards/"+card.ID()+"/statements/2026-09-10", "", nil)
			assertStatementError(t, response, test.statusCode, test.errorCode)
			if strings.Contains(response.Body.String(), "SQL secret statement") {
				t.Fatal("internal error leaked dependency detail")
			}
		})
	}
}

func httpStatementCard(t *testing.T, owner string, archived bool) domain.CreditCard {
	t.Helper()
	name, err := domain.NewCreditCardName("Cartão HTTP")
	if err != nil {
		t.Fatal(err)
	}
	closing, err := domain.NewDayOfMonthAnchor(5)
	if err != nil {
		t.Fatal(err)
	}
	due, err := domain.NewDayOfMonthAnchor(10)
	if err != nil {
		t.Fatal(err)
	}
	card, err := domain.NewCreditCard(domain.CreditCardParams{ID: "card_0123456789abcdef0123456789abcdef", Details: domain.CreditCardDetails{UserID: owner, Name: name, ClosingDayAnchor: closing, DueDayAnchor: due}, CreatedAt: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if archived {
		card, err = card.Archive(time.Date(2026, time.February, 1, 12, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
	}
	return card
}

func httpStatementExpense(t *testing.T, owner, cardID, id, description string, amountMinor int64, dueOn domain.CivilDate, occurredAt time.Time) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(amountMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{ID: id, Details: domain.ExpenseDetails{UserID: owner, Description: description, Amount: amount, PaymentMethod: domain.PaymentMethodCredit, CreditCardID: &cardID, StatementDueOn: &dueOn, OccurredAt: occurredAt, FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS}, CreatedAt: time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return expense
}

func httpStatementPlan(t *testing.T, owner, cardID, expenseID, planID string, totalMinor int64, count int, firstDue domain.CivilDate) domain.InstallmentPlan {
	t.Helper()
	amount, err := domain.NewMoney(totalMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := domain.NewDayOfMonthAnchor(firstDue.Day())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewInstallmentPlan(domain.InstallmentPlanParams{ID: planID, OwnerID: owner, CreditCardID: cardID, ExpenseID: expenseID, TotalAmount: amount, InstallmentCount: count, FirstDueDate: firstDue, DueDayAnchor: anchor, CreatedAt: time.Date(2026, time.January, 3, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func httpStatementDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func statementHTTPServe(t *testing.T, handler http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertStatementHTTPHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", response.Header())
	}
}

func assertStatementError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || !strings.Contains(response.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("response = %d %s, want %d/%s", response.Code, response.Body.String(), status, code)
	}
	assertStatementHTTPHeaders(t, response)
}
