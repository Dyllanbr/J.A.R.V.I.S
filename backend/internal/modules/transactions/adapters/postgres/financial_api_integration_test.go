//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestPGXTimestamptzRoundTripTruncatesToMicroseconds(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tests := []struct {
		name                string
		inputNanoseconds    int
		expectedNanoseconds int
	}{
		{name: "123ns", inputNanoseconds: 123, expectedNanoseconds: 0},
		{name: "499ns", inputNanoseconds: 499, expectedNanoseconds: 0},
		{name: "500ns", inputNanoseconds: 500, expectedNanoseconds: 0},
		{name: "501ns", inputNanoseconds: 501, expectedNanoseconds: 0},
		{name: "999ns", inputNanoseconds: 999, expectedNanoseconds: 0},
		{name: "nine digits", inputNanoseconds: 123_456_789, expectedNanoseconds: 123_456_000},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := time.Date(2026, time.August, 14, 15, 0, 0, test.inputNanoseconds, time.UTC)
			var stored time.Time
			if err := pool.QueryRow(ctx, "SELECT $1::timestamptz", input).Scan(&stored); err != nil {
				t.Fatal("TIMESTAMPTZ round trip failed")
			}
			if stored.Nanosecond() != test.expectedNanoseconds {
				t.Fatalf("stored nanoseconds = %d, want %d", stored.Nanosecond(), test.expectedNanoseconds)
			}
		})
	}
}

