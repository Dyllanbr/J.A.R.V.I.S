//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
)

func TestRecurrenceSuggestionHTTPPostgresFlowReplayFreshEvidenceAndIsolation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const ownerB = "usr-suggestion-http-owner-b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)
	for _, fixture := range []struct {
		id    string
		owner string
		month time.Month
	}{
		{"exp-suggestion-http-may", syntheticUserID, time.May},
		{"exp-suggestion-http-jun", syntheticUserID, time.June},
		{"exp-suggestion-http-jul", syntheticUserID, time.July},
		{"exp-suggestion-http-b-may", ownerB, time.May},
		{"exp-suggestion-http-b-jun", ownerB, time.June},
		{"exp-suggestion-http-b-jul", ownerB, time.July},
	} {
		value := validRawTransaction(fixture.id, fixture.owner)
		value.description = "Academia HTTP PostgreSQL"
		value.amountMinor = 11900
		value.occurredAt = time.Date(2026, fixture.month, 10, 15, 0, 0, 0, time.UTC)
		value.createdAt = value.occurredAt.Add(time.Hour)
		value.updatedAt = value.createdAt
		if err := insertRawTransaction(ctx, pool, value); err != nil {
			t.Fatalf("insert expense fixture: %v", err)
		}
	}

	server := newRecurrenceSuggestionHTTPIntegrationServer(t, pool, syntheticUserID,
		recurrenceHTTPIntegrationClock{now: time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)})
	list := getFinancialJSON(t, server.Client(), server.URL+"/v1/recurrence-suggestions")
	if list.status != http.StatusOK {
		t.Fatalf("list = %d %s", list.status, list.body)
	}
	firstID := suggestionIDFromHTTPList(t, list.body)
	if !bytes.Contains(list.body, []byte(`"description":"Academia HTTP PostgreSQL"`)) ||
		!bytes.Contains(list.body, []byte(`"observedDates":["2026-05-10","2026-06-10","2026-07-10"]`)) {
		t.Fatalf("suggestion body = %s", list.body)
	}
	preview := postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrence-suggestions/"+firstID+"/preview", nil, "")
	if preview.status != http.StatusOK || !bytes.Contains(preview.body, []byte(`"startsOn":"2026-08-10"`)) ||
		bytes.Contains(preview.body, []byte(`"id"`)) {
		t.Fatalf("preview = %d %s", preview.status, preview.body)
	}
	assertSuggestionHTTPOnlyExistingExpenses(t, ctx, pool, 6, 0)

	dismissed := postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrence-suggestions/"+firstID+"/dismiss", nil, "")
	if dismissed.status != http.StatusNoContent || len(dismissed.body) != 0 || dismissed.header.Get("Idempotency-Replayed") != "" {
		t.Fatalf("dismiss = %d replay=%q body=%q", dismissed.status, dismissed.header.Get("Idempotency-Replayed"), dismissed.body)
	}
	replay := postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrence-suggestions/"+firstID+"/dismiss", nil, "")
	if replay.status != http.StatusNoContent || replay.header.Get("Idempotency-Replayed") != "true" || len(replay.body) != 0 {
		t.Fatalf("dismiss replay = %d replay=%q body=%q", replay.status, replay.header.Get("Idempotency-Replayed"), replay.body)
	}
	hidden := getFinancialJSON(t, server.Client(), server.URL+"/v1/recurrence-suggestions")
	if !bytes.Equal(hidden.body, []byte("{\"items\":[]}\n")) {
		t.Fatalf("dismissed list = %s", hidden.body)
	}
	server.Close()

	value := validRawTransaction("exp-suggestion-http-aug", syntheticUserID)
	value.description = "Academia HTTP PostgreSQL"
	value.amountMinor = 11900
	value.occurredAt = time.Date(2026, time.August, 10, 15, 0, 0, 0, time.UTC)
	value.createdAt = value.occurredAt.Add(time.Hour)
	value.updatedAt = value.createdAt
	if err := insertRawTransaction(ctx, pool, value); err != nil {
		t.Fatal(err)
	}

	restartedPool := openRestartPool(t, ctx, pool)
	defer restartedPool.Close()
	restarted := newRecurrenceSuggestionHTTPIntegrationServer(t, restartedPool, syntheticUserID,
		recurrenceHTTPIntegrationClock{now: time.Date(2026, time.August, 24, 15, 0, 0, 0, time.UTC)})
	defer restarted.Close()
	current := getFinancialJSON(t, restarted.Client(), restarted.URL+"/v1/recurrence-suggestions")
	currentID := suggestionIDFromHTTPList(t, current.body)
	if currentID == firstID {
		t.Fatal("new material evidence did not change SuggestionID")
	}
	stale := postFinancialJSON(t, restarted.Client(), restarted.URL+"/v1/recurrence-suggestions/"+firstID+"/preview", nil, "")
	if stale.status != http.StatusNotFound {
		t.Fatalf("stale preview = %d %s", stale.status, stale.body)
	}

	responses := concurrentSuggestionDismisses(t, restarted, currentID, 8)
	firstWrites := 0
	for _, response := range responses {
		if response.status != http.StatusNoContent || len(response.body) != 0 {
			t.Fatalf("concurrent dismiss = %d %q", response.status, response.body)
		}
		if response.header.Get("Idempotency-Replayed") == "" {
			firstWrites++
		}
	}
	if firstWrites != 1 {
		t.Fatalf("concurrent first writes = %d, want 1", firstWrites)
	}

	ownerBServer := newRecurrenceSuggestionHTTPIntegrationServer(t, restartedPool, ownerB,
		recurrenceHTTPIntegrationClock{now: time.Date(2026, time.July, 24, 15, 0, 0, 0, time.UTC)})
	defer ownerBServer.Close()
	ownerBID := suggestionIDFromHTTPList(t, getFinancialJSON(t, ownerBServer.Client(), ownerBServer.URL+"/v1/recurrence-suggestions").body)
	cross := postFinancialJSON(t, restarted.Client(), restarted.URL+"/v1/recurrence-suggestions/"+ownerBID+"/preview", nil, "")
	unknown := postFinancialJSON(t, restarted.Client(), restarted.URL+"/v1/recurrence-suggestions/rsg_ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff/preview", nil, "")
	if cross.status != http.StatusNotFound || unknown.status != http.StatusNotFound || !bytes.Equal(cross.body, unknown.body) {
		t.Fatalf("cross-owner enumerates suggestion: cross=%d %s unknown=%d %s", cross.status, cross.body, unknown.status, unknown.body)
	}
	assertSuggestionHTTPOnlyExistingExpenses(t, ctx, pool, 7, 2)
}

