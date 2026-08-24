package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestPreviewRecurrenceNormalizesWithoutDependenciesOrSideEffects(t *testing.T) {
	input := validRecurrenceInput(t)
	input.Description = "  Serviço sintético mensal  "

	result, err := (application.PreviewRecurrence{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Details.Description != "Serviço sintético mensal" {
		t.Fatalf("Description = %q", result.Details.Description)
	}
	if result.Details.TransactionType != domain.TransactionTypeExpense || result.Details.Frequency != domain.RecurrenceFrequencyMonthly {
		t.Fatal("Preview lost the explicit Expense/Monthly semantics")
	}
	if result.Details.ExpectedAmount.MinorUnits() != input.ExpectedAmount || result.Details.StartsOn.String() != "2026-08-10" {
		t.Fatal("Preview changed exact amount or civil date")
	}
}

func TestPreviewRecurrenceRejectsInvalidInputAndCancellation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*application.RecurrenceInput)
		wantErr error
	}{
		{name: "income", mutate: func(input *application.RecurrenceInput) { input.TransactionType = domain.TransactionTypeIncome }, wantErr: domain.ErrInvalidRecurrenceType},
		{name: "amount", mutate: func(input *application.RecurrenceInput) { input.ExpectedAmount = 0 }, wantErr: domain.ErrInvalidRecurrenceExpectedAmount},
		{name: "currency", mutate: func(input *application.RecurrenceInput) { input.Currency = "USD" }, wantErr: domain.ErrUnsupportedCurrency},
		{name: "frequency", mutate: func(input *application.RecurrenceInput) { input.Frequency = "WEEKLY" }, wantErr: domain.ErrInvalidRecurrenceFrequency},
		{name: "start", mutate: func(input *application.RecurrenceInput) { input.StartsOn = domain.CivilDate{} }, wantErr: domain.ErrInvalidRecurrenceStartsOn},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validRecurrenceInput(t)
			test.mutate(&input)
			_, err := (application.PreviewRecurrence{}).Execute(context.Background(), input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, test.wantErr)
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (application.PreviewRecurrence{}).Execute(ctx, validRecurrenceInput(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(cancelled) error = %v", err)
	}
}

func TestRecordRecurrenceBuildsOwnAtomicCommand(t *testing.T) {
	store := newMemoryRecurrenceStore()
	idGenerator := &fixedRecurrenceIDGenerator{id: "recurrence-synthetic-record-001"}
	clock := &fixedClock{now: time.Date(2026, time.August, 16, 13, 30, 0, 123_456_789, time.UTC)}
	useCase := mustRecordRecurrence(t, store, idGenerator, clock)
	input := application.RecordRecurrenceInput{Recurrence: validRecurrenceInput(t), IdempotencyKey: "recurrence-create-key"}

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "synthetic-record")
	result, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.replayCalls != 1 || store.recordCalls != 1 || idGenerator.calls != 1 || clock.calls != 1 {
		t.Fatalf("calls replay/store/id/clock = %d/%d/%d/%d", store.replayCalls, store.recordCalls, idGenerator.calls, clock.calls)
	}
	if store.recordCtx != ctx {
		t.Fatal("RecordRecurrence did not propagate the exact context to its store")
	}
	if store.replayCtx != ctx || store.lastReplayQuery.Operation != application.IdempotencyOperationCreateRecurrence {
		t.Fatal("RecordRecurrence did not propagate context/operation to replay preflight")
	}
	command := store.lastRecordCommand
	if command.Operation != application.IdempotencyOperationCreateRecurrence || command.AuditEvent != application.RecurrenceAuditEventCreated {
		t.Fatalf("operation/audit = %q/%q", command.Operation, command.AuditEvent)
	}
	if command.IdempotencyKey != input.IdempotencyKey || command.Recurrence.ID() != idGenerator.id {
		t.Fatal("atomic command lost idempotency key or generated recurrence ID")
	}
	if command.Recurrence.UserID() != input.Recurrence.UserID || command.Recurrence.Type() != domain.TransactionTypeExpense {
		t.Fatal("atomic command lost owner or explicit Expense type")
	}
	wantCreatedAt := time.Date(2026, time.August, 16, 13, 30, 0, 123_456_000, time.UTC)
	if command.Recurrence.CreatedAt() != wantCreatedAt || result.Recurrence.CreatedAt() != wantCreatedAt {
		t.Fatalf("CreatedAt = %v, want canonical %v", command.Recurrence.CreatedAt(), wantCreatedAt)
	}
	if result.Replayed || store.createdAudits != 1 || store.cancelledAudits != 0 {
		t.Fatal("first write did not create exactly one recurrence audit intent")
	}
}

