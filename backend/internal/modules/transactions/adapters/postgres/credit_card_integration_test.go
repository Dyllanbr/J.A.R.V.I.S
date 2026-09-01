//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestCreditCardPostgresLifecycleReplayRestartOwnerIsolationAndSeparation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_credit_card_owner_a"
	const ownerB = "usr_credit_card_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	createdAt := time.Date(2026, time.August, 25, 12, 30, 0, 123_456_000, time.UTC)
	archivedAt := createdAt.Add(4 * time.Hour)
	repository := newCreditCardRepository(t, pool)
	create := newRecordCreditCardUseCase(
		t,
		repository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('a')},
		fixedCreditCardClock{now: createdAt},
	)
	input := fullCreditCardInput(ownerA, "credit-card-create-key")
	created, err := create.Execute(ctx, input)
	if err != nil || created.Replayed {
		t.Fatalf("create result = %+v error = %v, want first write", created, err)
	}
	assertCreditCardMatchesInput(t, created.CreditCard, input.CreditCard)
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)
	duplicateIDInput := input
	duplicateIDInput.IdempotencyKey = "credit-card-duplicate-id-key"
	duplicateIDInput.CreditCard.Name = "Outro cartão"
	if _, err := newRecordCreditCardUseCase(
		t,
		repository,
		&fixedCreditCardIDGenerator{id: created.CreditCard.ID()},
		fixedCreditCardClock{now: createdAt.Add(time.Hour)},
	).Execute(ctx, duplicateIDInput); !errors.Is(err, application.ErrCreditCardCommandPersistence) {
		t.Fatalf("duplicate card insert error = %v", err)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	assertCreditCardIdempotencyKeyCount(t, ctx, pool, ownerA, "credit-card-duplicate-id-key", 0)

	restartPool := openRestartPool(t, ctx, pool)
	restartedRepository := newCreditCardRepository(t, restartPool)
	replayGenerator := &fixedCreditCardIDGenerator{id: creditCardTestID('b')}
	replayCreate := newRecordCreditCardUseCase(
		t,
		restartedRepository,
		replayGenerator,
		fixedCreditCardClock{now: createdAt.Add(time.Hour)},
	)
	replayedCreate, err := replayCreate.Execute(ctx, input)
	if err != nil || !replayedCreate.Replayed {
		t.Fatalf("create replay result = %+v error = %v", replayedCreate, err)
	}
	if replayGenerator.calls.Load() != 0 || !creditCardsMatch(replayedCreate.CreditCard, created.CreditCard) {
		t.Fatal("persisted create replay changed result or consumed a new ID")
	}

	archive := newArchiveCreditCardUseCase(t, restartedRepository, fixedCreditCardClock{now: archivedAt})
	archived, err := archive.Execute(ctx, application.ArchiveCreditCardInput{
		UserID:         ownerA,
		CreditCardID:   created.CreditCard.ID(),
		IdempotencyKey: "credit-card-archive-key",
	})
	if err != nil || archived.Replayed || archived.CreditCard.Status() != domain.CreditCardStatusArchived {
		t.Fatalf("archive result = %+v error = %v", archived, err)
	}
	storedArchivedAt, ok := archived.CreditCard.ArchivedAt()
	if !ok || !storedArchivedAt.Equal(archivedAt) {
		t.Fatal("archive did not preserve the logical timestamp")
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertLegacyFinancialTablesEmpty(t, ctx, pool)

	secondRestartPool := openRestartPool(t, ctx, pool)
	secondRepository := newCreditCardRepository(t, secondRestartPool)
	replayedArchive, err := newArchiveCreditCardUseCase(
		t,
		secondRepository,
		fixedCreditCardClock{now: archivedAt.Add(time.Hour)},
	).Execute(ctx, application.ArchiveCreditCardInput{
		UserID:         ownerA,
		CreditCardID:   created.CreditCard.ID(),
		IdempotencyKey: "credit-card-archive-key",
	})
	if err != nil || !replayedArchive.Replayed || !creditCardsMatch(replayedArchive.CreditCard, archived.CreditCard) {
		t.Fatalf("archive restart replay result = %+v error = %v", replayedArchive, err)
	}
	if _, err := newArchiveCreditCardUseCase(
		t,
		secondRepository,
		fixedCreditCardClock{now: archivedAt.Add(90 * time.Minute)},
	).Execute(ctx, application.ArchiveCreditCardInput{
		UserID:         ownerA,
		CreditCardID:   created.CreditCard.ID(),
		IdempotencyKey: "credit-card-new-archive-key",
	}); !errors.Is(err, domain.ErrCreditCardAlreadyArchived) {
		t.Fatalf("new-key archive error = %v, want already archived", err)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertCreditCardIdempotencyKeyCount(t, ctx, pool, ownerA, "credit-card-new-archive-key", 0)
	currentAfterNewKey, err := secondRepository.FindCreditCard(ctx, ownerA, created.CreditCard.ID())
	if err != nil || !currentAfterNewKey.Found {
		t.Fatalf("lookup after new-key archive error = %v", err)
	}
	currentArchivedAt, ok := currentAfterNewKey.CreditCard.ArchivedAt()
	if !ok || !currentArchivedAt.Equal(archivedAt) {
		t.Fatal("new-key already-archived attempt changed ArchivedAt")
	}

	historicalCreate, err := newRecordCreditCardUseCase(
		t,
		secondRepository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('c')},
		fixedCreditCardClock{now: archivedAt.Add(2 * time.Hour)},
	).Execute(ctx, input)
	if err != nil || !historicalCreate.Replayed || historicalCreate.CreditCard.Status() != domain.CreditCardStatusActive {
		t.Fatalf("historical create replay = %+v error = %v", historicalCreate, err)
	}
	if _, hasArchivedAt := historicalCreate.CreditCard.ArchivedAt(); hasArchivedAt ||
		!historicalCreate.CreditCard.CreatedAt().Equal(createdAt) ||
		historicalCreate.CreditCard.ID() != created.CreditCard.ID() {
		t.Fatal("historical create replay used current ARCHIVED row")
	}

	lookupA, err := secondRepository.FindCreditCard(ctx, ownerA, created.CreditCard.ID())
	if err != nil || !lookupA.Found || lookupA.CreditCard.Status() != domain.CreditCardStatusArchived {
		t.Fatalf("owner lookup result = %+v error = %v", lookupA, err)
	}
	lookupB, err := secondRepository.FindCreditCard(ctx, ownerB, created.CreditCard.ID())
	if err != nil || lookupB.Found {
		t.Fatalf("cross-owner lookup result = %+v error = %v", lookupB, err)
	}
	itemsA, err := secondRepository.ListCreditCards(ctx, ownerA)
	if err != nil || len(itemsA) != 1 || itemsA[0].Status() != domain.CreditCardStatusArchived {
		t.Fatalf("owner A list = %+v error = %v", itemsA, err)
	}
	itemsB, err := secondRepository.ListCreditCards(ctx, ownerB)
	if err != nil || len(itemsB) != 0 {
		t.Fatalf("owner B list = %+v error = %v", itemsB, err)
	}

	crossOwnerArchive, err := newArchiveCreditCardUseCase(
		t,
		secondRepository,
		fixedCreditCardClock{now: archivedAt.Add(3 * time.Hour)},
	).Execute(ctx, application.ArchiveCreditCardInput{
		UserID:         ownerB,
		CreditCardID:   created.CreditCard.ID(),
		IdempotencyKey: "cross-owner-archive-key",
	})
	if !errors.Is(err, application.ErrCreditCardNotFound) || crossOwnerArchive.CreditCard.ID() != "" {
		t.Fatalf("cross-owner archive result = %+v error = %v", crossOwnerArchive, err)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	assertCreditCardIdempotencyKeyCount(t, ctx, pool, ownerB, "cross-owner-archive-key", 0)
}

func TestCreditCardPostgresRepositoryConstructionAndValidation(t *testing.T) {
	if _, err := adapter.NewCreditCardRepository(nil, time.Second); !errors.Is(err, adapter.ErrMissingCreditCardPool) {
		t.Fatalf("nil pool error = %v", err)
	}
	pool := newMigratedTestDatabase(t)
	if _, err := adapter.NewCreditCardRepository(pool, 0); !errors.Is(err, adapter.ErrInvalidCreditCardTimeout) {
		t.Fatalf("zero timeout error = %v", err)
	}
	if _, err := adapter.NewCreditCardRepository(pool, 31*time.Second); !errors.Is(err, adapter.ErrInvalidCreditCardTimeout) {
		t.Fatalf("excessive timeout error = %v", err)
	}
}

func TestCreditCardPostgresOptionalFieldsAndOwnerScopedOperationNamespaces(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const ownerA = "usr_credit_card_optional_a"
	const ownerB = "usr_credit_card_optional_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)
	repository := newCreditCardRepository(t, pool)
	minimalInput := func(owner string) application.RecordCreditCardInput {
		return application.RecordCreditCardInput{
			CreditCard: application.CreditCardInput{
				UserID: owner, Name: "Cartão sem opcionais", ClosingDayAnchor: 1, DueDayAnchor: 31,
			},
			IdempotencyKey: "shared-owner-operation-key",
		}
	}
	cardA, err := newRecordCreditCardUseCase(
		t,
		repository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('a')},
		fixedCreditCardClock{now: creditCardCreatedAt()},
	).Execute(ctx, minimalInput(ownerA))
	if err != nil || cardA.Replayed {
		t.Fatalf("minimal owner A create result=%+v error=%v", cardA, err)
	}
	cardB, err := newRecordCreditCardUseCase(
		t,
		repository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('b')},
		fixedCreditCardClock{now: creditCardCreatedAt()},
	).Execute(ctx, minimalInput(ownerB))
	if err != nil || cardB.Replayed {
		t.Fatalf("same-key owner B create result=%+v error=%v", cardB, err)
	}
	for _, card := range []domain.CreditCard{cardA.CreditCard, cardB.CreditCard} {
		if _, ok := card.LastFour(); ok {
			t.Fatal("absent last four became present")
		}
		if _, ok := card.Brand(); ok {
			t.Fatal("absent brand became present")
		}
		if _, ok := card.CreditLimit(); ok {
			t.Fatal("absent credit limit became present")
		}
	}
	lookup, err := repository.FindCreditCard(ctx, ownerA, cardA.CreditCard.ID())
	if err != nil || !lookup.Found || !creditCardsMatch(lookup.CreditCard, cardA.CreditCard) {
		t.Fatalf("minimal card lookup=%+v error=%v", lookup, err)
	}
	items, err := repository.ListCreditCards(ctx, ownerA)
	if err != nil || len(items) != 1 || !creditCardsMatch(items[0], cardA.CreditCard) {
		t.Fatalf("minimal card list=%+v error=%v", items, err)
	}
	archived, err := newArchiveCreditCardUseCase(
		t,
		repository,
		fixedCreditCardClock{now: creditCardArchivedAt()},
	).Execute(ctx, application.ArchiveCreditCardInput{
		UserID: ownerA, CreditCardID: cardA.CreditCard.ID(), IdempotencyKey: "shared-owner-operation-key",
	})
	if err != nil || archived.Replayed || archived.CreditCard.Status() != domain.CreditCardStatusArchived {
		t.Fatalf("same key in ARCHIVE namespace result=%+v error=%v", archived, err)
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 2, 3, 3)
}