func newRecurrenceSuggestionHTTPIntegrationServer(
	t testing.TB,
	pool *pgxpool.Pool,
	owner string,
	clock application.Clock,
) *httptest.Server {
	t.Helper()
	suggestionRepository := newRecurrenceSuggestionRepository(t, pool)
	recurrenceRepository := newRecurrenceRepository(t, pool)
	list, err := application.NewListRecurrenceSuggestions(suggestionRepository, recurrenceRepository, suggestionRepository, clock)
	if err != nil {
		t.Fatal(err)
	}
	dismiss, err := application.NewDismissRecurrenceSuggestion(list, suggestionRepository, suggestionRepository, clock)
	if err != nil {
		t.Fatal(err)
	}
	prepare, err := application.NewPrepareSuggestedRecurrence(list)
	if err != nil {
		t.Fatal(err)
	}
	routes := httpapi.NewRecurrenceSuggestion(owner, list, dismiss, prepare)
	mux := http.NewServeMux()
	routes.Register(mux)
	return httptest.NewServer(mux)
}

func suggestionIDFromHTTPList(t testing.TB, body []byte) string {
	t.Helper()
	var response struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &response); err != nil || len(response.Items) != 1 {
		t.Fatalf("suggestion list = %s", body)
	}
	return response.Items[0].ID
}

func concurrentSuggestionDismisses(t *testing.T, server *httptest.Server, id string, count int) []financialHTTPResponse {
	t.Helper()
	result := make([]financialHTTPResponse, count)
	ready := sync.WaitGroup{}
	done := sync.WaitGroup{}
	start := make(chan struct{})
	ready.Add(count)
	done.Add(count)
	for index := range result {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			result[index] = postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrence-suggestions/"+id+"/dismiss", nil, "")
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return result
}

func assertSuggestionHTTPOnlyExistingExpenses(t testing.TB, ctx context.Context, pool *pgxpool.Pool, wantExpenses, wantSuppressions int) {
	t.Helper()
	wants := map[string]int{
		"transactions": wantExpenses, "recurrence_suggestion_suppressions": wantSuppressions,
		"audit_events": 0, "idempotency_records": 0, "recurrences": 0,
		"recurrence_audit_events": 0, "recurrence_idempotency_records": 0,
	}
	for table, want := range wants {
		var got int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}