func TestRecordRecurrenceReplayConflictAndAuditOnce(t *testing.T) {
	store := newMemoryRecurrenceStore()
	idGenerator := &sequenceRecurrenceIDGenerator{ids: []string{"recurrence-first", "recurrence-unused"}}
	clock := &fixedClock{now: time.Date(2026, time.August, 16, 13, 30, 0, 0, time.UTC)}
	useCase := mustRecordRecurrence(
		t,
		store,
		idGenerator,
		clock,
	)
	input := application.RecordRecurrenceInput{Recurrence: validRecurrenceInput(t), IdempotencyKey: "same-create-key"}
	first, err := useCase.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	replay, err := useCase.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("replay Execute() error = %v", err)
	}
	if !replay.Replayed || replay.Recurrence.ID() != first.Recurrence.ID() {
		t.Fatal("same logical command did not replay the persisted recurrence")
	}
	if store.createdAudits != 1 || len(store.recurrences) != 1 {
		t.Fatalf("replay counts recurrence/audit = %d/%d, want 1/1", len(store.recurrences), store.createdAudits)
	}
	if idGenerator.calls != 1 || clock.calls != 1 || store.recordCalls != 1 {
		t.Fatalf("persisted replay consumed ID/Clock/final store: %d/%d/%d", idGenerator.calls, clock.calls, store.recordCalls)
	}

	conflicting := input
	conflicting.Recurrence.ExpectedAmount++
	if _, err := useCase.Execute(context.Background(), conflicting); !errors.Is(err, application.ErrRecurrenceIdempotencyConflict) {
		t.Fatalf("conflicting Execute() error = %v", err)
	}
	if store.createdAudits != 1 || len(store.recurrences) != 1 {
		t.Fatal("conflict changed persisted recurrence or audit count")
	}
	if idGenerator.calls != 1 || clock.calls != 1 || store.recordCalls != 1 {
		t.Fatalf("persisted conflict consumed ID/Clock/final store: %d/%d/%d", idGenerator.calls, clock.calls, store.recordCalls)
	}
}

func TestRecordRecurrenceReplayPreflightFailureStopsGeneratedValuesAndStore(t *testing.T) {
	const marker = "sensitive-replay-reader-marker"
	store := newMemoryRecurrenceStore()
	replayReader := &recordingRecurrenceReplayReader{err: errors.New(marker)}
	idGenerator := &fixedRecurrenceIDGenerator{id: "unused-recurrence-id"}
	clock := &fixedClock{now: time.Now()}
	useCase := mustRecordRecurrenceWithReplay(t, store, replayReader, idGenerator, clock)

	_, err := useCase.Execute(context.Background(), application.RecordRecurrenceInput{
		Recurrence:     validRecurrenceInput(t),
		IdempotencyKey: "preflight-error-key",
	})
	if !errors.Is(err, application.ErrRecurrenceReplayLookup) || strings.Contains(err.Error(), marker) {
		t.Fatalf("Execute() error = %v, want safe replay lookup category", err)
	}
	if replayReader.calls != 1 || idGenerator.calls != 0 || clock.calls != 0 || store.recordCalls != 0 {
		t.Fatalf("calls replay/id/clock/store = %d/%d/%d/%d", replayReader.calls, idGenerator.calls, clock.calls, store.recordCalls)
	}
}

func TestRecordRecurrenceFinalStoreRemainsAuthoritativeAfterPreflightRace(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*application.RecordRecurrenceInput)
		wantConflict bool
	}{
		{name: "replay won after absent preflight"},
		{name: "conflict won after absent preflight", mutate: func(input *application.RecordRecurrenceInput) {
			input.Recurrence.ExpectedAmount++
		}, wantConflict: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryRecurrenceStore()
			input := application.RecordRecurrenceInput{Recurrence: validRecurrenceInput(t), IdempotencyKey: "create-race-key"}
			winner, err := mustRecordRecurrence(
				t,
				store,
				&fixedRecurrenceIDGenerator{id: "recurrence-race-winner"},
				&fixedClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)},
			).Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("winner Execute() error = %v", err)
			}

			if test.mutate != nil {
				test.mutate(&input)
			}
			replayReader := &recordingRecurrenceReplayReader{}
			idGenerator := &fixedRecurrenceIDGenerator{id: "recurrence-race-loser"}
			clock := &fixedClock{now: time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)}
			useCase := mustRecordRecurrenceWithReplay(t, store, replayReader, idGenerator, clock)
			result, err := useCase.Execute(context.Background(), input)

			if test.wantConflict {
				if !errors.Is(err, application.ErrRecurrenceIdempotencyConflict) {
					t.Fatalf("race conflict error = %v", err)
				}
			} else {
				if err != nil || !result.Replayed || result.Recurrence.ID() != winner.Recurrence.ID() {
					t.Fatalf("race replay = %#v, error = %v", result, err)
				}
			}
			if replayReader.calls != 1 || idGenerator.calls != 1 || clock.calls != 1 || store.recordCalls != 2 {
				t.Fatalf("calls preflight/id/clock/final store = %d/%d/%d/%d", replayReader.calls, idGenerator.calls, clock.calls, store.recordCalls)
			}
			if store.createdAudits != 1 || len(store.recurrences) != 1 {
				t.Fatalf("race duplicated state/audit: recurrences=%d audits=%d", len(store.recurrences), store.createdAudits)
			}
		})
	}
}

