//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	"jarvis/backend/internal/modules/transactions/application"
)

const creditCardHTTPBody = `{"name":"  Cartão HTTP sintético  ","lastFour":"4242","brand":"VISA","closingDay":31,"dueDay":10,"creditLimit":{"minor":250000,"currency":"BRL"}}`

func TestCreditCardHTTPRejectsQueriesBeforePostgresWrites(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const owner = "usr_credit_card_http_query"
	const key = "http-card-query-rejected"
	const archiveKey = "http-card-archive-query-rejected"
	cardID := creditCardTestID('e')
	insertSyntheticUser(t, ctx, pool, owner)

	server := newCreditCardHTTPIntegrationServer(
		t, pool, owner, &fixedCreditCardIDGenerator{id: cardID},
		fixedCreditCardClock{now: time.Date(2026, time.August, 25, 13, 30, 0, 0, time.UTC)},
	)
	defer server.Close()

	preview := postFinancialJSON(t, server.Client(), server.URL+"/v1/cards/preview?foo;bar=baz", []byte(creditCardHTTPBody), "")
	if preview.status != http.StatusBadRequest || !bytes.Contains(preview.body, []byte(`"code":"INVALID_REQUEST"`)) {
		t.Fatalf("preview query = %d %s", preview.status, preview.body)
	}
	createdWithQuery := postFinancialJSON(t, server.Client(), server.URL+"/v1/cards?foo;bar=baz", []byte(creditCardHTTPBody), key)
	if createdWithQuery.status != http.StatusBadRequest || !bytes.Contains(createdWithQuery.body, []byte(`"code":"INVALID_REQUEST"`)) {
		t.Fatalf("create query = %d %s", createdWithQuery.status, createdWithQuery.body)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 0, 0, 0)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)

	created := postFinancialJSON(t, server.Client(), server.URL+"/v1/cards", []byte(creditCardHTTPBody), key)
	if created.status != http.StatusCreated || created.header.Get("Idempotency-Replayed") != "" {
		t.Fatalf("same key after rejected query = %d headers=%v body=%s", created.status, created.header, created.body)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)

	archivedWithQuery := postFinancialJSON(t, server.Client(), server.URL+"/v1/cards/"+cardID+"/archive?foo;bar=baz", nil, archiveKey)
	if archivedWithQuery.status != http.StatusBadRequest || !bytes.Contains(archivedWithQuery.body, []byte(`"code":"INVALID_REQUEST"`)) {
		t.Fatalf("archive query = %d %s", archivedWithQuery.status, archivedWithQuery.body)
	}
	var status string
	var archivedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, archived_at FROM credit_cards WHERE user_id = $1 AND id = $2`, owner, cardID).
		Scan(&status, &archivedAt); err != nil {
		t.Fatal("reading card after rejected archive failed")
	}
	if status != "ACTIVE" || archivedAt != nil {
		t.Fatalf("card after rejected archive = %s %v, want ACTIVE/nil", status, archivedAt)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	assertCreditCardIdempotencyKeyCount(t, ctx, pool, owner, archiveKey, 0)

	archived := postFinancialJSON(t, server.Client(), server.URL+"/v1/cards/"+cardID+"/archive", nil, archiveKey)
	if archived.status != http.StatusOK || archived.header.Get("Idempotency-Replayed") != "" ||
		!bytes.Contains(archived.body, []byte(`"status":"ARCHIVED"`)) {
		t.Fatalf("same key after rejected archive = %d headers=%v body=%s", archived.status, archived.header, archived.body)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)
}

func TestCreditCardHTTPPostgresLifecycleReplayRestartIsolationAndSeparation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const ownerA = "usr_credit_card_http_a"
	const ownerB = "usr_credit_card_http_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)
	createdAt := time.Date(2026, time.August, 25, 13, 30, 0, 123_456_789, time.UTC)
	archivedAt := createdAt.Add(4 * time.Hour)
	cardID := creditCardTestID('a')

	serverA := newCreditCardHTTPIntegrationServer(
		t, pool, ownerA, &fixedCreditCardIDGenerator{id: cardID}, fixedCreditCardClock{now: createdAt},
	)
	preview := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/cards/preview", []byte(creditCardHTTPBody), "")
	if preview.status != http.StatusOK || !bytes.Contains(preview.body, []byte(`"name":"Cartão HTTP sintético"`)) ||
		bytes.Contains(preview.body, []byte(`"id"`)) || bytes.Contains(preview.body, []byte(`"status"`)) {
		t.Fatalf("preview = %d %s", preview.status, preview.body)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 0, 0, 0)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)

	created := postFinancialJSON(t, serverA.Client(), serverA.URL+"/v1/cards", []byte(creditCardHTTPBody), "http-card-create")
	if created.status != http.StatusCreated || created.header.Get("Idempotency-Replayed") != "" ||
		!bytes.Contains(created.body, []byte(`"id":"`+cardID+`"`)) || !bytes.Contains(created.body, []byte(`"status":"ACTIVE"`)) ||
		bytes.Contains(created.body, []byte(`"archivedAt"`)) {
		t.Fatalf("create = %d headers=%v body=%s", created.status, created.header, created.body)
	}
	serverA.Close()
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)

	restartPool := openRestartPool(t, ctx, pool)
	serverB := newCreditCardHTTPIntegrationServer(
		t, restartPool, ownerA, &fixedCreditCardIDGenerator{id: creditCardTestID('b')}, fixedCreditCardClock{now: archivedAt},
	)
	createReplay := postFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/cards", []byte(creditCardHTTPBody), "http-card-create")
	if createReplay.status != http.StatusCreated || createReplay.header.Get("Idempotency-Replayed") != "true" ||
		!bytes.Equal(createReplay.body, created.body) {
		t.Fatalf("create replay = %d headers=%v body=%s; want %s", createReplay.status, createReplay.header, createReplay.body, created.body)
	}
	listed := getFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/cards")
	if listed.status != http.StatusOK || !bytes.Contains(listed.body, []byte(`"id":"`+cardID+`"`)) ||
		!bytes.Contains(listed.body, []byte(`"status":"ACTIVE"`)) {
		t.Fatalf("list = %d %s", listed.status, listed.body)
	}
	detail := getFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/cards/"+cardID)
	if detail.status != http.StatusOK || !bytes.Equal(detail.body, created.body) {
		t.Fatalf("detail = %d %s", detail.status, detail.body)
	}
	archived := postFinancialJSON(t, serverB.Client(), serverB.URL+"/v1/cards/"+cardID+"/archive", nil, "http-card-archive")
	if archived.status != http.StatusOK || archived.header.Get("Idempotency-Replayed") != "" ||
		!bytes.Contains(archived.body, []byte(`"status":"ARCHIVED"`)) || !bytes.Contains(archived.body, []byte(`"archivedAt":`)) {
		t.Fatalf("archive = %d headers=%v body=%s", archived.status, archived.header, archived.body)
	}
	serverB.Close()
	restartPool.Close()

	secondRestartPool := openRestartPool(t, ctx, pool)
	defer secondRestartPool.Close()
	serverC := newCreditCardHTTPIntegrationServer(
		t, secondRestartPool, ownerA, &fixedCreditCardIDGenerator{id: creditCardTestID('c')}, fixedCreditCardClock{now: archivedAt.Add(time.Hour)},
	)
	defer serverC.Close()
	archiveReplay := postFinancialJSON(t, serverC.Client(), serverC.URL+"/v1/cards/"+cardID+"/archive", nil, "http-card-archive")
	if archiveReplay.status != http.StatusOK || archiveReplay.header.Get("Idempotency-Replayed") != "true" ||
		!bytes.Equal(archiveReplay.body, archived.body) {
		t.Fatalf("archive replay = %d headers=%v body=%s", archiveReplay.status, archiveReplay.header, archiveReplay.body)
	}
	historicalCreate := postFinancialJSON(t, serverC.Client(), serverC.URL+"/v1/cards", []byte(creditCardHTTPBody), "http-card-create")
	if historicalCreate.status != http.StatusCreated || historicalCreate.header.Get("Idempotency-Replayed") != "true" ||
		!bytes.Equal(historicalCreate.body, created.body) || bytes.Contains(historicalCreate.body, []byte(`"ARCHIVED"`)) {
		t.Fatalf("historical create replay = %d headers=%v body=%s", historicalCreate.status, historicalCreate.header, historicalCreate.body)
	}
	var currentStatus string
	var currentArchivedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, archived_at FROM credit_cards WHERE user_id = $1 AND id = $2`, ownerA, cardID).
		Scan(&currentStatus, &currentArchivedAt); err != nil {
		t.Fatal("reading current credit card state failed")
	}
	if currentStatus != "ARCHIVED" || currentArchivedAt == nil || !currentArchivedAt.Equal(archivedAt.Truncate(time.Microsecond)) {
		t.Fatalf("current card = %s %v, want archived at %v", currentStatus, currentArchivedAt, archivedAt)
	}

	conflictBody := bytes.Replace([]byte(creditCardHTTPBody), []byte("250000"), []byte("260000"), 1)
	conflict := postFinancialJSON(t, serverC.Client(), serverC.URL+"/v1/cards", conflictBody, "http-card-create")
	if conflict.status != http.StatusConflict || !bytes.Contains(conflict.body, []byte(`"code":"IDEMPOTENCY_KEY_REUSED"`)) {
		t.Fatalf("create conflict = %d %s", conflict.status, conflict.body)
	}

	serverOwnerB := newCreditCardHTTPIntegrationServer(
		t, secondRestartPool, ownerB, &fixedCreditCardIDGenerator{id: creditCardTestID('d')}, fixedCreditCardClock{now: archivedAt.Add(2 * time.Hour)},
	)
	defer serverOwnerB.Close()
	crossOwner := getFinancialJSON(t, serverOwnerB.Client(), serverOwnerB.URL+"/v1/cards/"+cardID)
	unknown := getFinancialJSON(t, serverOwnerB.Client(), serverOwnerB.URL+"/v1/cards/"+creditCardTestID('f'))
	if crossOwner.status != http.StatusNotFound || unknown.status != http.StatusNotFound || !bytes.Equal(crossOwner.body, unknown.body) {
		t.Fatalf("cross-owner enumeration: cross=%d %s unknown=%d %s", crossOwner.status, crossOwner.body, unknown.status, unknown.body)
	}
	ownerBList := getFinancialJSON(t, serverOwnerB.Client(), serverOwnerB.URL+"/v1/cards")
	if ownerBList.status != http.StatusOK || !bytes.Equal(ownerBList.body, []byte("{\"items\":[]}\n")) {
		t.Fatalf("owner B list = %d %s", ownerBList.status, ownerBList.body)
	}
	crossArchive := postFinancialJSON(t, serverOwnerB.Client(), serverOwnerB.URL+"/v1/cards/"+cardID+"/archive", nil, "http-cross-owner")
	unknownArchive := postFinancialJSON(t, serverOwnerB.Client(), serverOwnerB.URL+"/v1/cards/"+creditCardTestID('f')+"/archive", nil, "http-unknown")
	if crossArchive.status != http.StatusNotFound || unknownArchive.status != http.StatusNotFound || !bytes.Equal(crossArchive.body, unknownArchive.body) {
		t.Fatalf("cross-owner archive enumeration: cross=%d %s unknown=%d %s", crossArchive.status, crossArchive.body, unknownArchive.status, unknownArchive.body)
	}

	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertCreditCardIdempotencyKeyCount(t, ctx, pool, ownerB, "http-cross-owner", 0)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)
	assertCreditCardHTTPPostconditions(t, ctx, pool, ownerA)
}

