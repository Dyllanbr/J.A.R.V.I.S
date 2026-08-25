//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestRecurrenceSuggestionExpenseReaderBoundariesOrderingAndOwnerIsolation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerB = "usr_suggestion_expense_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)
	expenseRepository := newRepository(t, pool)
	repository := newRecurrenceSuggestionRepository(t, pool)

	start := time.Date(2025, 12, 31, 3, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)
	fixtures := []domain.Expense{
		newSuggestionExpense(t, "exp-before-window", syntheticUserID, "Antes", 100, start.Add(-time.Microsecond), nil),
		newSuggestionExpense(t, "exp-boundary-b", syntheticUserID, "Limite B", 200, start, nil),
		newSuggestionExpense(t, "exp-boundary-a", syntheticUserID, "Limite A", 300, start, nil),
		newSuggestionExpense(t, "exp-with-category", syntheticUserID, "Com categoria", 400, time.Date(2026, 1, 15, 15, 0, 0, 0, time.UTC), categoryPointer(domain.CategoryIDExpenseSubscriptions)),
		newSuggestionExpense(t, "exp-before-upper", syntheticUserID, "Antes do fim", 500, end.Add(-time.Microsecond), nil),
		newSuggestionExpense(t, "exp-at-upper", syntheticUserID, "No fim", 600, end, nil),
		newSuggestionExpense(t, "exp-after-upper", syntheticUserID, "Depois", 700, end.Add(time.Microsecond), nil),
		newSuggestionExpense(t, "exp-owner-b", ownerB, "Outro owner", 800, start.Add(time.Hour), nil),
		newSuggestionExpenseWithTimezone(t, "exp-other-timezone", syntheticUserID, "Outro fuso", 900, start.Add(3*time.Hour), "UTC"),
	}
	for _, expense := range fixtures {
		if err := expenseRepository.Save(ctx, expense); err != nil {
			t.Fatalf("saving Expense %s failed: %v", expense.ID(), err)
		}
	}
	if err := insertRawFinancialTransaction(
		ctx,
		pool,
		"inc-inside-suggestion-window",
		syntheticUserID,
		"INCOME",
		nil,
		99900,
		start.Add(2*time.Hour),
	); err != nil {
		t.Fatalf("Income fixture failed: %v", err)
	}

	items, err := repository.ListRecurrenceSuggestionExpenses(ctx, application.RecurrenceSuggestionExpenseQuery{
		UserID:            syntheticUserID,
		Start:             start,
		End:               end,
		FinancialTimezone: application.FinancialTimezone,
	})
	if err != nil {
		t.Fatalf("ListRecurrenceSuggestionExpenses() error = %v", err)
	}
	wantIDs := []string{"exp-boundary-a", "exp-boundary-b", "exp-with-category", "exp-before-upper"}
	if len(items) != len(wantIDs) {
		t.Fatalf("expense count = %d, want %d", len(items), len(wantIDs))
	}
	for index, wantID := range wantIDs {
		if items[index].ID() != wantID {
			t.Fatalf("expense[%d].ID() = %q, want %q", index, items[index].ID(), wantID)
		}
		if items[index].UserID() != syntheticUserID || items[index].Type() != domain.TransactionTypeExpense {
			t.Fatalf("expense[%d] escaped owner/type scope", index)
		}
	}
	if items[2].Description() != "Com categoria" || items[2].Amount().MinorUnits() != 400 || !items[2].OccurredAt().Equal(time.Date(2026, 1, 15, 15, 0, 0, 0, time.UTC)) {
		t.Fatal("stored Expense fields were not reconstructed losslessly")
	}
	if category, ok := items[2].CategoryID(); !ok || category != domain.CategoryIDExpenseSubscriptions {
		t.Fatal("categorized Expense was not reconstructed")
	}

	empty, err := repository.ListRecurrenceSuggestionExpenses(ctx, application.RecurrenceSuggestionExpenseQuery{
		UserID: syntheticUserID, Start: end.AddDate(1, 0, 0), End: end.AddDate(1, 0, 1),
		FinancialTimezone: application.FinancialTimezone,
	})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty expense result = %#v, err %v", empty, err)
	}
	cancelled, cancelCall := context.WithCancel(ctx)
	cancelCall()
	if _, err := repository.ListRecurrenceSuggestionExpenses(cancelled, application.RecurrenceSuggestionExpenseQuery{
		UserID: syntheticUserID, Start: start, End: end, FinancialTimezone: application.FinancialTimezone,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled expense query error = %v", err)
	}
}

