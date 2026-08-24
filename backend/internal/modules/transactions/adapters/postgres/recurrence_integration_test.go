//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestRecurrenceCreateCancelReplayRestartHistoricalSnapshotOwnerAndTransactionSeparation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	const ownerB = "usr-recurrence-owner-b"
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	insertSyntheticUser(t, ctx, pool, ownerB)

	repository := newRecurrenceRepository(t, pool)
	createClock := &recurrenceFixedClock{now: time.Date(2026, 8, 16, 12, 0, 0, 123_456_789, time.UTC)}
	createID := &recurrenceFixedIDGenerator{id: "rec-persistent-001"}
	create := newRecordRecurrenceUseCase(t, repository, createID, createClock)
	createInput := recurrenceRecordInput(t, syntheticUserID, "create-persistent-key")
	created, err := create.Execute(ctx, createInput)
	if err != nil {
		t.Fatalf("RecordRecurrence.Execute() error = %v", err)
	}
	if created.Replayed || created.Recurrence.Status() != domain.RecurrenceStatusActive ||
		created.Recurrence.CreatedAt().Nanosecond() != 123_456_000 {
		t.Fatal("new recurrence did not preserve ACTIVE state and microsecond timestamp")
	}

	assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 1, 1)
	assertNoTransactionSubsystemWrites(t, ctx, pool)
	otherOwnerReplay, err := repository.FindRecurrenceReplay(ctx, application.RecurrenceReplayQuery{
		UserID:         ownerB,
		Operation:      application.IdempotencyOperationCreateRecurrence,
		IdempotencyKey: createInput.IdempotencyKey,
		Fingerprint:    recurrenceFingerprint(99),
	})
	if err != nil || otherOwnerReplay.Found {
		t.Fatalf("cross-owner replay lookup = %+v, error = %v", otherOwnerReplay, err)
	}
	if _, err := repository.FindRecurrenceReplay(ctx, application.RecurrenceReplayQuery{
		UserID:         syntheticUserID,
		Operation:      application.IdempotencyOperationCreateRecurrence,
		IdempotencyKey: createInput.IdempotencyKey,
		Fingerprint:    recurrenceFingerprint(99),
	}); !errors.Is(err, application.ErrRecurrenceIdempotencyConflict) {
		t.Fatalf("persisted replay conflict error = %v", err)
	}

	restartPool := openRestartPool(t, ctx, pool)
	restartRepository := newRecurrenceRepository(t, restartPool)
	unusedID := &recurrenceFixedIDGenerator{id: "rec-unused-replay"}
	unusedCreateClock := &recurrenceFixedClock{now: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)}
	createReplayUseCase := newRecordRecurrenceUseCase(t, restartRepository, unusedID, unusedCreateClock)
	createReplay, err := createReplayUseCase.Execute(ctx, createInput)
	if err != nil || !createReplay.Replayed || createReplay.Recurrence.ID() != created.Recurrence.ID() {
		t.Fatalf("CREATE replay after restart = %+v, error = %v", createReplay, err)
	}
	if unusedID.calls != 0 || unusedCreateClock.calls != 0 {
		t.Fatal("persisted CREATE replay consumed ID or Clock")
	}

	cancelClock := &recurrenceFixedClock{now: time.Date(2026, 9, 1, 14, 0, 0, 987_654_321, time.UTC)}
	cancelUseCase := newCancelRecurrenceUseCase(t, restartRepository, cancelClock)
	cancelInput := application.CancelRecurrenceInput{
		UserID:         syntheticUserID,
		RecurrenceID:   created.Recurrence.ID(),
		IdempotencyKey: createInput.IdempotencyKey,
	}
	cancelled, err := cancelUseCase.Execute(ctx, cancelInput)
	if err != nil {
		t.Fatalf("CancelRecurrence.Execute() error = %v", err)
	}
	cancelledAt, ok := cancelled.Recurrence.CancelledAt()
	if !ok || cancelledAt.Nanosecond() != 987_654_000 {
		t.Fatal("cancellation timestamp did not roundtrip at microsecond precision")
	}
	assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertNoTransactionSubsystemWrites(t, ctx, pool)
	_, err = newCancelRecurrenceUseCase(
		t,
		restartRepository,
		&recurrenceFixedClock{now: time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC)},
	).Execute(ctx, application.CancelRecurrenceInput{
		UserID:         syntheticUserID,
		RecurrenceID:   created.Recurrence.ID(),
		IdempotencyKey: "new-key-after-cancel",
	})
	if !errors.Is(err, domain.ErrRecurrenceAlreadyCancelled) {
		t.Fatalf("already-cancelled recurrence with new key error = %v", err)
	}
	assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)

	secondRestartPool := openRestartPool(t, ctx, pool)
	secondRestartRepository := newRecurrenceRepository(t, secondRestartPool)
	createAfterCancel := newRecordRecurrenceUseCase(
		t,
		secondRestartRepository,
		&recurrenceFixedIDGenerator{id: "rec-never-generated"},
		&recurrenceFixedClock{now: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	)
	historicalCreate, err := createAfterCancel.Execute(ctx, createInput)
	if err != nil || !historicalCreate.Replayed {
		t.Fatalf("historical CREATE replay after cancellation error = %v", err)
	}
	if historicalCreate.Recurrence.Status() != domain.RecurrenceStatusActive {
		t.Fatalf("CREATE replay returned current %s state instead of original ACTIVE snapshot", historicalCreate.Recurrence.Status())
	}
	if _, hasCancellation := historicalCreate.Recurrence.CancelledAt(); hasCancellation {
		t.Fatal("historical CREATE replay acquired a later cancellation timestamp")
	}

	cancelReplayClock := &recurrenceFixedClock{now: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	cancelReplayUseCase := newCancelRecurrenceUseCase(t, secondRestartRepository, cancelReplayClock)
	cancelReplay, err := cancelReplayUseCase.Execute(ctx, cancelInput)
	if err != nil || !cancelReplay.Replayed {
		t.Fatalf("CANCEL replay after restart error = %v", err)
	}
	replayedCancelledAt, replayedHasCancellation := cancelReplay.Recurrence.CancelledAt()
	if !replayedHasCancellation || replayedCancelledAt != cancelledAt || cancelReplayClock.calls != 0 {
		t.Fatal("CANCEL replay did not preserve the original cancellation result without Clock")
	}
	assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)

	listUseCase, err := application.NewListRecurrences(secondRestartRepository)
	if err != nil {
		t.Fatal("NewListRecurrences failed")
	}
	ownerList, err := listUseCase.Execute(ctx, syntheticUserID)
	if err != nil || len(ownerList.Items) != 1 || ownerList.Items[0].Status() != domain.RecurrenceStatusCancelled {
		t.Fatalf("owner list = %+v, error = %v", ownerList, err)
	}
	emptyOtherOwner, err := listUseCase.Execute(ctx, ownerB)
	if err != nil || len(emptyOtherOwner.Items) != 0 {
		t.Fatalf("other owner list = %+v, error = %v", emptyOtherOwner, err)
	}

	crossOwnerCancel := newCancelRecurrenceUseCase(
		t,
		secondRestartRepository,
		&recurrenceFixedClock{now: time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC)},
	)
	for _, attempt := range []struct {
		recurrenceID string
		key          string
	}{
		{recurrenceID: created.Recurrence.ID(), key: "cross-owner-cancel"},
		{recurrenceID: "rec-unknown", key: "unknown-cancel"},
	} {
		_, err = crossOwnerCancel.Execute(ctx, application.CancelRecurrenceInput{
			UserID:         ownerB,
			RecurrenceID:   attempt.recurrenceID,
			IdempotencyKey: attempt.key,
		})
		if !errors.Is(err, application.ErrRecurrenceNotFound) {
			t.Fatalf("unknown/cross-owner cancellation error = %v, want not found", err)
		}
	}
	assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)

	ownerBCreate := newRecordRecurrenceUseCase(
		t,
		secondRestartRepository,
		&recurrenceFixedIDGenerator{id: "rec-owner-b"},
		&recurrenceFixedClock{now: time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)},
	)
	ownerBInput := recurrenceRecordInput(t, ownerB, createInput.IdempotencyKey)
	if _, err := ownerBCreate.Execute(ctx, ownerBInput); err != nil {
		t.Fatalf("same idempotency key for another owner was rejected: %v", err)
	}
	assertRecurrenceSubsystemCounts(t, ctx, pool, 2, 3, 3)
	ownerBList, err := listUseCase.Execute(ctx, ownerB)
	if err != nil || len(ownerBList.Items) != 1 || ownerBList.Items[0].Status() != domain.RecurrenceStatusActive {
		t.Fatalf("other owner's ACTIVE list = %+v, error = %v", ownerBList, err)
	}
	assertNoTransactionSubsystemWrites(t, ctx, pool)
}

