package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestPreviewCreditCardReturnsCanonicalReviewWithoutDependencies(t *testing.T) {
	input := validCreditCardInput()
	input.Name = "  Nubank\u00a0  Ultravioleta  "

	result, err := (application.PreviewCreditCard{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	preview := result.Preview
	if preview.Name != "Nubank Ultravioleta" || preview.ClosingDayAnchor != 25 || preview.DueDayAnchor != 2 {
		t.Fatalf("Preview = %#v", preview)
	}
	if preview.LastFour == nil || *preview.LastFour != "4821" {
		t.Fatalf("LastFour = %v", preview.LastFour)
	}
	if preview.Brand == nil || *preview.Brand != domain.CreditCardBrandMastercard {
		t.Fatalf("Brand = %v", preview.Brand)
	}
	if preview.CreditLimit == nil || preview.CreditLimit.MinorUnits() != 250_000 || preview.CreditLimit.Currency() != domain.CurrencyBRL {
		t.Fatalf("CreditLimit = %v", preview.CreditLimit)
	}
}

func TestPreviewCreditCardAllowsAbsentOptionals(t *testing.T) {
	input := validCreditCardInput()
	input.LastFour = nil
	input.Brand = nil
	input.CreditLimit = nil
	result, err := (application.PreviewCreditCard{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Preview.LastFour != nil || result.Preview.Brand != nil || result.Preview.CreditLimit != nil {
		t.Fatalf("optional preview fields unexpectedly present: %#v", result.Preview)
	}
}

func TestPreviewCreditCardRejectsInvalidInputAndCancellation(t *testing.T) {
	invalidBrand := domain.CreditCardBrand("FREE_TEXT")
	tests := []struct {
		name    string
		mutate  func(*application.CreditCardInput)
		wantErr error
	}{
		{name: "owner", mutate: func(input *application.CreditCardInput) { input.UserID = " owner " }, wantErr: domain.ErrInvalidCreditCardUserID},
		{name: "name", mutate: func(input *application.CreditCardInput) { input.Name = "" }, wantErr: domain.ErrInvalidCreditCardName},
		{name: "last four", mutate: func(input *application.CreditCardInput) { value := "123"; input.LastFour = &value }, wantErr: domain.ErrInvalidCreditCardLastFour},
		{name: "brand", mutate: func(input *application.CreditCardInput) { input.Brand = &invalidBrand }, wantErr: domain.ErrInvalidCreditCardBrand},
		{name: "closing", mutate: func(input *application.CreditCardInput) { input.ClosingDayAnchor = 0 }, wantErr: domain.ErrInvalidDayOfMonthAnchor},
		{name: "due", mutate: func(input *application.CreditCardInput) { input.DueDayAnchor = 32 }, wantErr: domain.ErrInvalidDayOfMonthAnchor},
		{name: "limit zero", mutate: func(input *application.CreditCardInput) { input.CreditLimit.MinorUnits = 0 }, wantErr: domain.ErrInvalidCreditCardCreditLimit},
		{name: "limit currency", mutate: func(input *application.CreditCardInput) { input.CreditLimit.Currency = "USD" }, wantErr: domain.ErrUnsupportedCurrency},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validCreditCardInput()
			test.mutate(&input)
			if _, err := (application.PreviewCreditCard{}).Execute(context.Background(), input); !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, test.wantErr)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (application.PreviewCreditCard{}).Execute(ctx, validCreditCardInput()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(cancelled) error = %v", err)
	}
}

func TestRecordCreditCardFirstWriteBuildsAtomicCommand(t *testing.T) {
	store := newMemoryCreditCardStore()
	idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(1)}
	clock := &countingClock{now: time.Date(2026, time.August, 25, 13, 30, 0, 123_456_789, time.UTC)}
	useCase := mustRecordCreditCard(t, store, store, idGenerator, clock)

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "synthetic")
	result, err := useCase.Execute(ctx, application.RecordCreditCardInput{
		CreditCard: validCreditCardInput(), IdempotencyKey: "create-card-key",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Replayed || result.CreditCard.Status() != domain.CreditCardStatusActive {
		t.Fatalf("result = %#v", result)
	}
	if store.replayCalls != 1 || store.recordCalls != 1 || idGenerator.calls != 1 || clock.calls != 1 {
		t.Fatalf("calls replay/record/id/clock = %d/%d/%d/%d", store.replayCalls, store.recordCalls, idGenerator.calls, clock.calls)
	}
	if store.replayCtx != ctx || store.recordCtx != ctx {
		t.Fatal("exact context was not propagated")
	}
	command := store.lastRecordCommand
	if command.Operation != application.IdempotencyOperationCreateCreditCard || command.AuditEvent != application.CreditCardAuditEventCreated {
		t.Fatalf("operation/audit = %q/%q", command.Operation, command.AuditEvent)
	}
	if command.IdempotencyKey != "create-card-key" || command.CreditCard.ID() != idGenerator.id {
		t.Fatal("command lost key or generated ID")
	}
	wantTime := time.Date(2026, time.August, 25, 13, 30, 0, 123_456_000, time.UTC)
	if result.CreditCard.CreatedAt() != wantTime || command.CreditCard.CreatedAt() != wantTime {
		t.Fatalf("CreatedAt = %v, want %v", result.CreditCard.CreatedAt(), wantTime)
	}
	if store.createdAudits != 1 || len(store.cards) != 1 || len(store.operations) != 1 {
		t.Fatalf("counts cards/audits/operations = %d/%d/%d", len(store.cards), store.createdAudits, len(store.operations))
	}
}

func TestRecordCreditCardReplayPrecedesIDClockAndFinalStore(t *testing.T) {
	store := newMemoryCreditCardStore()
	idGenerator := &sequenceCreditCardIDGenerator{ids: []string{creditCardID(2), creditCardID(3)}}
	clock := &countingClock{now: time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)}
	useCase := mustRecordCreditCard(t, store, store, idGenerator, clock)
	input := application.RecordCreditCardInput{CreditCard: validCreditCardInput(), IdempotencyKey: "same-create-key"}

	first, err := useCase.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	replay, err := useCase.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("replay Execute() error = %v", err)
	}
	if !replay.Replayed || replay.CreditCard.ID() != first.CreditCard.ID() || replay.CreditCard.CreatedAt() != first.CreditCard.CreatedAt() {
		t.Fatalf("replay = %#v, first = %#v", replay, first)
	}
	if idGenerator.calls != 1 || clock.calls != 1 || store.recordCalls != 1 || store.createdAudits != 1 {
		t.Fatalf("replay consumed generated values or write: id=%d clock=%d record=%d audit=%d", idGenerator.calls, clock.calls, store.recordCalls, store.createdAudits)
	}

	conflicting := input
	conflicting.CreditCard.DueDayAnchor = 3
	if _, err := useCase.Execute(context.Background(), conflicting); !errors.Is(err, application.ErrCreditCardIdempotencyConflict) {
		t.Fatalf("conflict Execute() error = %v", err)
	}
	if idGenerator.calls != 1 || clock.calls != 1 || store.recordCalls != 1 {
		t.Fatal("preflight conflict consumed ID, Clock or final store")
	}
}

func TestRecordCreditCardFinalStoreRemainsRaceAuthority(t *testing.T) {
	store := newMemoryCreditCardStore()
	input := application.RecordCreditCardInput{CreditCard: validCreditCardInput(), IdempotencyKey: "race-key"}
	winner, err := mustRecordCreditCard(t, store, store,
		&fixedCreditCardIDGenerator{id: creditCardID(4)},
		&countingClock{now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)},
	).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("winner Execute() error = %v", err)
	}

	absentReplay := &scriptedCreditCardReplayReader{}
	idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(5)}
	clock := &countingClock{now: time.Date(2026, 8, 25, 13, 0, 0, 0, time.UTC)}
	loser := mustRecordCreditCard(t, store, absentReplay, idGenerator, clock)
	replay, err := loser.Execute(context.Background(), input)
	if err != nil || !replay.Replayed || replay.CreditCard.ID() != winner.CreditCard.ID() {
		t.Fatalf("race replay = %#v, error = %v", replay, err)
	}
	if idGenerator.calls != 1 || clock.calls != 1 || store.recordCalls != 2 || store.createdAudits != 1 || len(store.cards) != 1 {
		t.Fatalf("race counts id/clock/record/audit/cards = %d/%d/%d/%d/%d", idGenerator.calls, clock.calls, store.recordCalls, store.createdAudits, len(store.cards))
	}

	conflictInput := input
	conflictInput.CreditCard.Name = "Outro cartão"
	conflictID := &fixedCreditCardIDGenerator{id: creditCardID(6)}
	conflictClock := &countingClock{now: clock.now}
	if _, err := mustRecordCreditCard(t, store, absentReplay, conflictID, conflictClock).Execute(context.Background(), conflictInput); !errors.Is(err, application.ErrCreditCardIdempotencyConflict) {
		t.Fatalf("race conflict error = %v", err)
	}
	if store.createdAudits != 1 || len(store.cards) != 1 {
		t.Fatal("race conflict duplicated card or audit")
	}
}

