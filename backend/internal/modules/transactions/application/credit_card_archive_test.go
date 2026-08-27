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

func TestArchiveCreditCardTransitionsOnceAndReplaysOriginalTimestamp(t *testing.T) {
	store := newMemoryCreditCardStore()
	created := mustCreateCardInStore(t, store, creditCardID(100), "create-before-archive")
	clock := &countingClock{now: created.CreditCard.CreatedAt().Add(24*time.Hour + 987*time.Nanosecond)}
	useCase := mustArchiveCreditCard(t, store, store, store, clock)
	input := application.ArchiveCreditCardInput{
		UserID: validCreditCardInput().UserID, CreditCardID: created.CreditCard.ID(), IdempotencyKey: "archive-card-key",
	}

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("request"), "archive")
	first, err := useCase.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	archivedAt, exists := first.CreditCard.ArchivedAt()
	wantArchivedAt := clock.now.UTC().Truncate(time.Microsecond)
	if first.Replayed || first.CreditCard.Status() != domain.CreditCardStatusArchived || !exists || archivedAt != wantArchivedAt {
		t.Fatalf("first archive = %#v, archivedAt=%v/%v", first, archivedAt, exists)
	}
	if store.replayCtx != ctx || store.findCtx != ctx || store.archiveCtx != ctx {
		t.Fatal("archive did not propagate exact context")
	}
	command := store.lastArchive
	if command.Operation != application.IdempotencyOperationArchiveCreditCard || command.AuditEvent != application.CreditCardAuditEventArchived {
		t.Fatalf("operation/audit = %q/%q", command.Operation, command.AuditEvent)
	}
	if store.archivedAudits != 1 || store.archiveCalls != 1 || clock.calls != 1 {
		t.Fatalf("archive counts audit/store/clock = %d/%d/%d", store.archivedAudits, store.archiveCalls, clock.calls)
	}

	clock.now = clock.now.Add(48 * time.Hour)
	replay, err := useCase.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("replay Execute() error = %v", err)
	}
	replayedAt, exists := replay.CreditCard.ArchivedAt()
	if !replay.Replayed || !exists || replayedAt != archivedAt {
		t.Fatalf("replay = %#v, ArchivedAt=%v/%v", replay, replayedAt, exists)
	}
	if clock.calls != 1 || store.findCalls != 1 || store.archiveCalls != 1 || store.archivedAudits != 1 {
		t.Fatal("replay consumed Clock, lookup, final store or audit")
	}
}

func TestCreateReplayAfterArchiveReturnsHistoricalActiveSnapshot(t *testing.T) {
	store := newMemoryCreditCardStore()
	idGenerator := &fixedCreditCardIDGenerator{id: creditCardID(101)}
	createClock := &countingClock{now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)}
	record := mustRecordCreditCard(t, store, store, idGenerator, createClock)
	createInput := application.RecordCreditCardInput{CreditCard: validCreditCardInput(), IdempotencyKey: "historical-create-key"}
	created, err := record.Execute(context.Background(), createInput)
	if err != nil {
		t.Fatalf("create error = %v", err)
	}

	archiveClock := &countingClock{now: createClock.now.Add(24 * time.Hour)}
	archive := mustArchiveCreditCard(t, store, store, store, archiveClock)
	if _, err := archive.Execute(context.Background(), application.ArchiveCreditCardInput{
		UserID: createInput.CreditCard.UserID, CreditCardID: created.CreditCard.ID(), IdempotencyKey: "historical-archive-key",
	}); err != nil {
		t.Fatalf("archive error = %v", err)
	}
	if store.cards[created.CreditCard.ID()].Status() != domain.CreditCardStatusArchived {
		t.Fatal("current stored card is not ARCHIVED")
	}

	replay, err := record.Execute(context.Background(), createInput)
	if err != nil {
		t.Fatalf("create replay error = %v", err)
	}
	if !replay.Replayed || replay.CreditCard.Status() != domain.CreditCardStatusActive || replay.CreditCard.ID() != created.CreditCard.ID() || replay.CreditCard.CreatedAt() != created.CreditCard.CreatedAt() {
		t.Fatalf("historical create replay = %#v", replay)
	}
	if _, exists := replay.CreditCard.ArchivedAt(); exists {
		t.Fatal("historical ACTIVE snapshot contains ArchivedAt")
	}
	if idGenerator.calls != 1 || createClock.calls != 1 || store.recordCalls != 1 || store.createdAudits != 1 {
		t.Fatal("historical replay consumed ID/Clock/store/audit")
	}
}