func TestRecordRecurrenceFingerprintUsesCanonicalLogicalInput(t *testing.T) {
	fingerprintFor := func(t *testing.T, input application.RecurrenceInput, id string) application.RequestFingerprint {
		t.Helper()
		store := newMemoryRecurrenceStore()
		useCase := mustRecordRecurrence(t, store, &fixedRecurrenceIDGenerator{id: id}, &fixedClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)})
		if _, err := useCase.Execute(context.Background(), application.RecordRecurrenceInput{Recurrence: input, IdempotencyKey: "fingerprint-key"}); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		return store.lastRecordCommand.Fingerprint
	}

	canonical := validRecurrenceInput(t)
	canonical.Description = "Serviço sintético mensal"
	spaced := canonical
	spaced.Description = "  Serviço sintético mensal  "
	if fingerprintFor(t, canonical, "recurrence-fingerprint-a") != fingerprintFor(t, spaced, "recurrence-fingerprint-b") {
		t.Fatal("semantically equivalent recurrence descriptions changed the fingerprint")
	}
	differentStart := canonical
	differentStart.StartsOn = recurrenceDate(t, 2026, time.August, 11)
	if fingerprintFor(t, canonical, "recurrence-fingerprint-c") == fingerprintFor(t, differentStart, "recurrence-fingerprint-d") {
		t.Fatal("different civil dates produced the same fingerprint")
	}
}

func TestRecordRecurrenceRejectsBeforeGeneratedValuesAndStore(t *testing.T) {
	tests := []struct {
		name    string
		input   application.RecordRecurrenceInput
		wantErr error
	}{
		{name: "missing key", input: application.RecordRecurrenceInput{Recurrence: validRecurrenceInput(t)}, wantErr: application.ErrRecurrenceIdempotencyKeyRequired},
		{name: "invalid key", input: application.RecordRecurrenceInput{Recurrence: validRecurrenceInput(t), IdempotencyKey: "invalid key"}, wantErr: application.ErrRecurrenceIdempotencyKeyInvalid},
		{name: "invalid recurrence", input: application.RecordRecurrenceInput{Recurrence: invalidRecurrenceAmount(t), IdempotencyKey: "valid-key"}, wantErr: domain.ErrInvalidRecurrenceExpectedAmount},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryRecurrenceStore()
			idGenerator := &fixedRecurrenceIDGenerator{id: "unused"}
			clock := &fixedClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
			useCase := mustRecordRecurrence(t, store, idGenerator, clock)
			_, err := useCase.Execute(context.Background(), test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, test.wantErr)
			}
			if store.recordCalls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
				t.Fatal("invalid command consumed ID/Clock or called persistence")
			}
		})
	}
}

func TestRecordRecurrencePropagatesCancellationAndSafeFailures(t *testing.T) {
	store := newMemoryRecurrenceStore()
	idGenerator := &fixedRecurrenceIDGenerator{id: "unused"}
	clock := &fixedClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	useCase := mustRecordRecurrence(t, store, idGenerator, clock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(ctx, application.RecordRecurrenceInput{Recurrence: validRecurrenceInput(t), IdempotencyKey: "valid-key"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(cancelled) error = %v", err)
	}
	if store.recordCalls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
		t.Fatal("cancelled context called dependencies")
	}

	const marker = "sensitive-recurrence-store-marker"
	store.recordErr = errors.New(marker)
	_, err := useCase.Execute(context.Background(), application.RecordRecurrenceInput{Recurrence: validRecurrenceInput(t), IdempotencyKey: "valid-key"})
	if !errors.Is(err, application.ErrRecurrenceCommandPersistence) || strings.Contains(err.Error(), marker) {
		t.Fatalf("persistence error = %v, want safe categorized error", err)
	}
}

func TestNewRecordRecurrenceRequiresSpecificDependencies(t *testing.T) {
	store := newMemoryRecurrenceStore()
	idGenerator := &fixedRecurrenceIDGenerator{id: "recurrence-id"}
	clock := &fixedClock{now: time.Now()}
	tests := []struct {
		name         string
		store        application.RecurrenceCommandStore
		replayReader application.RecurrenceReplayReader
		idGenerator  application.RecurrenceIDGenerator
		clock        application.Clock
		wantErr      error
	}{
		{name: "store", replayReader: store, idGenerator: idGenerator, clock: clock, wantErr: application.ErrMissingRecurrenceCommandStore},
		{name: "replay reader", store: store, idGenerator: idGenerator, clock: clock, wantErr: application.ErrMissingRecurrenceReplayReader},
		{name: "id", store: store, replayReader: store, clock: clock, wantErr: application.ErrMissingRecurrenceIDGenerator},
		{name: "clock", store: store, replayReader: store, idGenerator: idGenerator, wantErr: application.ErrMissingRecurrenceClock},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := application.NewRecordRecurrence(test.store, test.replayReader, test.idGenerator, test.clock); !errors.Is(err, test.wantErr) {
				t.Fatalf("NewRecordRecurrence() error = %v", err)
			}
		})
	}
}

func TestRecordRecurrenceWrapsIDGenerationFailureSafely(t *testing.T) {
	const marker = "sensitive-recurrence-id-marker"
	store := newMemoryRecurrenceStore()
	idGenerator := &fixedRecurrenceIDGenerator{err: errors.New(marker)}
	clock := &fixedClock{now: time.Now()}
	useCase := mustRecordRecurrence(t, store, idGenerator, clock)

	_, err := useCase.Execute(context.Background(), application.RecordRecurrenceInput{
		Recurrence:     validRecurrenceInput(t),
		IdempotencyKey: "valid-key",
	})
	if !errors.Is(err, application.ErrRecurrenceIDGeneration) || strings.Contains(err.Error(), marker) {
		t.Fatalf("Execute() error = %v, want safe ID generation category", err)
	}
	if store.recordCalls != 0 || clock.calls != 0 {
		t.Fatal("ID failure called Clock or persistence")
	}
}

