//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestCardPurchasePostgresOneTimeInstallmentReplayAndCancellation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const owner = "usr_card_purchase_owner"
	insertSyntheticUser(t, ctx, pool, owner)

	cardRepository := newCreditCardRepository(t, pool)
	card, err := newRecordCreditCardUseCase(
		t,
		cardRepository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('a')},
		fixedCreditCardClock{now: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)},
	).Execute(ctx, fullCreditCardInput(owner, "card-purchase-card-key"))
	if err != nil {
		t.Fatalf("creating card: %v", err)
	}

	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("creating card purchase repository: %v", err)
	}
	occurrence := time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC)
	newPurchase := func(key string, count *int, expenseID, planID string) *application.RecordCardPurchase {
		t.Helper()
		useCase, constructorErr := application.NewRecordCardPurchase(
			purchaseRepository,
			purchaseRepository,
			cardRepository,
			nil,
			fixedCardPurchaseExpenseIDGenerator{id: expenseID},
			&fixedCardPurchasePlanIDGenerator{id: planID},
			fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 123_456_000, time.UTC)},
		)
		if constructorErr != nil {
			t.Fatalf("creating card purchase use case: %v", constructorErr)
		}
		return useCase
	}
	input := application.RecordCardPurchaseInput{Purchase: application.CardPurchaseInput{
		UserID: owner, Description: "Compra à vista", AmountMinor: 10000, Currency: domain.CurrencyBRL,
		OccurredAt: occurrence, CreditCardID: card.CreditCard.ID(), Origin: domain.OriginIOS,
	}, IdempotencyKey: "one-time"}
	oneTime, err := newPurchase("one-time", nil, "exp_00000000000000000000000000000001", "ipl_00000000000000000000000000000001").Execute(ctx, input)
	if err != nil || oneTime.Replayed || oneTime.InstallmentPlan != nil {
		t.Fatalf("one-time result=%+v error=%v", oneTime, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
		InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 1,
		InstallmentPlanIdempotency: 0,
	})
	replayed, err := newPurchase("one-time", nil, "exp_00000000000000000000000000000002", "ipl_00000000000000000000000000000002").Execute(ctx, input)
	if err != nil || !replayed.Replayed || replayed.Expense.ID() != oneTime.Expense.ID() {
		t.Fatalf("one-time replay result=%+v error=%v", replayed, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
		InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 1,
		InstallmentPlanIdempotency: 0,
	})

	count := 3
	installmentInput := application.RecordCardPurchaseInput{Purchase: application.CardPurchaseInput{
		UserID: owner, Description: "Compra parcelada", AmountMinor: 10100, Currency: domain.CurrencyBRL,
		OccurredAt: occurrence, CreditCardID: card.CreditCard.ID(), InstallmentCount: &count, Origin: domain.OriginIOS,
	}, IdempotencyKey: "installment"}
	installment, err := newPurchase("installment", &count, "exp_00000000000000000000000000000003", "ipl_00000000000000000000000000000003").Execute(ctx, installmentInput)
	if err != nil || installment.Replayed || installment.InstallmentPlan == nil {
		t.Fatalf("installment result=%+v error=%v", installment, err)
	}
	schedule, err := installment.InstallmentPlan.Schedule()
	if err != nil || len(schedule) != 3 || schedule[0].Amount().MinorUnits() != 3366 || schedule[1].Amount().MinorUnits() != 3366 || schedule[2].Amount().MinorUnits() != 3368 {
		t.Fatalf("installment schedule=%v error=%v", schedule, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 1, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 0,
	})

	replayedInstallment, err := newPurchase("installment", &count, "exp_00000000000000000000000000000004", "ipl_00000000000000000000000000000004").Execute(ctx, installmentInput)
	if err != nil || !replayedInstallment.Replayed || replayedInstallment.Expense.ID() != installment.Expense.ID() || replayedInstallment.InstallmentPlan == nil || replayedInstallment.InstallmentPlan.ID() != installment.InstallmentPlan.ID() {
		t.Fatalf("installment replay result=%+v error=%v", replayedInstallment, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 1, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 0,
	})

	cancellationDate, err := domain.NewCivilDate(2026, 8, 30)
	if err != nil {
		t.Fatal(err)
	}
	dateProvider := fixedCardPurchaseFinancialDate{date: cancellationDate}
	cancelPreview, err := application.NewPreviewInstallmentPlanCancellation(purchaseRepository, dateProvider)
	if err != nil {
		t.Fatalf("creating cancellation preview: %v", err)
	}
	preview, err := cancelPreview.Execute(ctx, application.PreviewInstallmentPlanCancellationInput{OwnerID: owner, PlanID: installment.InstallmentPlan.ID()})
	if err != nil || !preview.CancelledOn.Equal(dateProvider.date) {
		t.Fatalf("cancellation preview=%+v error=%v", preview, err)
	}
	cancelUseCase, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, dateProvider)
	if err != nil {
		t.Fatalf("creating cancellation use case: %v", err)
	}
	cancelled, err := cancelUseCase.Execute(ctx, application.CancelInstallmentPlanInput{
		OwnerID: owner, PlanID: installment.InstallmentPlan.ID(), ExpectedCancelledOn: preview.CancelledOn, IdempotencyKey: "cancel-installment",
	})
	if err != nil || cancelled.Replayed || cancelled.InstallmentPlan.Status() != domain.InstallmentPlanStatusCancelled {
		t.Fatalf("cancellation result=%+v error=%v", cancelled, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 1,
	})
	replayedCancel, err := cancelUseCase.Execute(ctx, application.CancelInstallmentPlanInput{
		OwnerID: owner, PlanID: installment.InstallmentPlan.ID(), ExpectedCancelledOn: preview.CancelledOn, IdempotencyKey: "cancel-installment",
	})
	if err != nil || !replayedCancel.Replayed || replayedCancel.InstallmentPlan.Status() != domain.InstallmentPlanStatusCancelled {
		t.Fatalf("cancellation replay=%+v error=%v", replayedCancel, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 1,
	})
	archivedAt := time.Date(2026, time.August, 31, 15, 0, 0, 0, time.UTC)
	if _, err := newArchiveCreditCardUseCase(t, cardRepository, fixedCreditCardClock{now: archivedAt}).Execute(ctx, application.ArchiveCreditCardInput{
		UserID: owner, CreditCardID: card.CreditCard.ID(), IdempotencyKey: "archive-card-after-purchase",
	}); err != nil {
		t.Fatalf("archiving card after purchase failed: %v", err)
	}
	historicalReplay, err := newPurchase("installment", &count, "exp_00000000000000000000000000000005", "ipl_00000000000000000000000000000005").Execute(ctx, installmentInput)
	if err != nil || !historicalReplay.Replayed || historicalReplay.Expense.ID() != installment.Expense.ID() || historicalReplay.InstallmentPlan == nil || historicalReplay.InstallmentPlan.Status() != domain.InstallmentPlanStatusActive {
		t.Fatalf("historical installment replay after card archive/plan cancellation=%+v error=%v", historicalReplay, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 2, CreditCardIdempotency: 2,
		Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 2,
		InstallmentPlanIdempotency: 1,
	})
	var expenseCreditCard, planStatus string
	if err := pool.QueryRow(ctx, "SELECT credit_card_id FROM transactions WHERE id=$1", installment.Expense.ID()).Scan(&expenseCreditCard); err != nil || expenseCreditCard != card.CreditCard.ID() {
		t.Fatalf("stored card link=%q error=%v", expenseCreditCard, err)
	}
	if err := pool.QueryRow(ctx, "SELECT status FROM installment_plans WHERE id=$1", installment.InstallmentPlan.ID()).Scan(&planStatus); err != nil || planStatus != "CANCELLED" {
		t.Fatalf("stored plan status=%q error=%v", planStatus, err)
	}
}