func TestRecurrenceSuggestionSuppressionStoreReaderReplayConflictAndRestart(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerB = "usr_suggestion_suppression_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)
	repository := newRecurrenceSuggestionRepository(t, pool)
	fingerprintA := suggestionFingerprint(0x11)
	fingerprintB := suggestionFingerprint(0x33)
	idA := derivedSuggestionID(t, syntheticUserID, fingerprintA)
	idB := derivedSuggestionID(t, syntheticUserID, fingerprintB)
	dismissedAt := time.Date(2026, 8, 24, 17, 0, 0, 123_456_000, time.UTC)
	command := application.DismissRecurrenceSuggestionCommand{
		Operation: application.DismissRecurrenceSuggestionOperation, UserID: syntheticUserID,
		SuggestionID: idA, Fingerprint: fingerprintA, DismissedAt: dismissedAt,
	}

	first, err := repository.DismissRecurrenceSuggestion(ctx, command)
	if err != nil || first.Replayed {
		t.Fatalf("first dismiss = %+v, err %v", first, err)
	}
	replayCommand := command
	replayCommand.DismissedAt = dismissedAt.Add(time.Hour)
	replay, err := repository.DismissRecurrenceSuggestion(ctx, replayCommand)
	if err != nil || !replay.Replayed {
		t.Fatalf("repeat dismiss = %+v, err %v", replay, err)
	}
	assertStoredSuggestionSuppression(t, ctx, pool, syntheticUserID, idA, fingerprintA, dismissedAt)

	restartPool := openRestartPool(t, ctx, pool)
	restarted := newRecurrenceSuggestionRepository(t, restartPool)
	restartReplay, err := restarted.DismissRecurrenceSuggestion(ctx, replayCommand)
	if err != nil || !restartReplay.Replayed {
		t.Fatalf("restart replay = %+v, err %v", restartReplay, err)
	}

	conflicting := command
	conflicting.Fingerprint = suggestionFingerprint(0x22)
	if _, err := repository.DismissRecurrenceSuggestion(ctx, conflicting); !errors.Is(err, adapter.ErrRecurrenceSuggestionSuppressionConflict) {
		t.Fatalf("conflicting fingerprint error = %v", err)
	}
	invalidOperation := command
	invalidOperation.Operation = "CREATE_RECURRENCE"
	if _, err := repository.DismissRecurrenceSuggestion(ctx, invalidOperation); !errors.Is(err, adapter.ErrInvalidRecurrenceSuggestionCommand) {
		t.Fatalf("incompatible operation error = %v", err)
	}
	assertStoredSuggestionSuppression(t, ctx, pool, syntheticUserID, idA, fingerprintA, dismissedAt)

	ownerBCommand := command
	ownerBCommand.UserID = ownerB
	ownerBCommand.SuggestionID = derivedSuggestionID(t, ownerB, fingerprintA)
	ownerBCommand.DismissedAt = dismissedAt.Add(2 * time.Hour)
	ownerBResult, err := repository.DismissRecurrenceSuggestion(ctx, ownerBCommand)
	if err != nil || ownerBResult.Replayed {
		t.Fatalf("owner B independent dismiss = %+v, err %v", ownerBResult, err)
	}
	if err := insertRawSuggestionSuppression(
		ctx,
		pool,
		syntheticUserID,
		string(idB),
		application.DismissRecurrenceSuggestionOperation,
		fingerprintB[:],
		dismissedAt.Add(3*time.Hour),
	); err != nil {
		t.Fatalf("second suppression fixture failed: %v", err)
	}

	empty, err := repository.ListDismissedRecurrenceSuggestionIDs(ctx, syntheticUserID, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty suppression lookup = %#v, err %v", empty, err)
	}
	missing := application.RecurrenceSuggestionID(recurrenceSuggestionTestID('c'))
	listed, err := repository.ListDismissedRecurrenceSuggestionIDs(ctx, syntheticUserID, []application.RecurrenceSuggestionID{idB, missing, idA, idB})
	if err != nil {
		t.Fatalf("batch suppression lookup failed: %v", err)
	}
	wantListed := []application.RecurrenceSuggestionID{idA, idB}
	sort.Slice(wantListed, func(left, right int) bool { return wantListed[left] < wantListed[right] })
	if len(listed) != 2 || listed[0] != wantListed[0] || listed[1] != wantListed[1] {
		t.Fatalf("batch suppression IDs = %v, want %v", listed, wantListed)
	}
	ownerBListed, err := repository.ListDismissedRecurrenceSuggestionIDs(
		ctx,
		ownerB,
		[]application.RecurrenceSuggestionID{ownerBCommand.SuggestionID, idB},
	)
	if err != nil || len(ownerBListed) != 1 || ownerBListed[0] != ownerBCommand.SuggestionID {
		t.Fatalf("owner B suppression IDs = %v, err %v", ownerBListed, err)
	}

	canceled, cancelCall := context.WithCancel(ctx)
	cancelCall()
	if _, err := repository.ListDismissedRecurrenceSuggestionIDs(canceled, syntheticUserID, []application.RecurrenceSuggestionID{idA}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled suppression read error = %v", err)
	}
	canceledFingerprint := suggestionFingerprint(0x44)
	canceledID := derivedSuggestionID(t, syntheticUserID, canceledFingerprint)
	canceledCommand := command
	canceledCommand.SuggestionID = canceledID
	canceledCommand.Fingerprint = canceledFingerprint
	if _, err := repository.DismissRecurrenceSuggestion(canceled, canceledCommand); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled suppression write error = %v", err)
	}
	if count := countSuggestionSuppressions(t, ctx, pool, syntheticUserID, canceledID); count != 0 {
		t.Fatalf("canceled write row count = %d, want 0", count)
	}
}