func TestRecurrenceDateRoundtripIsIndependentOfPostgresDateStyle(t *testing.T) {
	basePool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pool := openRecurrencePoolWithDateStyle(t, ctx, basePool, "SQL, DMY")
	assertPoolDateStyle(t, ctx, pool, "SQL, DMY")
	insertSyntheticUser(t, ctx, pool, syntheticUserID)

	tests := []struct {
		id, key     string
		year, day   int
		month       time.Month
		isLifecycle bool
	}{
		{id: "rec-datestyle-leap", key: "datestyle-leap", year: 2024, month: time.February, day: 29},
		{id: "rec-datestyle-thirty", key: "datestyle-thirty", year: 2026, month: time.April, day: 30},
		{id: "rec-datestyle-lifecycle", key: "datestyle-lifecycle", year: 2026, month: time.August, day: 31, isLifecycle: true},
		{id: "rec-datestyle-december", key: "datestyle-december", year: 2026, month: time.December, day: 31},
		{id: "rec-datestyle-january", key: "datestyle-january", year: 2027, month: time.January, day: 1},
	}

	repository := newRecurrenceRepository(t, pool)
	expectedDates := make(map[string]domain.CivilDate, len(tests))
	var lifecycleInput application.RecordRecurrenceInput
	var lifecycleCreated application.RecordRecurrenceResult
	for index, test := range tests {
		startsOn, err := domain.NewCivilDate(test.year, test.month, test.day)
		if err != nil {
			t.Fatal("NewCivilDate() failed for DateStyle fixture")
		}
		input := recurrenceRecordInput(t, syntheticUserID, test.key)
		input.Recurrence.StartsOn = startsOn
		result, err := newRecordRecurrenceUseCase(
			t,
			repository,
			&recurrenceFixedIDGenerator{id: test.id},
			&recurrenceFixedClock{now: time.Date(2026, 8, 16, 12, 0, index, 123_456_789, time.UTC)},
		).Execute(ctx, input)
		if err != nil {
			t.Fatalf("creating recurrence under SQL, DMY failed: %v", err)
		}
		expectedDates[test.id] = startsOn
		if test.isLifecycle {
			lifecycleInput = input
			lifecycleCreated = result
		}
	}

	list, err := repository.ListRecurrences(ctx, syntheticUserID)
	if err != nil || len(list) != len(tests) {
		t.Fatalf("listing recurrences under SQL, DMY returned %d items, error = %v", len(list), err)
	}
	for _, recurrence := range list {
		if expected, ok := expectedDates[recurrence.ID()]; !ok || !recurrence.StartsOn().Equal(expected) {
			t.Fatalf("typed DATE roundtrip changed StartsOn for %q", recurrence.ID())
		}
	}

	restartPool := openRecurrencePoolWithDateStyle(t, ctx, pool, "SQL, DMY")
	assertPoolDateStyle(t, ctx, restartPool, "SQL, DMY")
	restartRepository := newRecurrenceRepository(t, restartPool)
	createReplayClock := &recurrenceFixedClock{now: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	createReplayID := &recurrenceFixedIDGenerator{id: "rec-datestyle-unused"}
	createReplay, err := newRecordRecurrenceUseCase(t, restartRepository, createReplayID, createReplayClock).Execute(ctx, lifecycleInput)
	if err != nil || !createReplay.Replayed || createReplay.Recurrence.ID() != lifecycleCreated.Recurrence.ID() ||
		createReplay.Recurrence.CreatedAt() != lifecycleCreated.Recurrence.CreatedAt() ||
		!createReplay.Recurrence.StartsOn().Equal(lifecycleCreated.Recurrence.StartsOn()) {
		t.Fatalf("CREATE replay under SQL, DMY = %+v, error = %v", createReplay, err)
	}
	if createReplayID.calls != 0 || createReplayClock.calls != 0 {
		t.Fatal("persisted CREATE replay under SQL, DMY consumed ID or Clock")
	}

	cancelInput := application.CancelRecurrenceInput{
		UserID:         syntheticUserID,
		RecurrenceID:   lifecycleCreated.Recurrence.ID(),
		IdempotencyKey: "datestyle-cancel",
	}
	cancelled, err := newCancelRecurrenceUseCase(
		t,
		restartRepository,
		&recurrenceFixedClock{now: time.Date(2026, 9, 1, 12, 0, 0, 654_321_987, time.UTC)},
	).Execute(ctx, cancelInput)
	if err != nil || !cancelled.Recurrence.StartsOn().Equal(lifecycleCreated.Recurrence.StartsOn()) {
		t.Fatalf("cancelling recurrence under SQL, DMY = %+v, error = %v", cancelled, err)
	}
	cancelledAt, ok := cancelled.Recurrence.CancelledAt()
	if !ok {
		t.Fatal("cancellation under SQL, DMY lost CancelledAt")
	}

	secondRestartPool := openRecurrencePoolWithDateStyle(t, ctx, restartPool, "SQL, DMY")
	secondRestartRepository := newRecurrenceRepository(t, secondRestartPool)
	cancelReplayClock := &recurrenceFixedClock{now: time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC)}
	cancelReplay, err := newCancelRecurrenceUseCase(t, secondRestartRepository, cancelReplayClock).Execute(ctx, cancelInput)
	replayedCancelledAt, hasCancellation := cancelReplay.Recurrence.CancelledAt()
	if err != nil || !cancelReplay.Replayed || cancelReplayClock.calls != 0 || !hasCancellation ||
		replayedCancelledAt != cancelledAt ||
		!cancelReplay.Recurrence.StartsOn().Equal(lifecycleCreated.Recurrence.StartsOn()) {
		t.Fatalf("CANCEL replay under SQL, DMY = %+v, error = %v", cancelReplay, err)
	}

	historicalCreate, err := newRecordRecurrenceUseCase(
		t,
		secondRestartRepository,
		&recurrenceFixedIDGenerator{id: "rec-datestyle-never-generated"},
		&recurrenceFixedClock{now: time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC)},
	).Execute(ctx, lifecycleInput)
	_, historicalHasCancellation := historicalCreate.Recurrence.CancelledAt()
	if err != nil || !historicalCreate.Replayed || historicalCreate.Recurrence.Status() != domain.RecurrenceStatusActive ||
		historicalHasCancellation || !historicalCreate.Recurrence.StartsOn().Equal(lifecycleCreated.Recurrence.StartsOn()) {
		t.Fatalf("historical CREATE replay under SQL, DMY = %+v, error = %v", historicalCreate, err)
	}
}