func TestCardPurchasePostgresCreateConcurrencyHasOneFirstWriteAndReplays(t *testing.T) {
	pool, ctx, owner := newCreditCardConcurrentFixture(t)
	cardRepository := newCreditCardRepository(t, pool)
	card, err := newRecordCreditCardUseCase(
		t,
		cardRepository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('c')},
		fixedCreditCardClock{now: creditCardCreatedAt()},
	).Execute(ctx, fullCreditCardInput(owner, "card-purchase-concurrency-card"))
	if err != nil {
		t.Fatalf("creating card fixture: %v", err)
	}
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	count := 3
	useCase, err := application.NewRecordCardPurchase(
		purchaseRepository,
		purchaseRepository,
		cardRepository,
		nil,
		fixedCardPurchaseExpenseIDGenerator{id: "exp_00000000000000000000000000000006"},
		&fixedCardPurchasePlanIDGenerator{id: "ipl_00000000000000000000000000000006"},
		fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := application.RecordCardPurchaseInput{Purchase: application.CardPurchaseInput{
		UserID: owner, Description: "Compra concorrente", AmountMinor: 10100, Currency: domain.CurrencyBRL,
		OccurredAt: time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC), CreditCardID: card.CreditCard.ID(),
		InstallmentCount: &count, Origin: domain.OriginIOS,
	}, IdempotencyKey: "card-purchase-concurrent-key"}
	const callers = 8
	results := make([]application.RecordCardPurchaseResult, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(callers)
	for index := range results {
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errs[index] = useCase.Execute(ctx, input)
		}(index)
	}
	close(start)
	wait.Wait()
	first, replay := 0, 0
	for index := range results {
		if errs[index] != nil {
			t.Fatalf("concurrent create %d error=%v", index, errs[index])
		}
		if results[index].Replayed {
			replay++
		} else {
			first++
		}
		if results[index].Expense.ID() != "exp_00000000000000000000000000000006" || results[index].InstallmentPlan == nil || results[index].InstallmentPlan.ID() != "ipl_00000000000000000000000000000006" {
			t.Fatalf("concurrent create %d returned a noncanonical snapshot=%+v", index, results[index])
		}
	}
	if first != 1 || replay != callers-1 {
		t.Fatalf("first/replay=%d/%d, want 1/%d", first, replay, callers-1)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 1, CardPurchaseIdempotency: 1,
		InstallmentPlanIdempotency: 0,
	})
}

func TestCardPurchasePostgresCancellationConcurrencyHasOneTransition(t *testing.T) {
	pool, ctx, owner := newCreditCardConcurrentFixture(t)
	cardRepository := newCreditCardRepository(t, pool)
	card, err := newRecordCreditCardUseCase(
		t,
		cardRepository,
		&fixedCreditCardIDGenerator{id: creditCardTestID('d')},
		fixedCreditCardClock{now: creditCardCreatedAt()},
	).Execute(ctx, fullCreditCardInput(owner, "card-purchase-cancel-concurrency-card"))
	if err != nil {
		t.Fatalf("creating card fixture: %v", err)
	}
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	count := 2
	createUseCase, err := application.NewRecordCardPurchase(
		purchaseRepository, purchaseRepository, cardRepository, nil,
		fixedCardPurchaseExpenseIDGenerator{id: "exp_00000000000000000000000000000007"},
		&fixedCardPurchasePlanIDGenerator{id: "ipl_00000000000000000000000000000007"},
		fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	created, err := createUseCase.Execute(ctx, application.RecordCardPurchaseInput{Purchase: application.CardPurchaseInput{
		UserID: owner, Description: "Plano cancelável", AmountMinor: 5000, Currency: domain.CurrencyBRL,
		OccurredAt: time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC), CreditCardID: card.CreditCard.ID(),
		InstallmentCount: &count, Origin: domain.OriginIOS,
	}, IdempotencyKey: "card-purchase-cancel-source"})
	if err != nil || created.InstallmentPlan == nil {
		t.Fatalf("creating cancellation source=%+v error=%v", created, err)
	}
	dateProvider := fixedCardPurchaseFinancialDate{date: mustCivilDate(t, 2026, 8, 30)}
	captureStore := &captureInstallmentCancellationStore{}
	cancelUseCase, err := application.NewCancelInstallmentPlan(captureStore, emptyInstallmentCancellationReplay{}, purchaseRepository, dateProvider)
	if err != nil {
		t.Fatal(err)
	}
	input := application.CancelInstallmentPlanInput{OwnerID: owner, PlanID: created.InstallmentPlan.ID(), ExpectedCancelledOn: dateProvider.date, IdempotencyKey: "card-purchase-cancel-concurrent-key"}
	if _, err := cancelUseCase.Execute(ctx, input); err != nil {
		t.Fatalf("building cancellation command failed: %v", err)
	}
	command := captureStore.command
	const callers = 8
	results := make([]application.IdempotentInstallmentPlanCancellationResult, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(callers)
	for index := range results {
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errs[index] = purchaseRepository.CancelInstallmentPlan(ctx, command)
		}(index)
	}
	close(start)
	wait.Wait()
	first, replay := 0, 0
	for index := range results {
		if errs[index] != nil {
			t.Fatalf("concurrent cancellation %d error=%v", index, errs[index])
		}
		if results[index].Replayed {
			replay++
		} else {
			first++
		}
		if results[index].InstallmentPlan.Status() != domain.InstallmentPlanStatusCancelled {
			t.Fatalf("concurrent cancellation %d status=%s", index, results[index].InstallmentPlan.Status())
		}
	}
	if first != 1 || replay != callers-1 {
		t.Fatalf("cancellation first/replay=%d/%d, want 1/%d", first, replay, callers-1)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
		InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 1,
		InstallmentPlanIdempotency: 1,
	})
}

type captureInstallmentCancellationStore struct {
	command application.IdempotentInstallmentPlanCancellationCommand
}

func (store *captureInstallmentCancellationStore) CancelInstallmentPlan(_ context.Context, command application.IdempotentInstallmentPlanCancellationCommand) (application.IdempotentInstallmentPlanCancellationResult, error) {
	store.command = command
	return application.IdempotentInstallmentPlanCancellationResult{InstallmentPlan: command.InstallmentPlan}, nil
}

type emptyInstallmentCancellationReplay struct{}

func (emptyInstallmentCancellationReplay) FindInstallmentPlanCancellationReplay(context.Context, application.InstallmentPlanCancellationReplayQuery) (application.InstallmentPlanCancellationReplayLookup, error) {
	return application.InstallmentPlanCancellationReplayLookup{}, nil
}

func TestCardPurchasePostgresOwnerIsolationAndInvalidCount(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	const ownerA = "usr_card_purchase_owner_a"
	const ownerB = "usr_card_purchase_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)
	cardRepository := newCreditCardRepository(t, pool)
	created, err := newRecordCreditCardUseCase(t, cardRepository, &fixedCreditCardIDGenerator{id: creditCardTestID('b')}, fixedCreditCardClock{now: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)}).Execute(ctx, fullCreditCardInput(ownerA, "owner-isolation-card"))
	if err != nil {
		t.Fatalf("creating owner A card: %v", err)
	}
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	count := 1
	useCase, err := application.NewRecordCardPurchase(purchaseRepository, purchaseRepository, cardRepository, nil, fixedCardPurchaseExpenseIDGenerator{id: "exp_00000000000000000000000000000004"}, &fixedCardPurchasePlanIDGenerator{id: "ipl_00000000000000000000000000000004"}, fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = useCase.Execute(ctx, application.RecordCardPurchaseInput{Purchase: application.CardPurchaseInput{UserID: ownerB, Description: "não deve gravar", AmountMinor: 100, Currency: domain.CurrencyBRL, OccurredAt: time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC), CreditCardID: created.CreditCard.ID(), InstallmentCount: &count, Origin: domain.OriginIOS}, IdempotencyKey: "owner-b-key"})
	if !errors.Is(err, domain.ErrInvalidInstallmentCount) {
		t.Fatalf("invalid count error=%v, want domain validation before lookup", err)
	}
	_, err = useCase.Execute(ctx, application.RecordCardPurchaseInput{Purchase: application.CardPurchaseInput{UserID: ownerB, Description: "não deve gravar", AmountMinor: 100, Currency: domain.CurrencyBRL, OccurredAt: time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC), CreditCardID: created.CreditCard.ID(), Origin: domain.OriginIOS}, IdempotencyKey: "owner-b-valid-key"})
	if !errors.Is(err, application.ErrCardPurchaseCreditCardNotFound) {
		t.Fatalf("cross-owner card error=%v, want not found", err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
		Transactions: 0, TransactionAudits: 0, LegacyIdempotency: 0,
		InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 0,
		InstallmentPlanIdempotency: 0,
	})
}

