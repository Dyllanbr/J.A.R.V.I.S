//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/adapters/randomid"
	"jarvis/backend/internal/modules/transactions/application"
)

const recurrenceHTTPBody = `{"type":"EXPENSE","description":"  Serviço HTTP sintético  ","expectedAmount":{"minor":11900,"currency":"BRL"},"frequency":"MONTHLY","startsOn":"2026-08-31"}`

func TestRecurrenceHTTPPreviewCreateListCancelReplayRestartAndIsolation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const ownerB = "usr-recurrence-http-owner-b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)

	serverA := newRecurrenceHTTPIntegrationServer(
		t,
		pool,
		syntheticUserID,
		&recurrenceFixedIDGenerator{id: "rec-http-persistent-001"},
		recurrenceHTTPIntegrationClock{now: time.Date(2026, 8, 16, 12, 0, 0, 123_456_789, time.UTC)},
	)
	preview := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/recurrences/preview", []byte(recurrenceHTTPBody), "")
	if preview.status != http.StatusOK || !bytes.Contains(preview.body, []byte(`"startsOn":"2026-08-31"`)) ||
		bytes.Contains(preview.body, []byte(`"id"`)) {
		t.Fatalf("preview = %d %s", preview.status, preview.body)
	}
	assertRecurrenceSubsystemCounts(t, ctx, pool, 0, 0, 0)
	assertNoTransactionSubsystemWrites(t, ctx, pool)

	created := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/recurrences", []byte(recurrenceHTTPBody), "http-create-key")
	if created.status != http.StatusCreated || created.header.Get("Idempotency-Replayed") != "" ||
		!bytes.Contains(created.body, []byte(`"status":"ACTIVE"`)) || bytes.Contains(created.body, []byte(`"cancelledAt"`)) {
		t.Fatalf("create = %d headers=%v body=%s", created.status, created.header, created.body)
	}
	serverA.Close()

	poolB := openRestartPool(t, ctx, pool)
	serverB := newRecurrenceHTTPIntegrationServer(
		t,
		poolB,
		syntheticUserID,
		&recurrenceFixedIDGenerator{id: "rec-http-unused-after-restart"},
		recurrenceHTTPIntegrationClock{now: time.Date(2026, 9, 1, 14, 0, 0, 987_654_321, time.UTC)},
	)
	createReplay := postFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/recurrences", []byte(recurrenceHTTPBody), "http-create-key")
	if createReplay.status != http.StatusCreated || createReplay.header.Get("Idempotency-Replayed") != "true" ||
		!bytes.Equal(createReplay.body, created.body) {
		t.Fatalf("CREATE replay = %d headers=%v body=%s; created=%s", createReplay.status, createReplay.header, createReplay.body, created.body)
	}
	listed := getFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/recurrences")
	if listed.status != http.StatusOK || !bytes.Contains(listed.body, []byte(`"id":"rec-http-persistent-001"`)) ||
		!bytes.Contains(listed.body, []byte(`"status":"ACTIVE"`)) {
		t.Fatalf("list = %d %s", listed.status, listed.body)
	}

	cancelled := postFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/recurrences/rec-http-persistent-001/cancel", nil, "http-cancel-key")
	if cancelled.status != http.StatusOK || cancelled.header.Get("Idempotency-Replayed") != "" ||
		!bytes.Contains(cancelled.body, []byte(`"status":"CANCELLED"`)) || !bytes.Contains(cancelled.body, []byte(`"cancelledAt":`)) {
		t.Fatalf("cancel = %d headers=%v body=%s", cancelled.status, cancelled.header, cancelled.body)
	}
	serverB.Close()
	poolB.Close()

	poolC := openRestartPool(t, ctx, pool)
	defer poolC.Close()
	serverC := newRecurrenceHTTPIntegrationServer(
		t,
		poolC,
		syntheticUserID,
		&recurrenceFixedIDGenerator{id: "rec-http-never-generated"},
		recurrenceHTTPIntegrationClock{now: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	)
	defer serverC.Close()
	cancelReplay := postFinancialJSON(t, serverC.Client(), serverC.URL+"/v1/recurrences/rec-http-persistent-001/cancel", nil, "http-cancel-key")
	if cancelReplay.status != http.StatusOK || cancelReplay.header.Get("Idempotency-Replayed") != "true" ||
		!bytes.Equal(cancelReplay.body, cancelled.body) {
		t.Fatalf("CANCEL replay = %d headers=%v body=%s; cancelled=%s", cancelReplay.status, cancelReplay.header, cancelReplay.body, cancelled.body)
	}
	historicalCreate := postFinancialJSON(t, serverC.Client(), serverC.URL+"/v1/recurrences", []byte(recurrenceHTTPBody), "http-create-key")
	if historicalCreate.header.Get("Idempotency-Replayed") != "true" || !bytes.Equal(historicalCreate.body, created.body) ||
		bytes.Contains(historicalCreate.body, []byte(`"cancelledAt"`)) {
		t.Fatalf("historical CREATE replay = %d headers=%v body=%s", historicalCreate.status, historicalCreate.header, historicalCreate.body)
	}
	var (
		currentStatus      string
		currentCancelledAt *time.Time
	)
	if err := pool.QueryRow(ctx, `
		SELECT status, cancelled_at
		FROM recurrences
		WHERE user_id = $1 AND id = $2
	`, syntheticUserID, "rec-http-persistent-001").Scan(&currentStatus, &currentCancelledAt); err != nil {
		t.Fatal("reading current recurrence state after historical HTTP replay failed")
	}
	if currentStatus != "CANCELLED" || currentCancelledAt == nil {
		t.Fatalf("current row = status %s cancelledAt %v, want persisted CANCELLED state", currentStatus, currentCancelledAt)
	}
	conflictBody := []byte(strings.Replace(recurrenceHTTPBody, "11900", "12900", 1))
	conflict := postFinancialJSON(t, serverC.Client(), serverC.URL+"/v1/recurrences", conflictBody, "http-create-key")
	if conflict.status != http.StatusConflict || !bytes.Contains(conflict.body, []byte(`"code":"IDEMPOTENCY_KEY_REUSED"`)) {
		t.Fatalf("create conflict = %d %s", conflict.status, conflict.body)
	}

	serverBForOtherOwner := newRecurrenceHTTPIntegrationServer(
		t,
		poolC,
		ownerB,
		&recurrenceFixedIDGenerator{id: "rec-http-owner-b-unused"},
		recurrenceHTTPIntegrationClock{now: time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)},
	)
	defer serverBForOtherOwner.Close()
	crossOwner := postFinancialJSON(t, serverBForOtherOwner.Client(), serverBForOtherOwner.URL+"/v1/recurrences/rec-http-persistent-001/cancel", nil, "http-cross-owner")
	unknown := postFinancialJSON(t, serverBForOtherOwner.Client(), serverBForOtherOwner.URL+"/v1/recurrences/rec-http-unknown/cancel", nil, "http-unknown")
	if crossOwner.status != http.StatusNotFound || unknown.status != http.StatusNotFound || !bytes.Equal(crossOwner.body, unknown.body) {
		t.Fatalf("cross-owner enumerates resource: cross=%d %s unknown=%d %s", crossOwner.status, crossOwner.body, unknown.status, unknown.body)
	}
	ownerBList := getFinancialJSON(t, serverBForOtherOwner.Client(), serverBForOtherOwner.URL+"/v1/recurrences")
	if ownerBList.status != http.StatusOK || !bytes.Equal(ownerBList.body, []byte("{\"items\":[]}\n")) {
		t.Fatalf("owner B list = %d %s", ownerBList.status, ownerBList.body)
	}

	assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertNoTransactionSubsystemWrites(t, ctx, pool)
	var createdAudits, cancelledAudits, createCompletions, cancelCompletions int
	if err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE event_type = 'RECURRENCE_CREATED'),
			count(*) FILTER (WHERE event_type = 'RECURRENCE_CANCELLED')
		FROM recurrence_audit_events
		WHERE user_id = $1
	`, syntheticUserID).Scan(&createdAudits, &cancelledAudits); err != nil {
		t.Fatal("reading recurrence HTTP audit postconditions failed")
	}
	if err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE operation = 'CREATE_RECURRENCE' AND state = 'COMPLETED'),
			count(*) FILTER (WHERE operation = 'CANCEL_RECURRENCE' AND state = 'COMPLETED')
		FROM recurrence_idempotency_records
		WHERE user_id = $1
	`, syntheticUserID).Scan(&createCompletions, &cancelCompletions); err != nil {
		t.Fatal("reading recurrence HTTP idempotency postconditions failed")
	}
	if createdAudits != 1 || cancelledAudits != 1 || createCompletions != 1 || cancelCompletions != 1 {
		t.Fatalf(
			"HTTP persistence postconditions = audits %d/%d completions %d/%d, want 1/1/1/1",
			createdAudits,
			cancelledAudits,
			createCompletions,
			cancelCompletions,
		)
	}
}