func TestCreditCardIdempotencyIsOwnerAndOperationScoped(t *testing.T) {
	store := newMemoryCreditCardStore()
	sharedKey := "shared-owner-operation-key"
	inputA := validCreditCardInput()
	inputB := validCreditCardInput()
	inputB.UserID = "second-owner"

	first, err := mustRecordCreditCard(t, store, store,
		&fixedCreditCardIDGenerator{id: creditCardID(150)},
		&countingClock{now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)},
	).Execute(context.Background(), application.RecordCreditCardInput{CreditCard: inputA, IdempotencyKey: sharedKey})
	if err != nil {
		t.Fatalf("owner A create error = %v", err)
	}
	second, err := mustRecordCreditCard(t, store, store,
		&fixedCreditCardIDGenerator{id: creditCardID(151)},
		&countingClock{now: time.Date(2026, 8, 25, 12, 1, 0, 0, time.UTC)},
	).Execute(context.Background(), application.RecordCreditCardInput{CreditCard: inputB, IdempotencyKey: sharedKey})
	if err != nil || second.Replayed {
		t.Fatalf("owner B create = %#v, %v", second, err)
	}

	archiveClock := &countingClock{now: first.CreditCard.CreatedAt().Add(time.Hour)}
	archived, err := mustArchiveCreditCard(t, store, store, store, archiveClock).Execute(context.Background(), application.ArchiveCreditCardInput{
		UserID: inputA.UserID, CreditCardID: first.CreditCard.ID(), IdempotencyKey: sharedKey,
	})
	if err != nil || archived.Replayed {
		t.Fatalf("same-key different-operation archive = %#v, %v", archived, err)
	}
	if len(store.cards) != 2 || len(store.operations) != 3 || store.createdAudits != 2 || store.archivedAudits != 1 {
		t.Fatalf("scoped counts cards/operations/created/archived = %d/%d/%d/%d", len(store.cards), len(store.operations), store.createdAudits, store.archivedAudits)
	}
}

func TestArchiveCreditCardNewKeyAfterArchiveFailsBeforeClockAndStore(t *testing.T) {
	store := newMemoryCreditCardStore()
	created := mustCreateCardInStore(t, store, creditCardID(102), "create-for-new-key")
	clock := &countingClock{now: created.CreditCard.CreatedAt().Add(time.Hour)}
	useCase := mustArchiveCreditCard(t, store, store, store, clock)
	firstInput := application.ArchiveCreditCardInput{
		UserID: created.CreditCard.UserID(), CreditCardID: created.CreditCard.ID(), IdempotencyKey: "first-archive-key",
	}
	first, err := useCase.Execute(context.Background(), firstInput)
	if err != nil {
		t.Fatalf("first archive error = %v", err)
	}
	firstArchivedAt, _ := first.CreditCard.ArchivedAt()

	secondInput := firstInput
	secondInput.IdempotencyKey = "new-archive-key"
	if _, err := useCase.Execute(context.Background(), secondInput); !errors.Is(err, domain.ErrCreditCardAlreadyArchived) {
		t.Fatalf("new-key archive error = %v", err)
	}
	currentArchivedAt, _ := store.cards[first.CreditCard.ID()].ArchivedAt()
	if currentArchivedAt != firstArchivedAt || clock.calls != 1 || store.archiveCalls != 1 || store.archivedAudits != 1 {
		t.Fatal("new key changed ArchivedAt or created completion/audit")
	}
	if _, exists := store.operations[creditCardOperationKey(secondInput.UserID, application.IdempotencyOperationArchiveCreditCard, secondInput.IdempotencyKey)]; exists {
		t.Fatal("already-archived operation received a completion")
	}
}