func TestCardPurchasePostgresSnapshotOwnerBindingAndCorruption(t *testing.T) {
	t.Run("one-time replay rejects externally rebound snapshot owner", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		const otherOwner = "usr_card_purchase_snapshot_owner_b"
		insertSyntheticUser(t, ctx, pool, otherOwner)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('e'), "snapshot-one-time-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		useCase, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "snapshot-one-time-key", nil, "exp_00000000000000000000000000000020", "ipl_00000000000000000000000000000020")
		if _, err := useCase.Execute(ctx, input); err != nil {
			t.Fatalf("creating snapshot fixture: %v", err)
		}
		assertCardPurchaseSnapshotOwners(t, ctx, pool, owner, "snapshot-one-time-key", owner, nil)
		if _, err := pool.Exec(ctx, `ALTER TABLE card_purchase_idempotency_records DROP CONSTRAINT card_purchase_idem_snapshot_valid`); err != nil {
			t.Fatal("dropping disposable snapshot guard failed")
		}
		if _, err := pool.Exec(ctx, `UPDATE card_purchase_idempotency_records SET user_id=$1 WHERE user_id=$2 AND idempotency_key=$3`, otherOwner, owner, input.IdempotencyKey); err != nil {
			t.Fatal("re-binding disposable idempotency owner failed")
		}
		fingerprint := cardPurchaseStoredFingerprint(t, ctx, pool, otherOwner, input.IdempotencyKey)
		lookup, err := purchaseRepository.FindCardPurchaseReplay(ctx, application.CardPurchaseReplayQuery{UserID: otherOwner, Operation: application.CardPurchaseOperationCreate, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
		if lookup.Found || !errors.Is(err, adapter.ErrInvalidCardPurchaseIdempotencyState) {
			t.Fatalf("cross-owner one-time replay lookup=%+v error=%v, want fail-closed", lookup, err)
		}
	})

	t.Run("installment replay rejects mismatched plan owner", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		const otherOwner = "usr_card_purchase_snapshot_owner_c"
		insertSyntheticUser(t, ctx, pool, otherOwner)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('f'), "snapshot-installment-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		count := 3
		useCase, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "snapshot-installment-key", &count, "exp_00000000000000000000000000000021", "ipl_00000000000000000000000000000021")
		created, err := useCase.Execute(ctx, input)
		if err != nil || created.InstallmentPlan == nil {
			t.Fatalf("creating installment snapshot fixture=%+v error=%v", created, err)
		}
		assertCardPurchaseSnapshotOwners(t, ctx, pool, owner, input.IdempotencyKey, owner, &owner)
		if _, err := pool.Exec(ctx, `ALTER TABLE card_purchase_idempotency_records DROP CONSTRAINT card_purchase_idem_snapshot_valid`); err != nil {
			t.Fatal("dropping disposable installment snapshot guard failed")
		}
		if _, err := pool.Exec(ctx, `UPDATE card_purchase_idempotency_records SET user_id=$1, expense_user_id=$1 WHERE user_id=$2 AND idempotency_key=$3`, otherOwner, owner, input.IdempotencyKey); err != nil {
			t.Fatal("re-binding disposable installment expense owner failed")
		}
		fingerprint := cardPurchaseStoredFingerprint(t, ctx, pool, otherOwner, input.IdempotencyKey)
		lookup, err := purchaseRepository.FindCardPurchaseReplay(ctx, application.CardPurchaseReplayQuery{UserID: otherOwner, Operation: application.CardPurchaseOperationCreate, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
		if lookup.Found || !errors.Is(err, adapter.ErrInvalidCardPurchaseIdempotencyState) {
			t.Fatalf("cross-owner installment replay lookup=%+v error=%v, want fail-closed", lookup, err)
		}
	})

	t.Run("cancellation replay rejects mismatched plan owner", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		const otherOwner = "usr_card_purchase_snapshot_owner_d"
		insertSyntheticUser(t, ctx, pool, otherOwner)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('1'), "snapshot-cancel-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		count := 2
		create, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "snapshot-cancel-source", &count, "exp_00000000000000000000000000000022", "ipl_00000000000000000000000000000022")
		created, err := create.Execute(ctx, input)
		if err != nil || created.InstallmentPlan == nil {
			t.Fatalf("creating cancellation fixture=%+v error=%v", created, err)
		}
		date := mustCivilDate(t, 2026, 8, 30)
		cancelUseCase, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, fixedCardPurchaseFinancialDate{date: date})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cancelUseCase.Execute(ctx, application.CancelInstallmentPlanInput{OwnerID: owner, PlanID: created.InstallmentPlan.ID(), ExpectedCancelledOn: date, IdempotencyKey: "snapshot-cancel-key"}); err != nil {
			t.Fatalf("creating cancellation snapshot: %v", err)
		}
		assertCancellationSnapshotOwner(t, ctx, pool, owner, "snapshot-cancel-key", owner)
		if _, err := pool.Exec(ctx, `ALTER TABLE installment_plan_idempotency_records DROP CONSTRAINT installment_plan_idem_snapshot_valid`); err != nil {
			t.Fatal("dropping disposable cancellation snapshot guard failed")
		}
		if _, err := pool.Exec(ctx, `UPDATE installment_plan_idempotency_records SET user_id=$1 WHERE user_id=$2 AND idempotency_key=$3`, otherOwner, owner, "snapshot-cancel-key"); err != nil {
			t.Fatal("re-binding disposable cancellation owner failed")
		}
		fingerprint := installmentCancellationStoredFingerprint(t, ctx, pool, otherOwner, "snapshot-cancel-key")
		lookup, err := purchaseRepository.FindInstallmentPlanCancellationReplay(ctx, application.InstallmentPlanCancellationReplayQuery{OwnerID: otherOwner, Operation: application.CancelInstallmentPlanOperation, IdempotencyKey: "snapshot-cancel-key", Fingerprint: fingerprint})
		if lookup.Found || !errors.Is(err, adapter.ErrInvalidStoredInstallmentPlan) {
			t.Fatalf("cross-owner cancellation replay lookup=%+v error=%v, want fail-closed", lookup, err)
		}
	})
}

func TestCardPurchasePostgresArchivedCardRaceFailsWithoutWrites(t *testing.T) {
	pool, ctx, owner := newCreditCardConcurrentFixture(t)
	cardRepository := newCreditCardRepository(t, pool)
	card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('2'), "archive-race-card")
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	archiveCommand := directArchiveCreditCardCommand(t, card, "archive-race-key", 0x91)
	store := &archiveBeforeCardPurchaseStore{archiveRepository: cardRepository, archiveCommand: archiveCommand, delegate: purchaseRepository}
	useCase, input := newSnapshotCardPurchaseUseCase(t, store, cardRepository, owner, card.ID(), "archive-race-purchase-key", nil, "exp_00000000000000000000000000000023", "ipl_00000000000000000000000000000023")
	if _, err := useCase.Execute(ctx, input); !errors.Is(err, application.ErrCardPurchasePersistence) {
		t.Fatalf("archive race error=%v, want persistence category", err)
	}
	lookup, err := cardRepository.FindCreditCard(ctx, owner, card.ID())
	if err != nil || !lookup.Found || lookup.CreditCard.Status() != domain.CreditCardStatusArchived {
		t.Fatalf("archived card lookup=%+v error=%v", lookup, err)
	}
	assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
		CreditCards: 1, CreditCardAudits: 2, CreditCardIdempotency: 2,
		Transactions: 0, TransactionAudits: 0, LegacyIdempotency: 0,
		InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 0,
		InstallmentPlanIdempotency: 0,
	})
}

type archiveBeforeCardPurchaseStore struct {
	archiveRepository *adapter.CreditCardRepository
	archiveCommand    application.IdempotentArchiveCreditCardCommand
	delegate          application.CardPurchaseCommandStore
}