func TestRecurrenceHTTPCreateConcurrency(t *testing.T) {
	t.Run("same key same payload", func(t *testing.T) {
		pool, server, ctx := newConcurrentRecurrenceHTTPFixture(t)
		responses := concurrentRecurrenceHTTPPosts(t, server, "/v1/recurrences", []recurrenceHTTPPost{
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
			{body: recurrenceHTTPBody, key: "http-concurrent-same"},
		})
		var original []byte
		newCount := 0
		for _, response := range responses {
			if response.status != http.StatusCreated {
				t.Fatalf("same-key create = %d %s", response.status, response.body)
			}
			if original == nil {
				original = response.body
			} else if !bytes.Equal(original, response.body) {
				t.Fatalf("same-key results differ: %s vs %s", original, response.body)
			}
			if response.header.Get("Idempotency-Replayed") == "" {
				newCount++
			}
		}
		if newCount != 1 {
			t.Fatalf("non-replayed responses = %d, want 1", newCount)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 1, 1)
		assertNoTransactionSubsystemWrites(t, ctx, pool)
	})

	t.Run("same key different fingerprint", func(t *testing.T) {
		pool, server, ctx := newConcurrentRecurrenceHTTPFixture(t)
		other := strings.Replace(recurrenceHTTPBody, "11900", "12900", 1)
		responses := concurrentRecurrenceHTTPPosts(t, server, "/v1/recurrences", []recurrenceHTTPPost{
			{body: recurrenceHTTPBody, key: "http-concurrent-conflict"},
			{body: other, key: "http-concurrent-conflict"},
		})
		assertHTTPStatusMultiset(t, responses, map[int]int{http.StatusCreated: 1, http.StatusConflict: 1})
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 1, 1)
		assertNoTransactionSubsystemWrites(t, ctx, pool)
	})

	t.Run("different keys are distinct confirmations", func(t *testing.T) {
		pool, server, ctx := newConcurrentRecurrenceHTTPFixture(t)
		responses := concurrentRecurrenceHTTPPosts(t, server, "/v1/recurrences", []recurrenceHTTPPost{
			{body: recurrenceHTTPBody, key: "http-distinct-a"},
			{body: recurrenceHTTPBody, key: "http-distinct-b"},
		})
		assertHTTPStatusMultiset(t, responses, map[int]int{http.StatusCreated: 2})
		assertRecurrenceSubsystemCounts(t, ctx, pool, 2, 2, 2)
		assertNoTransactionSubsystemWrites(t, ctx, pool)
	})
}