func TestCreditCardPostgresRoundTripsEveryOptionalFieldCombination(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	owner := "usr_credit_card_optional_matrix"
	insertSyntheticUser(t, ctx, pool, owner)
	repository := newCreditCardRepository(t, pool)
	want := make(map[string]application.CreditCardInput, 8)
	hexCharacters := "01234567"
	for mask := 0; mask < 8; mask++ {
		input := application.CreditCardInput{
			UserID: owner, Name: fmt.Sprintf("Cartão opcional %d", mask),
			ClosingDayAnchor: 28 + mask%4, DueDayAnchor: 1 + mask,
		}
		if mask&1 != 0 {
			value := "4242"
			input.LastFour = &value
		}
		if mask&2 != 0 {
			value := domain.CreditCardBrandMastercard
			input.Brand = &value
		}
		if mask&4 != 0 {
			input.CreditLimit = &application.CreditCardLimitInput{
				MinorUnits: int64(100000 + mask), Currency: domain.CurrencyBRL,
			}
		}
		result, err := newRecordCreditCardUseCase(
			t,
			repository,
			&fixedCreditCardIDGenerator{id: creditCardTestID(hexCharacters[mask])},
			fixedCreditCardClock{now: creditCardCreatedAt()},
		).Execute(ctx, application.RecordCreditCardInput{
			CreditCard: input, IdempotencyKey: fmt.Sprintf("optional-matrix-%d", mask),
		})
		if err != nil || result.Replayed {
			t.Fatalf("optional mask %d result=%+v error=%v", mask, result, err)
		}
		want[result.CreditCard.ID()] = input
	}

	restartPool := openRestartPool(t, ctx, pool)
	items, err := newCreditCardRepository(t, restartPool).ListCreditCards(ctx, owner)
	if err != nil || len(items) != len(want) {
		t.Fatalf("optional matrix list count=%d error=%v, want %d", len(items), err, len(want))
	}
	for _, card := range items {
		expected, ok := want[card.ID()]
		if !ok {
			t.Fatalf("unexpected optional matrix card %q", card.ID())
		}
		lastFour, hasLastFour := card.LastFour()
		brand, hasBrand := card.Brand()
		limit, hasLimit := card.CreditLimit()
		if hasLastFour != (expected.LastFour != nil) ||
			(hasLastFour && lastFour.String() != *expected.LastFour) ||
			hasBrand != (expected.Brand != nil) ||
			(hasBrand && brand != *expected.Brand) ||
			hasLimit != (expected.CreditLimit != nil) ||
			(hasLimit && (limit.MinorUnits() != expected.CreditLimit.MinorUnits ||
				limit.Currency() != expected.CreditLimit.Currency)) {
			t.Fatalf("optional mask for card %q did not round-trip", card.ID())
		}
	}
	assertCreditCardSubsystemCounts(t, ctx, pool, 8, 8, 8)
}