func newSnapshotCardPurchaseUseCase(
	t testing.TB,
	store application.CardPurchaseCommandStore,
	cardReader application.CreditCardLookupReader,
	owner, cardID, key string,
	count *int,
	expenseID, planID string,
) (*application.RecordCardPurchase, application.RecordCardPurchaseInput) {
	t.Helper()
	useCase, err := application.NewRecordCardPurchase(
		store, store.(application.CardPurchaseReplayReader), cardReader, nil,
		fixedCardPurchaseExpenseIDGenerator{id: expenseID},
		&fixedCardPurchasePlanIDGenerator{id: planID},
		fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 123_456_000, time.UTC)},
	)
	if err != nil {
		t.Fatalf("creating card purchase use case: %v", err)
	}
	return useCase, application.RecordCardPurchaseInput{Purchase: application.CardPurchaseInput{
		UserID: owner, Description: "Compra de teste", AmountMinor: 10100, Currency: domain.CurrencyBRL,
		OccurredAt: time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC), CreditCardID: cardID,
		InstallmentCount: count, Origin: domain.OriginIOS,
	}, IdempotencyKey: key}
}

func cardPurchaseStoredFingerprint(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner, key string) application.RequestFingerprint {
	t.Helper()
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT request_fingerprint FROM card_purchase_idempotency_records WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.CardPurchaseOperationCreate, key).Scan(&stored); err != nil {
		t.Fatalf("reading card purchase fingerprint: %v", err)
	}
	var fingerprint application.RequestFingerprint
	copy(fingerprint[:], stored)
	return fingerprint
}

func storedCardPurchaseExpenseFingerprint(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner, key string) application.RequestFingerprint {
	t.Helper()
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT request_fingerprint FROM idempotency_records WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.IdempotencyOperationCreateExpense, key).Scan(&stored); err != nil {
		t.Fatalf("reading expense replay fingerprint: %v", err)
	}
	var fingerprint application.RequestFingerprint
	copy(fingerprint[:], stored)
	return fingerprint
}

func installmentCancellationStoredFingerprint(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner, key string) application.RequestFingerprint {
	t.Helper()
	var stored []byte
	if err := pool.QueryRow(ctx, `SELECT request_fingerprint FROM installment_plan_idempotency_records WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.CancelInstallmentPlanOperation, key).Scan(&stored); err != nil {
		t.Fatalf("reading cancellation fingerprint: %v", err)
	}
	var fingerprint application.RequestFingerprint
	copy(fingerprint[:], stored)
	return fingerprint
}

func assertCardPurchaseSnapshotOwners(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner, key, expenseOwner string, planOwner *string) {
	t.Helper()
	var storedExpenseOwner string
	var storedPlanOwner *string
	if err := pool.QueryRow(ctx, `SELECT expense_user_id, plan_user_id FROM card_purchase_idempotency_records WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.CardPurchaseOperationCreate, key).Scan(&storedExpenseOwner, &storedPlanOwner); err != nil {
		t.Fatalf("reading card purchase snapshot owners: %v", err)
	}
	if storedExpenseOwner != expenseOwner || (planOwner == nil && storedPlanOwner != nil) || (planOwner != nil && (storedPlanOwner == nil || *storedPlanOwner != *planOwner)) {
		t.Fatalf("snapshot owners=%q/%v, want %q/%v", storedExpenseOwner, storedPlanOwner, expenseOwner, planOwner)
	}
}

func assertCancellationSnapshotOwner(t testing.TB, ctx context.Context, pool *pgxpool.Pool, owner, key, want string) {
	t.Helper()
	var stored string
	if err := pool.QueryRow(ctx, `SELECT plan_user_id FROM installment_plan_idempotency_records WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.CancelInstallmentPlanOperation, key).Scan(&stored); err != nil {
		t.Fatalf("reading cancellation snapshot owner: %v", err)
	}
	if stored != want {
		t.Fatalf("cancellation snapshot owner=%q, want %q", stored, want)
	}
}

func (store *archiveBeforeCardPurchaseStore) RecordCardPurchase(ctx context.Context, command application.CardPurchaseCommand) (application.CardPurchaseCommandResult, error) {
	if _, err := store.archiveRepository.ArchiveCreditCard(ctx, store.archiveCommand); err != nil {
		return application.CardPurchaseCommandResult{}, err
	}
	return store.delegate.RecordCardPurchase(ctx, command)
}

func (store *archiveBeforeCardPurchaseStore) FindCardPurchaseReplay(ctx context.Context, query application.CardPurchaseReplayQuery) (application.CardPurchaseReplayLookup, error) {
	return store.delegate.(application.CardPurchaseReplayReader).FindCardPurchaseReplay(ctx, query)
}

func TestCardPurchasePostgresRollbackAtEachStepAllowsSameKeyRetry(t *testing.T) {
	tests := []struct {
		name         string
		failurePoint string
		installment  bool
	}{
		{name: "expense insert", failurePoint: "expense_insert"},
		{name: "expense audit", failurePoint: "expense_audit"},
		{name: "plan insert", failurePoint: "plan_insert", installment: true},
		{name: "plan audit", failurePoint: "plan_audit", installment: true},
		{name: "card purchase completion", failurePoint: "completion", installment: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool, ctx, owner := newCreditCardConcurrentFixture(t)
			cardRepository := newCreditCardRepository(t, pool)
			card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('3'), "rollback-card-"+test.failurePoint)
			purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var count *int
			if test.installment {
				value := 3
				count = &value
			}
			useCase, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "rollback-"+test.failurePoint, count, "exp_00000000000000000000000000000030", "ipl_00000000000000000000000000000030")
			installCardPurchaseFailureTrigger(t, ctx, pool, test.failurePoint)
			t.Cleanup(func() { dropCardPurchaseFailureTrigger(t, ctx, pool, test.failurePoint) })
			if _, err := useCase.Execute(ctx, input); !errors.Is(err, application.ErrCardPurchasePersistence) {
				t.Fatalf("injected failure error=%v, want persistence category", err)
			}
			assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
				CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
				Transactions: 0, TransactionAudits: 0, LegacyIdempotency: 0,
				InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 0,
				InstallmentPlanIdempotency: 0,
			})
			dropCardPurchaseFailureTrigger(t, ctx, pool, test.failurePoint)
			retry, err := useCase.Execute(ctx, input)
			if err != nil || retry.Replayed {
				t.Fatalf("same-key retry=%+v error=%v, want first write", retry, err)
			}
			wantPlans, wantPlanAudits := 0, 0
			if test.installment {
				wantPlans, wantPlanAudits = 1, 1
			}
			assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
				CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
				Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
				InstallmentPlans: wantPlans, InstallmentPlanAudits: wantPlanAudits, CardPurchaseIdempotency: 1,
				InstallmentPlanIdempotency: 0,
			})
		})
	}
}

func installCardPurchaseFailureTrigger(t testing.TB, ctx context.Context, pool *pgxpool.Pool, point string) {
	t.Helper()
	functionName := "fail_card_purchase_" + point
	triggerName := functionName
	var table string
	var body string
	switch point {
	case "expense_insert":
		table = "transactions"
		body = "RAISE EXCEPTION 'synthetic card purchase expense failure';"
	case "expense_audit":
		table = "audit_events"
		body = "RAISE EXCEPTION 'synthetic card purchase expense audit failure';"
	case "plan_insert":
		table = "installment_plans"
		body = "RAISE EXCEPTION 'synthetic card purchase plan failure';"
	case "plan_audit":
		table = "installment_plan_audit_events"
		body = "RAISE EXCEPTION 'synthetic card purchase plan audit failure';"
	case "completion":
		table = "card_purchase_idempotency_records"
		body = "IF OLD.state = 'PENDING' AND NEW.state = 'COMPLETED' THEN RAISE EXCEPTION 'synthetic card purchase completion failure'; END IF; RETURN NEW;"
	default:
		t.Fatalf("unknown card purchase failure point %q", point)
	}
	triggerTiming := "BEFORE INSERT"
	triggerEvents := "FOR EACH ROW"
	if point == "completion" {
		triggerTiming = "BEFORE UPDATE"
	}
	ddl := fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN %s END $$; CREATE TRIGGER %s %s ON %s %s EXECUTE FUNCTION %s();`, functionName, body, triggerName, triggerTiming, table, triggerEvents, functionName)
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("installing %s trigger: %v", point, err)
	}
}