func TestRecurrenceHTTPCancelConcurrency(t *testing.T) {
	t.Run("same key shares one cancellation", func(t *testing.T) {
		pool, server, ctx := newConcurrentRecurrenceHTTPFixture(t)
		created := postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrences", []byte(recurrenceHTTPBody), "cancel-create")
		id := jsonStringField(t, created.body, "id")
		posts := make([]recurrenceHTTPPost, 8)
		for index := range posts {
			posts[index].key = "http-cancel-same"
		}
		responses := concurrentRecurrenceHTTPPosts(t, server, "/v1/recurrences/"+id+"/cancel", posts)
		var original []byte
		newCount := 0
		for _, response := range responses {
			if response.status != http.StatusOK {
				t.Fatalf("same-key cancel = %d %s", response.status, response.body)
			}
			if original == nil {
				original = response.body
			} else if !bytes.Equal(original, response.body) {
				t.Fatalf("same-key cancel results differ: %s vs %s", original, response.body)
			}
			if response.header.Get("Idempotency-Replayed") == "" {
				newCount++
			}
		}
		if newCount != 1 {
			t.Fatalf("non-replayed cancels = %d, want 1", newCount)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)
		assertNoTransactionSubsystemWrites(t, ctx, pool)
	})

	t.Run("different keys observe terminal lifecycle", func(t *testing.T) {
		pool, server, ctx := newConcurrentRecurrenceHTTPFixture(t)
		created := postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrences", []byte(recurrenceHTTPBody), "cancel-different-create")
		id := jsonStringField(t, created.body, "id")
		responses := concurrentRecurrenceHTTPPosts(t, server, "/v1/recurrences/"+id+"/cancel", []recurrenceHTTPPost{
			{key: "http-cancel-different-a"},
			{key: "http-cancel-different-b"},
		})
		assertHTTPStatusMultiset(t, responses, map[int]int{http.StatusOK: 1, http.StatusConflict: 1})
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)
		assertNoTransactionSubsystemWrites(t, ctx, pool)
	})

	t.Run("same key different resources conflicts", func(t *testing.T) {
		pool, server, ctx := newConcurrentRecurrenceHTTPFixture(t)
		first := postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrences", []byte(recurrenceHTTPBody), "cancel-resource-create-a")
		second := postFinancialJSON(t, server.Client(), server.URL+"/v1/recurrences", []byte(recurrenceHTTPBody), "cancel-resource-create-b")
		responses := concurrentRecurrenceHTTPPosts(t, server, "", []recurrenceHTTPPost{
			{path: "/v1/recurrences/" + jsonStringField(t, first.body, "id") + "/cancel", key: "http-cancel-resource-key"},
			{path: "/v1/recurrences/" + jsonStringField(t, second.body, "id") + "/cancel", key: "http-cancel-resource-key"},
		})
		assertHTTPStatusMultiset(t, responses, map[int]int{http.StatusOK: 1, http.StatusConflict: 1})
		assertRecurrenceSubsystemCounts(t, ctx, pool, 2, 3, 3)
		assertNoTransactionSubsystemWrites(t, ctx, pool)
	})
}