func TestCreditCardPostgresCreateConcurrency(t *testing.T) {
	t.Run("same key and fingerprint", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		generator := &sequenceCreditCardIDGenerator{}
		useCase := newRecordCreditCardUseCase(
			t,
			repository,
			generator,
			fixedCreditCardClock{now: creditCardCreatedAt()},
		)
		input := fullCreditCardInput(owner, "same-create-key")
		results, errs := runConcurrentCreditCardCreates(ctx, useCase, []application.RecordCreditCardInput{input, input, input, input, input, input})
		first, replay := 0, 0
		var expectedID string
		for index, err := range errs {
			if err != nil {
				t.Fatalf("create %d error = %v", index, err)
			}
			if results[index].Replayed {
				replay++
			} else {
				first++
				expectedID = results[index].CreditCard.ID()
			}
		}
		for _, result := range results {
			if result.CreditCard.ID() != expectedID {
				t.Fatal("same-key create returned different persisted cards")
			}
		}
		if first != 1 || replay != 5 {
			t.Fatalf("first/replay = %d/%d, want 1/5", first, replay)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("same key different fingerprint", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		useCase := newRecordCreditCardUseCase(
			t,
			repository,
			&sequenceCreditCardIDGenerator{},
			fixedCreditCardClock{now: creditCardCreatedAt()},
		)
		left := fullCreditCardInput(owner, "conflicting-create-key")
		right := left
		right.CreditCard.Name = "Cartão diferente"
		results, errs := runConcurrentCreditCardCreates(ctx, useCase, []application.RecordCreditCardInput{left, right})
		success, conflict := 0, 0
		for index, err := range errs {
			switch {
			case err == nil && !results[index].Replayed:
				success++
			case errors.Is(err, application.ErrCreditCardIdempotencyConflict):
				conflict++
			default:
				t.Fatalf("unexpected create %d result=%+v error=%v", index, results[index], err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("success/conflict = %d/%d, want 1/1", success, conflict)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("different keys", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		useCase := newRecordCreditCardUseCase(
			t,
			repository,
			&sequenceCreditCardIDGenerator{},
			fixedCreditCardClock{now: creditCardCreatedAt()},
		)
		left := fullCreditCardInput(owner, "different-create-key-a")
		right := fullCreditCardInput(owner, "different-create-key-b")
		results, errs := runConcurrentCreditCardCreates(ctx, useCase, []application.RecordCreditCardInput{left, right})
		for index, err := range errs {
			if err != nil || results[index].Replayed {
				t.Fatalf("different-key create %d result=%+v error=%v", index, results[index], err)
			}
		}
		if results[0].CreditCard.ID() == results[1].CreditCard.ID() {
			t.Fatal("different-key creates reused a card ID")
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 2, 2, 2)
	})
}

func TestCreditCardPostgresReadCommittedReservationInterleavings(t *testing.T) {
	t.Run("winner commits and blocked contender replays its snapshot", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		winner := directCreateCreditCardCommand(
			t,
			newDirectActiveCreditCard(t, owner, creditCardTestID('a'), creditCardCreatedAt()),
			"read-committed-commit-key",
			0x71,
		)
		contender := directCreateCreditCardCommand(
			t,
			newDirectActiveCreditCard(t, owner, creditCardTestID('b'), creditCardCreatedAt().Add(time.Minute)),
			winner.IdempotencyKey,
			0x71,
		)

		winnerConnection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal("acquiring reservation winner connection failed")
		}
		defer winnerConnection.Release()
		winnerPID := connectionBackendPID(t, ctx, winnerConnection)
		winnerTx, err := winnerConnection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal("beginning READ COMMITTED winner failed")
		}
		defer func() { _ = winnerTx.Rollback(context.Background()) }()
		assertCreditCardTransactionIsolation(t, ctx, winnerTx, "read committed")
		insertDirectPendingCreditCardReservation(t, ctx, winnerTx, winner)
		assertDirectPendingReservationVisibility(t, ctx, pool, winnerTx, winner, 1, 0)

		contenderPool, contenderPID := newSingleConnectionCreditCardPool(t, ctx, pool)
		contenderRepository := newCreditCardRepository(t, contenderPool)
		result := make(chan directCreateResult, 1)
		started := make(chan struct{})
		go func() {
			close(started)
			stored, storeErr := contenderRepository.RecordCreditCard(ctx, contender)
			result <- directCreateResult{result: stored, err: storeErr}
		}()
		<-started
		waitForBackendBlockedBy(t, ctx, pool, contenderPID, winnerPID)
		assertDirectCreateStillBlocked(t, result)

		completeDirectCreditCardCreate(t, ctx, winnerTx, winner)
		if err := winnerTx.Commit(ctx); err != nil {
			t.Fatal("committing reservation winner failed")
		}

		contenderResult := waitForDirectCreateResult(t, ctx, result)
		if contenderResult.err != nil || !contenderResult.result.Replayed {
			t.Fatalf("blocked contender result=%+v error=%v, want replay", contenderResult.result, contenderResult.err)
		}
		if !creditCardsMatch(contenderResult.result.CreditCard, winner.CreditCard) {
			t.Fatal("blocked contender did not replay the committed winner snapshot")
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
		assertCompletedCreditCardIdempotencyRows(t, ctx, pool, owner, winner.IdempotencyKey, 1)
		assertLegacyFinancialTablesEmpty(t, ctx, pool)
	})

	t.Run("winner rolls back and blocked contender becomes first writer", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		aborted := directCreateCreditCardCommand(
			t,
			newDirectActiveCreditCard(t, owner, creditCardTestID('c'), creditCardCreatedAt()),
			"read-committed-rollback-key",
			0x72,
		)
		contender := directCreateCreditCardCommand(
			t,
			newDirectActiveCreditCard(t, owner, creditCardTestID('d'), creditCardCreatedAt().Add(time.Minute)),
			aborted.IdempotencyKey,
			0x72,
		)

		winnerConnection, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal("acquiring rollback winner connection failed")
		}
		defer winnerConnection.Release()
		winnerPID := connectionBackendPID(t, ctx, winnerConnection)
		winnerTx, err := winnerConnection.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal("beginning READ COMMITTED rollback winner failed")
		}
		defer func() { _ = winnerTx.Rollback(context.Background()) }()
		assertCreditCardTransactionIsolation(t, ctx, winnerTx, "read committed")
		insertDirectPendingCreditCardReservation(t, ctx, winnerTx, aborted)
		insertDirectCreditCardAndAudit(t, ctx, winnerTx, aborted)
		assertDirectPendingReservationVisibility(t, ctx, pool, winnerTx, aborted, 1, 0)

		contenderPool, contenderPID := newSingleConnectionCreditCardPool(t, ctx, pool)
		contenderRepository := newCreditCardRepository(t, contenderPool)
		result := make(chan directCreateResult, 1)
		started := make(chan struct{})
		go func() {
			close(started)
			stored, storeErr := contenderRepository.RecordCreditCard(ctx, contender)
			result <- directCreateResult{result: stored, err: storeErr}
		}()
		<-started
		waitForBackendBlockedBy(t, ctx, pool, contenderPID, winnerPID)
		assertDirectCreateStillBlocked(t, result)

		if err := winnerTx.Rollback(ctx); err != nil {
			t.Fatal("rolling back reservation winner failed")
		}

		contenderResult := waitForDirectCreateResult(t, ctx, result)
		if contenderResult.err != nil || contenderResult.result.Replayed {
			t.Fatalf("blocked contender result=%+v error=%v, want first writer", contenderResult.result, contenderResult.err)
		}
		if !creditCardsMatch(contenderResult.result.CreditCard, contender.CreditCard) {
			t.Fatal("rollback contender did not persist its own snapshot")
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
		assertCompletedCreditCardIdempotencyRows(t, ctx, pool, owner, aborted.IdempotencyKey, 1)
		assertCreditCardIDCount(t, ctx, pool, aborted.CreditCard.ID(), 0)
		assertCreditCardIDCount(t, ctx, pool, contender.CreditCard.ID(), 1)
		assertLegacyFinancialTablesEmpty(t, ctx, pool)
	})
}

func TestCreditCardPostgresArchiveConcurrency(t *testing.T) {
	t.Run("same key same card", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('1'), "same-archive-create")
		command := directArchiveCreditCardCommand(t, card, "same-archive-key", 0x11)
		commands := []application.IdempotentArchiveCreditCardCommand{
			command, command, command, command, command, command,
		}
		results, errs := runConcurrentCreditCardArchiveStores(ctx, repository, commands)
		first, replay := 0, 0
		for index, err := range errs {
			if err != nil {
				t.Fatalf("archive %d error = %v", index, err)
			}
			if results[index].Replayed {
				replay++
			} else {
				first++
			}
		}
		if first != 1 || replay != 5 {
			t.Fatalf("same-key archive first/replay = %d/%d", first, replay)
		}
		for index := 1; index < len(results); index++ {
			if !creditCardsMatch(results[0].CreditCard, results[index].CreditCard) {
				t.Fatal("same-key archive returned different persisted snapshots")
			}
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	})

	t.Run("different keys same card", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('2'), "different-archive-create")
		commands := []application.IdempotentArchiveCreditCardCommand{
			directArchiveCreditCardCommand(t, card, "archive-key-a", 0x22),
			directArchiveCreditCardCommand(t, card, "archive-key-b", 0x22),
		}
		results, errs := runConcurrentCreditCardArchiveStores(ctx, repository, commands)
		success, already := 0, 0
		for index, err := range errs {
			switch {
			case err == nil && !results[index].Replayed:
				success++
			case errors.Is(err, domain.ErrCreditCardAlreadyArchived):
				already++
			default:
				t.Fatalf("unexpected archive %d result=%+v error=%v", index, results[index], err)
			}
		}
		if success != 1 || already != 1 {
			t.Fatalf("success/already = %d/%d, want 1/1", success, already)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
		var archiveCompletions int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM credit_card_idempotency_records
			WHERE operation = 'ARCHIVE_CREDIT_CARD'
		`).Scan(&archiveCompletions); err != nil || archiveCompletions != 1 {
			t.Fatalf("archive completions = %d error = %v, want 1", archiveCompletions, err)
		}
	})

	t.Run("same key different cards", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		left := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('3'), "conflict-card-create-a")
		right := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('4'), "conflict-card-create-b")
		commands := []application.IdempotentArchiveCreditCardCommand{
			directArchiveCreditCardCommand(t, left, "archive-conflict-key", 0x33),
			directArchiveCreditCardCommand(t, right, "archive-conflict-key", 0x44),
		}
		results, errs := runConcurrentCreditCardArchiveStores(ctx, repository, commands)
		success, conflict := 0, 0
		for index, err := range errs {
			switch {
			case err == nil && !results[index].Replayed:
				success++
			case errors.Is(err, application.ErrCreditCardIdempotencyConflict):
				conflict++
			default:
				t.Fatalf("unexpected archive %d result=%+v error=%v", index, results[index], err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("success/conflict = %d/%d, want 1/1", success, conflict)
		}
		var active, archived int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE status = 'ACTIVE'),
			       count(*) FILTER (WHERE status = 'ARCHIVED')
			FROM credit_cards
		`).Scan(&active, &archived); err != nil || active != 1 || archived != 1 {
			t.Fatalf("active/archived = %d/%d error=%v, want 1/1", active, archived, err)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 2, 3, 3)
	})
}