func dropCardPurchaseFailureTrigger(t testing.TB, ctx context.Context, pool *pgxpool.Pool, point string) {
	t.Helper()
	functionName := "fail_card_purchase_" + point
	ddl := fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s; DROP FUNCTION IF EXISTS %s();", functionName, cardPurchaseFailureTable(point), functionName)
	if _, err := pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("dropping %s trigger: %v", point, err)
	}
}

func cardPurchaseFailureTable(point string) string {
	switch point {
	case "expense_insert":
		return "transactions"
	case "expense_audit":
		return "audit_events"
	case "plan_insert":
		return "installment_plans"
	case "plan_audit":
		return "installment_plan_audit_events"
	case "completion":
		return "card_purchase_idempotency_records"
	default:
		return "transactions"
	}
}

func TestCardPurchasePostgresConcurrencyMatrix(t *testing.T) {
	t.Run("same key different fingerprint has one winner and one conflict", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('4'), "concurrent-conflict-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		useCase, firstInput := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "concurrent-conflict-key", nil, "exp_00000000000000000000000000000040", "ipl_00000000000000000000000000000040")
		secondInput := firstInput
		secondInput.Purchase.Description = "Outra compra"
		results := make([]application.RecordCardPurchaseResult, 2)
		errs := make([]error, 2)
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		for index, input := range []application.RecordCardPurchaseInput{firstInput, secondInput} {
			go func(index int, input application.RecordCardPurchaseInput) {
				defer wait.Done()
				<-start
				results[index], errs[index] = useCase.Execute(ctx, input)
			}(index, input)
		}
		close(start)
		wait.Wait()
		success, conflict := 0, 0
		for index := range errs {
			switch {
			case errs[index] == nil && !results[index].Replayed:
				success++
			case errors.Is(errs[index], application.ErrCardPurchaseIdempotencyConflict):
				conflict++
			default:
				t.Fatalf("same-key conflict call %d result=%+v error=%v", index, results[index], errs[index])
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("same-key success/conflict=%d/%d, want 1/1", success, conflict)
		}
		assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
			CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
			Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
			InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 1,
			InstallmentPlanIdempotency: 0,
		})
	})

	t.Run("different keys remain independent", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('5'), "concurrent-independent-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		useCase, err := application.NewRecordCardPurchase(
			purchaseRepository, purchaseRepository, cardRepository, nil,
			&sequenceCardPurchaseExpenseIDGenerator{}, &sequenceCardPurchasePlanIDGenerator{},
			fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
		)
		if err != nil {
			t.Fatal(err)
		}
		inputs := []application.RecordCardPurchaseInput{
			{Purchase: application.CardPurchaseInput{UserID: owner, Description: "Independente A", AmountMinor: 1000, Currency: domain.CurrencyBRL, OccurredAt: time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC), CreditCardID: card.ID(), Origin: domain.OriginIOS}, IdempotencyKey: "concurrent-independent-a"},
			{Purchase: application.CardPurchaseInput{UserID: owner, Description: "Independente B", AmountMinor: 2000, Currency: domain.CurrencyBRL, OccurredAt: time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC), CreditCardID: card.ID(), Origin: domain.OriginIOS}, IdempotencyKey: "concurrent-independent-b"},
		}
		results := make([]application.RecordCardPurchaseResult, 2)
		errs := make([]error, 2)
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		for index, input := range inputs {
			go func(index int, input application.RecordCardPurchaseInput) {
				defer wait.Done()
				<-start
				results[index], errs[index] = useCase.Execute(ctx, input)
			}(index, input)
		}
		close(start)
		wait.Wait()
		for index, err := range errs {
			if err != nil || results[index].Replayed {
				t.Fatalf("independent call %d result=%+v error=%v", index, results[index], err)
			}
		}
		assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
			CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
			Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
			InstallmentPlans: 0, InstallmentPlanAudits: 0, CardPurchaseIdempotency: 2,
			InstallmentPlanIdempotency: 0,
		})
	})

	t.Run("different cancellation keys have one transition", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('6'), "concurrent-cancel-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		count := 2
		create, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "concurrent-cancel-source", &count, "exp_00000000000000000000000000000041", "ipl_00000000000000000000000000000041")
		created, err := create.Execute(ctx, input)
		if err != nil || created.InstallmentPlan == nil {
			t.Fatalf("creating cancellation source=%+v error=%v", created, err)
		}
		date := mustCivilDate(t, 2026, 8, 30)
		command := buildInstallmentCancellationCommand(t, ctx, purchaseRepository, owner, created.InstallmentPlan.ID(), "concurrent-cancel-a", date)
		commandB := command
		commandB.IdempotencyKey = "concurrent-cancel-b"
		results := make([]application.IdempotentInstallmentPlanCancellationResult, 2)
		errs := make([]error, 2)
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		for index, candidate := range []application.IdempotentInstallmentPlanCancellationCommand{command, commandB} {
			go func(index int, candidate application.IdempotentInstallmentPlanCancellationCommand) {
				defer wait.Done()
				<-start
				results[index], errs[index] = purchaseRepository.CancelInstallmentPlan(ctx, candidate)
			}(index, candidate)
		}
		close(start)
		wait.Wait()
		success, already := 0, 0
		for index := range errs {
			switch {
			case errs[index] == nil && !results[index].Replayed:
				success++
			case errors.Is(errs[index], domain.ErrInstallmentPlanAlreadyCancelled):
				already++
			default:
				t.Fatalf("different-key cancellation %d result=%+v error=%v", index, results[index], errs[index])
			}
		}
		if success != 1 || already != 1 {
			t.Fatalf("different-key cancellation success/already=%d/%d, want 1/1", success, already)
		}
		assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
			CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
			Transactions: 1, TransactionAudits: 1, LegacyIdempotency: 0,
			InstallmentPlans: 1, InstallmentPlanAudits: 2, CardPurchaseIdempotency: 1,
			InstallmentPlanIdempotency: 1,
		})
	})

	t.Run("same cancellation key different plan conflicts", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('7'), "concurrent-cancel-conflict-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		count := 2
		createA, inputA := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "concurrent-plan-a", &count, "exp_00000000000000000000000000000042", "ipl_00000000000000000000000000000042")
		createdA, err := createA.Execute(ctx, inputA)
		if err != nil || createdA.InstallmentPlan == nil {
			t.Fatalf("creating plan A=%+v error=%v", createdA, err)
		}
		createB, inputB := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "concurrent-plan-b", &count, "exp_00000000000000000000000000000043", "ipl_00000000000000000000000000000043")
		createdB, err := createB.Execute(ctx, inputB)
		if err != nil || createdB.InstallmentPlan == nil {
			t.Fatalf("creating plan B=%+v error=%v", createdB, err)
		}
		date := mustCivilDate(t, 2026, 8, 30)
		commandA := buildInstallmentCancellationCommand(t, ctx, purchaseRepository, owner, createdA.InstallmentPlan.ID(), "concurrent-same-key", date)
		commandB := buildInstallmentCancellationCommand(t, ctx, purchaseRepository, owner, createdB.InstallmentPlan.ID(), "concurrent-same-key", date)
		results := make([]application.IdempotentInstallmentPlanCancellationResult, 2)
		errs := make([]error, 2)
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(2)
		for index, candidate := range []application.IdempotentInstallmentPlanCancellationCommand{commandA, commandB} {
			go func(index int, candidate application.IdempotentInstallmentPlanCancellationCommand) {
				defer wait.Done()
				<-start
				results[index], errs[index] = purchaseRepository.CancelInstallmentPlan(ctx, candidate)
			}(index, candidate)
		}
		close(start)
		wait.Wait()
		success, conflict := 0, 0
		for index := range errs {
			switch {
			case errs[index] == nil && !results[index].Replayed:
				success++
			case errors.Is(errs[index], application.ErrInstallmentCancelConflict):
				conflict++
			default:
				t.Fatalf("same-key plan conflict %d result=%+v error=%v", index, results[index], errs[index])
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("same-key plan success/conflict=%d/%d, want 1/1", success, conflict)
		}
		statusA := planStatus(t, ctx, pool, createdA.InstallmentPlan.ID())
		statusB := planStatus(t, ctx, pool, createdB.InstallmentPlan.ID())
		if statusA == statusB || statusA != "CANCELLED" && statusB != "CANCELLED" {
			t.Fatalf("same-key plan statuses=%s/%s, want exactly one CANCELLED", statusA, statusB)
		}
		assertCardPurchaseCounts(t, ctx, pool, cardPurchaseCountExpectation{
			CreditCards: 1, CreditCardAudits: 1, CreditCardIdempotency: 1,
			Transactions: 2, TransactionAudits: 2, LegacyIdempotency: 0,
			InstallmentPlans: 2, InstallmentPlanAudits: 3, CardPurchaseIdempotency: 2,
			InstallmentPlanIdempotency: 1,
		})
	})
}