func TestListRecurrencesIsOwnerScopedAndDeterministicallyOrdered(t *testing.T) {
	owner := "user-synthetic-001"
	activeOlder := recurrenceForList(t, "recurrence-a", owner, domain.RecurrenceStatusActive, recurrenceDate(t, 2026, 7, 10), time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC))
	activeNewer := recurrenceForList(t, "recurrence-b", owner, domain.RecurrenceStatusActive, recurrenceDate(t, 2026, 8, 10), time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	cancelled := recurrenceForList(t, "recurrence-c", owner, domain.RecurrenceStatusCancelled, recurrenceDate(t, 2026, 9, 10), time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC))
	reader := &recordingRecurrenceReader{items: []domain.Recurrence{cancelled, activeOlder, activeNewer}}
	useCase, err := application.NewListRecurrences(reader)
	if err != nil {
		t.Fatalf("NewListRecurrences() error = %v", err)
	}
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "synthetic")
	result, err := useCase.Execute(ctx, owner)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if reader.userID != owner || reader.ctx != ctx {
		t.Fatal("reader did not receive exact owner/context scope")
	}
	want := []string{"recurrence-b", "recurrence-a", "recurrence-c"}
	for index := range want {
		if result.Items[index].ID() != want[index] {
			t.Fatalf("Items[%d] = %s, want %s", index, result.Items[index].ID(), want[index])
		}
	}
}

func TestListRecurrencesUsesCreatedAtAndIDAsDeterministicTieBreakers(t *testing.T) {
	owner := "user-synthetic-001"
	startsOn := recurrenceDate(t, 2026, 8, 10)
	older := recurrenceForList(t, "recurrence-z", owner, domain.RecurrenceStatusActive, startsOn, time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	newerA := recurrenceForList(t, "recurrence-a", owner, domain.RecurrenceStatusActive, startsOn, time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC))
	newerB := recurrenceForList(t, "recurrence-b", owner, domain.RecurrenceStatusActive, startsOn, time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC))
	readerItems := []domain.Recurrence{older, newerA, newerB}
	reader := &recordingRecurrenceReader{items: readerItems}
	useCase, _ := application.NewListRecurrences(reader)

	result, err := useCase.Execute(context.Background(), owner)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := []string{"recurrence-b", "recurrence-a", "recurrence-z"}
	for index := range want {
		if result.Items[index].ID() != want[index] {
			t.Fatalf("Items[%d] = %s, want %s", index, result.Items[index].ID(), want[index])
		}
	}
	if readerItems[0].ID() != "recurrence-z" {
		t.Fatal("ListRecurrences mutated the slice owned by its reader")
	}
}