func TestRecordCreditCardFingerprintIsCanonicalVersionedAndSemantic(t *testing.T) {
	fingerprintFor := func(t *testing.T, input application.CreditCardInput, owner string, id uint64) application.RequestFingerprint {
		t.Helper()
		input.UserID = owner
		store := newMemoryCreditCardStore()
		_, err := mustRecordCreditCard(t, store, store,
			&fixedCreditCardIDGenerator{id: creditCardID(id)},
			&countingClock{now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)},
		).Execute(context.Background(), application.RecordCreditCardInput{CreditCard: input, IdempotencyKey: "fingerprint-key"})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		return store.lastRecordCommand.Fingerprint
	}

	base := validCreditCardInput()
	spaced := base
	spaced.Name = " \u00a0Nubank   Ultravioleta "
	canonical := fingerprintFor(t, base, base.UserID, 10)
	if canonical != fingerprintFor(t, spaced, base.UserID, 11) {
		t.Fatal("canonical whitespace changed create fingerprint")
	}
	if canonical != fingerprintFor(t, base, "another-owner", 12) {
		t.Fatal("owner unexpectedly entered create fingerprint despite owner-scoped idempotency")
	}

	mutations := []func(*application.CreditCardInput){
		func(input *application.CreditCardInput) { input.Name = "Outro cartão" },
		func(input *application.CreditCardInput) { value := "1111"; input.LastFour = &value },
		func(input *application.CreditCardInput) { input.LastFour = nil },
		func(input *application.CreditCardInput) { brand := domain.CreditCardBrandVisa; input.Brand = &brand },
		func(input *application.CreditCardInput) { input.Brand = nil },
		func(input *application.CreditCardInput) { input.ClosingDayAnchor = 24 },
		func(input *application.CreditCardInput) { input.DueDayAnchor = 3 },
		func(input *application.CreditCardInput) { input.CreditLimit.MinorUnits++ },
		func(input *application.CreditCardInput) { input.CreditLimit = nil },
	}
	for index, mutate := range mutations {
		changed := validCreditCardInput()
		mutate(&changed)
		if canonical == fingerprintFor(t, changed, changed.UserID, uint64(20+index)) {
			t.Fatalf("semantic mutation %d did not change fingerprint", index)
		}
	}
}