func TestCardPurchasePostgresReadersContextAndCorruption(t *testing.T) {
	t.Run("expense replay found not-found conflict and corruption", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		const otherOwner = "usr_card_purchase_reader_owner_b"
		insertSyntheticUser(t, ctx, pool, otherOwner)
		repository := newRepository(t, pool)
		input := integrationRecordInput(owner, "reader-expense-key")
		created, err := newRecordUseCase(t, repository, fixedIntegrationIDGenerator{id: "exp_reader_replay"}).Execute(ctx, input)
		if err != nil || created.Replayed {
			t.Fatalf("creating expense replay fixture=%+v error=%v", created, err)
		}
		fingerprint := storedCardPurchaseExpenseFingerprint(t, ctx, pool, owner, input.IdempotencyKey)
		found, err := repository.FindExpenseReplay(ctx, application.ExpenseReplayQuery{UserID: owner, Operation: application.IdempotencyOperationCreateExpense, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
		if err != nil || !found.Found || found.Expense.ID() != created.Expense.ID() {
			t.Fatalf("expense replay found=%+v error=%v", found, err)
		}
		missing, err := repository.FindExpenseReplay(ctx, application.ExpenseReplayQuery{UserID: owner, Operation: application.IdempotencyOperationCreateExpense, IdempotencyKey: "reader-expense-missing", Fingerprint: fingerprint})
		if err != nil || missing.Found {
			t.Fatalf("missing expense replay=%+v error=%v", missing, err)
		}
		crossOwner, err := repository.FindExpenseReplay(ctx, application.ExpenseReplayQuery{UserID: otherOwner, Operation: application.IdempotencyOperationCreateExpense, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
		if err != nil || crossOwner.Found {
			t.Fatalf("cross-owner expense replay=%+v error=%v", crossOwner, err)
		}
		var wrong application.RequestFingerprint
		if _, err := repository.FindExpenseReplay(ctx, application.ExpenseReplayQuery{UserID: owner, Operation: application.IdempotencyOperationCreateExpense, IdempotencyKey: input.IdempotencyKey, Fingerprint: wrong}); !errors.Is(err, application.ErrIdempotencyConflict) {
			t.Fatalf("expense replay conflict error=%v", err)
		}
		if _, err := pool.Exec(ctx, `ALTER TABLE idempotency_records DROP CONSTRAINT idempotency_records_transaction_owner_type_fkey`); err != nil {
			t.Fatal("dropping disposable expense replay guard failed")
		}
		if _, err := pool.Exec(ctx, `UPDATE idempotency_records SET transaction_id='exp_reader_missing' WHERE user_id=$1 AND idempotency_key=$2`, owner, input.IdempotencyKey); err != nil {
			t.Fatal("installing corrupt expense replay fixture failed")
		}
		corrupt, err := repository.FindExpenseReplay(ctx, application.ExpenseReplayQuery{UserID: owner, Operation: application.IdempotencyOperationCreateExpense, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
		if corrupt.Found || !errors.Is(err, adapter.ErrLoadExpense) {
			t.Fatalf("corrupt expense replay=%+v error=%v, want fail-closed", corrupt, err)
		}
	})

	t.Run("installment list is owner-scoped deterministic and fail-closed", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		const otherOwner = "usr_card_purchase_list_owner_b"
		insertSyntheticUser(t, ctx, pool, otherOwner)
		cardRepository := newCreditCardRepository(t, pool)
		card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('8'), "list-plans-card")
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		empty, err := purchaseRepository.ListInstallmentPlans(ctx, otherOwner)
		if err != nil || empty == nil || len(empty) != 0 {
			t.Fatalf("empty owner list=%v error=%v, want non-nil empty slice", empty, err)
		}
		count := 2
		firstUseCase, firstInput := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "list-plan-a", &count, "exp_00000000000000000000000000000050", "ipl_00000000000000000000000000000050")
		if _, err := firstUseCase.Execute(ctx, firstInput); err != nil {
			t.Fatalf("creating first list plan: %v", err)
		}
		secondUseCase, secondInput := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "list-plan-b", &count, "exp_00000000000000000000000000000051", "ipl_00000000000000000000000000000051")
		if _, err := secondUseCase.Execute(ctx, secondInput); err != nil {
			t.Fatalf("creating second list plan: %v", err)
		}
		plans, err := purchaseRepository.ListInstallmentPlans(ctx, owner)
		if err != nil || len(plans) != 2 || plans[0].ID() != "ipl_00000000000000000000000000000050" || plans[1].ID() != "ipl_00000000000000000000000000000051" {
			t.Fatalf("deterministic owner list=%v error=%v", plans, err)
		}
		if _, err := pool.Exec(ctx, `ALTER TABLE installment_plans DROP CONSTRAINT installment_plans_due_day_valid`); err != nil {
			t.Fatal("dropping disposable plan guard failed")
		}
		if _, err := pool.Exec(ctx, `UPDATE installment_plans SET due_day=0 WHERE id=$1`, plans[0].ID()); err != nil {
			t.Fatal("installing corrupt plan fixture failed")
		}
		corrupt, err := purchaseRepository.ListInstallmentPlans(ctx, owner)
		if corrupt != nil || !errors.Is(err, adapter.ErrInvalidStoredInstallmentPlan) {
			t.Fatalf("corrupt plan list=%v error=%v, want nil fail-closed result", corrupt, err)
		}
	})

	t.Run("context cancellation and closed pool are categorized safely", func(t *testing.T) {
		pool, ctx, owner := newCreditCardConcurrentFixture(t)
		purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := purchaseRepository.ListInstallmentPlans(cancelled, owner); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled plan list error=%v, want context.Canceled", err)
		}
		if _, err := purchaseRepository.FindCardPurchaseReplay(cancelled, application.CardPurchaseReplayQuery{UserID: owner, Operation: application.CardPurchaseOperationCreate, IdempotencyKey: "cancelled-replay", Fingerprint: application.RequestFingerprint{}}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled card replay error=%v, want context.Canceled", err)
		}
		pool.Close()
		if _, err := purchaseRepository.ListInstallmentPlans(context.Background(), owner); !errors.Is(err, adapter.ErrListInstallmentPlans) {
			t.Fatalf("closed pool list error=%v, want safe list category", err)
		}
	})
}