type recurrenceHTTPIntegrationClock struct {
	now time.Time
}

func (clock recurrenceHTTPIntegrationClock) Now() time.Time { return clock.now }

func newRecurrenceHTTPIntegrationServer(
	t testing.TB,
	pool *pgxpool.Pool,
	owner string,
	idGenerator application.RecurrenceIDGenerator,
	clock application.Clock,
) *httptest.Server {
	t.Helper()
	repository := newRecurrenceRepository(t, pool)
	record := newRecordRecurrenceUseCase(t, repository, idGenerator, clock)
	list, err := application.NewListRecurrences(repository)
	if err != nil {
		t.Fatalf("NewListRecurrences() error = %v", err)
	}
	cancel := newCancelRecurrenceUseCase(t, repository, clock)
	routes := httpapi.NewRecurrence(owner, application.PreviewRecurrence{}, record, list, cancel)
	mux := http.NewServeMux()
	routes.Register(mux)
	return httptest.NewServer(mux)
}

func newConcurrentRecurrenceHTTPFixture(t *testing.T) (*pgxpool.Pool, *httptest.Server, context.Context) {
	t.Helper()
	pool := newMigratedTestDatabase(t)
	ctx := context.Background()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	server := newRecurrenceHTTPIntegrationServer(
		t,
		pool,
		syntheticUserID,
		randomid.Generator{},
		recurrenceHTTPIntegrationClock{now: time.Date(2026, 8, 16, 12, 0, 0, 123_456_000, time.UTC)},
	)
	t.Cleanup(server.Close)
	return pool, server, ctx
}

type recurrenceHTTPPost struct {
	path string
	body string
	key  string
}

func concurrentRecurrenceHTTPPosts(
	t *testing.T,
	server *httptest.Server,
	defaultPath string,
	posts []recurrenceHTTPPost,
) []financialHTTPResponse {
	t.Helper()
	responses := make([]financialHTTPResponse, len(posts))
	ready := sync.WaitGroup{}
	done := sync.WaitGroup{}
	start := make(chan struct{})
	ready.Add(len(posts))
	done.Add(len(posts))
	for index := range posts {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			path := posts[index].path
			if path == "" {
				path = defaultPath
			}
			responses[index] = postFinancialJSON(t, server.Client(), server.URL+path, []byte(posts[index].body), posts[index].key)
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return responses
}

func assertHTTPStatusMultiset(t *testing.T, responses []financialHTTPResponse, want map[int]int) {
	t.Helper()
	got := make(map[int]int)
	for _, response := range responses {
		got[response.status]++
	}
	if len(got) != len(want) {
		t.Fatalf("HTTP statuses = %v, want %v", got, want)
	}
	for status, count := range want {
		if got[status] != count {
			t.Fatalf("HTTP statuses = %v, want %v", got, want)
		}
	}
}