func TestCreditCardPostgresRollbackAllowsSameKeyRetry(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		useCase := newRecordCreditCardUseCase(
			t,
			repository,
			&fixedCreditCardIDGenerator{id: creditCardTestID('d')},
			fixedCreditCardClock{now: creditCardCreatedAt()},
		)
		installCreditCardAuditFailureTrigger(t, ctx, pool)
		input := fullCreditCardInput(owner, "rollback-create-key")
		if _, err := useCase.Execute(ctx, input); !errors.Is(err, application.ErrCreditCardCommandPersistence) {
			t.Fatalf("create failure error = %v", err)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 0, 0, 0)
		dropCreditCardAuditFailureTrigger(t, ctx, pool)
		result, err := useCase.Execute(ctx, input)
		if err != nil || result.Replayed {
			t.Fatalf("same-key create retry result=%+v error=%v", result, err)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("create completion", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		useCase := newRecordCreditCardUseCase(
			t,
			repository,
			&fixedCreditCardIDGenerator{id: creditCardTestID('c')},
			fixedCreditCardClock{now: creditCardCreatedAt()},
		)
		installCreditCardCompletionFailureTrigger(t, ctx, pool)
		input := fullCreditCardInput(owner, "rollback-completion-key")
		if _, err := useCase.Execute(ctx, input); !errors.Is(err, application.ErrCreditCardCommandPersistence) {
			t.Fatalf("completion failure error = %v", err)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 0, 0, 0)
		dropCreditCardCompletionFailureTrigger(t, ctx, pool)
		result, err := useCase.Execute(ctx, input)
		if err != nil || result.Replayed {
			t.Fatalf("same-key completion retry result=%+v error=%v", result, err)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
	})

	t.Run("archive", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('e'), "rollback-archive-create")
		useCase := newArchiveCreditCardUseCase(t, repository, fixedCreditCardClock{now: creditCardArchivedAt()})
		installCreditCardAuditFailureTrigger(t, ctx, pool)
		input := application.ArchiveCreditCardInput{
			UserID: owner, CreditCardID: card.ID(), IdempotencyKey: "rollback-archive-key",
		}
		if _, err := useCase.Execute(ctx, input); !errors.Is(err, application.ErrCreditCardArchivePersistence) {
			t.Fatalf("archive failure error = %v", err)
		}
		var status string
		var archivedAt *time.Time
		if err := pool.QueryRow(ctx, `
			SELECT status, archived_at FROM credit_cards WHERE id = $1
		`, card.ID()).Scan(&status, &archivedAt); err != nil || status != "ACTIVE" || archivedAt != nil {
			t.Fatalf("rolled-back card status/time = %q/%v error=%v", status, archivedAt, err)
		}
		assertCreditCardIdempotencyKeyCount(t, ctx, pool, owner, "rollback-archive-key", 0)
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
		dropCreditCardAuditFailureTrigger(t, ctx, pool)
		result, err := useCase.Execute(ctx, input)
		if err != nil || result.Replayed || result.CreditCard.Status() != domain.CreditCardStatusArchived {
			t.Fatalf("same-key archive retry result=%+v error=%v", result, err)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
	})
}