func TestRecurrenceSuggestionSuppressionReaderRejectsSemanticCorruptionWithoutCrossOwnerContamination(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerB = "usr_suggestion_corruption_owner_b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)
	repository := newRecurrenceSuggestionRepository(t, pool)
	dismissedAt := time.Date(2026, 8, 24, 20, 0, 0, 123_456_000, time.UTC)

	identityFingerprint := suggestionFingerprint(0x61)
	storedIncompatibleFingerprint := suggestionFingerprint(0x62)
	corruptedID := derivedSuggestionID(t, syntheticUserID, identityFingerprint)
	if err := insertRawSuggestionSuppression(
		ctx,
		pool,
		syntheticUserID,
		string(corruptedID),
		application.DismissRecurrenceSuggestionOperation,
		storedIncompatibleFingerprint[:],
		dismissedAt,
	); err != nil {
		t.Fatalf("structurally valid semantic-corruption fixture was rejected: %v", err)
	}
	if count := countSuggestionSuppressions(t, ctx, pool, syntheticUserID, corruptedID); count != 1 {
		t.Fatalf("semantic-corruption fixture row count = %d, want 1", count)
	}

	dismissed, err := repository.ListDismissedRecurrenceSuggestionIDs(
		ctx,
		syntheticUserID,
		[]application.RecurrenceSuggestionID{corruptedID},
	)
	if !errors.Is(err, adapter.ErrInvalidStoredSuggestionSuppression) {
		t.Fatalf("semantic-corruption read error = %v, want invalid stored suppression", err)
	}
	if dismissed != nil {
		t.Fatalf("semantic-corruption read returned partial suppressions: %v", dismissed)
	}
	publicMessage := strings.ToLower(err.Error())
	for _, unsafeFragment := range []string{
		"select ",
		"insert ",
		"postgres://",
		"recurrence_suggestion_suppressions_fingerprint_sha256",
		strings.ToLower(string(corruptedID)),
		strings.ToLower(syntheticUserID),
		strings.Repeat("62", len(storedIncompatibleFingerprint)),
	} {
		if strings.Contains(publicMessage, unsafeFragment) {
			t.Fatalf("semantic-corruption error exposed unsafe detail %q: %q", unsafeFragment, err.Error())
		}
	}

	validFingerprint := suggestionFingerprint(0x63)
	validID := derivedSuggestionID(t, syntheticUserID, validFingerprint)
	if err := insertRawSuggestionSuppression(
		ctx,
		pool,
		syntheticUserID,
		string(validID),
		application.DismissRecurrenceSuggestionOperation,
		validFingerprint[:],
		dismissedAt.Add(time.Microsecond),
	); err != nil {
		t.Fatalf("valid owner A suppression fixture failed: %v", err)
	}
	// The same technical ID is structurally legal for another owner, but this
	// owner B row is semantically incompatible with its stored fingerprint.
	if err := insertRawSuggestionSuppression(
		ctx,
		pool,
		ownerB,
		string(validID),
		application.DismissRecurrenceSuggestionOperation,
		storedIncompatibleFingerprint[:],
		dismissedAt.Add(2*time.Microsecond),
	); err != nil {
		t.Fatalf("owner B semantic-corruption fixture failed: %v", err)
	}

	ownerAResult, err := repository.ListDismissedRecurrenceSuggestionIDs(
		ctx,
		syntheticUserID,
		[]application.RecurrenceSuggestionID{validID},
	)
	if err != nil || len(ownerAResult) != 1 || ownerAResult[0] != validID {
		t.Fatalf("owner B corruption contaminated owner A: result=%v err=%v", ownerAResult, err)
	}
	ownerBResult, err := repository.ListDismissedRecurrenceSuggestionIDs(
		ctx,
		ownerB,
		[]application.RecurrenceSuggestionID{validID},
	)
	if !errors.Is(err, adapter.ErrInvalidStoredSuggestionSuppression) || ownerBResult != nil {
		t.Fatalf("owner B semantic-corruption read = %v, err %v", ownerBResult, err)
	}

	mixed, err := repository.ListDismissedRecurrenceSuggestionIDs(
		ctx,
		syntheticUserID,
		[]application.RecurrenceSuggestionID{validID, corruptedID},
	)
	if !errors.Is(err, adapter.ErrInvalidStoredSuggestionSuppression) {
		t.Fatalf("mixed valid/corrupt read error = %v, want invalid stored suppression", err)
	}
	if mixed != nil {
		t.Fatalf("mixed valid/corrupt read returned partial suppressions: %v", mixed)
	}
}