func TestListRecurrencesReturnsStableEmptyAndSafeErrors(t *testing.T) {
	reader := &recordingRecurrenceReader{}
	useCase, _ := application.NewListRecurrences(reader)
	result, err := useCase.Execute(context.Background(), "user-synthetic-001")
	if err != nil || result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty Execute() = %#v, %v", result, err)
	}
	if _, err := useCase.Execute(context.Background(), ""); !errors.Is(err, domain.ErrInvalidRecurrenceUserID) {
		t.Fatalf("invalid owner error = %v", err)
	}

	const marker = "sensitive-reader-marker"
	reader.err = errors.New(marker)
	_, err = useCase.Execute(context.Background(), "user-synthetic-001")
	if !errors.Is(err, application.ErrRecurrenceQuery) || strings.Contains(err.Error(), marker) {
		t.Fatalf("query error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader.err = nil
	if _, err := useCase.Execute(ctx, "user-synthetic-001"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(cancelled) error = %v", err)
	}
}

func TestNewListRecurrencesRequiresReader(t *testing.T) {
	if _, err := application.NewListRecurrences(nil); !errors.Is(err, application.ErrMissingRecurrenceReader) {
		t.Fatalf("NewListRecurrences(nil) error = %v", err)
	}
}

func TestCancelRecurrenceTransitionReplayConflictAndAuditOnce(t *testing.T) {
	owner := "user-synthetic-001"
	first := recurrenceForList(t, "recurrence-cancel-first", owner, domain.RecurrenceStatusActive, recurrenceDate(t, 2026, 8, 10), time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	second := recurrenceForList(t, "recurrence-cancel-second", owner, domain.RecurrenceStatusActive, recurrenceDate(t, 2026, 9, 10), time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC))
	store := newMemoryRecurrenceStore()
	store.recurrences[first.ID()] = first
	store.recurrences[second.ID()] = second
	clock := &fixedClock{now: time.Date(2026, 8, 16, 18, 0, 0, 123_456_789, time.UTC)}
	useCase := mustCancelRecurrence(t, store, clock)
	input := application.CancelRecurrenceInput{UserID: owner, RecurrenceID: first.ID(), IdempotencyKey: "cancel-key"}

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "synthetic-cancel")
	result, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if result.Recurrence.Status() != domain.RecurrenceStatusCancelled || result.Replayed {
		t.Fatal("first cancellation did not return a newly CANCELLED recurrence")
	}
	if store.cancelCtx != ctx {
		t.Fatal("CancelRecurrence did not propagate the exact context to its store")
	}
	if store.replayCtx != ctx || store.lastReplayQuery.Operation != application.IdempotencyOperationCancelRecurrence {
		t.Fatal("CancelRecurrence did not propagate context/operation to replay preflight")
	}
	cancelledAt, exists := result.Recurrence.CancelledAt()
	wantCancelledAt := time.Date(2026, 8, 16, 18, 0, 0, 123_456_000, time.UTC)
	if !exists || cancelledAt != wantCancelledAt {
		t.Fatalf("CancelledAt = %v/%v, want %v/true", cancelledAt, exists, wantCancelledAt)
	}
	command := store.lastCancelCommand
	if command.Operation != application.IdempotencyOperationCancelRecurrence || command.AuditEvent != application.RecurrenceAuditEventCancelled {
		t.Fatalf("operation/audit = %q/%q", command.Operation, command.AuditEvent)
	}
	if store.replayCalls != 1 || store.cancelCalls != 1 || clock.calls != 1 {
		t.Fatalf("new cancellation calls replay/store/clock = %d/%d/%d", store.replayCalls, store.cancelCalls, clock.calls)
	}

	replay, err := useCase.Execute(context.Background(), input)
	if err != nil || !replay.Replayed || replay.Recurrence.ID() != first.ID() {
		t.Fatalf("replay = %#v, error = %v", replay, err)
	}
	if store.cancelledAudits != 1 {
		t.Fatalf("cancel audit count = %d, want 1", store.cancelledAudits)
	}
	if store.cancelCalls != 1 || clock.calls != 1 {
		t.Fatalf("persisted cancellation replay called store/clock = %d/%d", store.cancelCalls, clock.calls)
	}

	otherResource := input
	otherResource.RecurrenceID = second.ID()
	if _, err := useCase.Execute(context.Background(), otherResource); !errors.Is(err, application.ErrRecurrenceIdempotencyConflict) {
		t.Fatalf("same key for another recurrence error = %v", err)
	}
	if store.recurrences[second.ID()].Status() != domain.RecurrenceStatusActive {
		t.Fatal("conflicting cancellation changed the other recurrence")
	}
	if store.cancelCalls != 1 || clock.calls != 1 {
		t.Fatalf("persisted cancellation conflict called store/clock = %d/%d", store.cancelCalls, clock.calls)
	}

	alreadyCancelled := input
	alreadyCancelled.IdempotencyKey = "different-cancel-key"
	if _, err := useCase.Execute(context.Background(), alreadyCancelled); !errors.Is(err, domain.ErrRecurrenceAlreadyCancelled) {
		t.Fatalf("already-cancelled error = %v", err)
	}
	if store.cancelledAudits != 1 {
		t.Fatal("already-cancelled command duplicated audit")
	}
}

func TestCancelRecurrenceUnknownAndCrossOwnerAreIndistinguishable(t *testing.T) {
	stored := recurrenceForList(t, "recurrence-private", "user-owner-a", domain.RecurrenceStatusActive, recurrenceDate(t, 2026, 8, 10), time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	store := newMemoryRecurrenceStore()
	store.recurrences[stored.ID()] = stored
	useCase := mustCancelRecurrence(t, store, &fixedClock{now: time.Date(2026, 8, 16, 18, 0, 0, 0, time.UTC)})

	for _, input := range []application.CancelRecurrenceInput{
		{UserID: "user-owner-a", RecurrenceID: "recurrence-unknown", IdempotencyKey: "unknown-key"},
		{UserID: "user-owner-b", RecurrenceID: stored.ID(), IdempotencyKey: "cross-owner-key"},
	} {
		if _, err := useCase.Execute(context.Background(), input); !errors.Is(err, application.ErrRecurrenceNotFound) || err.Error() != application.ErrRecurrenceNotFound.Error() {
			t.Fatalf("Execute(%s/%s) error = %v", input.UserID, input.RecurrenceID, err)
		}
	}
	if store.cancelledAudits != 0 || store.recurrences[stored.ID()].Status() != domain.RecurrenceStatusActive {
		t.Fatal("unknown/cross-owner cancellation changed state or audit")
	}
}

func TestCancelRecurrenceRejectsBeforeClockAndStore(t *testing.T) {
	tests := []struct {
		name    string
		input   application.CancelRecurrenceInput
		wantErr error
	}{
		{name: "owner", input: application.CancelRecurrenceInput{RecurrenceID: "recurrence-id", IdempotencyKey: "key"}, wantErr: domain.ErrInvalidRecurrenceUserID},
		{name: "id", input: application.CancelRecurrenceInput{UserID: "user-id", IdempotencyKey: "key"}, wantErr: domain.ErrInvalidRecurrenceID},
		{name: "key", input: application.CancelRecurrenceInput{UserID: "user-id", RecurrenceID: "recurrence-id"}, wantErr: application.ErrRecurrenceIdempotencyKeyRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryRecurrenceStore()
			clock := &fixedClock{now: time.Now()}
			useCase := mustCancelRecurrence(t, store, clock)
			if _, err := useCase.Execute(context.Background(), test.input); !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() error = %v", err)
			}
			if store.cancelCalls != 0 || clock.calls != 0 {
				t.Fatal("invalid cancellation called Clock/store")
			}
		})
	}
}