func TestIdempotentExpenseFirstWriteReplayAndConflict(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, &sequenceIDGenerator{prefix: "exp_idem"})
	input := integrationRecordInput(syntheticUserID, "idem-synthetic-first")

	created, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if created.Replayed {
		t.Fatal("first command was reported as replay")
	}
	replayed, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("replay Execute() error = %v", err)
	}
	if !replayed.Replayed || replayed.Expense.ID() != created.Expense.ID() {
		t.Fatal("replay did not load the original PostgreSQL resource")
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)

	conflictInput := input
	conflictInput.Expense.Description = "Transporte teste"
	if _, err := useCase.Execute(ctx, conflictInput); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("conflict Execute() error = %v, want ErrIdempotencyConflict", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)

	var (
		fingerprintLength int
		state             string
		transactionID     string
		completedAt       time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT octet_length(request_fingerprint), state, transaction_id, completed_at
		FROM idempotency_records
		WHERE user_id = $1 AND operation = 'CREATE_EXPENSE' AND idempotency_key = $2
	`, syntheticUserID, input.IdempotencyKey).Scan(&fingerprintLength, &state, &transactionID, &completedAt); err != nil {
		t.Fatal("idempotency metadata lookup failed")
	}
	if fingerprintLength != 32 || state != "COMPLETED" || transactionID != created.Expense.ID() || completedAt.IsZero() {
		t.Fatal("idempotency metadata is incomplete or not linked to the original transaction")
	}
}

func TestIdempotencyUsesCanonicalMicrosecondInstant(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, &sequenceIDGenerator{prefix: "exp_precision"})
	input := integrationRecordInput(syntheticUserID, "idem-canonical-precision")
	input.Expense.OccurredAt = time.Date(2026, time.August, 14, 15, 0, 0, 123, time.UTC)

	created, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if created.Expense.OccurredAt().Nanosecond() != 0 {
		t.Fatal("created Expense retained discarded sub-microsecond precision")
	}

	equivalent := input
	equivalent.Expense.OccurredAt = time.Date(2026, time.August, 14, 15, 0, 0, 999, time.UTC)
	replayed, err := useCase.Execute(ctx, equivalent)
	if err != nil {
		t.Fatalf("equivalent replay Execute() error = %v", err)
	}
	if !replayed.Replayed || replayed.Expense.ID() != created.Expense.ID() || replayed.Expense.OccurredAt() != created.Expense.OccurredAt() {
		t.Fatal("equivalent sub-microsecond request did not replay the canonical resource")
	}

	different := input
	different.Expense.OccurredAt = time.Date(2026, time.August, 14, 15, 0, 0, 1_000, time.UTC)
	if _, err := useCase.Execute(ctx, different); !errors.Is(err, application.ErrIdempotencyConflict) {
		t.Fatalf("different canonical instant error = %v, want ErrIdempotencyConflict", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestFinancialAPIReplayAfterRestartReturnsIdenticalCanonicalResource(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	requestBody := []byte(`{"type":"EXPENSE","description":"  Reinício sintético  ","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"2026-08-14T15:00:00.000000123Z"}`)
	serverA := newFinancialIntegrationServer(
		t,
		pool,
		fixedIntegrationIDGenerator{id: "exp_cross_restart"},
		fixedIncomeIntegrationIDGenerator{id: "inc_unused_expense_server_a"},
		fixedFinancialClock{now: time.Date(2026, time.August, 14, 18, 0, 0, 123_456_789, time.UTC)},
	)
	defer serverA.Close()

	preview := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/transactions/preview", requestBody, "")
	if preview.status != http.StatusOK || jsonStringField(t, preview.body, "occurredAt") != "2026-08-14T15:00:00Z" {
		t.Fatalf("preview response = %d %s", preview.status, preview.body)
	}
	assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)

	created := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/transactions", requestBody, "idem-cross-restart")
	if created.status != http.StatusCreated || created.header.Get("Idempotency-Replayed") != "" {
		t.Fatalf("create response = %d headers=%v body=%s", created.status, created.header, created.body)
	}
	if jsonStringField(t, created.body, "occurredAt") != "2026-08-14T15:00:00Z" ||
		jsonStringField(t, created.body, "createdAt") != "2026-08-14T18:00:00.123456Z" {
		t.Fatalf("create response is not canonical: %s", created.body)
	}
	serverA.Close()

	poolB, err := pgxpool.New(ctx, pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal("opening independent replay pool failed")
	}
	defer poolB.Close()
	if err := poolB.Ping(ctx); err != nil {
		t.Fatal("independent replay pool readiness failed")
	}
	serverB := newFinancialIntegrationServer(
		t,
		poolB,
		fixedIntegrationIDGenerator{id: "exp_unused_after_restart"},
		fixedIncomeIntegrationIDGenerator{id: "inc_unused_expense_server_b"},
		fixedFinancialClock{now: time.Date(2026, time.August, 15, 12, 0, 0, 987_654_321, time.UTC)},
	)
	defer serverB.Close()

	replayed := postFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/transactions", requestBody, "idem-cross-restart")
	if replayed.status != http.StatusCreated || replayed.header.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay response = %d headers=%v body=%s", replayed.status, replayed.header, replayed.body)
	}
	if !bytes.Equal(created.body, replayed.body) {
		t.Fatalf("cross-restart bodies differ:\ncreated=%s\nreplayed=%s", created.body, replayed.body)
	}
	if jsonStringField(t, created.body, "id") != jsonStringField(t, replayed.body, "id") {
		t.Fatal("cross-restart replay changed the resource ID")
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIncomeFinancialAPIReplayAfterRestartReturnsIdenticalCanonicalResource(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	requestBody := []byte(`{"type":"INCOME","description":"  Salário reinício sintético  ","amount":{"minor":725000,"currency":"BRL"},"occurredAt":"2026-08-14T15:00:00.123456789Z"}`)
	serverA := newFinancialIntegrationServer(
		t,
		pool,
		fixedIntegrationIDGenerator{id: "exp_unused_income_server_a"},
		fixedIncomeIntegrationIDGenerator{id: "inc_http_cross_restart"},
		fixedFinancialClock{now: time.Date(2026, time.August, 14, 18, 0, 0, 987_654_321, time.UTC)},
	)
	defer serverA.Close()

	preview := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/transactions/preview", requestBody, "")
	if preview.status != http.StatusOK || jsonStringField(t, preview.body, "occurredAt") != "2026-08-14T15:00:00.123456Z" ||
		bytes.Contains(preview.body, []byte(`"paymentMethod"`)) {
		t.Fatalf("Income preview response = %d %s", preview.status, preview.body)
	}
	assertIncomeFinancialRowCounts(t, ctx, pool, 0, 0, 0)

	created := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/transactions", requestBody, "income-http-cross-restart")
	if created.status != http.StatusCreated || created.header.Get("Idempotency-Replayed") != "" ||
		bytes.Contains(created.body, []byte(`"paymentMethod"`)) {
		t.Fatalf("Income create response = %d headers=%v body=%s", created.status, created.header, created.body)
	}
	if jsonStringField(t, created.body, "occurredAt") != "2026-08-14T15:00:00.123456Z" ||
		jsonStringField(t, created.body, "createdAt") != "2026-08-14T18:00:00.987654Z" {
		t.Fatalf("Income create response is not canonical: %s", created.body)
	}
	serverA.Close()

	poolB, err := pgxpool.New(ctx, pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatal("opening independent Income replay pool failed")
	}
	defer poolB.Close()
	if err := poolB.Ping(ctx); err != nil {
		t.Fatal("independent Income replay pool readiness failed")
	}
	serverB := newFinancialIntegrationServer(
		t,
		poolB,
		fixedIntegrationIDGenerator{id: "exp_unused_income_server_b"},
		fixedIncomeIntegrationIDGenerator{id: "inc_unused_after_http_restart"},
		fixedFinancialClock{now: time.Date(2026, time.August, 15, 12, 0, 0, 123_456_789, time.UTC)},
	)
	defer serverB.Close()

	replayed := postFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/transactions", requestBody, "income-http-cross-restart")
	if replayed.status != http.StatusCreated || replayed.header.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("Income replay response = %d headers=%v body=%s", replayed.status, replayed.header, replayed.body)
	}
	if !bytes.Equal(created.body, replayed.body) {
		t.Fatalf("Income cross-restart bodies differ:\ncreated=%s\nreplayed=%s", created.body, replayed.body)
	}
	assertIncomeFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestCategorizedFinancialAPIUsesRealCatalogPersistenceAndHistory(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	server := newFinancialIntegrationServer(
		t,
		pool,
		&sequenceIDGenerator{prefix: "exp_http_category"},
		&sequenceIncomeIDGenerator{prefix: "inc_http_category"},
		fixedFinancialClock{now: time.Date(2026, time.August, 14, 18, 0, 0, 123_456_789, time.UTC)},
	)
	defer server.Close()

	categories := getFinancialJSON(t, server.Client(), server.URL+"/v1/categories")
	if categories.status != http.StatusOK {
		t.Fatalf("GET categories = %d %s", categories.status, categories.body)
	}
	var catalog []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(categories.body, &catalog); err != nil {
		t.Fatalf("decode real catalog: %v", err)
	}
	if len(catalog) != 17 || catalog[0].ID != "expense.food" || catalog[9].ID != "expense.other" ||
		catalog[10].ID != "income.salary" || catalog[16].ID != "income.other" {
		t.Fatalf("real catalog ordering/content = %s", categories.body)
	}

	expenseBody := []byte(`{"type":"EXPENSE","description":"Despesa categorizada HTTP","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","categoryId":"expense.food","occurredAt":"2026-08-14T15:00:00.000000123Z"}`)
	preview := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions/preview", expenseBody, "")
	if preview.status != http.StatusOK || jsonStringField(t, preview.body, "categoryId") != "expense.food" {
		t.Fatalf("categorized Expense preview = %d %s", preview.status, preview.body)
	}
	assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)

	createdExpense := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", expenseBody, "http-expense-category")
	if createdExpense.status != http.StatusCreated || jsonStringField(t, createdExpense.body, "categoryId") != "expense.food" {
		t.Fatalf("categorized Expense create = %d %s", createdExpense.status, createdExpense.body)
	}
	replayedExpense := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", expenseBody, "http-expense-category")
	if replayedExpense.status != http.StatusCreated || replayedExpense.header.Get("Idempotency-Replayed") != "true" ||
		!bytes.Equal(createdExpense.body, replayedExpense.body) {
		t.Fatalf("categorized Expense replay differs: first=%s replay=%s", createdExpense.body, replayedExpense.body)
	}
	differentCategory := bytes.Replace(expenseBody, []byte("expense.food"), []byte("expense.transport"), 1)
	conflict := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", differentCategory, "http-expense-category")
	if conflict.status != http.StatusConflict {
		t.Fatalf("Category A/B conflict = %d %s", conflict.status, conflict.body)
	}

	incomeBody := []byte(`{"type":"INCOME","description":"Receita categorizada HTTP","amount":{"minor":725000,"currency":"BRL"},"categoryId":"income.salary","occurredAt":"2026-08-14T16:00:00.123456789Z"}`)
	createdIncome := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", incomeBody, "http-income-category")
	if createdIncome.status != http.StatusCreated || jsonStringField(t, createdIncome.body, "categoryId") != "income.salary" ||
		bytes.Contains(createdIncome.body, []byte(`"paymentMethod"`)) {
		t.Fatalf("categorized Income create = %d %s", createdIncome.status, createdIncome.body)
	}

	uncategorizedBody := []byte(`{"type":"EXPENSE","description":"Despesa sem categoria HTTP","amount":{"minor":4250,"currency":"BRL"},"paymentMethod":"PIX","occurredAt":"2026-08-14T17:00:00Z"}`)
	uncategorized := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", uncategorizedBody, "http-uncategorized-first")
	if uncategorized.status != http.StatusCreated || bytes.Contains(uncategorized.body, []byte(`"categoryId"`)) {
		t.Fatalf("legacy uncategorized create = %d %s", uncategorized.status, uncategorized.body)
	}
	uncategorizedToCategorized := bytes.Replace(uncategorizedBody, []byte(`,"occurredAt"`), []byte(`,"categoryId":"expense.food","occurredAt"`), 1)
	if changed := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", uncategorizedToCategorized, "http-uncategorized-first"); changed.status != http.StatusConflict {
		t.Fatalf("uncategorized/category conflict = %d %s", changed.status, changed.body)
	}

	categorizedFirstBody := bytes.Replace(uncategorizedBody, []byte("Despesa sem categoria HTTP"), []byte("Despesa categoria removida HTTP"), 1)
	categorizedFirstBody = bytes.Replace(categorizedFirstBody, []byte(`,"occurredAt"`), []byte(`,"categoryId":"expense.food","occurredAt"`), 1)
	categorizedFirst := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", categorizedFirstBody, "http-categorized-first")
	if categorizedFirst.status != http.StatusCreated {
		t.Fatalf("categorized-first create = %d %s", categorizedFirst.status, categorizedFirst.body)
	}
	categoryRemoved := bytes.Replace(categorizedFirstBody, []byte(`,"categoryId":"expense.food"`), nil, 1)
	if changed := postFinancialJSON(t, server.Client(), server.URL+"/v1/transactions", categoryRemoved, "http-categorized-first"); changed.status != http.StatusConflict {
		t.Fatalf("categorized/uncategorized conflict = %d %s", changed.status, changed.body)
	}

	assertCategorizedHTTPPostcondition(t, ctx, pool, jsonStringField(t, createdExpense.body, "id"),
		"EXPENSE", "expense.food", "PIX", "EXPENSE_RECORDED", "CREATE_EXPENSE")
	assertCategorizedHTTPPostcondition(t, ctx, pool, jsonStringField(t, createdIncome.body, "id"),
		"INCOME", "income.salary", "", "INCOME_RECORDED", "CREATE_INCOME")
	assertFinancialRowCounts(t, ctx, pool, 4, 4, 4)

	history := getFinancialJSON(t, server.Client(), server.URL+"/v1/transactions?month=2026-08")
	if history.status != http.StatusOK || !bytes.Contains(history.body, []byte(`"categoryId":"expense.food"`)) ||
		!bytes.Contains(history.body, []byte(`"categoryId":"income.salary"`)) {
		t.Fatalf("categorized mixed history = %d %s", history.status, history.body)
	}
}