func TestRecurrenceSuggestionSuppressionStoreConcurrentAuthority(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	repository := newRecurrenceSuggestionRepository(t, pool)
	fingerprint := suggestionFingerprint(0x44)
	id := derivedSuggestionID(t, syntheticUserID, fingerprint)
	command := application.DismissRecurrenceSuggestionCommand{
		Operation: application.DismissRecurrenceSuggestionOperation,
		UserID:    syntheticUserID, SuggestionID: id, Fingerprint: fingerprint,
		DismissedAt: time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC),
	}

	const callers = 12
	start := make(chan struct{})
	results := make(chan application.DismissRecurrenceSuggestionStoreResult, callers)
	errorsChannel := make(chan error, callers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer waitGroup.Done()
			<-start
			result, err := repository.DismissRecurrenceSuggestion(ctx, command)
			results <- result
			errorsChannel <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent dismiss error = %v", err)
		}
	}
	firstWrites := 0
	for result := range results {
		if !result.Replayed {
			firstWrites++
		}
	}
	if firstWrites != 1 {
		t.Fatalf("concurrent first writes = %d, want 1", firstWrites)
	}
	if count := countSuggestionSuppressions(t, ctx, pool, syntheticUserID, id); count != 1 {
		t.Fatalf("concurrent physical row count = %d, want 1", count)
	}
}