func TestArchiveCreditCardFinalStoreRemainsRaceAuthority(t *testing.T) {
	store := newMemoryCreditCardStore()
	created := mustCreateCardInStore(t, store, creditCardID(103), "create-race-archive")
	activeSnapshot := created.CreditCard
	winnerClock := &countingClock{now: activeSnapshot.CreatedAt().Add(time.Hour)}
	winnerInput := application.ArchiveCreditCardInput{
		UserID: activeSnapshot.UserID(), CreditCardID: activeSnapshot.ID(), IdempotencyKey: "archive-race-key",
	}
	winner, err := mustArchiveCreditCard(t, store, store, store, winnerClock).Execute(context.Background(), winnerInput)
	if err != nil {
		t.Fatalf("winner error = %v", err)
	}

	absentReplay := &scriptedCreditCardReplayReader{}
	staleLookup := &scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: activeSnapshot, Found: true}}
	loserClock := &countingClock{now: winnerClock.now.Add(time.Hour)}
	replay, err := mustArchiveCreditCard(t, store, absentReplay, staleLookup, loserClock).Execute(context.Background(), winnerInput)
	if err != nil || !replay.Replayed {
		t.Fatalf("race replay = %#v, error = %v", replay, err)
	}
	winnerAt, _ := winner.CreditCard.ArchivedAt()
	replayAt, _ := replay.CreditCard.ArchivedAt()
	if replayAt != winnerAt || store.archivedAudits != 1 || store.archiveCalls != 2 {
		t.Fatal("final store race replay changed snapshot or duplicated audit")
	}

	newKey := winnerInput
	newKey.IdempotencyKey = "different-race-key"
	if _, err := mustArchiveCreditCard(t, store, absentReplay, staleLookup, loserClock).Execute(context.Background(), newKey); !errors.Is(err, domain.ErrCreditCardAlreadyArchived) {
		t.Fatalf("different-key race error = %v", err)
	}
	if store.archivedAudits != 1 {
		t.Fatal("different-key race duplicated audit")
	}
}

func TestArchiveCreditCardSameKeyDifferentCardConflicts(t *testing.T) {
	store := newMemoryCreditCardStore()
	first := mustCreateCardInStore(t, store, creditCardID(104), "create-card-one")
	secondInput := validCreditCardInput()
	secondInput.Name = "Segundo cartão"
	second, err := mustRecordCreditCard(t, store, store,
		&fixedCreditCardIDGenerator{id: creditCardID(105)},
		&countingClock{now: first.CreditCard.CreatedAt().Add(time.Minute)},
	).Execute(context.Background(), application.RecordCreditCardInput{CreditCard: secondInput, IdempotencyKey: "create-card-two"})
	if err != nil {
		t.Fatalf("second create error = %v", err)
	}
	clock := &countingClock{now: first.CreditCard.CreatedAt().Add(time.Hour)}
	useCase := mustArchiveCreditCard(t, store, store, store, clock)
	key := "same-key-different-card"
	if _, err := useCase.Execute(context.Background(), application.ArchiveCreditCardInput{UserID: first.CreditCard.UserID(), CreditCardID: first.CreditCard.ID(), IdempotencyKey: key}); err != nil {
		t.Fatalf("first archive error = %v", err)
	}
	if _, err := useCase.Execute(context.Background(), application.ArchiveCreditCardInput{UserID: second.CreditCard.UserID(), CreditCardID: second.CreditCard.ID(), IdempotencyKey: key}); !errors.Is(err, application.ErrCreditCardIdempotencyConflict) {
		t.Fatalf("conflicting archive error = %v", err)
	}
	if store.cards[second.CreditCard.ID()].Status() != domain.CreditCardStatusActive || store.archivedAudits != 1 {
		t.Fatal("conflict archived wrong card or duplicated audit")
	}
}