func TestCancelRecurrenceRejectsZeroClockBeforeStore(t *testing.T) {
	store := newMemoryRecurrenceStore()
	useCase := mustCancelRecurrence(t, store, &fixedClock{})
	_, err := useCase.Execute(context.Background(), application.CancelRecurrenceInput{
		UserID:         "user-id",
		RecurrenceID:   "recurrence-id",
		IdempotencyKey: "cancel-key",
	})
	if !errors.Is(err, domain.ErrInvalidRecurrenceCancelledAt) {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.cancelCalls != 0 {
		t.Fatal("zero Clock value reached persistence")
	}
}

func TestCancelRecurrencePropagatesContextAndSafePersistenceFailure(t *testing.T) {
	store := newMemoryRecurrenceStore()
	clock := &fixedClock{now: time.Date(2026, 8, 16, 18, 0, 0, 0, time.UTC)}
	useCase := mustCancelRecurrence(t, store, clock)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	input := application.CancelRecurrenceInput{UserID: "user-id", RecurrenceID: "recurrence-id", IdempotencyKey: "cancel-key"}
	if _, err := useCase.Execute(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(cancelled) error = %v", err)
	}
	if store.cancelCalls != 0 || clock.calls != 0 {
		t.Fatal("cancelled context called dependencies")
	}

	const marker = "sensitive-cancel-store-marker"
	store.cancelErr = errors.New(marker)
	_, err := useCase.Execute(context.Background(), input)
	if !errors.Is(err, application.ErrRecurrenceCancellationPersistence) || strings.Contains(err.Error(), marker) {
		t.Fatalf("persistence error = %v", err)
	}
}

func TestCancelRecurrenceReplayPreflightFailureStopsClockAndStore(t *testing.T) {
	const marker = "sensitive-cancel-replay-reader-marker"
	store := newMemoryRecurrenceStore()
	replayReader := &recordingRecurrenceReplayReader{err: errors.New(marker)}
	clock := &fixedClock{now: time.Now()}
	useCase := mustCancelRecurrenceWithReplay(t, store, replayReader, clock)

	_, err := useCase.Execute(context.Background(), application.CancelRecurrenceInput{
		UserID:         "user-synthetic-001",
		RecurrenceID:   "recurrence-synthetic-001",
		IdempotencyKey: "cancel-preflight-error-key",
	})
	if !errors.Is(err, application.ErrRecurrenceReplayLookup) || strings.Contains(err.Error(), marker) {
		t.Fatalf("Execute() error = %v, want safe replay lookup category", err)
	}
	if replayReader.calls != 1 || clock.calls != 0 || store.cancelCalls != 0 {
		t.Fatalf("calls replay/clock/store = %d/%d/%d", replayReader.calls, clock.calls, store.cancelCalls)
	}
}

func TestCancelRecurrenceFinalStoreRemainsAuthoritativeAfterPreflightRace(t *testing.T) {
	tests := []struct {
		name     string
		conflict bool
	}{
		{name: "replay won after absent preflight"},
		{name: "conflict won after absent preflight", conflict: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			owner := "user-synthetic-001"
			first := recurrenceForList(t, "recurrence-cancel-race-first", owner, domain.RecurrenceStatusActive, recurrenceDate(t, 2026, 8, 10), time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
			second := recurrenceForList(t, "recurrence-cancel-race-second", owner, domain.RecurrenceStatusActive, recurrenceDate(t, 2026, 9, 10), time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC))
			store := newMemoryRecurrenceStore()
			store.recurrences[first.ID()] = first
			store.recurrences[second.ID()] = second
			input := application.CancelRecurrenceInput{UserID: owner, RecurrenceID: first.ID(), IdempotencyKey: "cancel-race-key"}
			winner, err := mustCancelRecurrence(
				t,
				store,
				&fixedClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)},
			).Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("winner Execute() error = %v", err)
			}

			if test.conflict {
				input.RecurrenceID = second.ID()
			}
			replayReader := &recordingRecurrenceReplayReader{}
			clock := &fixedClock{now: time.Date(2026, 8, 16, 13, 0, 0, 0, time.UTC)}
			useCase := mustCancelRecurrenceWithReplay(t, store, replayReader, clock)
			result, err := useCase.Execute(context.Background(), input)

			if test.conflict {
				if !errors.Is(err, application.ErrRecurrenceIdempotencyConflict) {
					t.Fatalf("race conflict error = %v", err)
				}
			} else if err != nil || !result.Replayed || result.Recurrence.ID() != winner.Recurrence.ID() {
				t.Fatalf("race replay = %#v, error = %v", result, err)
			}
			if replayReader.calls != 1 || clock.calls != 1 || store.cancelCalls != 2 {
				t.Fatalf("calls preflight/clock/final store = %d/%d/%d", replayReader.calls, clock.calls, store.cancelCalls)
			}
			if store.cancelledAudits != 1 || store.recurrences[second.ID()].Status() != domain.RecurrenceStatusActive {
				t.Fatal("race duplicated audit or changed the conflicting recurrence")
			}
		})
	}
}