func TestCreditCardPostgresFailsClosedForCorruptSnapshotsAndPreservesContext(t *testing.T) {
	pool, ctx, owner := newCreditCardConcurrentFixture(t)
	repository := newCreditCardRepository(t, pool)
	card := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('f'), "corruption-create-key")

	if _, err := pool.Exec(ctx, `
		ALTER TABLE credit_card_idempotency_records
		DROP CONSTRAINT credit_card_idem_result_valid
	`); err != nil {
		t.Fatal("dropping synthetic corruption guard failed")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE credit_card_idempotency_records
		SET result_last_four = '12AB'
		WHERE user_id = $1 AND operation = 'CREATE_CREDIT_CARD'
	`, owner); err != nil {
		t.Fatal("installing corrupt replay snapshot failed")
	}
	var storedFingerprint []byte
	if err := pool.QueryRow(ctx, `
		SELECT request_fingerprint FROM credit_card_idempotency_records
		WHERE user_id = $1 AND operation = 'CREATE_CREDIT_CARD'
	`, owner).Scan(&storedFingerprint); err != nil {
		t.Fatal("reading replay fingerprint failed")
	}
	var fingerprint application.RequestFingerprint
	copy(fingerprint[:], storedFingerprint)
	_, err := repository.FindCreditCardReplay(ctx, application.CreditCardReplayQuery{
		UserID: owner, Operation: application.IdempotencyOperationCreateCreditCard,
		IdempotencyKey: "corruption-create-key", Fingerprint: fingerprint,
	})
	if !errors.Is(err, adapter.ErrInvalidStoredCreditCard) || errorContainsSensitiveDatabaseDetail(err) {
		t.Fatalf("corrupt replay error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
		ALTER TABLE credit_cards DROP CONSTRAINT credit_cards_closing_day_valid
	`); err != nil {
		t.Fatal("dropping synthetic aggregate corruption guard failed")
	}
	if _, err := pool.Exec(ctx, `
		UPDATE credit_cards SET closing_day = 0 WHERE id = $1
	`, card.ID()); err != nil {
		t.Fatal("installing corrupt aggregate row failed")
	}
	if _, err := repository.ListCreditCards(ctx, owner); !errors.Is(err, adapter.ErrInvalidStoredCreditCard) ||
		errorContainsSensitiveDatabaseDetail(err) {
		t.Fatalf("corrupt aggregate list error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.ListCreditCards(cancelled, owner); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled list error = %v", err)
	}
	if _, err := repository.FindCreditCard(cancelled, owner, card.ID()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup error = %v", err)
	}
}

func TestCreditCardPostgresCancelledContextsDoNotWrite(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		command := directCreateCreditCardCommand(
			t,
			newDirectActiveCreditCard(t, owner, creditCardTestID('1'), creditCardCreatedAt()),
			"cancelled-create-key",
			0x81,
		)
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := repository.RecordCreditCard(cancelled, command)
		if !errors.Is(err, context.Canceled) || result.Replayed || result.CreditCard.ID() != "" {
			t.Fatalf("cancelled create result=%+v error=%v", result, err)
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 0, 0, 0)
		assertCompletedCreditCardIdempotencyRows(t, ctx, pool, owner, command.IdempotencyKey, 0)
		assertLegacyFinancialTablesEmpty(t, ctx, pool)
	})

	t.Run("archive", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('2'), "cancelled-archive-create")
		command := directArchiveCreditCardCommand(t, card, "cancelled-archive-key", 0x82)
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := repository.ArchiveCreditCard(cancelled, command)
		if !errors.Is(err, context.Canceled) || result.Replayed || result.CreditCard.ID() != "" {
			t.Fatalf("cancelled archive result=%+v error=%v", result, err)
		}
		lookup, lookupErr := repository.FindCreditCard(ctx, owner, card.ID())
		if lookupErr != nil || !lookup.Found || lookup.CreditCard.Status() != domain.CreditCardStatusActive {
			t.Fatalf("card after cancelled archive=%+v error=%v", lookup, lookupErr)
		}
		if _, archived := lookup.CreditCard.ArchivedAt(); archived {
			t.Fatal("cancelled archive installed ArchivedAt")
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 1, 1)
		assertCreditCardIdempotencyKeyCount(t, ctx, pool, owner, command.IdempotencyKey, 0)
		assertCompletedCreditCardIdempotencyRows(t, ctx, pool, owner, "cancelled-archive-create", 1)
		assertLegacyFinancialTablesEmpty(t, ctx, pool)
	})

	t.Run("replay", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		repository := newCreditCardRepository(t, pool)
		create := directCreateCreditCardCommand(
			t,
			newDirectActiveCreditCard(t, owner, creditCardTestID('3'), creditCardCreatedAt()),
			"cancelled-replay-create-key",
			0x83,
		)
		created, err := repository.RecordCreditCard(ctx, create)
		if err != nil || created.Replayed {
			t.Fatalf("replay cancellation create fixture=%+v error=%v", created, err)
		}
		archive := directArchiveCreditCardCommand(t, created.CreditCard, "cancelled-replay-archive-key", 0x84)
		archived, err := repository.ArchiveCreditCard(ctx, archive)
		if err != nil || archived.Replayed {
			t.Fatalf("replay cancellation archive fixture=%+v error=%v", archived, err)
		}
		before := creditCardIdempotencySignatures(t, ctx, pool, owner)
		for _, query := range []application.CreditCardReplayQuery{
			{UserID: owner, Operation: create.Operation, IdempotencyKey: create.IdempotencyKey, Fingerprint: create.Fingerprint},
			{UserID: owner, Operation: archive.Operation, IdempotencyKey: archive.IdempotencyKey, Fingerprint: archive.Fingerprint},
		} {
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			lookup, replayErr := repository.FindCreditCardReplay(cancelled, query)
			if !errors.Is(replayErr, context.Canceled) || lookup.Found || lookup.CreditCard.ID() != "" {
				t.Fatalf("cancelled %s replay lookup=%+v error=%v", query.Operation, lookup, replayErr)
			}
		}
		after := creditCardIdempotencySignatures(t, ctx, pool, owner)
		if before != after {
			t.Fatal("cancelled replay lookup changed completed snapshots")
		}
		assertCreditCardSubsystemCounts(t, ctx, pool, 1, 2, 2)
		assertCompletedCreditCardIdempotencyRows(t, ctx, pool, owner, create.IdempotencyKey, 1)
		assertCompletedCreditCardIdempotencyRows(t, ctx, pool, owner, archive.IdempotencyKey, 1)
		assertLegacyFinancialTablesEmpty(t, ctx, pool)
	})
}

func TestCreditCardPostgresClosedPoolFailuresAreCategorizedAndSafe(t *testing.T) {
	pool, ctx, owner := newCreditCardConcurrentFixture(t)
	repository := newCreditCardRepository(t, pool)
	card := createCreditCardFixture(t, ctx, repository, owner, creditCardTestID('a'), "closed-pool-create")
	pool.Close()

	assertSafeCategory := func(err, category error) {
		t.Helper()
		if !errors.Is(err, category) || errorContainsSensitiveDatabaseDetail(err) {
			t.Fatalf("closed-pool error=%v, want safe category %v", err, category)
		}
	}
	_, err := repository.ListCreditCards(ctx, owner)
	assertSafeCategory(err, adapter.ErrListCreditCards)
	_, err = repository.FindCreditCard(ctx, owner, card.ID())
	assertSafeCategory(err, adapter.ErrReadCreditCard)
	var fingerprint application.RequestFingerprint
	_, err = repository.FindCreditCardReplay(ctx, application.CreditCardReplayQuery{
		UserID: owner, Operation: application.IdempotencyOperationCreateCreditCard,
		IdempotencyKey: "closed-pool-replay", Fingerprint: fingerprint,
	})
	assertSafeCategory(err, adapter.ErrReadCreditCardIdempotency)
	_, err = repository.RecordCreditCard(ctx, application.IdempotentCreditCardCommand{
		Operation: application.IdempotencyOperationCreateCreditCard, IdempotencyKey: "closed-pool-create-2",
		Fingerprint: fingerprint, AuditEvent: application.CreditCardAuditEventCreated, CreditCard: card,
	})
	assertSafeCategory(err, adapter.ErrBeginCreditCardTransaction)
	archived, archiveErr := card.Archive(creditCardArchivedAt())
	if archiveErr != nil {
		t.Fatalf("building closed-pool archive failed: %v", archiveErr)
	}
	_, err = repository.ArchiveCreditCard(ctx, application.IdempotentArchiveCreditCardCommand{
		Operation: application.IdempotencyOperationArchiveCreditCard, IdempotencyKey: "closed-pool-archive",
		Fingerprint: fingerprint, AuditEvent: application.CreditCardAuditEventArchived,
		UserID: owner, CreditCardID: card.ID(), CreditCard: archived,
	})
	assertSafeCategory(err, adapter.ErrBeginCreditCardTransaction)
}