func TestArchiveFingerprintIsStableAndOwnerScoped(t *testing.T) {
	fingerprintFor := func(t *testing.T, owner, id string, number uint64) application.RequestFingerprint {
		t.Helper()
		active := creditCardForTest(t, id, owner, "Cartão sintético", domain.CreditCardStatusActive, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
		store := newMemoryCreditCardStore()
		store.cards[id] = active
		_, err := mustArchiveCreditCard(t, store, &scriptedCreditCardReplayReader{}, store,
			&countingClock{now: active.CreatedAt().Add(time.Duration(number) * time.Hour)},
		).Execute(context.Background(), application.ArchiveCreditCardInput{UserID: owner, CreditCardID: id, IdempotencyKey: "fingerprint-key"})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		return store.lastArchive.Fingerprint
	}
	owner := "owner-fingerprint"
	id := creditCardID(106)
	base := fingerprintFor(t, owner, id, 1)
	if base != fingerprintFor(t, owner, id, 2) {
		t.Fatal("Clock changed archive fingerprint")
	}
	if base == fingerprintFor(t, "other-owner-fingerprint", id, 3) {
		t.Fatal("owner did not change owner-scoped archive fingerprint")
	}
	if base == fingerprintFor(t, owner, creditCardID(107), 4) {
		t.Fatal("card ID did not change archive fingerprint")
	}
}

func TestArchiveCreditCardNotFoundAndOwnerIsolationDoNotConsumeClock(t *testing.T) {
	store := newMemoryCreditCardStore()
	created := mustCreateCardInStore(t, store, creditCardID(108), "create-owner-a")
	clock := &countingClock{now: time.Now()}
	useCase := mustArchiveCreditCard(t, store, store, store, clock)

	for _, input := range []application.ArchiveCreditCardInput{
		{UserID: created.CreditCard.UserID(), CreditCardID: creditCardID(999), IdempotencyKey: "unknown-card-key"},
		{UserID: "owner-b", CreditCardID: created.CreditCard.ID(), IdempotencyKey: "cross-owner-key"},
	} {
		if _, err := useCase.Execute(context.Background(), input); !errors.Is(err, application.ErrCreditCardNotFound) {
			t.Fatalf("Execute(%q) error = %v", input.UserID, err)
		}
	}
	if clock.calls != 0 || store.archiveCalls != 0 || store.archivedAudits != 0 {
		t.Fatal("unknown/cross-owner archive consumed Clock or wrote state")
	}
}

func TestCreditCardOwnerIsolationAcrossGetListAndArchive(t *testing.T) {
	store := newMemoryCreditCardStore()
	created := mustCreateCardInStore(t, store, creditCardID(112), "owner-isolation-create")
	ownerB := "owner-b-isolated"

	get, _ := application.NewGetCreditCard(store)
	if _, err := get.Execute(context.Background(), application.GetCreditCardInput{UserID: ownerB, CreditCardID: created.CreditCard.ID()}); !errors.Is(err, application.ErrCreditCardNotFound) {
		t.Fatalf("cross-owner Get error = %v", err)
	}
	list, _ := application.NewListCreditCards(store)
	listed, err := list.Execute(context.Background(), ownerB)
	if err != nil || len(listed.Items) != 0 {
		t.Fatalf("owner B list = %#v, %v", listed, err)
	}
	clock := &countingClock{now: time.Now()}
	archive := mustArchiveCreditCard(t, store, store, store, clock)
	if _, err := archive.Execute(context.Background(), application.ArchiveCreditCardInput{
		UserID: ownerB, CreditCardID: created.CreditCard.ID(), IdempotencyKey: "owner-b-archive-key",
	}); !errors.Is(err, application.ErrCreditCardNotFound) {
		t.Fatalf("cross-owner Archive error = %v", err)
	}
	if store.cards[created.CreditCard.ID()].Status() != domain.CreditCardStatusActive || clock.calls != 0 || store.archiveCalls != 0 {
		t.Fatal("owner B affected owner A card")
	}
}

func TestArchiveCreditCardFailsClosedForCorruptDependencies(t *testing.T) {
	owner := validCreditCardInput().UserID
	id := creditCardID(109)
	active := creditCardForTest(t, id, owner, "Cartão sintético", domain.CreditCardStatusActive, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	archived, _ := active.Archive(active.CreatedAt().Add(time.Hour))
	wrongOwner := creditCardForTest(t, id, "other-owner", "Cartão sintético", domain.CreditCardStatusActive, active.CreatedAt())
	wrongID := creditCardForTest(t, creditCardID(110), owner, "Cartão sintético", domain.CreditCardStatusActive, active.CreatedAt())
	input := application.ArchiveCreditCardInput{UserID: owner, CreditCardID: id, IdempotencyKey: "corrupt-key"}

	for _, snapshot := range []domain.CreditCard{{}, active, wrongOwner, wrongID} {
		replay := &scriptedCreditCardReplayReader{lookup: application.CreditCardReplayLookup{CreditCard: snapshot, Found: true}}
		clock := &countingClock{now: time.Now()}
		_, err := mustArchiveCreditCard(t, newMemoryCreditCardStore(), replay, &scriptedCreditCardLookupReader{}, clock).Execute(context.Background(), input)
		if !errors.Is(err, application.ErrCreditCardDependencyResult) {
			t.Fatalf("corrupt replay error = %v", err)
		}
		if clock.calls != 0 {
			t.Fatal("corrupt replay consumed Clock")
		}
	}

	for _, snapshot := range []domain.CreditCard{{}, wrongOwner, wrongID} {
		lookup := &scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: snapshot, Found: true}}
		clock := &countingClock{now: time.Now()}
		_, err := mustArchiveCreditCard(t, newMemoryCreditCardStore(), &scriptedCreditCardReplayReader{}, lookup, clock).Execute(context.Background(), input)
		if !errors.Is(err, application.ErrCreditCardDependencyResult) {
			t.Fatalf("corrupt lookup error = %v", err)
		}
		if clock.calls != 0 {
			t.Fatal("corrupt lookup consumed Clock")
		}
	}

	store := &scriptedArchiveCreditCardStore{result: application.IdempotentArchiveCreditCardResult{CreditCard: active}}
	clock := &countingClock{now: active.CreatedAt().Add(time.Hour)}
	_, err := mustArchiveCreditCard(t, store, &scriptedCreditCardReplayReader{},
		&scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: active, Found: true}}, clock,
	).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardDependencyResult) {
		t.Fatalf("corrupt store result error = %v", err)
	}

	differentArchivedAt, _ := active.Archive(active.CreatedAt().Add(2 * time.Hour))
	store = &scriptedArchiveCreditCardStore{result: application.IdempotentArchiveCreditCardResult{CreditCard: differentArchivedAt}}
	_, err = mustArchiveCreditCard(t, store, &scriptedCreditCardReplayReader{},
		&scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: active, Found: true}},
		&countingClock{now: active.CreatedAt().Add(time.Hour)},
	).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardDependencyResult) {
		t.Fatalf("mismatched first-write snapshot error = %v", err)
	}

	replayArchived := &scriptedCreditCardReplayReader{lookup: application.CreditCardReplayLookup{CreditCard: archived, Found: true}}
	if result, err := mustArchiveCreditCard(t, newMemoryCreditCardStore(), replayArchived, &scriptedCreditCardLookupReader{}, clock).Execute(context.Background(), input); err != nil || !result.Replayed {
		t.Fatalf("valid archived replay = %#v, %v", result, err)
	}
}