func TestNewCancelRecurrenceRequiresSpecificDependencies(t *testing.T) {
	store := newMemoryRecurrenceStore()
	clock := &fixedClock{now: time.Now()}
	if _, err := application.NewCancelRecurrence(nil, store, clock); !errors.Is(err, application.ErrMissingRecurrenceCancellationStore) {
		t.Fatalf("NewCancelRecurrence(nil, store, clock) error = %v", err)
	}
	if _, err := application.NewCancelRecurrence(store, nil, clock); !errors.Is(err, application.ErrMissingRecurrenceReplayReader) {
		t.Fatalf("NewCancelRecurrence(store, nil, clock) error = %v", err)
	}
	if _, err := application.NewCancelRecurrence(store, store, nil); !errors.Is(err, application.ErrMissingRecurrenceClock) {
		t.Fatalf("NewCancelRecurrence(store, store, nil) error = %v", err)
	}
}

type fixedRecurrenceIDGenerator struct {
	id    string
	err   error
	calls int
}

func (generator *fixedRecurrenceIDGenerator) NewRecurrenceID() (string, error) {
	generator.calls++
	return generator.id, generator.err
}

type sequenceRecurrenceIDGenerator struct {
	ids   []string
	calls int
}

func (generator *sequenceRecurrenceIDGenerator) NewRecurrenceID() (string, error) {
	index := generator.calls
	generator.calls++
	if index >= len(generator.ids) {
		return "recurrence-sequence-overflow", nil
	}
	return generator.ids[index], nil
}

type storedRecurrenceOperation struct {
	fingerprint application.RequestFingerprint
	recurrence  domain.Recurrence
}

type memoryRecurrenceStore struct {
	replayCalls       int
	recordCalls       int
	cancelCalls       int
	createdAudits     int
	cancelledAudits   int
	recordErr         error
	cancelErr         error
	replayErr         error
	replayCtx         context.Context
	recordCtx         context.Context
	cancelCtx         context.Context
	lastReplayQuery   application.RecurrenceReplayQuery
	lastRecordCommand application.IdempotentRecurrenceCommand
	lastCancelCommand application.IdempotentCancelRecurrenceCommand
	recurrences       map[string]domain.Recurrence
	operations        map[string]storedRecurrenceOperation
}

func newMemoryRecurrenceStore() *memoryRecurrenceStore {
	return &memoryRecurrenceStore{
		recurrences: make(map[string]domain.Recurrence),
		operations:  make(map[string]storedRecurrenceOperation),
	}
}

func (store *memoryRecurrenceStore) FindRecurrenceReplay(
	ctx context.Context,
	query application.RecurrenceReplayQuery,
) (application.RecurrenceReplayLookup, error) {
	store.replayCalls++
	store.replayCtx = ctx
	store.lastReplayQuery = query
	if store.replayErr != nil {
		return application.RecurrenceReplayLookup{}, store.replayErr
	}
	key := query.UserID + "|" + query.Operation + "|" + query.IdempotencyKey
	existing, ok := store.operations[key]
	if !ok {
		return application.RecurrenceReplayLookup{}, nil
	}
	if existing.fingerprint != query.Fingerprint {
		return application.RecurrenceReplayLookup{}, application.ErrRecurrenceIdempotencyConflict
	}
	return application.RecurrenceReplayLookup{Recurrence: existing.recurrence, Found: true}, nil
}

func (store *memoryRecurrenceStore) RecordRecurrence(
	ctx context.Context,
	command application.IdempotentRecurrenceCommand,
) (application.IdempotentRecurrenceResult, error) {
	store.recordCalls++
	store.recordCtx = ctx
	store.lastRecordCommand = command
	if store.recordErr != nil {
		return application.IdempotentRecurrenceResult{}, store.recordErr
	}
	key := command.Recurrence.UserID() + "|" + command.Operation + "|" + command.IdempotencyKey
	if existing, ok := store.operations[key]; ok {
		if existing.fingerprint != command.Fingerprint {
			return application.IdempotentRecurrenceResult{}, application.ErrRecurrenceIdempotencyConflict
		}
		return application.IdempotentRecurrenceResult{Recurrence: existing.recurrence, Replayed: true}, nil
	}
	store.recurrences[command.Recurrence.ID()] = command.Recurrence
	store.operations[key] = storedRecurrenceOperation{fingerprint: command.Fingerprint, recurrence: command.Recurrence}
	if command.AuditEvent == application.RecurrenceAuditEventCreated {
		store.createdAudits++
	}
	return application.IdempotentRecurrenceResult{Recurrence: command.Recurrence}, nil
}