func assertCategorizedHTTPPostcondition(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	transactionID string,
	wantedType string,
	wantedCategory string,
	wantedPaymentMethod string,
	wantedEvent string,
	wantedOperation string,
) {
	t.Helper()
	var transactionType, categoryID, eventType, operation, state string
	var paymentMethod *string
	if err := pool.QueryRow(ctx, `
		SELECT t.type, t.category_id, t.payment_method, a.event_type, i.operation, i.state
		FROM transactions t
		JOIN audit_events a ON a.user_id = t.user_id AND a.aggregate_id = t.id
		JOIN idempotency_records i ON i.user_id = t.user_id AND i.transaction_id = t.id
		WHERE t.id = $1
	`, transactionID).Scan(&transactionType, &categoryID, &paymentMethod, &eventType, &operation, &state); err != nil {
		t.Fatalf("categorized HTTP postcondition query: %v", err)
	}
	storedPaymentMethod := ""
	if paymentMethod != nil {
		storedPaymentMethod = *paymentMethod
	}
	if transactionType != wantedType || categoryID != wantedCategory || storedPaymentMethod != wantedPaymentMethod ||
		eventType != wantedEvent || operation != wantedOperation || state != "COMPLETED" {
		t.Fatalf("categorized HTTP postcondition = %s/%s/%s/%s/%s/%s", transactionType, categoryID,
			storedPaymentMethod, eventType, operation, state)
	}
}