func newCreditCardConcurrentFixture(t *testing.T) (*pgxpool.Pool, context.Context, string) {
	t.Helper()
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	owner := "usr_credit_card_concurrency_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	return pool, ctx, owner
}

func newCreditCardRepository(t testing.TB, pool *pgxpool.Pool) *adapter.CreditCardRepository {
	t.Helper()
	repository, err := adapter.NewCreditCardRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewCreditCardRepository() error = %v", err)
	}
	return repository
}

func newRecordCreditCardUseCase(
	t testing.TB,
	repository *adapter.CreditCardRepository,
	generator application.CreditCardIDGenerator,
	clock application.Clock,
) *application.RecordCreditCard {
	t.Helper()
	useCase, err := application.NewRecordCreditCard(repository, repository, generator, clock)
	if err != nil {
		t.Fatalf("NewRecordCreditCard() error = %v", err)
	}
	return useCase
}

func newArchiveCreditCardUseCase(
	t testing.TB,
	repository *adapter.CreditCardRepository,
	clock application.Clock,
) *application.ArchiveCreditCard {
	t.Helper()
	useCase, err := application.NewArchiveCreditCard(repository, repository, repository, clock)
	if err != nil {
		t.Fatalf("NewArchiveCreditCard() error = %v", err)
	}
	return useCase
}

type fixedCreditCardIDGenerator struct {
	id    string
	calls atomic.Int64
}

func (generator *fixedCreditCardIDGenerator) NewCreditCardID() (string, error) {
	generator.calls.Add(1)
	return generator.id, nil
}

type sequenceCreditCardIDGenerator struct{ calls atomic.Uint64 }

func (generator *sequenceCreditCardIDGenerator) NewCreditCardID() (string, error) {
	value := generator.calls.Add(1)
	return fmt.Sprintf("card_%032x", value), nil
}

type fixedCreditCardClock struct{ now time.Time }

func (clock fixedCreditCardClock) Now() time.Time { return clock.now }

func fullCreditCardInput(owner, key string) application.RecordCreditCardInput {
	lastFour := "4242"
	brand := domain.CreditCardBrandVisa
	return application.RecordCreditCardInput{
		CreditCard: application.CreditCardInput{
			UserID:           owner,
			Name:             "Cartão principal",
			LastFour:         &lastFour,
			Brand:            &brand,
			ClosingDayAnchor: 31,
			DueDayAnchor:     10,
			CreditLimit: &application.CreditCardLimitInput{
				MinorUnits: 250000,
				Currency:   domain.CurrencyBRL,
			},
		},
		IdempotencyKey: key,
	}
}

func creditCardCreatedAt() time.Time {
	return time.Date(2026, time.August, 25, 10, 0, 0, 123_456_000, time.UTC)
}

func creditCardArchivedAt() time.Time { return creditCardCreatedAt().Add(6 * time.Hour) }

func creditCardTestID(character byte) string {
	return "card_" + strings.Repeat(string(character), 32)
}

type directCreateResult struct {
	result application.IdempotentCreditCardResult
	err    error
}

func newDirectActiveCreditCard(
	t testing.TB,
	owner, id string,
	createdAt time.Time,
) domain.CreditCard {
	t.Helper()
	name, err := domain.NewCreditCardName("Cartão concorrente")
	if err != nil {
		t.Fatalf("building direct credit card name failed: %v", err)
	}
	lastFour, err := domain.NewCreditCardLastFour("4242")
	if err != nil {
		t.Fatalf("building direct credit card last four failed: %v", err)
	}
	brand := domain.CreditCardBrandVisa
	closingDay, err := domain.NewDayOfMonthAnchor(31)
	if err != nil {
		t.Fatalf("building direct credit card closing day failed: %v", err)
	}
	dueDay, err := domain.NewDayOfMonthAnchor(10)
	if err != nil {
		t.Fatalf("building direct credit card due day failed: %v", err)
	}
	limit, err := domain.NewCreditCardCreditLimit(250000, domain.CurrencyBRL)
	if err != nil {
		t.Fatalf("building direct credit card limit failed: %v", err)
	}
	card, err := domain.NewCreditCard(domain.CreditCardParams{
		ID: id,
		Details: domain.CreditCardDetails{
			UserID: owner, Name: name, LastFour: &lastFour, Brand: &brand,
			ClosingDayAnchor: closingDay, DueDayAnchor: dueDay, CreditLimit: &limit,
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("building direct credit card failed: %v", err)
	}
	return card
}

func directCreateCreditCardCommand(
	t testing.TB,
	card domain.CreditCard,
	key string,
	fingerprintByte byte,
) application.IdempotentCreditCardCommand {
	t.Helper()
	var fingerprint application.RequestFingerprint
	for index := range fingerprint {
		fingerprint[index] = fingerprintByte
	}
	return application.IdempotentCreditCardCommand{
		Operation: application.IdempotencyOperationCreateCreditCard, IdempotencyKey: key,
		Fingerprint: fingerprint, AuditEvent: application.CreditCardAuditEventCreated, CreditCard: card,
	}
}

func newSingleConnectionCreditCardPool(
	t testing.TB,
	ctx context.Context,
	source *pgxpool.Pool,
) (*pgxpool.Pool, int32) {
	t.Helper()
	config := source.Config().Copy()
	config.MaxConns = 1
	config.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("opening single-connection credit card pool failed: %v", err)
	}
	t.Cleanup(pool.Close)
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquiring single credit card connection failed: %v", err)
	}
	pid := connectionBackendPIDForTB(t, ctx, connection)
	connection.Release()
	return pool, pid
}

func connectionBackendPIDForTB(t testing.TB, ctx context.Context, connection *pgxpool.Conn) int32 {
	t.Helper()
	var pid int32
	if err := connection.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("PostgreSQL backend PID lookup failed: %v", err)
	}
	return pid
}

func assertCreditCardTransactionIsolation(t testing.TB, ctx context.Context, tx pgx.Tx, want string) {
	t.Helper()
	var actual string
	if err := tx.QueryRow(ctx, "SHOW transaction_isolation").Scan(&actual); err != nil {
		t.Fatalf("reading credit card transaction isolation failed: %v", err)
	}
	if actual != want {
		t.Fatalf("credit card transaction isolation=%q, want %q", actual, want)
	}
}

func insertDirectPendingCreditCardReservation(
	t testing.TB,
	ctx context.Context,
	tx pgx.Tx,
	command application.IdempotentCreditCardCommand,
) {
	t.Helper()
	if _, err := tx.Exec(ctx, `
		INSERT INTO credit_card_idempotency_records (
			user_id, operation, idempotency_key, request_fingerprint, state, created_at
		) VALUES ($1, $2, $3, $4, 'PENDING', $5)
	`, command.CreditCard.UserID(), command.Operation, command.IdempotencyKey,
		command.Fingerprint[:], command.CreditCard.CreatedAt()); err != nil {
		t.Fatalf("inserting direct credit card reservation failed: %v", err)
	}
}