func TestArchiveCreditCardMapsFinalStoreRaceErrorsAndSafeFailures(t *testing.T) {
	const marker = "sensitive-archive-marker"
	owner := validCreditCardInput().UserID
	id := creditCardID(111)
	active := creditCardForTest(t, id, owner, "Cartão sintético", domain.CreditCardStatusActive, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	input := application.ArchiveCreditCardInput{UserID: owner, CreditCardID: id, IdempotencyKey: "archive-error-key"}

	for _, failure := range []error{application.ErrCreditCardIdempotencyConflict, application.ErrCreditCardNotFound, domain.ErrCreditCardAlreadyArchived} {
		store := &scriptedArchiveCreditCardStore{err: failure}
		_, err := mustArchiveCreditCard(t, store, &scriptedCreditCardReplayReader{},
			&scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: active, Found: true}},
			&countingClock{now: active.CreatedAt().Add(time.Hour)},
		).Execute(context.Background(), input)
		if !errors.Is(err, failure) {
			t.Fatalf("store error = %v, want %v", err, failure)
		}
	}

	store := &scriptedArchiveCreditCardStore{err: errors.New(marker)}
	_, err := mustArchiveCreditCard(t, store, &scriptedCreditCardReplayReader{},
		&scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: active, Found: true}},
		&countingClock{now: active.CreatedAt().Add(time.Hour)},
	).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardArchivePersistence) || strings.Contains(err.Error(), marker) {
		t.Fatalf("safe store error = %v", err)
	}

	zeroClockStore := &scriptedArchiveCreditCardStore{}
	_, err = mustArchiveCreditCard(t, zeroClockStore, &scriptedCreditCardReplayReader{},
		&scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: active, Found: true}},
		&countingClock{},
	).Execute(context.Background(), input)
	if !errors.Is(err, application.ErrCreditCardDependencyResult) || zeroClockStore.calls != 0 {
		t.Fatalf("zero Clock archive error = %v, store calls=%d", err, zeroClockStore.calls)
	}
}