func TestPreviewExpenseLeavesPostgresUnchanged(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	result, err := (application.PreviewExpense{}).Execute(ctx, integrationRecordInput(
		syntheticUserID,
		"unused-preview-key",
	).Expense)
	if err != nil {
		t.Fatalf("preview Execute() error = %v", err)
	}
	if result.Details.Description != "Mercado sintético" {
		t.Fatal("preview did not return canonical synthetic details")
	}
	assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)
}

func TestDatabaseIdempotencyKeyConstraints(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	now := time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)
	fingerprint := make([]byte, 32)

	insert := func(key string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO idempotency_records (
				user_id, operation, idempotency_key, request_fingerprint,
				state, transaction_id, created_at, completed_at
			) VALUES ($1, 'CREATE_EXPENSE', $2, $3, 'PENDING', NULL, $4, NULL)
		`, syntheticUserID, key, fingerprint, now)
		return err
	}

	for _, key := range []string{"a", strings.Repeat("x", 128), "synthetic-key:ABC_123"} {
		if err := insert(key); err != nil {
			t.Fatal("valid idempotency key was rejected")
		}
	}
	for _, key := range []string{
		strings.Repeat("x", 129),
		" leading",
		"trailing ",
		"internal space",
		"line\nfeed",
		"não-ascii",
	} {
		if err := insert(key); err == nil {
			t.Fatal("invalid idempotency key was accepted")
		}
	}
}

func TestIdempotentExpenseIsConcurrencySafeForSamePayload(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, &sequenceIDGenerator{prefix: "exp_concurrent_same"})
	input := integrationRecordInput(syntheticUserID, "idem-concurrent-same")

	const requests = 8
	start := make(chan struct{})
	results := make(chan application.RecordExpenseResult, requests)
	errorsChannel := make(chan error, requests)
	var wait sync.WaitGroup
	for range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := useCase.Execute(ctx, input)
			results <- result
			errorsChannel <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsChannel)

	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent Execute() error = %v", err)
		}
	}
	ids := map[string]struct{}{}
	newWrites := 0
	for result := range results {
		ids[result.Expense.ID()] = struct{}{}
		if !result.Replayed {
			newWrites++
		}
	}
	if len(ids) != 1 || newWrites != 1 {
		t.Fatalf("concurrent results unique IDs/new writes = %d/%d, want 1/1", len(ids), newWrites)
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIdempotentExpenseIsConcurrencySafeForDifferentPayloads(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, &sequenceIDGenerator{prefix: "exp_concurrent_different"})
	first := integrationRecordInput(syntheticUserID, "idem-concurrent-different")
	second := first
	second.Expense.Description = "Restaurante QA"

	start := make(chan struct{})
	errorsChannel := make(chan error, 2)
	var wait sync.WaitGroup
	for _, input := range []application.RecordExpenseInput{first, second} {
		input := input
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := useCase.Execute(ctx, input)
			errorsChannel <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsChannel)

	successes, conflicts := 0, 0
	for err := range errorsChannel {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, application.ErrIdempotencyConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success/conflict = %d/%d, want 1/1", successes, conflicts)
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIdempotencyRollsBackAndAllowsRetryAfterAuditFailure(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	const expenseID = "exp_idempotency_retry"
	if _, err := pool.Exec(ctx, `
		ALTER TABLE audit_events
		ADD CONSTRAINT audit_events_idempotency_synthetic_failure
		CHECK (aggregate_id <> 'exp_idempotency_retry')
	`); err != nil {
		t.Fatal("installing synthetic audit constraint failed")
	}

	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: expenseID})
	input := integrationRecordInput(syntheticUserID, "idem-retry-after-rollback")
	if _, err := useCase.Execute(ctx, input); !errors.Is(err, adapter.ErrInsertAuditEvent) {
		t.Fatalf("first Execute() error = %v, want audit insert failure", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)

	if _, err := pool.Exec(ctx, `
		ALTER TABLE audit_events DROP CONSTRAINT audit_events_idempotency_synthetic_failure
	`); err != nil {
		t.Fatal("removing synthetic audit constraint failed")
	}
	result, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("retry Execute() error = %v", err)
	}
	if result.Replayed {
		t.Fatal("retry after rollback was incorrectly classified as replay")
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIdempotencyRollsBackAndAllowsRetryAfterExpenseFailure(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	if _, err := pool.Exec(ctx, `
		ALTER TABLE transactions
		ADD CONSTRAINT transactions_idempotency_synthetic_failure
		CHECK (id <> 'exp_idempotency_expense_failure')
	`); err != nil {
		t.Fatal("installing synthetic expense constraint failed")
	}
	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: "exp_idempotency_expense_failure"})
	input := integrationRecordInput(syntheticUserID, "idem-retry-expense-failure")

	if _, err := useCase.Execute(ctx, input); !errors.Is(err, adapter.ErrInsertExpense) {
		t.Fatalf("first Execute() error = %v, want expense insert failure", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)

	if _, err := pool.Exec(ctx, `
		ALTER TABLE transactions DROP CONSTRAINT transactions_idempotency_synthetic_failure
	`); err != nil {
		t.Fatal("removing synthetic expense constraint failed")
	}
	if _, err := useCase.Execute(ctx, input); err != nil {
		t.Fatalf("retry Execute() error = %v", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIdempotencyDoesNotReportSuccessWhenCommitFails(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE synthetic_commit_guard (description TEXT PRIMARY KEY);
		ALTER TABLE transactions
		ADD CONSTRAINT transactions_synthetic_deferred_failure
		FOREIGN KEY (description) REFERENCES synthetic_commit_guard(description)
		DEFERRABLE INITIALLY DEFERRED;
	`); err != nil {
		t.Fatal("installing deferred synthetic commit failure failed")
	}

	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: "exp_idempotency_commit_failure"})
	input := integrationRecordInput(syntheticUserID, "idem-retry-commit-failure")
	if _, err := useCase.Execute(ctx, input); !errors.Is(err, adapter.ErrCommitTransaction) {
		t.Fatalf("Execute() error = %v, want commit failure", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 0, 0, 0)

	if _, err := pool.Exec(ctx, `
		ALTER TABLE transactions DROP CONSTRAINT transactions_synthetic_deferred_failure;
		DROP TABLE synthetic_commit_guard;
	`); err != nil {
		t.Fatal("removing deferred synthetic commit failure failed")
	}
	if _, err := useCase.Execute(ctx, input); err != nil {
		t.Fatalf("retry after commit failure error = %v", err)
	}
	assertFinancialRowCounts(t, ctx, pool, 1, 1, 1)
}