func insertDirectCreditCardAndAudit(
	t testing.TB,
	ctx context.Context,
	tx pgx.Tx,
	command application.IdempotentCreditCardCommand,
) {
	t.Helper()
	card := command.CreditCard
	lastFour, _ := card.LastFour()
	brand, _ := card.Brand()
	limit, _ := card.CreditLimit()
	if err := insertRawCreditCard(ctx, tx, rawCreditCard{
		id: card.ID(), userID: card.UserID(), name: card.Name().String(),
		lastFour: stringPointer(lastFour.String()), brand: stringPointer(string(brand)),
		closingDay: card.ClosingDayAnchor().Day(), dueDay: card.DueDayAnchor().Day(),
		limitMinor: int64Pointer(limit.MinorUnits()), limitCurrency: stringPointer(string(limit.Currency())),
		status: string(card.Status()), createdAt: card.CreatedAt(),
	}); err != nil {
		t.Fatalf("inserting direct credit card aggregate failed: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO credit_card_audit_events (user_id, credit_card_id, event_type, created_at)
		VALUES ($1, $2, 'CREDIT_CARD_CREATED', $3)
	`, card.UserID(), card.ID(), card.CreatedAt()); err != nil {
		t.Fatalf("inserting direct credit card audit failed: %v", err)
	}
}

func completeDirectCreditCardCreate(
	t testing.TB,
	ctx context.Context,
	tx pgx.Tx,
	command application.IdempotentCreditCardCommand,
) {
	t.Helper()
	insertDirectCreditCardAndAudit(t, ctx, tx, command)
	card := command.CreditCard
	lastFour, _ := card.LastFour()
	brand, _ := card.Brand()
	limit, _ := card.CreditLimit()
	tag, err := tx.Exec(ctx, `
		UPDATE credit_card_idempotency_records
		SET state = 'COMPLETED', credit_card_id = $4, result_name = $5,
			result_last_four = $6, result_brand = $7, result_closing_day = $8,
			result_due_day = $9, result_credit_limit_minor = $10,
			result_credit_limit_currency = $11, result_status = 'ACTIVE',
			result_created_at = $12, result_archived_at = NULL, completed_at = $12
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3 AND state = 'PENDING'
	`, card.UserID(), command.Operation, command.IdempotencyKey, card.ID(), card.Name().String(),
		lastFour.String(), string(brand), card.ClosingDayAnchor().Day(), card.DueDayAnchor().Day(),
		limit.MinorUnits(), string(limit.Currency()), card.CreatedAt())
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("completing direct credit card reservation rows=%d error=%v", tag.RowsAffected(), err)
	}
}

func assertDirectPendingReservationVisibility(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	tx pgx.Tx,
	command application.IdempotentCreditCardCommand,
	wantInside, wantOutside int,
) {
	t.Helper()
	query := `
		SELECT count(*) FROM credit_card_idempotency_records
		WHERE user_id = $1 AND operation = $2 AND idempotency_key = $3 AND state = 'PENDING'
	`
	var inside, outside int
	if err := tx.QueryRow(ctx, query, command.CreditCard.UserID(), command.Operation, command.IdempotencyKey).Scan(&inside); err != nil {
		t.Fatalf("reading reservation inside winner transaction failed: %v", err)
	}
	if err := pool.QueryRow(ctx, query, command.CreditCard.UserID(), command.Operation, command.IdempotencyKey).Scan(&outside); err != nil {
		t.Fatalf("reading reservation outside winner transaction failed: %v", err)
	}
	if inside != wantInside || outside != wantOutside {
		t.Fatalf("pending reservation visibility=%d/%d, want %d/%d", inside, outside, wantInside, wantOutside)
	}
}

func assertDirectCreateStillBlocked(t testing.TB, result <-chan directCreateResult) {
	t.Helper()
	select {
	case completed := <-result:
		t.Fatalf("credit card contender completed while reservation was uncommitted: %+v error=%v", completed.result, completed.err)
	default:
	}
}

func waitForDirectCreateResult(
	t testing.TB,
	ctx context.Context,
	result <-chan directCreateResult,
) directCreateResult {
	t.Helper()
	select {
	case completed := <-result:
		return completed
	case <-ctx.Done():
		t.Fatalf("timed out waiting for direct credit card create: %v", ctx.Err())
		return directCreateResult{}
	}
}

func createCreditCardFixture(
	t testing.TB,
	ctx context.Context,
	repository *adapter.CreditCardRepository,
	owner, id, key string,
) domain.CreditCard {
	t.Helper()
	input := fullCreditCardInput(owner, key)
	input.CreditCard.Name = "Cartão " + id[len(id)-1:]
	result, err := newRecordCreditCardUseCase(
		t,
		repository,
		&fixedCreditCardIDGenerator{id: id},
		fixedCreditCardClock{now: creditCardCreatedAt()},
	).Execute(ctx, input)
	if err != nil || result.Replayed {
		t.Fatalf("creating card fixture result=%+v error=%v", result, err)
	}
	return result.CreditCard
}

func runConcurrentCreditCardCreates(
	ctx context.Context,
	useCase *application.RecordCreditCard,
	inputs []application.RecordCreditCardInput,
) ([]application.RecordCreditCardResult, []error) {
	results := make([]application.RecordCreditCardResult, len(inputs))
	errs := make([]error, len(inputs))
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(len(inputs))
	for index := range inputs {
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errs[index] = useCase.Execute(ctx, inputs[index])
		}(index)
	}
	close(start)
	wait.Wait()
	return results, errs
}

func runConcurrentCreditCardArchiveStores(
	ctx context.Context,
	repository *adapter.CreditCardRepository,
	commands []application.IdempotentArchiveCreditCardCommand,
) ([]application.IdempotentArchiveCreditCardResult, []error) {
	results := make([]application.IdempotentArchiveCreditCardResult, len(commands))
	errs := make([]error, len(commands))
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(len(commands))
	for index := range commands {
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errs[index] = repository.ArchiveCreditCard(ctx, commands[index])
		}(index)
	}
	close(start)
	wait.Wait()
	return results, errs
}

func directArchiveCreditCardCommand(
	t testing.TB,
	card domain.CreditCard,
	key string,
	fingerprintByte byte,
) application.IdempotentArchiveCreditCardCommand {
	t.Helper()
	archived, err := card.Archive(creditCardArchivedAt())
	if err != nil {
		t.Fatalf("building direct archive command failed: %v", err)
	}
	var fingerprint application.RequestFingerprint
	for index := range fingerprint {
		fingerprint[index] = fingerprintByte
	}
	return application.IdempotentArchiveCreditCardCommand{
		Operation:      application.IdempotencyOperationArchiveCreditCard,
		IdempotencyKey: key,
		Fingerprint:    fingerprint,
		AuditEvent:     application.CreditCardAuditEventArchived,
		UserID:         card.UserID(),
		CreditCardID:   card.ID(),
		CreditCard:     archived,
	}
}

func assertCreditCardMatchesInput(t testing.TB, card domain.CreditCard, input application.CreditCardInput) {
	t.Helper()
	if card.UserID() != input.UserID || card.Name().String() != input.Name ||
		card.ClosingDayAnchor().Day() != input.ClosingDayAnchor ||
		card.DueDayAnchor().Day() != input.DueDayAnchor ||
		card.Status() != domain.CreditCardStatusActive {
		t.Fatal("stored card does not match normalized input")
	}
	lastFour, hasLastFour := card.LastFour()
	brand, hasBrand := card.Brand()
	limit, hasLimit := card.CreditLimit()
	if !hasLastFour || input.LastFour == nil || lastFour.String() != *input.LastFour ||
		!hasBrand || input.Brand == nil || brand != *input.Brand ||
		!hasLimit || input.CreditLimit == nil || limit.MinorUnits() != input.CreditLimit.MinorUnits ||
		limit.Currency() != input.CreditLimit.Currency {
		t.Fatal("stored optional card fields do not match input")
	}
}

func creditCardsMatch(left, right domain.CreditCard) bool {
	if left.ID() != right.ID() || left.UserID() != right.UserID() ||
		left.Name() != right.Name() || left.ClosingDayAnchor() != right.ClosingDayAnchor() ||
		left.DueDayAnchor() != right.DueDayAnchor() || left.Status() != right.Status() ||
		!left.CreatedAt().Equal(right.CreatedAt()) {
		return false
	}
	leftLast, leftHasLast := left.LastFour()
	rightLast, rightHasLast := right.LastFour()
	leftBrand, leftHasBrand := left.Brand()
	rightBrand, rightHasBrand := right.Brand()
	leftLimit, leftHasLimit := left.CreditLimit()
	rightLimit, rightHasLimit := right.CreditLimit()
	leftArchive, leftHasArchive := left.ArchivedAt()
	rightArchive, rightHasArchive := right.ArchivedAt()
	return leftHasLast == rightHasLast && (!leftHasLast || leftLast == rightLast) &&
		leftHasBrand == rightHasBrand && (!leftHasBrand || leftBrand == rightBrand) &&
		leftHasLimit == rightHasLimit && (!leftHasLimit || leftLimit == rightLimit) &&
		leftHasArchive == rightHasArchive && (!leftHasArchive || leftArchive.Equal(rightArchive))
}

func assertCreditCardSubsystemCounts(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	cards, audits, idempotency int,
) {
	t.Helper()
	var actualCards, actualAudits, actualIdempotency int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM credit_cards").Scan(&actualCards); err != nil {
		t.Fatal("counting credit cards failed")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM credit_card_audit_events").Scan(&actualAudits); err != nil {
		t.Fatal("counting credit card audits failed")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM credit_card_idempotency_records").Scan(&actualIdempotency); err != nil {
		t.Fatal("counting credit card idempotency failed")
	}
	if actualCards != cards || actualAudits != audits || actualIdempotency != idempotency {
		t.Fatalf(
			"credit card counts = %d/%d/%d, want %d/%d/%d",
			actualCards,
			actualAudits,
			actualIdempotency,
			cards,
			audits,
			idempotency,
		)
	}
}

func assertLegacyFinancialTablesEmpty(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{
		"transactions",
		"audit_events",
		"idempotency_records",
		"recurrences",
		"recurrence_audit_events",
		"recurrence_idempotency_records",
		"recurrence_suggestion_suppressions",
	} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("legacy table %s count = %d error = %v, want zero", table, count, err)
		}
	}
	var categoryCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM categories").Scan(&categoryCount); err != nil || categoryCount != 17 {
		t.Fatalf("category count = %d error = %v, want unchanged 17", categoryCount, err)
	}
}

func assertCreditCardIdempotencyKeyCount(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner, key string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM credit_card_idempotency_records
		WHERE user_id = $1 AND idempotency_key = $2
	`, owner, key).Scan(&count); err != nil || count != want {
		t.Fatalf("idempotency key count = %d error=%v, want %d", count, err, want)
	}
}

func assertCompletedCreditCardIdempotencyRows(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner, key string,
	want int,
) {
	t.Helper()
	var total, completed, pending int
	if err := pool.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE state = 'COMPLETED'),
		       count(*) FILTER (WHERE state = 'PENDING')
		FROM credit_card_idempotency_records
		WHERE user_id = $1 AND idempotency_key = $2
	`, owner, key).Scan(&total, &completed, &pending); err != nil ||
		total != want || completed != want || pending != 0 {
		t.Fatalf(
			"idempotency rows total/completed/pending=%d/%d/%d error=%v, want %d/%d/0",
			total,
			completed,
			pending,
			err,
			want,
			want,
		)
	}
}

func assertCreditCardIDCount(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	id string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM credit_cards WHERE id = $1", id).Scan(&count); err != nil || count != want {
		t.Fatalf("credit card %q count=%d error=%v, want %d", id, count, err, want)
	}
}

func creditCardIdempotencySignatures(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner string,
) string {
	t.Helper()
	var signature string
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(jsonb_agg(to_jsonb(snapshot) ORDER BY operation, idempotency_key)::text, '[]')
		FROM (
			SELECT operation, idempotency_key, encode(request_fingerprint, 'hex') AS request_fingerprint,
			       state, credit_card_id, result_name, result_last_four, result_brand,
			       result_closing_day, result_due_day, result_credit_limit_minor,
			       result_credit_limit_currency, result_status, result_created_at,
			       result_archived_at, created_at, completed_at
			FROM credit_card_idempotency_records
			WHERE user_id = $1
		) AS snapshot
	`, owner).Scan(&signature); err != nil {
		t.Fatalf("reading credit card idempotency signatures failed: %v", err)
	}
	return signature
}