func TestRecordCreditCardRejectsBeforeGeneratedValues(t *testing.T) {
	tests := []struct {
		name    string
		input   application.RecordCreditCardInput
		wantErr error
	}{
		{name: "missing key", input: application.RecordCreditCardInput{CreditCard: validCreditCardInput()}, wantErr: application.ErrCreditCardIdempotencyKeyRequired},
		{name: "invalid key", input: application.RecordCreditCardInput{CreditCard: validCreditCardInput(), IdempotencyKey: "invalid key"}, wantErr: application.ErrCreditCardIdempotencyKeyInvalid},
		{name: "invalid owner", input: application.RecordCreditCardInput{CreditCard: func() application.CreditCardInput {
			input := validCreditCardInput()
			input.UserID = " owner "
			return input
		}(), IdempotencyKey: "valid-key"}, wantErr: domain.ErrInvalidCreditCardUserID},
		{name: "invalid input", input: application.RecordCreditCardInput{CreditCard: invalidCreditCardNameInput(), IdempotencyKey: "valid-key"}, wantErr: domain.ErrInvalidCreditCardName},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryCreditCardStore()
			idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(30)}
			clock := &countingClock{now: time.Now()}
			_, err := mustRecordCreditCard(t, store, store, idGenerator, clock).Execute(context.Background(), test.input)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, test.wantErr)
			}
			if store.replayCalls != 0 || store.recordCalls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
				t.Fatalf("invalid input caused dependencies: replay=%d record=%d id=%d clock=%d", store.replayCalls, store.recordCalls, idGenerator.calls, clock.calls)
			}
		})
	}
}

func TestCreditCardCommandsRejectCanceledContextBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := newMemoryCreditCardStore()
	idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(31)}
	clock := &countingClock{now: time.Now()}
	_, err := mustRecordCreditCard(t, store, store, idGenerator, clock).Execute(ctx, application.RecordCreditCardInput{
		CreditCard: validCreditCardInput(), IdempotencyKey: "cancelled-create-key",
	})
	if !errors.Is(err, context.Canceled) || store.replayCalls != 0 || idGenerator.calls != 0 || clock.calls != 0 {
		t.Fatalf("cancelled Record = %v, dependencies=%d/%d/%d", err, store.replayCalls, idGenerator.calls, clock.calls)
	}
	_, err = mustArchiveCreditCard(t, store, store, store, clock).Execute(ctx, application.ArchiveCreditCardInput{
		UserID: validCreditCardInput().UserID, CreditCardID: creditCardID(31), IdempotencyKey: "cancelled-archive-key",
	})
	if !errors.Is(err, context.Canceled) || store.findCalls != 0 || store.archiveCalls != 0 || clock.calls != 0 {
		t.Fatalf("cancelled Archive = %v, dependencies=%d/%d/%d", err, store.findCalls, store.archiveCalls, clock.calls)
	}
}

func TestRecordCreditCardFailsClosedForCorruptReplay(t *testing.T) {
	input := validCreditCardInput()
	active := creditCardForTest(t, creditCardID(40), input.UserID, "Nubank Ultravioleta", domain.CreditCardStatusActive, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	archived, err := active.Archive(active.CreatedAt().Add(time.Hour))
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	wrongOwner := creditCardForTest(t, creditCardID(41), "other-owner", "Nubank Ultravioleta", domain.CreditCardStatusActive, active.CreatedAt())
	wrongDetails := creditCardForTest(t, creditCardID(42), input.UserID, "Outro cartão", domain.CreditCardStatusActive, active.CreatedAt())

	for _, snapshot := range []domain.CreditCard{{}, archived, wrongOwner, wrongDetails} {
		replayReader := &scriptedCreditCardReplayReader{lookup: application.CreditCardReplayLookup{CreditCard: snapshot, Found: true}}
		store := newMemoryCreditCardStore()
		idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(43)}
		clock := &countingClock{now: time.Now()}
		_, err := mustRecordCreditCard(t, store, replayReader, idGenerator, clock).Execute(context.Background(), application.RecordCreditCardInput{
			CreditCard: input, IdempotencyKey: "corrupt-replay-key",
		})
		if !errors.Is(err, application.ErrCreditCardDependencyResult) {
			t.Fatalf("corrupt replay error = %v", err)
		}
		if idGenerator.calls != 0 || clock.calls != 0 || store.recordCalls != 0 {
			t.Fatal("corrupt replay reached generated values or store")
		}
	}
}

func TestRecordCreditCardDependencyFailuresAreSafe(t *testing.T) {
	const marker = "sensitive-credit-card-store-marker"
	input := application.RecordCreditCardInput{CreditCard: validCreditCardInput(), IdempotencyKey: "safe-error-key"}

	replayFailure := &scriptedCreditCardReplayReader{err: errors.New(marker)}
	idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(50)}
	clock := &countingClock{now: time.Now()}
	_, err := mustRecordCreditCard(t, newMemoryCreditCardStore(), replayFailure, idGenerator, clock).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardReplayLookup) || strings.Contains(err.Error(), marker) {
		t.Fatalf("replay failure = %v", err)
	}
	if idGenerator.calls != 0 || clock.calls != 0 {
		t.Fatal("replay failure consumed generated values")
	}

	contextFailure := &scriptedCreditCardReplayReader{err: context.DeadlineExceeded}
	_, err = mustRecordCreditCard(t, newMemoryCreditCardStore(), contextFailure,
		&fixedCreditCardIDGenerator{id: creditCardID(55)}, clock,
	).Execute(context.Background(), input)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, application.ErrCreditCardReplayLookup) {
		t.Fatalf("replay context error = %v", err)
	}

	idFailure := errors.New(marker)
	idGenerator = &fixedCreditCardIDGenerator{err: idFailure}
	_, err = mustRecordCreditCard(t, newMemoryCreditCardStore(), &scriptedCreditCardReplayReader{}, idGenerator, clock).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardIDGeneration) || !errors.Is(err, idFailure) || strings.Contains(err.Error(), marker) {
		t.Fatalf("ID failure = %v", err)
	}

	store := newMemoryCreditCardStore()
	store.recordErr = errors.New(marker)
	_, err = mustRecordCreditCard(t, store, store, &fixedCreditCardIDGenerator{id: creditCardID(51)}, clock).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardCommandPersistence) || strings.Contains(err.Error(), marker) {
		t.Fatalf("store failure = %v", err)
	}
}