func TestGetCreditCardKnownUnknownCrossOwnerAndCorrupt(t *testing.T) {
	store := newMemoryCreditCardStore()
	created := mustCreateCardInStore(t, store, creditCardID(120), "create-for-get")
	useCase, err := application.NewGetCreditCard(store)
	if err != nil {
		t.Fatalf("NewGetCreditCard() error = %v", err)
	}
	result, err := useCase.Execute(context.Background(), application.GetCreditCardInput{UserID: created.CreditCard.UserID(), CreditCardID: created.CreditCard.ID()})
	if err != nil || result.CreditCard.ID() != created.CreditCard.ID() {
		t.Fatalf("known Get = %#v, %v", result, err)
	}
	for _, input := range []application.GetCreditCardInput{
		{UserID: created.CreditCard.UserID(), CreditCardID: creditCardID(999)},
		{UserID: "owner-b", CreditCardID: created.CreditCard.ID()},
	} {
		if _, err := useCase.Execute(context.Background(), input); !errors.Is(err, application.ErrCreditCardNotFound) {
			t.Fatalf("unknown/cross-owner Get error = %v", err)
		}
	}

	malicious := &scriptedCreditCardLookupReader{lookup: application.CreditCardLookup{CreditCard: creditCardForTest(t, created.CreditCard.ID(), "owner-b", "Malicious", domain.CreditCardStatusActive, created.CreditCard.CreatedAt()), Found: true}}
	maliciousUseCase, _ := application.NewGetCreditCard(malicious)
	if _, err := maliciousUseCase.Execute(context.Background(), application.GetCreditCardInput{UserID: created.CreditCard.UserID(), CreditCardID: created.CreditCard.ID()}); !errors.Is(err, application.ErrCreditCardDependencyResult) {
		t.Fatalf("malicious Get error = %v", err)
	}
}