func TestRecurrenceCreateConcurrency(t *testing.T) {
	t.Run("same key and fingerprint", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		repository := newRecurrenceRepository(t, pool)
		fingerprint := recurrenceFingerprint(1)
		commands := []application.IdempotentRecurrenceCommand{
			newDirectCreateCommand(t, "rec-concurrent-same-a", syntheticUserID, "same-create-key", fingerprint, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)),
			newDirectCreateCommand(t, "rec-concurrent-same-b", syntheticUserID, "same-create-key", fingerprint, time.Date(2026, 8, 16, 12, 0, 1, 0, time.UTC)),
		}
		results, errs := runConcurrentCreates(ctx, repository, commands)
		for _, err := range errs {
			if err != nil {
				t.Fatalf("same command concurrent create error = %v", err)
			}
		}
		if results[0].Recurrence.ID() != results[1].Recurrence.ID() || results[0].Replayed == results[1].Replayed {
			t.Fatalf("same command results = %+v / %+v", results[0], results[1])
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("same key different fingerprint", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		repository := newRecurrenceRepository(t, pool)
		commands := []application.IdempotentRecurrenceCommand{
			newDirectCreateCommand(t, "rec-concurrent-conflict-a", syntheticUserID, "conflict-create-key", recurrenceFingerprint(2), time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)),
			newDirectCreateCommand(t, "rec-concurrent-conflict-b", syntheticUserID, "conflict-create-key", recurrenceFingerprint(3), time.Date(2026, 8, 16, 12, 0, 1, 0, time.UTC)),
		}
		_, errs := runConcurrentCreates(ctx, repository, commands)
		var successes, conflicts int
		for _, err := range errs {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, application.ErrRecurrenceIdempotencyConflict):
				conflicts++
			default:
				t.Fatalf("unexpected concurrent conflict error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("success/conflict counts = %d/%d, want 1/1", successes, conflicts)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("different keys remain distinct", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		repository := newRecurrenceRepository(t, pool)
		fingerprint := recurrenceFingerprint(4)
		commands := []application.IdempotentRecurrenceCommand{
			newDirectCreateCommand(t, "rec-distinct-key-a", syntheticUserID, "distinct-key-a", fingerprint, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)),
			newDirectCreateCommand(t, "rec-distinct-key-b", syntheticUserID, "distinct-key-b", fingerprint, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)),
		}
		results, errs := runConcurrentCreates(ctx, repository, commands)
		if errs[0] != nil || errs[1] != nil || results[0].Recurrence.ID() == results[1].Recurrence.ID() {
			t.Fatalf("different-key results = %+v/%+v errors=%v", results[0], results[1], errs)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 2, 2, 2)
	})
}