func TestRecurrenceSuggestionApplicationPostgresDismissNewEvidenceAndRecurrenceExclusions(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	const cancelledOwner = "usr_suggestion_cancelled_owner"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, cancelledOwner)
	expenseRepository := newRepository(t, pool)
	suggestionRepository := newRecurrenceSuggestionRepository(t, pool)
	recurrenceRepository := newRecurrenceRepository(t, pool)
	clock := &mutableSuggestionClock{now: time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC)}
	list := newListRecurrenceSuggestions(t, suggestionRepository, recurrenceRepository, clock)
	dismiss := newDismissRecurrenceSuggestion(t, list, suggestionRepository, clock)

	for index, occurredAt := range []time.Time{
		suggestionOccurrence(2026, time.May, 10),
		suggestionOccurrence(2026, time.June, 10),
		suggestionOccurrence(2026, time.July, 10),
	} {
		expense := newSuggestionExpense(t, "exp-suggestion-evidence-"+string(rune('a'+index)), syntheticUserID, "Serviço sintético recorrente", 11900, occurredAt, nil)
		if err := expenseRepository.Save(ctx, expense); err != nil {
			t.Fatalf("saving suggestion evidence failed: %v", err)
		}
	}
	initial, err := list.Execute(ctx, syntheticUserID)
	if err != nil || len(initial.Items) != 1 {
		t.Fatalf("initial suggestion list = %+v, err %v", initial, err)
	}
	initialID := initial.Items[0].ID
	financialBefore := financialSubsystemCounts(t, ctx, pool)
	firstDismiss, err := dismiss.Execute(ctx, application.DismissRecurrenceSuggestionInput{UserID: syntheticUserID, SuggestionID: initialID})
	if err != nil || firstDismiss.Replayed {
		t.Fatalf("first application dismiss = %+v, err %v", firstDismiss, err)
	}
	afterDismiss, err := list.Execute(ctx, syntheticUserID)
	if err != nil || len(afterDismiss.Items) != 0 {
		t.Fatalf("list after dismiss = %+v, err %v", afterDismiss, err)
	}
	replayedDismiss, err := dismiss.Execute(ctx, application.DismissRecurrenceSuggestionInput{UserID: syntheticUserID, SuggestionID: initialID})
	if err != nil || !replayedDismiss.Replayed {
		t.Fatalf("application dismiss replay = %+v, err %v", replayedDismiss, err)
	}
	if got := financialSubsystemCounts(t, ctx, pool); got != financialBefore {
		t.Fatalf("dismiss/list changed financial subsystem counts: before=%+v after=%+v", financialBefore, got)
	}

	clock.now = time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	august := newSuggestionExpense(t, "exp-suggestion-evidence-d", syntheticUserID, "Serviço sintético recorrente", 11900, suggestionOccurrence(2026, time.August, 10), nil)
	if err := expenseRepository.Save(ctx, august); err != nil {
		t.Fatalf("saving new suggestion evidence failed: %v", err)
	}
	newEvidence, err := list.Execute(ctx, syntheticUserID)
	if err != nil || len(newEvidence.Items) != 1 {
		t.Fatalf("new-evidence suggestion list = %+v, err %v", newEvidence, err)
	}
	if newEvidence.Items[0].ID == initialID {
		t.Fatal("material new evidence reused the suppressed SuggestionID")
	}

	activeCreatedAt := time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC)
	if err := insertRawRecurrence(ctx, pool, "rec-active-suggestion-block", syntheticUserID, "2026-09-10", "ACTIVE", activeCreatedAt, nil); err != nil {
		t.Fatalf("ACTIVE Recurrence fixture failed: %v", err)
	}
	blocked, err := list.Execute(ctx, syntheticUserID)
	if err != nil || len(blocked.Items) != 0 {
		t.Fatalf("ACTIVE Recurrence did not block suggestion: %+v, err %v", blocked, err)
	}

	for index, occurredAt := range []time.Time{
		suggestionOccurrence(2026, time.March, 10),
		suggestionOccurrence(2026, time.April, 10),
		suggestionOccurrence(2026, time.May, 10),
		suggestionOccurrence(2026, time.June, 10),
		suggestionOccurrence(2026, time.July, 10),
		suggestionOccurrence(2026, time.August, 10),
	} {
		expense := newSuggestionExpense(t, "exp-post-cancel-"+string(rune('a'+index)), cancelledOwner, "Serviço sintético recorrente", 11900, occurredAt, nil)
		if err := expenseRepository.Save(ctx, expense); err != nil {
			t.Fatalf("saving cancelled-owner evidence failed: %v", err)
		}
	}
	cancelledAt := time.Date(2026, 5, 15, 15, 0, 0, 0, time.UTC)
	if err := insertRawRecurrence(ctx, pool, "rec-cancelled-suggestion-cutoff", cancelledOwner, "2026-02-10", "CANCELLED", activeCreatedAt, cancelledAt); err != nil {
		t.Fatalf("CANCELLED Recurrence fixture failed: %v", err)
	}
	cancelledOwnerResult, err := list.Execute(ctx, cancelledOwner)
	if err != nil || len(cancelledOwnerResult.Items) != 1 || cancelledOwnerResult.Items[0].Suggestion.EvidenceCount() != 3 {
		t.Fatalf("post-cancellation suggestion = %+v, err %v", cancelledOwnerResult, err)
	}
}