func TestIdempotencyIsScopedByOwner(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const ownerB = "usr_test_owner_idempotency_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)

	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, &sequenceIDGenerator{prefix: "exp_owner_scope"})
	for _, owner := range []string{syntheticUserID, ownerB} {
		if _, err := useCase.Execute(ctx, integrationRecordInput(owner, "shared-owner-scoped-key")); err != nil {
			t.Fatalf("Execute(%s) error = %v", owner, err)
		}
	}
	assertFinancialRowCounts(t, ctx, pool, 2, 2, 2)
}

func TestMonthlyReaderUsesOwnerAndInclusiveExclusiveBoundaries(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const ownerB = "usr_test_month_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)

	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		t.Fatal("financial timezone unavailable")
	}
	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, location).UTC()
	end := time.Date(2026, time.September, 1, 0, 0, 0, 0, location).UTC()
	repository := newRepository(t, pool)
	fixtures := []struct {
		id      string
		owner   string
		instant time.Time
	}{
		{id: "exp_month_before", owner: syntheticUserID, instant: start.Add(-time.Microsecond)},
		{id: "exp_month_start", owner: syntheticUserID, instant: start},
		{id: "exp_month_end", owner: syntheticUserID, instant: end.Add(-time.Microsecond)},
		{id: "exp_month_next", owner: syntheticUserID, instant: end},
		{id: "exp_month_other_owner", owner: ownerB, instant: start.Add(time.Hour)},
	}
	for _, fixture := range fixtures {
		if err := repository.Save(ctx, expenseAt(t, fixture.id, fixture.owner, fixture.instant)); err != nil {
			t.Fatalf("Save(%s) error = %v", fixture.id, err)
		}
	}

	useCase, err := application.NewListExpensesByMonth(repository)
	if err != nil {
		t.Fatalf("NewListExpensesByMonth() error = %v", err)
	}
	result, err := useCase.Execute(ctx, syntheticUserID, "2026-08")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	ids := make([]string, 0, len(result.Expenses))
	for _, expense := range result.Expenses {
		ids = append(ids, expense.ID())
	}
	expected := []string{"exp_month_end", "exp_month_start"}
	if fmt.Sprint(ids) != fmt.Sprint(expected) {
		t.Fatalf("monthly IDs = %v, want deterministic %v", ids, expected)
	}
}