func TestRecurrenceCancelConcurrency(t *testing.T) {
	t.Run("same key same recurrence", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		repository := newRecurrenceRepository(t, pool)
		recurrence := persistDirectRecurrence(t, ctx, repository, "rec-cancel-same", syntheticUserID, "create-cancel-same")
		cancelledAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		fingerprint := recurrenceFingerprint(11)
		commands := []application.IdempotentCancelRecurrenceCommand{
			newDirectCancelCommand(recurrence, "cancel-same-key", fingerprint, cancelledAt),
			newDirectCancelCommand(recurrence, "cancel-same-key", fingerprint, cancelledAt.Add(time.Hour)),
		}
		results, errs := runConcurrentCancels(ctx, repository, commands)
		if errs[0] != nil || errs[1] != nil || results[0].Recurrence.ID() != results[1].Recurrence.ID() || results[0].Replayed == results[1].Replayed {
			t.Fatalf("same-key cancel results=%+v/%+v errors=%v", results[0], results[1], errs)
		}
		firstTime, _ := results[0].Recurrence.CancelledAt()
		secondTime, _ := results[1].Recurrence.CancelledAt()
		if firstTime != secondTime {
			t.Fatal("same-key cancel replay changed CancelledAt")
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)
	})

	t.Run("different keys one terminal transition", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		repository := newRecurrenceRepository(t, pool)
		recurrence := persistDirectRecurrence(t, ctx, repository, "rec-cancel-distinct", syntheticUserID, "create-cancel-distinct")
		commands := []application.IdempotentCancelRecurrenceCommand{
			newDirectCancelCommand(recurrence, "cancel-distinct-a", recurrenceFingerprint(12), time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)),
			newDirectCancelCommand(recurrence, "cancel-distinct-b", recurrenceFingerprint(12), time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)),
		}
		_, errs := runConcurrentCancels(ctx, repository, commands)
		var successes, alreadyCancelled int
		for _, err := range errs {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, domain.ErrRecurrenceAlreadyCancelled):
				alreadyCancelled++
			default:
				t.Fatalf("unexpected distinct-key cancel error = %v", err)
			}
		}
		if successes != 1 || alreadyCancelled != 1 {
			t.Fatalf("cancel success/already counts = %d/%d", successes, alreadyCancelled)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)
	})

	t.Run("same key different recurrence IDs", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		repository := newRecurrenceRepository(t, pool)
		first := persistDirectRecurrence(t, ctx, repository, "rec-cancel-conflict-a", syntheticUserID, "create-cancel-conflict-a")
		second := persistDirectRecurrence(t, ctx, repository, "rec-cancel-conflict-b", syntheticUserID, "create-cancel-conflict-b")
		commands := []application.IdempotentCancelRecurrenceCommand{
			newDirectCancelCommand(first, "cancel-conflict-key", recurrenceFingerprint(13), time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)),
			newDirectCancelCommand(second, "cancel-conflict-key", recurrenceFingerprint(14), time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)),
		}
		_, errs := runConcurrentCancels(ctx, repository, commands)
		var successes, conflicts int
		for _, err := range errs {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, application.ErrRecurrenceIdempotencyConflict):
				conflicts++
			default:
				t.Fatalf("unexpected same-key/different-ID cancel error = %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("cancel success/conflict counts = %d/%d", successes, conflicts)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 2, 3, 3)
		var activeCount int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM recurrences WHERE status = 'ACTIVE'").Scan(&activeCount); err != nil || activeCount != 1 {
			t.Fatalf("active recurrence count = %d, error = %v", activeCount, err)
		}
	})
}