func TestRecurrenceSuggestionRepositoryReturnsSafeDatabaseFailure(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	repository := newRecurrenceSuggestionRepository(t, pool)
	pool.Close()
	empty, emptyErr := repository.ListDismissedRecurrenceSuggestionIDs(ctx, syntheticUserID, nil)
	if emptyErr != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty lookup touched closed pool: result=%v err=%v", empty, emptyErr)
	}
	fingerprint := suggestionFingerprint(0x55)
	command := application.DismissRecurrenceSuggestionCommand{
		Operation:    application.DismissRecurrenceSuggestionOperation,
		UserID:       syntheticUserID,
		SuggestionID: derivedSuggestionID(t, syntheticUserID, fingerprint),
		Fingerprint:  fingerprint,
		DismissedAt:  time.Date(2026, 8, 24, 19, 0, 0, 0, time.UTC),
	}
	_, err := repository.DismissRecurrenceSuggestion(ctx, command)
	if !errors.Is(err, adapter.ErrInsertRecurrenceSuggestionSuppression) || err.Error() != adapter.ErrInsertRecurrenceSuggestionSuppression.Error() {
		t.Fatalf("closed pool error = %v, want safe insertion category", err)
	}
}

type mutableSuggestionClock struct {
	now time.Time
}

func (clock *mutableSuggestionClock) Now() time.Time { return clock.now }

func newRecurrenceSuggestionRepository(t testing.TB, pool *pgxpool.Pool) *adapter.RecurrenceSuggestionRepository {
	t.Helper()
	repository, err := adapter.NewRecurrenceSuggestionRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewRecurrenceSuggestionRepository() error = %v", err)
	}
	return repository
}

func newListRecurrenceSuggestions(
	t testing.TB,
	suggestions *adapter.RecurrenceSuggestionRepository,
	recurrences *adapter.RecurrenceRepository,
	clock application.Clock,
) *application.ListRecurrenceSuggestions {
	t.Helper()
	useCase, err := application.NewListRecurrenceSuggestions(suggestions, recurrences, suggestions, clock)
	if err != nil {
		t.Fatalf("NewListRecurrenceSuggestions() error = %v", err)
	}
	return useCase
}

func newDismissRecurrenceSuggestion(
	t testing.TB,
	list *application.ListRecurrenceSuggestions,
	repository *adapter.RecurrenceSuggestionRepository,
	clock application.Clock,
) *application.DismissRecurrenceSuggestion {
	t.Helper()
	useCase, err := application.NewDismissRecurrenceSuggestion(list, repository, repository, clock)
	if err != nil {
		t.Fatalf("NewDismissRecurrenceSuggestion() error = %v", err)
	}
	return useCase
}

func newSuggestionExpense(
	t testing.TB,
	id, userID, description string,
	amountMinor int64,
	occurredAt time.Time,
	categoryID *domain.CategoryID,
) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(amountMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID: userID, Description: description, Amount: amount,
			PaymentMethod: domain.PaymentMethodPIX, CategoryID: categoryID,
			OccurredAt: occurredAt, FinancialTimezone: application.FinancialTimezone,
			Origin: domain.OriginIOS,
		},
		CreatedAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("NewExpense() error = %v", err)
	}
	return expense
}