func installCreditCardAuditFailureTrigger(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_credit_card_audit() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION 'synthetic audit failure';
		END
		$$;
		CREATE TRIGGER fail_credit_card_audit
		BEFORE INSERT ON credit_card_audit_events
		FOR EACH ROW EXECUTE FUNCTION fail_credit_card_audit();
	`); err != nil {
		t.Fatal("installing synthetic audit failure failed")
	}
}

func dropCreditCardAuditFailureTrigger(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		DROP TRIGGER fail_credit_card_audit ON credit_card_audit_events;
		DROP FUNCTION fail_credit_card_audit();
	`); err != nil {
		t.Fatal("removing synthetic audit failure failed")
	}
}

func installCreditCardCompletionFailureTrigger(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION fail_credit_card_completion() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF OLD.state = 'PENDING' AND NEW.state = 'COMPLETED' THEN
				RAISE EXCEPTION 'synthetic completion failure';
			END IF;
			RETURN NEW;
		END
		$$;
		CREATE TRIGGER fail_credit_card_completion
		BEFORE UPDATE ON credit_card_idempotency_records
		FOR EACH ROW EXECUTE FUNCTION fail_credit_card_completion();
	`); err != nil {
		t.Fatal("installing synthetic completion failure failed")
	}
}

func dropCreditCardCompletionFailureTrigger(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		DROP TRIGGER fail_credit_card_completion ON credit_card_idempotency_records;
		DROP FUNCTION fail_credit_card_completion();
	`); err != nil {
		t.Fatal("removing synthetic completion failure failed")
	}
}

func errorContainsSensitiveDatabaseDetail(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	for _, forbidden := range []string{"credit_card_idempotency_records", "result_last_four", "SELECT", "SQLSTATE"} {
		if strings.Contains(message, forbidden) {
			return true
		}
	}
	return false
}