func TestRecurrenceAtomicRollbackOnAuditFailure(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		if _, err := pool.Exec(ctx, `
			ALTER TABLE recurrence_audit_events
			ADD CONSTRAINT recurrence_audit_events_synthetic_create_failure
			CHECK (recurrence_id <> 'rec-audit-create-failure')
		`); err != nil {
			t.Fatal("installing create audit failure failed")
		}
		repository := newRecurrenceRepository(t, pool)
		command := newDirectCreateCommand(t, "rec-audit-create-failure", syntheticUserID, "audit-create-failure", recurrenceFingerprint(21), time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
		if _, err := repository.RecordRecurrence(ctx, command); !errors.Is(err, adapter.ErrInsertRecurrenceAuditEvent) {
			t.Fatalf("create audit failure error = %v", err)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 0, 0, 0)
		if _, err := pool.Exec(ctx, `
			ALTER TABLE recurrence_audit_events
			DROP CONSTRAINT recurrence_audit_events_synthetic_create_failure
		`); err != nil {
			t.Fatal("removing create audit failure failed")
		}
		retried, err := repository.RecordRecurrence(ctx, command)
		if err != nil || retried.Replayed {
			t.Fatalf("same-key create retry after rollback = %+v, error = %v", retried, err)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("cancel", func(t *testing.T) {
		pool := newMigratedTestDatabase(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		insertSyntheticUser(t, ctx, pool, syntheticUserID)
		repository := newRecurrenceRepository(t, pool)
		recurrence := persistDirectRecurrence(t, ctx, repository, "rec-audit-cancel-failure", syntheticUserID, "create-audit-cancel-failure")
		if _, err := pool.Exec(ctx, `
			ALTER TABLE recurrence_audit_events
			ADD CONSTRAINT recurrence_audit_events_synthetic_cancel_failure
			CHECK (event_type <> 'RECURRENCE_CANCELLED')
		`); err != nil {
			t.Fatal("installing cancel audit failure failed")
		}
		command := newDirectCancelCommand(recurrence, "audit-cancel-failure", recurrenceFingerprint(22), time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
		if _, err := repository.CancelRecurrence(ctx, command); !errors.Is(err, adapter.ErrInsertRecurrenceAuditEvent) {
			t.Fatalf("cancel audit failure error = %v", err)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 1, 1)
		var status string
		var storedCancelledAt *time.Time
		if err := pool.QueryRow(ctx, "SELECT status, cancelled_at FROM recurrences WHERE id = $1", recurrence.ID()).Scan(&status, &storedCancelledAt); err != nil || status != "ACTIVE" || storedCancelledAt != nil {
			t.Fatalf("cancel rollback status/cancelled_at = %q/%v, error = %v", status, storedCancelledAt, err)
		}
		var failedKeyCount int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM recurrence_idempotency_records
			WHERE user_id = $1 AND operation = 'CANCEL_RECURRENCE' AND idempotency_key = $2
		`, syntheticUserID, command.IdempotencyKey).Scan(&failedKeyCount); err != nil || failedKeyCount != 0 {
			t.Fatalf("failed cancel left %d idempotency rows, error = %v", failedKeyCount, err)
		}
		if _, err := pool.Exec(ctx, `
			ALTER TABLE recurrence_audit_events
			DROP CONSTRAINT recurrence_audit_events_synthetic_cancel_failure
		`); err != nil {
			t.Fatal("removing cancel audit failure failed")
		}
		retried, err := repository.CancelRecurrence(ctx, command)
		if err != nil || retried.Replayed || retried.Recurrence.Status() != domain.RecurrenceStatusCancelled {
			t.Fatalf("same-key cancel retry after rollback = %+v, error = %v", retried, err)
		}
		assertRecurrenceSubsystemCounts(t, ctx, pool, 1, 2, 2)
	})
}

func TestRecurrenceReaderFailsClosedForStructurallyInvalidStoredRow(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	if _, err := pool.Exec(ctx, "ALTER TABLE recurrences DROP CONSTRAINT recurrences_lifecycle_valid"); err != nil {
		t.Fatal("dropping lifecycle constraint in disposable corruption fixture failed")
	}
	createdAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if err := insertRawRecurrence(
		ctx,
		pool,
		"rec-corrupt-active-cancelled",
		syntheticUserID,
		"2026-08-10",
		"ACTIVE",
		createdAt,
		createdAt.Add(time.Hour),
	); err != nil {
		t.Fatalf("installing disposable corrupted row failed: %v", err)
	}
	repository := newRecurrenceRepository(t, pool)
	if _, err := repository.ListRecurrences(ctx, syntheticUserID); !errors.Is(err, adapter.ErrInvalidStoredRecurrence) {
		t.Fatalf("ListRecurrences corrupted row error = %v, want safe invalid-row category", err)
	}
}

func TestRecurrenceReaderRejectsInfinitePostgresDate(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	insertSyntheticUser(t, ctx, pool, syntheticUserID)
	if _, err := pool.Exec(ctx, "ALTER TABLE recurrences DROP CONSTRAINT recurrences_starts_on_supported"); err != nil {
		t.Fatal("dropping starts_on constraint in disposable corruption fixture failed")
	}
	createdAt := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if err := insertRawRecurrence(
		ctx,
		pool,
		"rec-corrupt-infinite-date",
		syntheticUserID,
		"infinity",
		"ACTIVE",
		createdAt,
		nil,
	); err != nil {
		t.Fatalf("installing disposable infinite DATE row failed: %v", err)
	}
	repository := newRecurrenceRepository(t, pool)
	if _, err := repository.ListRecurrences(ctx, syntheticUserID); !errors.Is(err, adapter.ErrInvalidStoredRecurrence) {
		t.Fatalf("ListRecurrences infinite DATE error = %v, want safe invalid-row category", err)
	}
}

func TestRecurrencePostgresAdapterPreservesContextCancellation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	repository := newRecurrenceRepository(t, pool)
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.FindRecurrenceReplay(cancelledContext, application.RecurrenceReplayQuery{
		UserID:         syntheticUserID,
		Operation:      application.IdempotencyOperationCreateRecurrence,
		IdempotencyKey: "cancelled-lookup",
		Fingerprint:    recurrenceFingerprint(31),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindRecurrenceReplay cancelled error = %v", err)
	}
	if _, err := repository.ListRecurrences(cancelledContext, syntheticUserID); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListRecurrences cancelled error = %v", err)
	}
}

type recurrenceFixedIDGenerator struct {
	id    string
	calls int
}

func (generator *recurrenceFixedIDGenerator) NewRecurrenceID() (string, error) {
	generator.calls++
	return generator.id, nil
}

type recurrenceFixedClock struct {
	now   time.Time
	calls int
}

func (clock *recurrenceFixedClock) Now() time.Time {
	clock.calls++
	return clock.now
}

func newRecurrenceRepository(t testing.TB, pool *pgxpool.Pool) *adapter.RecurrenceRepository {
	t.Helper()
	repository, err := adapter.NewRecurrenceRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewRecurrenceRepository() error = %v", err)
	}
	return repository
}

func newRecordRecurrenceUseCase(
	t testing.TB,
	repository *adapter.RecurrenceRepository,
	idGenerator application.RecurrenceIDGenerator,
	clock application.Clock,
) *application.RecordRecurrence {
	t.Helper()
	useCase, err := application.NewRecordRecurrence(repository, repository, idGenerator, clock)
	if err != nil {
		t.Fatalf("NewRecordRecurrence() error = %v", err)
	}
	return useCase
}

func newCancelRecurrenceUseCase(
	t testing.TB,
	repository *adapter.RecurrenceRepository,
	clock application.Clock,
) *application.CancelRecurrence {
	t.Helper()
	useCase, err := application.NewCancelRecurrence(repository, repository, clock)
	if err != nil {
		t.Fatalf("NewCancelRecurrence() error = %v", err)
	}
	return useCase
}

func recurrenceRecordInput(t testing.TB, userID, key string) application.RecordRecurrenceInput {
	t.Helper()
	startsOn, err := domain.NewCivilDate(2026, time.August, 31)
	if err != nil {
		t.Fatal("NewCivilDate() failed")
	}
	return application.RecordRecurrenceInput{
		Recurrence: application.RecurrenceInput{
			UserID:          userID,
			TransactionType: domain.TransactionTypeExpense,
			Description:     "Serviço sintético recorrente",
			ExpectedAmount:  11900,
			Currency:        domain.CurrencyBRL,
			Frequency:       domain.RecurrenceFrequencyMonthly,
			StartsOn:        startsOn,
		},
		IdempotencyKey: key,
	}
}

func openRestartPool(t testing.TB, ctx context.Context, source *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, source.Config().ConnString())
	if err != nil {
		t.Fatalf("opening restarted pool failed: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("pinging restarted pool failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func openRecurrencePoolWithDateStyle(
	t testing.TB,
	ctx context.Context,
	source *pgxpool.Pool,
	dateStyle string,
) *pgxpool.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.ConnConfig.Config.RuntimeParams["DateStyle"] = dateStyle
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("opening recurrence DateStyle pool failed: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("pinging recurrence DateStyle pool failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func assertPoolDateStyle(t testing.TB, ctx context.Context, pool *pgxpool.Pool, expected string) {
	t.Helper()
	connections := make([]*pgxpool.Conn, 0, pool.Config().MaxConns)
	defer func() {
		for _, connection := range connections {
			connection.Release()
		}
	}()
	for range pool.Config().MaxConns {
		connection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquiring recurrence DateStyle connection failed: %v", err)
		}
		connections = append(connections, connection)
		var actual string
		if err := connection.QueryRow(ctx, "SHOW DateStyle").Scan(&actual); err != nil || actual != expected {
			t.Fatalf("DateStyle = %q, want %q, error = %v", actual, expected, err)
		}
	}
}

func recurrenceFingerprint(marker byte) application.RequestFingerprint {
	var fingerprint application.RequestFingerprint
	fingerprint[0] = marker
	return fingerprint
}

func newDirectCreateCommand(
	t testing.TB,
	id, userID, key string,
	fingerprint application.RequestFingerprint,
	createdAt time.Time,
) application.IdempotentRecurrenceCommand {
	t.Helper()
	amount, err := domain.NewMoney(11900, domain.CurrencyBRL)
	if err != nil {
		t.Fatal("NewMoney failed")
	}
	startsOn, err := domain.NewCivilDate(2026, time.August, 31)
	if err != nil {
		t.Fatal("NewCivilDate failed")
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID: id,
		Details: domain.RecurrenceDetails{
			UserID:          userID,
			TransactionType: domain.TransactionTypeExpense,
			Description:     "Serviço sintético recorrente",
			ExpectedAmount:  amount,
			Frequency:       domain.RecurrenceFrequencyMonthly,
			StartsOn:        startsOn,
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("NewRecurrence() error = %v", err)
	}
	return application.IdempotentRecurrenceCommand{
		Operation:      application.IdempotencyOperationCreateRecurrence,
		IdempotencyKey: key,
		Fingerprint:    fingerprint,
		AuditEvent:     application.RecurrenceAuditEventCreated,
		Recurrence:     recurrence,
	}
}

func newDirectCancelCommand(
	recurrence domain.Recurrence,
	key string,
	fingerprint application.RequestFingerprint,
	cancelledAt time.Time,
) application.IdempotentCancelRecurrenceCommand {
	return application.IdempotentCancelRecurrenceCommand{
		Operation:      application.IdempotencyOperationCancelRecurrence,
		IdempotencyKey: key,
		Fingerprint:    fingerprint,
		AuditEvent:     application.RecurrenceAuditEventCancelled,
		UserID:         recurrence.UserID(),
		RecurrenceID:   recurrence.ID(),
		CancelledAt:    cancelledAt,
	}
}

func persistDirectRecurrence(
	t testing.TB,
	ctx context.Context,
	repository *adapter.RecurrenceRepository,
	id, userID, key string,
) domain.Recurrence {
	t.Helper()
	command := newDirectCreateCommand(t, id, userID, key, recurrenceFingerprint(byte(len(key))), time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	result, err := repository.RecordRecurrence(ctx, command)
	if err != nil {
		t.Fatalf("direct recurrence setup failed: %v", err)
	}
	return result.Recurrence
}

func runConcurrentCreates(
	ctx context.Context,
	repository *adapter.RecurrenceRepository,
	commands []application.IdempotentRecurrenceCommand,
) ([]application.IdempotentRecurrenceResult, []error) {
	results := make([]application.IdempotentRecurrenceResult, len(commands))
	errs := make([]error, len(commands))
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(len(commands))
	done.Add(len(commands))
	for index := range commands {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			results[index], errs[index] = repository.RecordRecurrence(ctx, commands[index])
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return results, errs
}

func runConcurrentCancels(
	ctx context.Context,
	repository *adapter.RecurrenceRepository,
	commands []application.IdempotentCancelRecurrenceCommand,
) ([]application.IdempotentCancelRecurrenceResult, []error) {
	results := make([]application.IdempotentCancelRecurrenceResult, len(commands))
	errs := make([]error, len(commands))
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(len(commands))
	done.Add(len(commands))
	for index := range commands {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			results[index], errs[index] = repository.CancelRecurrence(ctx, commands[index])
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()
	return results, errs
}

func assertRecurrenceSubsystemCounts(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	wantRecurrences, wantAudits, wantIdempotency int,
) {
	t.Helper()
	queries := []struct {
		name  string
		query string
		want  int
	}{
		{name: "recurrences", query: "SELECT count(*) FROM recurrences", want: wantRecurrences},
		{name: "recurrence audits", query: "SELECT count(*) FROM recurrence_audit_events", want: wantAudits},
		{name: "recurrence idempotency", query: "SELECT count(*) FROM recurrence_idempotency_records", want: wantIdempotency},
	}
	for _, item := range queries {
		var got int
		if err := pool.QueryRow(ctx, item.query).Scan(&got); err != nil {
			t.Fatalf("%s count failed: %v", item.name, err)
		}
		if got != item.want {
			t.Fatalf("%s count = %d, want %d", item.name, got, item.want)
		}
	}
}

func assertNoTransactionSubsystemWrites(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{"transactions", "audit_events", "idempotency_records"} {
		var count int
		if err := pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count); err != nil {
			t.Fatalf("%s count failed: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("recurrence operation wrote %d rows to %s", count, table)
		}
	}
}