func TestListCreditCardsIsOwnerScopedNonNilAndDeterministic(t *testing.T) {
	owner := "list-owner"
	base := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	items := []domain.CreditCard{
		creditCardForTest(t, creditCardID(125), owner, "Zulu", domain.CreditCardStatusActive, base),
		creditCardForTest(t, creditCardID(124), owner, "Alpha", domain.CreditCardStatusArchived, base),
		creditCardForTest(t, creditCardID(123), owner, "Alpha", domain.CreditCardStatusActive, base),
		creditCardForTest(t, creditCardID(122), owner, "Alpha", domain.CreditCardStatusActive, base.Add(time.Hour)),
		creditCardForTest(t, creditCardID(121), owner, "Alpha", domain.CreditCardStatusActive, base),
	}
	originalFirst := items[0].ID()
	reader := &scriptedCreditCardListReader{items: items}
	useCase, err := application.NewListCreditCards(reader)
	if err != nil {
		t.Fatalf("NewListCreditCards() error = %v", err)
	}
	result, err := useCase.Execute(context.Background(), owner)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := []string{creditCardID(122), creditCardID(121), creditCardID(123), creditCardID(125), creditCardID(124)}
	for index, id := range want {
		if result.Items[index].ID() != id {
			t.Fatalf("Items[%d] = %s, want %s", index, result.Items[index].ID(), id)
		}
	}
	if items[0].ID() != originalFirst {
		t.Fatal("ListCreditCards mutated reader-owned slice")
	}

	emptyUseCase, _ := application.NewListCreditCards(&scriptedCreditCardListReader{})
	empty, err := emptyUseCase.Execute(context.Background(), owner)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty list = %#v, %v", empty, err)
	}
}