func TestRecordCreditCardRejectsInvalidGeneratedValuesAndCorruptFinalResult(t *testing.T) {
	input := application.RecordCreditCardInput{CreditCard: validCreditCardInput(), IdempotencyKey: "generated-value-key"}

	invalidID := &fixedCreditCardIDGenerator{id: "card_invalid"}
	clock := &countingClock{now: time.Now()}
	store := &scriptedCreditCardCommandStore{}
	_, err := mustRecordCreditCard(t, store, &scriptedCreditCardReplayReader{}, invalidID, clock).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardIDGeneration) || clock.calls != 0 || store.calls != 0 {
		t.Fatalf("invalid generated ID = %v, clock/store=%d/%d", err, clock.calls, store.calls)
	}

	zeroClock := &countingClock{}
	_, err = mustRecordCreditCard(t, store, &scriptedCreditCardReplayReader{},
		&fixedCreditCardIDGenerator{id: creditCardID(52)}, zeroClock,
	).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardDependencyResult) || store.calls != 0 {
		t.Fatalf("zero Clock = %v, store calls=%d", err, store.calls)
	}

	detailsInput := validCreditCardInput()
	wrongCard := creditCardForTest(t, creditCardID(54), detailsInput.UserID, detailsInput.Name, domain.CreditCardStatusActive, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	store.result = application.IdempotentCreditCardResult{CreditCard: wrongCard, Replayed: false}
	_, err = mustRecordCreditCard(t, store, &scriptedCreditCardReplayReader{},
		&fixedCreditCardIDGenerator{id: creditCardID(53)},
		&countingClock{now: wrongCard.CreatedAt()},
	).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardDependencyResult) {
		t.Fatalf("corrupt first-write result error = %v", err)
	}
}

func TestNewRecordCreditCardRequiresDependencies(t *testing.T) {
	store := newMemoryCreditCardStore()
	idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(60)}
	clock := &countingClock{now: time.Now()}
	tests := []struct {
		name   string
		store  application.CreditCardCommandStore
		replay application.CreditCardReplayReader
		id     application.CreditCardIDGenerator
		clock  application.Clock
		want   error
	}{
		{name: "store", replay: store, id: idGenerator, clock: clock, want: application.ErrMissingCreditCardCommandStore},
		{name: "replay", store: store, id: idGenerator, clock: clock, want: application.ErrMissingCreditCardReplayReader},
		{name: "id", store: store, replay: store, clock: clock, want: application.ErrMissingCreditCardIDGenerator},
		{name: "clock", store: store, replay: store, id: idGenerator, want: application.ErrMissingCreditCardClock},
	}
	for _, test := range tests {
		if _, err := application.NewRecordCreditCard(test.store, test.replay, test.id, test.clock); !errors.Is(err, test.want) {
			t.Fatalf("NewRecordCreditCard(%s) error = %v", test.name, err)
		}
	}
}

type fixedCreditCardIDGenerator struct {
	id    string
	err   error
	calls int
}

func (generator *fixedCreditCardIDGenerator) NewCreditCardID() (string, error) {
	generator.calls++
	return generator.id, generator.err
}

type sequenceCreditCardIDGenerator struct {
	ids   []string
	calls int
}

func (generator *sequenceCreditCardIDGenerator) NewCreditCardID() (string, error) {
	index := generator.calls
	generator.calls++
	if index >= len(generator.ids) {
		return "", errors.New("synthetic sequence exhausted")
	}
	return generator.ids[index], nil
}

type countingClock struct {
	now   time.Time
	calls int
}

func (clock *countingClock) Now() time.Time {
	clock.calls++
	return clock.now
}

type storedCreditCardOperation struct {
	fingerprint application.RequestFingerprint
	card        domain.CreditCard
}