func TestCardPurchasePostgresListRejectsActivePlanWithCancelledOn(t *testing.T) {
	pool, ctx, owner := newCreditCardConcurrentFixture(t)
	cardRepository := newCreditCardRepository(t, pool)
	card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('9'), "active-cancelled-on-list")
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	count := 2
	useCase, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "active-cancelled-on-list-key", &count, "exp_00000000000000000000000000000060", "ipl_00000000000000000000000000000060")
	if _, err := useCase.Execute(ctx, input); err != nil {
		t.Fatalf("creating list fixture: %v", err)
	}
	dropAndRestoreInstallmentPlanLifecycleConstraint(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE installment_plans SET cancelled_on=DATE '2026-08-30' WHERE id='ipl_00000000000000000000000000000060'`); err != nil {
		t.Fatalf("installing corrupt ACTIVE plan fixture: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `UPDATE installment_plans SET cancelled_on=NULL WHERE id='ipl_00000000000000000000000000000060'`); err != nil {
			t.Errorf("restoring ACTIVE plan fixture failed: %v", err)
		}
	})
	plans, err := purchaseRepository.ListInstallmentPlans(ctx, owner)
	if plans != nil || !errors.Is(err, adapter.ErrInvalidStoredInstallmentPlan) {
		t.Fatalf("corrupt ACTIVE plan list=%v error=%v, want fail-closed with no partial list", plans, err)
	}
}

func dropAndRestoreInstallmentPlanLifecycleConstraint(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `ALTER TABLE installment_plans DROP CONSTRAINT installment_plans_lifecycle_valid`); err != nil {
		t.Fatalf("dropping disposable installment plan lifecycle guard failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `ALTER TABLE installment_plans ADD CONSTRAINT installment_plans_lifecycle_valid CHECK (
			(status = 'ACTIVE' AND cancelled_on IS NULL)
			OR (status = 'CANCELLED' AND cancelled_on IS NOT NULL
				AND cancelled_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31')
		)`); err != nil {
			t.Errorf("restoring installment plan lifecycle guard failed: %v", err)
		}
	})
}

func TestCardPurchasePostgresReplayRejectsActivePlanWithCancelledOn(t *testing.T) {
	pool, ctx, owner := newCreditCardConcurrentFixture(t)
	cardRepository := newCreditCardRepository(t, pool)
	card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('a'), "active-cancelled-on-replay")
	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	count := 2
	useCase, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "active-cancelled-on-replay-key", &count, "exp_00000000000000000000000000000061", "ipl_00000000000000000000000000000061")
	created, err := useCase.Execute(ctx, input)
	if err != nil || created.InstallmentPlan == nil {
		t.Fatalf("creating replay fixture=%+v error=%v", created, err)
	}
	dropAndRestoreCardPurchaseSnapshotConstraint(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE card_purchase_idempotency_records SET plan_status='ACTIVE', plan_cancelled_on=DATE '2026-08-30' WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.CardPurchaseOperationCreate, input.IdempotencyKey); err != nil {
		t.Fatalf("installing corrupt replay plan snapshot: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `UPDATE card_purchase_idempotency_records SET plan_status='ACTIVE', plan_cancelled_on=NULL WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.CardPurchaseOperationCreate, input.IdempotencyKey); err != nil {
			t.Errorf("restoring card purchase replay fixture failed: %v", err)
		}
	})
	fingerprint := cardPurchaseStoredFingerprint(t, ctx, pool, owner, input.IdempotencyKey)
	lookup, err := purchaseRepository.FindCardPurchaseReplay(ctx, application.CardPurchaseReplayQuery{UserID: owner, Operation: application.CardPurchaseOperationCreate, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
	if lookup.Found || !errors.Is(err, adapter.ErrInvalidCardPurchaseIdempotencyState) {
		t.Fatalf("corrupt card purchase replay=%+v error=%v, want fail-closed", lookup, err)
	}
}

func dropAndRestoreCardPurchaseSnapshotConstraint(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `ALTER TABLE card_purchase_idempotency_records DROP CONSTRAINT card_purchase_idem_snapshot_valid`); err != nil {
		t.Fatalf("dropping disposable card purchase snapshot guard failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `ALTER TABLE card_purchase_idempotency_records ADD CONSTRAINT card_purchase_idem_snapshot_valid CHECK (
			(state = 'PENDING' AND expense_id IS NULL AND expense_user_id IS NULL AND plan_id IS NULL AND plan_user_id IS NULL AND completed_at IS NULL
			 AND num_nonnulls(
				expense_user_id, expense_description, expense_amount_minor, expense_currency,
				expense_payment_method, expense_category_id, expense_credit_card_id,
				expense_statement_due_on, expense_occurred_at, expense_financial_timezone,
				expense_origin, expense_status, expense_version, expense_created_at,
				expense_updated_at, plan_user_id, plan_credit_card_id, plan_expense_id, plan_total_minor,
				plan_total_currency, plan_installment_count, plan_first_due_on,
				plan_due_day, plan_status, plan_created_at, plan_cancelled_on
			 ) = 0)
			OR
			(state = 'COMPLETED'
			 AND expense_id IS NOT NULL
			 AND expense_user_id IS NOT NULL AND expense_user_id = user_id
			 AND expense_description IS NOT NULL
			 AND expense_amount_minor IS NOT NULL AND expense_amount_minor > 0
			 AND expense_currency IS NOT NULL AND expense_currency = 'BRL'
			 AND expense_payment_method IS NOT NULL AND expense_payment_method = 'CREDIT'
			 AND expense_credit_card_id IS NOT NULL
			 AND expense_credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
			 AND expense_statement_due_on IS NOT NULL
			 AND expense_statement_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
			 AND expense_occurred_at IS NOT NULL
			 AND expense_financial_timezone IS NOT NULL
			 AND expense_origin IS NOT NULL
			 AND expense_status IS NOT NULL AND expense_status = 'RECORDED'
			 AND expense_version IS NOT NULL AND expense_version = 1
			 AND expense_created_at IS NOT NULL
			 AND expense_updated_at IS NOT NULL
			 AND completed_at IS NOT NULL
			 AND ((plan_id IS NULL AND plan_user_id IS NULL AND plan_credit_card_id IS NULL AND plan_expense_id IS NULL
			       AND plan_total_minor IS NULL AND plan_total_currency IS NULL
			       AND plan_installment_count IS NULL AND plan_first_due_on IS NULL
			       AND plan_due_day IS NULL AND plan_status IS NULL AND plan_created_at IS NULL
			       AND plan_cancelled_on IS NULL)
			      OR
			      (plan_id IS NOT NULL AND plan_user_id IS NOT NULL AND plan_user_id = user_id
			       AND plan_user_id = expense_user_id
			       AND plan_credit_card_id IS NOT NULL AND plan_expense_id IS NOT NULL
			       AND plan_id COLLATE "C" ~ '^ipl_[0-9a-f]{32}$'
			       AND plan_credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
			       AND plan_expense_id = expense_id
			       AND plan_credit_card_id = expense_credit_card_id
			       AND plan_total_minor IS NOT NULL AND plan_total_minor >= 2
			       AND plan_total_minor >= plan_installment_count
			       AND plan_total_minor = expense_amount_minor
			       AND plan_total_currency IS NOT NULL AND plan_total_currency = 'BRL'
			       AND plan_installment_count IS NOT NULL AND plan_installment_count BETWEEN 2 AND 120
			       AND plan_first_due_on IS NOT NULL AND plan_due_day IS NOT NULL AND plan_due_day BETWEEN 1 AND 31
			       AND plan_first_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
			       AND plan_status IS NOT NULL AND plan_status = 'ACTIVE' AND plan_created_at IS NOT NULL
			       AND plan_cancelled_on IS NULL)))
		)`); err != nil {
			t.Errorf("restoring card purchase snapshot guard failed: %v", err)
		}
	})
}

func TestCardPurchasePostgresCancellationReplayRejectsIncompatibleSnapshot(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		cancelledOnSQL string
	}{
		{name: "active with cancelled date", status: "ACTIVE", cancelledOnSQL: "DATE '2026-08-30'"},
		{name: "cancelled without cancelled date", status: "CANCELLED", cancelledOnSQL: "NULL"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool, ctx, owner := newCreditCardConcurrentFixture(t)
			cardRepository := newCreditCardRepository(t, pool)
			card := createCreditCardFixture(t, ctx, cardRepository, owner, creditCardTestID('b'), "incompatible-cancellation-replay")
			purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			count := 2
			create, input := newSnapshotCardPurchaseUseCase(t, purchaseRepository, cardRepository, owner, card.ID(), "incompatible-cancellation-source", &count, "exp_00000000000000000000000000000062", "ipl_00000000000000000000000000000062")
			created, err := create.Execute(ctx, input)
			if err != nil || created.InstallmentPlan == nil {
				t.Fatalf("creating cancellation fixture=%+v error=%v", created, err)
			}
			date := mustCivilDate(t, 2026, 8, 30)
			cancelUseCase, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, fixedCardPurchaseFinancialDate{date: date})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cancelUseCase.Execute(ctx, application.CancelInstallmentPlanInput{OwnerID: owner, PlanID: created.InstallmentPlan.ID(), ExpectedCancelledOn: date, IdempotencyKey: "incompatible-cancellation-key"}); err != nil {
				t.Fatalf("creating cancellation replay fixture: %v", err)
			}
			dropAndRestoreInstallmentPlanIdempotencySnapshotConstraint(t, ctx, pool)
			if _, err := pool.Exec(ctx, fmt.Sprintf(`UPDATE installment_plan_idempotency_records SET plan_status=$1, plan_cancelled_on=%s WHERE user_id=$2 AND operation=$3 AND idempotency_key=$4`, test.cancelledOnSQL), test.status, owner, application.CancelInstallmentPlanOperation, "incompatible-cancellation-key"); err != nil {
				t.Fatalf("installing corrupt cancellation snapshot: %v", err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), `UPDATE installment_plan_idempotency_records SET plan_status='CANCELLED', plan_cancelled_on=DATE '2026-08-30' WHERE user_id=$1 AND operation=$2 AND idempotency_key=$3`, owner, application.CancelInstallmentPlanOperation, "incompatible-cancellation-key"); err != nil {
					t.Errorf("restoring cancellation replay fixture failed: %v", err)
				}
			})
			fingerprint := installmentCancellationStoredFingerprint(t, ctx, pool, owner, "incompatible-cancellation-key")
			lookup, err := purchaseRepository.FindInstallmentPlanCancellationReplay(ctx, application.InstallmentPlanCancellationReplayQuery{OwnerID: owner, Operation: application.CancelInstallmentPlanOperation, IdempotencyKey: "incompatible-cancellation-key", Fingerprint: fingerprint})
			if lookup.Found || !errors.Is(err, adapter.ErrInvalidStoredInstallmentPlan) {
				t.Fatalf("corrupt cancellation replay=%+v error=%v, want fail-closed", lookup, err)
			}
		})
	}
}

func dropAndRestoreInstallmentPlanIdempotencySnapshotConstraint(t testing.TB, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `ALTER TABLE installment_plan_idempotency_records DROP CONSTRAINT installment_plan_idem_snapshot_valid`); err != nil {
		t.Fatalf("dropping disposable cancellation snapshot guard failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `ALTER TABLE installment_plan_idempotency_records ADD CONSTRAINT installment_plan_idem_snapshot_valid CHECK (
			(state = 'PENDING' AND plan_id IS NULL AND plan_user_id IS NULL AND completed_at IS NULL
			 AND num_nonnulls(
				plan_user_id, plan_credit_card_id, plan_expense_id, plan_total_minor,
				plan_total_currency, plan_installment_count, plan_first_due_on,
				plan_due_day, plan_status, plan_created_at, plan_cancelled_on
			 ) = 0)
			OR
			(state = 'COMPLETED'
			 AND plan_id IS NOT NULL
			 AND plan_user_id IS NOT NULL AND plan_user_id = user_id
			 AND plan_id COLLATE "C" ~ '^ipl_[0-9a-f]{32}$'
			 AND plan_credit_card_id IS NOT NULL
			 AND plan_credit_card_id COLLATE "C" ~ '^card_[0-9a-f]{32}$'
			 AND plan_expense_id IS NOT NULL
			 AND plan_total_minor IS NOT NULL AND plan_total_minor > 0
			 AND plan_total_currency IS NOT NULL AND plan_total_currency = 'BRL'
			 AND plan_installment_count IS NOT NULL AND plan_installment_count BETWEEN 2 AND 120
			 AND plan_total_minor >= plan_installment_count
			 AND plan_first_due_on IS NOT NULL
			 AND plan_first_due_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
			 AND plan_due_day IS NOT NULL AND plan_due_day BETWEEN 1 AND 31
			 AND plan_status IS NOT NULL AND plan_status = 'CANCELLED'
			 AND plan_created_at IS NOT NULL
			 AND plan_cancelled_on IS NOT NULL
			 AND plan_cancelled_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'
			 AND completed_at IS NOT NULL)
		)`); err != nil {
			t.Errorf("restoring cancellation snapshot guard failed: %v", err)
		}
	})
}

type fixedCardPurchaseExpenseIDGenerator struct{ id string }

func (generator fixedCardPurchaseExpenseIDGenerator) NewExpenseID() (string, error) {
	return generator.id, nil
}

type sequenceCardPurchaseExpenseIDGenerator struct{ calls atomic.Uint64 }

func (generator *sequenceCardPurchaseExpenseIDGenerator) NewExpenseID() (string, error) {
	return fmt.Sprintf("exp_%032x", generator.calls.Add(1)), nil
}

type fixedCardPurchasePlanIDGenerator struct {
	id    string
	calls atomic.Int64
}

func (generator *fixedCardPurchasePlanIDGenerator) NewInstallmentPlanID() (string, error) {
	generator.calls.Add(1)
	return generator.id, nil
}

type sequenceCardPurchasePlanIDGenerator struct{ calls atomic.Uint64 }

func (generator *sequenceCardPurchasePlanIDGenerator) NewInstallmentPlanID() (string, error) {
	return fmt.Sprintf("ipl_%032x", generator.calls.Add(1)), nil
}

type fixedCardPurchaseClock struct{ now time.Time }

func (clock fixedCardPurchaseClock) Now() time.Time { return clock.now }

type fixedCardPurchaseFinancialDate struct{ date domain.CivilDate }

func (provider fixedCardPurchaseFinancialDate) CurrentFinancialDate(context.Context) (domain.CivilDate, error) {
	return provider.date, nil
}

func buildInstallmentCancellationCommand(t testing.TB, ctx context.Context, repository *adapter.CardPurchaseRepository, owner, planID, key string, date domain.CivilDate) application.IdempotentInstallmentPlanCancellationCommand {
	t.Helper()
	capture := &captureInstallmentCancellationStore{}
	useCase, err := application.NewCancelInstallmentPlan(capture, emptyInstallmentCancellationReplay{}, repository, fixedCardPurchaseFinancialDate{date: date})
	if err != nil {
		t.Fatalf("creating cancellation command builder: %v", err)
	}
	if _, err := useCase.Execute(ctx, application.CancelInstallmentPlanInput{OwnerID: owner, PlanID: planID, ExpectedCancelledOn: date, IdempotencyKey: key}); err != nil {
		t.Fatalf("building cancellation command: %v", err)
	}
	return capture.command
}

func planStatus(t testing.TB, ctx context.Context, pool *pgxpool.Pool, planID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM installment_plans WHERE id=$1`, planID).Scan(&status); err != nil {
		t.Fatalf("reading plan status: %v", err)
	}
	return status
}