func TestListCreditCardsFailsClosedForWrongOwnerDuplicateAndCorruption(t *testing.T) {
	owner := "list-owner"
	valid := creditCardForTest(t, creditCardID(130), owner, "Valid", domain.CreditCardStatusActive, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	wrongOwner := creditCardForTest(t, creditCardID(131), "other-owner", "Wrong", domain.CreditCardStatusActive, valid.CreatedAt())
	tests := [][]domain.CreditCard{
		{wrongOwner},
		{valid, valid},
		{{}},
	}
	for _, items := range tests {
		useCase, _ := application.NewListCreditCards(&scriptedCreditCardListReader{items: items})
		if _, err := useCase.Execute(context.Background(), owner); !errors.Is(err, application.ErrCreditCardDependencyResult) {
			t.Fatalf("corrupt list error = %v", err)
		}
	}
}

func TestCreditCardQueriesPreserveContextAndHideInfrastructureErrors(t *testing.T) {
	const marker = "sensitive-query-marker"
	owner := "query-owner"
	id := creditCardID(140)
	getReader := &scriptedCreditCardLookupReader{err: errors.New(marker)}
	get, _ := application.NewGetCreditCard(getReader)
	_, err := get.Execute(context.Background(), application.GetCreditCardInput{UserID: owner, CreditCardID: id})
	if !errors.Is(err, application.ErrCreditCardLookup) || strings.Contains(err.Error(), marker) {
		t.Fatalf("Get error = %v", err)
	}
	listReader := &scriptedCreditCardListReader{err: errors.New(marker)}
	list, _ := application.NewListCreditCards(listReader)
	_, err = list.Execute(context.Background(), owner)
	if !errors.Is(err, application.ErrCreditCardListQuery) || strings.Contains(err.Error(), marker) {
		t.Fatalf("List error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := get.Execute(ctx, application.GetCreditCardInput{UserID: owner, CreditCardID: id}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Get cancelled error = %v", err)
	}
	if _, err := list.Execute(ctx, owner); !errors.Is(err, context.Canceled) {
		t.Fatalf("List cancelled error = %v", err)
	}
	if getReader.calls != 1 || listReader.calls != 1 {
		t.Fatal("already-cancelled context reached readers")
	}
}

func TestCreditCardUseCasesValidateInputAndConstructors(t *testing.T) {
	if _, err := application.NewGetCreditCard(nil); !errors.Is(err, application.ErrMissingCreditCardLookupReader) {
		t.Fatalf("NewGetCreditCard(nil) error = %v", err)
	}
	if _, err := application.NewListCreditCards(nil); !errors.Is(err, application.ErrMissingCreditCardListReader) {
		t.Fatalf("NewListCreditCards(nil) error = %v", err)
	}
	store := newMemoryCreditCardStore()
	clock := &countingClock{now: time.Now()}
	tests := []struct {
		name   string
		store  application.CreditCardArchiveStore
		replay application.CreditCardReplayReader
		reader application.CreditCardLookupReader
		clock  application.Clock
		want   error
	}{
		{name: "store", replay: store, reader: store, clock: clock, want: application.ErrMissingCreditCardArchiveStore},
		{name: "replay", store: store, reader: store, clock: clock, want: application.ErrMissingCreditCardReplayReader},
		{name: "reader", store: store, replay: store, clock: clock, want: application.ErrMissingCreditCardLookupReader},
		{name: "clock", store: store, replay: store, reader: store, want: application.ErrMissingCreditCardClock},
	}
	for _, test := range tests {
		if _, err := application.NewArchiveCreditCard(test.store, test.replay, test.reader, test.clock); !errors.Is(err, test.want) {
			t.Fatalf("NewArchiveCreditCard(%s) error = %v", test.name, err)
		}
	}

	archive := mustArchiveCreditCard(t, store, store, store, clock)
	invalidInputs := []struct {
		input application.ArchiveCreditCardInput
		want  error
	}{
		{input: application.ArchiveCreditCardInput{UserID: " owner ", CreditCardID: creditCardID(1), IdempotencyKey: "key"}, want: domain.ErrInvalidCreditCardUserID},
		{input: application.ArchiveCreditCardInput{UserID: "owner", CreditCardID: "bad", IdempotencyKey: "key"}, want: domain.ErrInvalidCreditCardID},
		{input: application.ArchiveCreditCardInput{UserID: "owner", CreditCardID: creditCardID(1)}, want: application.ErrCreditCardIdempotencyKeyRequired},
		{input: application.ArchiveCreditCardInput{UserID: "owner", CreditCardID: creditCardID(1), IdempotencyKey: "bad key"}, want: application.ErrCreditCardIdempotencyKeyInvalid},
	}
	for _, test := range invalidInputs {
		if _, err := archive.Execute(context.Background(), test.input); !errors.Is(err, test.want) {
			t.Fatalf("Archive invalid error = %v, want %v", err, test.want)
		}
	}

	get, _ := application.NewGetCreditCard(store)
	for _, test := range []struct {
		input application.GetCreditCardInput
		want  error
	}{
		{input: application.GetCreditCardInput{UserID: " owner ", CreditCardID: creditCardID(1)}, want: domain.ErrInvalidCreditCardUserID},
		{input: application.GetCreditCardInput{UserID: "owner", CreditCardID: "bad"}, want: domain.ErrInvalidCreditCardID},
	} {
		if _, err := get.Execute(context.Background(), test.input); !errors.Is(err, test.want) {
			t.Fatalf("Get invalid error = %v, want %v", err, test.want)
		}
	}
	list, _ := application.NewListCreditCards(store)
	if _, err := list.Execute(context.Background(), " owner "); !errors.Is(err, domain.ErrInvalidCreditCardUserID) {
		t.Fatalf("List invalid owner error = %v", err)
	}
}

type scriptedArchiveCreditCardStore struct {
	calls  int
	result application.IdempotentArchiveCreditCardResult
	err    error
}

func (store *scriptedArchiveCreditCardStore) ArchiveCreditCard(context.Context, application.IdempotentArchiveCreditCardCommand) (application.IdempotentArchiveCreditCardResult, error) {
	store.calls++
	return store.result, store.err
}

func mustArchiveCreditCard(
	t testing.TB,
	store application.CreditCardArchiveStore,
	replayReader application.CreditCardReplayReader,
	reader application.CreditCardLookupReader,
	clock application.Clock,
) *application.ArchiveCreditCard {
	t.Helper()
	useCase, err := application.NewArchiveCreditCard(store, replayReader, reader, clock)
	if err != nil {
		t.Fatalf("NewArchiveCreditCard() error = %v", err)
	}
	return useCase
}

func mustCreateCardInStore(t testing.TB, store *memoryCreditCardStore, id string, key string) application.RecordCreditCardResult {
	t.Helper()
	result, err := mustRecordCreditCard(t, store, store,
		&fixedCreditCardIDGenerator{id: id},
		&countingClock{now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)},
	).Execute(context.Background(), application.RecordCreditCardInput{CreditCard: validCreditCardInput(), IdempotencyKey: key})
	if err != nil {
		t.Fatalf("create card error = %v", err)
	}
	return result
}