func newCreditCardHTTPIntegrationServer(
	t *testing.T,
	pool *pgxpool.Pool,
	owner string,
	generator application.CreditCardIDGenerator,
	clock application.Clock,
) *httptest.Server {
	t.Helper()
	repository := newCreditCardRepository(t, pool)
	record := newRecordCreditCardUseCase(t, repository, generator, clock)
	list, err := application.NewListCreditCards(repository)
	if err != nil {
		t.Fatalf("NewListCreditCards() error = %v", err)
	}
	get, err := application.NewGetCreditCard(repository)
	if err != nil {
		t.Fatalf("NewGetCreditCard() error = %v", err)
	}
	archive := newArchiveCreditCardUseCase(t, repository, clock)
	routes := httpapi.NewCreditCard(owner, application.PreviewCreditCard{}, record, list, get, archive)
	mux := http.NewServeMux()
	routes.Register(mux)
	return httptest.NewServer(mux)
}

func assertCreditCardHTTPPostconditions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, owner string) {
	t.Helper()
	var createdAudits, archivedAudits, createCompletions, archiveCompletions int
	if err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE event_type = 'CREDIT_CARD_CREATED'),
			count(*) FILTER (WHERE event_type = 'CREDIT_CARD_ARCHIVED')
		FROM credit_card_audit_events WHERE user_id = $1
	`, owner).Scan(&createdAudits, &archivedAudits); err != nil {
		t.Fatal("reading credit card HTTP audits failed")
	}
	if err := pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE operation = 'CREATE_CREDIT_CARD' AND state = 'COMPLETED'),
			count(*) FILTER (WHERE operation = 'ARCHIVE_CREDIT_CARD' AND state = 'COMPLETED')
		FROM credit_card_idempotency_records WHERE user_id = $1
	`, owner).Scan(&createCompletions, &archiveCompletions); err != nil {
		t.Fatal("reading credit card HTTP idempotency failed")
	}
	if createdAudits != 1 || archivedAudits != 1 || createCompletions != 1 || archiveCompletions != 1 {
		t.Fatalf("HTTP postconditions audits=%d/%d completions=%d/%d, want 1/1/1/1",
			createdAudits, archivedAudits, createCompletions, archiveCompletions)
	}
}