func (store *memoryRecurrenceStore) CancelRecurrence(
	ctx context.Context,
	command application.IdempotentCancelRecurrenceCommand,
) (application.IdempotentCancelRecurrenceResult, error) {
	store.cancelCalls++
	store.cancelCtx = ctx
	store.lastCancelCommand = command
	if store.cancelErr != nil {
		return application.IdempotentCancelRecurrenceResult{}, store.cancelErr
	}
	key := command.UserID + "|" + command.Operation + "|" + command.IdempotencyKey
	if existing, ok := store.operations[key]; ok {
		if existing.fingerprint != command.Fingerprint {
			return application.IdempotentCancelRecurrenceResult{}, application.ErrRecurrenceIdempotencyConflict
		}
		return application.IdempotentCancelRecurrenceResult{Recurrence: existing.recurrence, Replayed: true}, nil
	}
	recurrence, ok := store.recurrences[command.RecurrenceID]
	if !ok || recurrence.UserID() != command.UserID {
		return application.IdempotentCancelRecurrenceResult{}, application.ErrRecurrenceNotFound
	}
	cancelled, err := recurrence.Cancel(command.CancelledAt)
	if err != nil {
		return application.IdempotentCancelRecurrenceResult{}, err
	}
	store.recurrences[command.RecurrenceID] = cancelled
	store.operations[key] = storedRecurrenceOperation{fingerprint: command.Fingerprint, recurrence: cancelled}
	if command.AuditEvent == application.RecurrenceAuditEventCancelled {
		store.cancelledAudits++
	}
	return application.IdempotentCancelRecurrenceResult{Recurrence: cancelled}, nil
}

type recordingRecurrenceReader struct {
	ctx    context.Context
	userID string
	items  []domain.Recurrence
	err    error
}

type recordingRecurrenceReplayReader struct {
	calls  int
	ctx    context.Context
	query  application.RecurrenceReplayQuery
	result application.RecurrenceReplayLookup
	err    error
}

func (reader *recordingRecurrenceReplayReader) FindRecurrenceReplay(
	ctx context.Context,
	query application.RecurrenceReplayQuery,
) (application.RecurrenceReplayLookup, error) {
	reader.calls++
	reader.ctx = ctx
	reader.query = query
	return reader.result, reader.err
}

func (reader *recordingRecurrenceReader) ListRecurrences(ctx context.Context, userID string) ([]domain.Recurrence, error) {
	reader.ctx = ctx
	reader.userID = userID
	return reader.items, reader.err
}

func validRecurrenceInput(t testing.TB) application.RecurrenceInput {
	t.Helper()
	return application.RecurrenceInput{
		UserID:          "user-synthetic-001",
		TransactionType: domain.TransactionTypeExpense,
		Description:     "Serviço sintético mensal",
		ExpectedAmount:  11900,
		Currency:        domain.CurrencyBRL,
		Frequency:       domain.RecurrenceFrequencyMonthly,
		StartsOn:        recurrenceDate(t, 2026, time.August, 10),
	}
}

func invalidRecurrenceAmount(t testing.TB) application.RecurrenceInput {
	t.Helper()
	input := validRecurrenceInput(t)
	input.ExpectedAmount = 0
	return input
}

func recurrenceDate(t testing.TB, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatalf("NewCivilDate() error = %v", err)
	}
	return date
}

func mustRecordRecurrence(
	t testing.TB,
	store *memoryRecurrenceStore,
	idGenerator application.RecurrenceIDGenerator,
	clock application.Clock,
) *application.RecordRecurrence {
	t.Helper()
	return mustRecordRecurrenceWithReplay(t, store, store, idGenerator, clock)
}

func mustRecordRecurrenceWithReplay(
	t testing.TB,
	store application.RecurrenceCommandStore,
	replayReader application.RecurrenceReplayReader,
	idGenerator application.RecurrenceIDGenerator,
	clock application.Clock,
) *application.RecordRecurrence {
	t.Helper()
	useCase, err := application.NewRecordRecurrence(store, replayReader, idGenerator, clock)
	if err != nil {
		t.Fatalf("NewRecordRecurrence() error = %v", err)
	}
	return useCase
}

func mustCancelRecurrence(
	t testing.TB,
	store *memoryRecurrenceStore,
	clock application.Clock,
) *application.CancelRecurrence {
	t.Helper()
	return mustCancelRecurrenceWithReplay(t, store, store, clock)
}

func mustCancelRecurrenceWithReplay(
	t testing.TB,
	store application.RecurrenceCancellationStore,
	replayReader application.RecurrenceReplayReader,
	clock application.Clock,
) *application.CancelRecurrence {
	t.Helper()
	useCase, err := application.NewCancelRecurrence(store, replayReader, clock)
	if err != nil {
		t.Fatalf("NewCancelRecurrence() error = %v", err)
	}
	return useCase
}

func recurrenceForList(
	t testing.TB,
	id string,
	owner string,
	status domain.RecurrenceStatus,
	startsOn domain.CivilDate,
	createdAt time.Time,
) domain.Recurrence {
	t.Helper()
	amount, err := domain.NewMoney(11900, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("NewMoney() error = %v", err)
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID: id,
		Details: domain.RecurrenceDetails{
			UserID:          owner,
			TransactionType: domain.TransactionTypeExpense,
			Description:     "Serviço sintético mensal",
			ExpectedAmount:  amount,
			Frequency:       domain.RecurrenceFrequencyMonthly,
			StartsOn:        startsOn,
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("NewRecurrence() error = %v", err)
	}
	if status == domain.RecurrenceStatusCancelled {
		recurrence, err = recurrence.Cancel(createdAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
	}
	return recurrence
}