type cardPurchaseCountExpectation struct {
	CreditCards                int
	CreditCardAudits           int
	CreditCardIdempotency      int
	Transactions               int
	TransactionAudits          int
	LegacyIdempotency          int
	InstallmentPlans           int
	InstallmentPlanAudits      int
	CardPurchaseIdempotency    int
	InstallmentPlanIdempotency int
}

func assertCardPurchaseCounts(t testing.TB, ctx context.Context, pool *pgxpool.Pool, expected cardPurchaseCountExpectation) {
	t.Helper()
	// This helper intentionally uses the pool's concrete QueryRow contract via
	// the small local adapter below; keeping each count explicit catches partial
	// transaction commits.
	queries := []struct {
		query string
		want  int
	}{
		{"SELECT count(*) FROM credit_cards", expected.CreditCards},
		{"SELECT count(*) FROM credit_card_audit_events", expected.CreditCardAudits},
		{"SELECT count(*) FROM credit_card_idempotency_records", expected.CreditCardIdempotency},
		{"SELECT count(*) FROM transactions", expected.Transactions},
		{"SELECT count(*) FROM audit_events", expected.TransactionAudits},
		{"SELECT count(*) FROM idempotency_records", expected.LegacyIdempotency},
		{"SELECT count(*) FROM installment_plans", expected.InstallmentPlans},
		{"SELECT count(*) FROM installment_plan_audit_events", expected.InstallmentPlanAudits},
		{"SELECT count(*) FROM card_purchase_idempotency_records", expected.CardPurchaseIdempotency},
		{"SELECT count(*) FROM installment_plan_idempotency_records", expected.InstallmentPlanIdempotency},
	}
	for _, check := range queries {
		var got int
		if err := pool.QueryRow(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s count=%d error=%v, want %d", check.query, got, err, check.want)
		}
	}
}

func mustCivilDate(t testing.TB, year, month, day int) domain.CivilDate {
	t.Helper()
	value, err := domain.NewCivilDate(year, time.Month(month), day)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