func newSuggestionExpenseWithTimezone(
	t testing.TB,
	id, userID, description string,
	amountMinor int64,
	occurredAt time.Time,
	timezone string,
) domain.Expense {
	t.Helper()
	amount, err := domain.NewMoney(amountMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	expense, err := domain.NewExpense(domain.ExpenseParams{
		ID: id,
		Details: domain.ExpenseDetails{
			UserID: userID, Description: description, Amount: amount,
			PaymentMethod: domain.PaymentMethodPIX, OccurredAt: occurredAt,
			FinancialTimezone: timezone, Origin: domain.OriginIOS,
		},
		CreatedAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("NewExpense() error = %v", err)
	}
	return expense
}

func categoryPointer(value domain.CategoryID) *domain.CategoryID { return &value }

func suggestionOccurrence(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 15, 0, 0, 0, time.UTC)
}

func suggestionFingerprint(value byte) application.RecurrenceSuggestionFingerprint {
	var fingerprint application.RecurrenceSuggestionFingerprint
	for index := range fingerprint {
		fingerprint[index] = value
	}
	return fingerprint
}

func derivedSuggestionID(
	t testing.TB,
	userID string,
	fingerprint application.RecurrenceSuggestionFingerprint,
) application.RecurrenceSuggestionID {
	t.Helper()
	id, err := application.DeriveRecurrenceSuggestionID(userID, fingerprint)
	if err != nil {
		t.Fatalf("DeriveRecurrenceSuggestionID() error = %v", err)
	}
	return id
}

func assertStoredSuggestionSuppression(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID string,
	id application.RecurrenceSuggestionID,
	fingerprint application.RecurrenceSuggestionFingerprint,
	dismissedAt time.Time,
) {
	t.Helper()
	var operation string
	var storedFingerprint []byte
	var storedDismissedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT operation, evidence_fingerprint, dismissed_at
		FROM recurrence_suggestion_suppressions
		WHERE user_id = $1 AND suggestion_id = $2
	`, userID, id).Scan(&operation, &storedFingerprint, &storedDismissedAt); err != nil {
		t.Fatal("stored suppression lookup failed")
	}
	if operation != application.DismissRecurrenceSuggestionOperation ||
		!bytes.Equal(storedFingerprint, fingerprint[:]) ||
		!storedDismissedAt.Equal(dismissedAt) {
		t.Fatalf("stored suppression changed: operation=%q fingerprint=%x dismissedAt=%s", operation, storedFingerprint, storedDismissedAt)
	}
}

func countSuggestionSuppressions(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID string,
	id application.RecurrenceSuggestionID,
) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM recurrence_suggestion_suppressions
		WHERE user_id = $1 AND suggestion_id = $2
	`, userID, id).Scan(&count); err != nil {
		t.Fatal("suppression count failed")
	}
	return count
}

type suggestionFinancialCounts struct {
	transactions, auditEvents, idempotencyRecords        int
	recurrences, recurrenceAudits, recurrenceIdempotency int
}

func financialSubsystemCounts(t testing.TB, ctx context.Context, pool *pgxpool.Pool) suggestionFinancialCounts {
	t.Helper()
	queries := []string{
		"SELECT count(*) FROM transactions",
		"SELECT count(*) FROM audit_events",
		"SELECT count(*) FROM idempotency_records",
		"SELECT count(*) FROM recurrences",
		"SELECT count(*) FROM recurrence_audit_events",
		"SELECT count(*) FROM recurrence_idempotency_records",
	}
	values := make([]int, len(queries))
	for index, query := range queries {
		if err := pool.QueryRow(ctx, query).Scan(&values[index]); err != nil {
			t.Fatal("financial subsystem count failed")
		}
	}
	return suggestionFinancialCounts{
		transactions: values[0], auditEvents: values[1], idempotencyRecords: values[2],
		recurrences: values[3], recurrenceAudits: values[4], recurrenceIdempotency: values[5],
	}
}