func TestIdempotentCommandHonorsCanceledContext(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	setupContext, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	insertSyntheticUser(t, setupContext, pool, syntheticUserID)
	cancelSetup()
	repository := newRepository(t, pool)
	useCase := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: "exp_idem_canceled"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := useCase.Execute(ctx, integrationRecordInput(syntheticUserID, "idem-canceled"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
	assertContext, cancelAssert := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAssert()
	assertFinancialRowCounts(t, assertContext, pool, 0, 0, 0)
}

type fixedIntegrationIDGenerator struct{ id string }

func (generator fixedIntegrationIDGenerator) NewExpenseID() (string, error) { return generator.id, nil }

type sequenceIDGenerator struct {
	prefix string
	next   atomic.Uint64
}

func (generator *sequenceIDGenerator) NewExpenseID() (string, error) {
	return fmt.Sprintf("%s_%03d", generator.prefix, generator.next.Add(1)), nil
}

type fixedIntegrationClock struct{}

func (fixedIntegrationClock) Now() time.Time {
	return time.Date(2026, time.August, 14, 18, 0, 0, 0, time.UTC)
}

type fixedFinancialClock struct{ now time.Time }

func (clock fixedFinancialClock) Now() time.Time { return clock.now }

func newRecordUseCase(
	t *testing.T,
	repository *adapter.ExpenseRepository,
	idGenerator application.ExpenseIDGenerator,
) *application.RecordExpense {
	t.Helper()
	useCase, err := application.NewRecordExpense(repository, idGenerator, fixedIntegrationClock{})
	if err != nil {
		t.Fatalf("NewRecordExpense() error = %v", err)
	}
	return useCase
}

func integrationRecordInput(userID, key string) application.RecordExpenseInput {
	return application.RecordExpenseInput{
		IdempotencyKey: key,
		Expense: application.CreateExpenseInput{
			UserID:            userID,
			Description:       "Mercado sintético",
			AmountMinor:       4250,
			Currency:          domain.CurrencyBRL,
			PaymentMethod:     domain.PaymentMethodPIX,
			OccurredAt:        time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC),
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
	}
}

func expenseAt(t *testing.T, id, userID string, occurredAt time.Time) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(4250, domain.CurrencyBRL)
	if err != nil {
		t.Fatal("NewMoney() failed")
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID:            userID,
			Description:       "Boundary sintético",
			Amount:            amount,
			PaymentMethod:     domain.PaymentMethodCash,
			OccurredAt:        occurredAt,
			FinancialTimezone: application.FinancialTimezone,
			Origin:            domain.OriginIOS,
		},
		CreatedAt: time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewExpense() error = %v", err)
	}
	return expense
}

func assertFinancialRowCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	transactions int,
	auditEvents int,
	idempotencyRecords int,
) {
	t.Helper()
	counts := make([]int, 3)
	queries := []string{
		"SELECT count(*) FROM transactions",
		"SELECT count(*) FROM audit_events",
		"SELECT count(*) FROM idempotency_records",
	}
	for index, query := range queries {
		if err := pool.QueryRow(ctx, query).Scan(&counts[index]); err != nil {
			t.Fatal("financial row count failed")
		}
	}
	wanted := []int{transactions, auditEvents, idempotencyRecords}
	if fmt.Sprint(counts) != fmt.Sprint(wanted) {
		t.Fatalf("financial row counts = %v, want %v", counts, wanted)
	}
}

type financialHTTPResponse struct {
	status int
	header http.Header
	body   []byte
}

func newFinancialIntegrationServer(
	t *testing.T,
	pool *pgxpool.Pool,
	expenseIDGenerator application.ExpenseIDGenerator,
	incomeIDGenerator application.IncomeIDGenerator,
	clock application.Clock,
) *httptest.Server {
	t.Helper()
	repository := newRepository(t, pool)
	previewExpense, err := application.NewPreviewExpenseWithCategoryCatalog(repository)
	if err != nil {
		t.Fatalf("NewPreviewExpenseWithCategoryCatalog() error = %v", err)
	}
	previewIncome, err := application.NewPreviewIncomeWithCategoryCatalog(repository)
	if err != nil {
		t.Fatalf("NewPreviewIncomeWithCategoryCatalog() error = %v", err)
	}
	recordExpense, err := application.NewRecordExpenseWithCategoryCatalog(repository, expenseIDGenerator, clock, repository)
	if err != nil {
		t.Fatalf("NewRecordExpenseWithCategoryCatalog() error = %v", err)
	}
	recordIncome, err := application.NewRecordIncomeWithCategoryCatalog(repository, incomeIDGenerator, clock, repository)
	if err != nil {
		t.Fatalf("NewRecordIncomeWithCategoryCatalog() error = %v", err)
	}
	list, err := application.NewListTransactionsByMonth(repository)
	if err != nil {
		t.Fatalf("NewListTransactionsByMonth() error = %v", err)
	}
	listCategories, err := application.NewListCategories(repository)
	if err != nil {
		t.Fatalf("NewListCategories() error = %v", err)
	}
	routes := httpapi.New(
		syntheticUserID,
		previewExpense,
		previewIncome,
		recordExpense,
		recordIncome,
		list,
		listCategories,
	)
	mux := http.NewServeMux()
	routes.Register(mux)
	return httptest.NewServer(mux)
}

func postFinancialJSON(
	t *testing.T,
	client *http.Client,
	url string,
	body []byte,
	idempotencyKey string,
) financialHTTPResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal("creating financial HTTP request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("financial HTTP request failed")
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal("reading financial HTTP response failed")
	}
	return financialHTTPResponse{status: response.StatusCode, header: response.Header, body: responseBody}
}

func getFinancialJSON(t *testing.T, client *http.Client, url string) financialHTTPResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal("creating financial GET request failed")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("financial GET request failed")
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal("reading financial GET response failed")
	}
	return financialHTTPResponse{status: response.StatusCode, header: response.Header, body: responseBody}
}

func jsonStringField(t *testing.T, body []byte, field string) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal("decoding financial HTTP response failed")
	}
	fieldValue, ok := value[field].(string)
	if !ok {
		t.Fatalf("financial HTTP response field %s is missing or not a string", field)
	}
	return fieldValue
}