type memoryCreditCardStore struct {
	replayCalls       int
	recordCalls       int
	archiveCalls      int
	findCalls         int
	listCalls         int
	createdAudits     int
	archivedAudits    int
	replayErr         error
	recordErr         error
	archiveErr        error
	findErr           error
	listErr           error
	replayCtx         context.Context
	recordCtx         context.Context
	archiveCtx        context.Context
	findCtx           context.Context
	listCtx           context.Context
	lastReplayQuery   application.CreditCardReplayQuery
	lastRecordCommand application.IdempotentCreditCardCommand
	lastArchive       application.IdempotentArchiveCreditCardCommand
	cards             map[string]domain.CreditCard
	operations        map[string]storedCreditCardOperation
}

func newMemoryCreditCardStore() *memoryCreditCardStore {
	return &memoryCreditCardStore{
		cards:      make(map[string]domain.CreditCard),
		operations: make(map[string]storedCreditCardOperation),
	}
}

func (store *memoryCreditCardStore) FindCreditCardReplay(ctx context.Context, query application.CreditCardReplayQuery) (application.CreditCardReplayLookup, error) {
	store.replayCalls++
	store.replayCtx = ctx
	store.lastReplayQuery = query
	if store.replayErr != nil {
		return application.CreditCardReplayLookup{}, store.replayErr
	}
	operation, found := store.operations[creditCardOperationKey(query.UserID, query.Operation, query.IdempotencyKey)]
	if !found {
		return application.CreditCardReplayLookup{}, nil
	}
	if operation.fingerprint != query.Fingerprint {
		return application.CreditCardReplayLookup{}, application.ErrCreditCardIdempotencyConflict
	}
	return application.CreditCardReplayLookup{CreditCard: operation.card, Found: true}, nil
}

func (store *memoryCreditCardStore) RecordCreditCard(ctx context.Context, command application.IdempotentCreditCardCommand) (application.IdempotentCreditCardResult, error) {
	store.recordCalls++
	store.recordCtx = ctx
	store.lastRecordCommand = command
	if store.recordErr != nil {
		return application.IdempotentCreditCardResult{}, store.recordErr
	}
	key := creditCardOperationKey(command.CreditCard.UserID(), command.Operation, command.IdempotencyKey)
	if operation, found := store.operations[key]; found {
		if operation.fingerprint != command.Fingerprint {
			return application.IdempotentCreditCardResult{}, application.ErrCreditCardIdempotencyConflict
		}
		return application.IdempotentCreditCardResult{CreditCard: operation.card, Replayed: true}, nil
	}
	store.cards[command.CreditCard.ID()] = command.CreditCard
	store.operations[key] = storedCreditCardOperation{fingerprint: command.Fingerprint, card: command.CreditCard}
	if command.AuditEvent == application.CreditCardAuditEventCreated {
		store.createdAudits++
	}
	return application.IdempotentCreditCardResult{CreditCard: command.CreditCard}, nil
}

func (store *memoryCreditCardStore) ArchiveCreditCard(ctx context.Context, command application.IdempotentArchiveCreditCardCommand) (application.IdempotentArchiveCreditCardResult, error) {
	store.archiveCalls++
	store.archiveCtx = ctx
	store.lastArchive = command
	if store.archiveErr != nil {
		return application.IdempotentArchiveCreditCardResult{}, store.archiveErr
	}
	key := creditCardOperationKey(command.UserID, command.Operation, command.IdempotencyKey)
	if operation, found := store.operations[key]; found {
		if operation.fingerprint != command.Fingerprint {
			return application.IdempotentArchiveCreditCardResult{}, application.ErrCreditCardIdempotencyConflict
		}
		return application.IdempotentArchiveCreditCardResult{CreditCard: operation.card, Replayed: true}, nil
	}
	current, found := store.cards[command.CreditCardID]
	if !found || current.UserID() != command.UserID {
		return application.IdempotentArchiveCreditCardResult{}, application.ErrCreditCardNotFound
	}
	if current.Status() == domain.CreditCardStatusArchived {
		return application.IdempotentArchiveCreditCardResult{}, domain.ErrCreditCardAlreadyArchived
	}
	store.cards[command.CreditCardID] = command.CreditCard
	store.operations[key] = storedCreditCardOperation{fingerprint: command.Fingerprint, card: command.CreditCard}
	if command.AuditEvent == application.CreditCardAuditEventArchived {
		store.archivedAudits++
	}
	return application.IdempotentArchiveCreditCardResult{CreditCard: command.CreditCard}, nil
}

func (store *memoryCreditCardStore) FindCreditCard(ctx context.Context, userID, cardID string) (application.CreditCardLookup, error) {
	store.findCalls++
	store.findCtx = ctx
	if store.findErr != nil {
		return application.CreditCardLookup{}, store.findErr
	}
	card, found := store.cards[cardID]
	if !found || card.UserID() != userID {
		return application.CreditCardLookup{}, nil
	}
	return application.CreditCardLookup{CreditCard: card, Found: true}, nil
}

func (store *memoryCreditCardStore) ListCreditCards(ctx context.Context, userID string) ([]domain.CreditCard, error) {
	store.listCalls++
	store.listCtx = ctx
	if store.listErr != nil {
		return nil, store.listErr
	}
	items := make([]domain.CreditCard, 0)
	for _, card := range store.cards {
		if card.UserID() == userID {
			items = append(items, card)
		}
	}
	return items, nil
}

type scriptedCreditCardReplayReader struct {
	calls  int
	lookup application.CreditCardReplayLookup
	err    error
}

type scriptedCreditCardCommandStore struct {
	calls  int
	result application.IdempotentCreditCardResult
	err    error
}

func (store *scriptedCreditCardCommandStore) RecordCreditCard(context.Context, application.IdempotentCreditCardCommand) (application.IdempotentCreditCardResult, error) {
	store.calls++
	return store.result, store.err
}

func (reader *scriptedCreditCardReplayReader) FindCreditCardReplay(context.Context, application.CreditCardReplayQuery) (application.CreditCardReplayLookup, error) {
	reader.calls++
	return reader.lookup, reader.err
}

type scriptedCreditCardLookupReader struct {
	calls  int
	lookup application.CreditCardLookup
	err    error
}

func (reader *scriptedCreditCardLookupReader) FindCreditCard(context.Context, string, string) (application.CreditCardLookup, error) {
	reader.calls++
	return reader.lookup, reader.err
}

type scriptedCreditCardListReader struct {
	calls int
	items []domain.CreditCard
	err   error
}

func (reader *scriptedCreditCardListReader) ListCreditCards(context.Context, string) ([]domain.CreditCard, error) {
	reader.calls++
	return reader.items, reader.err
}

func creditCardOperationKey(userID, operation, key string) string {
	return userID + "|" + operation + "|" + key
}

func creditCardID(number uint64) string {
	return fmt.Sprintf("card_%032x", number)
}

func validCreditCardInput() application.CreditCardInput {
	lastFour := "4821"
	brand := domain.CreditCardBrandMastercard
	return application.CreditCardInput{
		UserID:           "user-credit-card-synthetic",
		Name:             "Nubank Ultravioleta",
		LastFour:         &lastFour,
		Brand:            &brand,
		ClosingDayAnchor: 25,
		DueDayAnchor:     2,
		CreditLimit: &application.CreditCardLimitInput{
			MinorUnits: 250_000,
			Currency:   domain.CurrencyBRL,
		},
	}
}

func invalidCreditCardNameInput() application.CreditCardInput {
	input := validCreditCardInput()
	input.Name = ""
	return input
}

func creditCardForTest(
	t testing.TB,
	id string,
	owner string,
	nameValue string,
	status domain.CreditCardStatus,
	createdAt time.Time,
) domain.CreditCard {
	t.Helper()
	name, err := domain.NewCreditCardName(nameValue)
	if err != nil {
		t.Fatalf("NewCreditCardName() error = %v", err)
	}
	closing, _ := domain.NewDayOfMonthAnchor(25)
	due, _ := domain.NewDayOfMonthAnchor(2)
	card, err := domain.NewCreditCard(domain.CreditCardParams{
		ID: id,
		Details: domain.CreditCardDetails{
			UserID: owner, Name: name, ClosingDayAnchor: closing, DueDayAnchor: due,
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("NewCreditCard() error = %v", err)
	}
	if status == domain.CreditCardStatusArchived {
		card, err = card.Archive(createdAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("Archive() error = %v", err)
		}
	}
	return card
}

func mustRecordCreditCard(
	t testing.TB,
	store application.CreditCardCommandStore,
	replayReader application.CreditCardReplayReader,
	idGenerator application.CreditCardIDGenerator,
	clock application.Clock,
) *application.RecordCreditCard {
	t.Helper()
	useCase, err := application.NewRecordCreditCard(store, replayReader, idGenerator, clock)
	if err != nil {
		t.Fatalf("NewRecordCreditCard() error = %v", err)
	}
	return useCase
}
