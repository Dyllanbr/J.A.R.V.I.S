//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestScheduledCommitmentPostgresReaderIsOwnerScopedReadOnlyAndUsesPersistedState(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_scheduled_commitment_a"
	const ownerB = "usr_scheduled_commitment_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	cardRepository := newCreditCardRepository(t, pool)
	cardA := createCreditCardFixture(t, ctx, cardRepository, ownerA, creditCardTestID('a'), "scheduled-card-a")
	cardB := createCreditCardFixture(t, ctx, cardRepository, ownerB, creditCardTestID('b'), "scheduled-card-b")

	purchaseRepository, err := adapter.NewCardPurchaseRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewCardPurchaseRepository() error = %v", err)
	}
	count := 3
	createPurchase := func(owner, cardID, key, expenseID, planID string) application.RecordCardPurchaseResult {
		t.Helper()
		useCase, err := application.NewRecordCardPurchase(
			purchaseRepository,
			purchaseRepository,
			cardRepository,
			nil,
			fixedCardPurchaseExpenseIDGenerator{id: expenseID},
			&fixedCardPurchasePlanIDGenerator{id: planID},
			fixedCardPurchaseClock{now: time.Date(2026, time.August, 25, 15, 0, 0, 0, time.UTC)},
		)
		if err != nil {
			t.Fatalf("NewRecordCardPurchase() error = %v", err)
		}
		result, err := useCase.Execute(ctx, application.RecordCardPurchaseInput{
			Purchase: application.CardPurchaseInput{
				UserID: owner, Description: "Compromisso sintético", AmountMinor: 10100,
				Currency: domain.CurrencyBRL, OccurredAt: time.Date(2026, time.August, 25, 14, 0, 0, 0, time.UTC),
				CreditCardID: cardID, InstallmentCount: &count, Origin: domain.OriginIOS,
			},
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("RecordCardPurchase.Execute() error = %v", err)
		}
		return result
	}
	planA := createPurchase(ownerA, cardA.ID(), "scheduled-purchase-a", "exp_00000000000000000000000000000070", "ipl_00000000000000000000000000000070").InstallmentPlan
	planB := createPurchase(ownerB, cardB.ID(), "scheduled-purchase-b", "exp_00000000000000000000000000000071", "ipl_00000000000000000000000000000071").InstallmentPlan
	if planA == nil || planB == nil {
		t.Fatal("purchase fixture did not create installment plans")
	}

	recurrenceRepository := newRecurrenceRepository(t, pool)
	recurrenceA := persistDirectRecurrence(t, ctx, recurrenceRepository, "rec-scheduled-a", ownerA, "scheduled-recurrence-a")
	if _, err := recurrenceRepository.RecordRecurrence(ctx, newDirectCreateCommand(t, "rec-scheduled-b", ownerB, "scheduled-recurrence-b", recurrenceFingerprint(42), recurrenceA.CreatedAt())); err != nil {
		t.Fatalf("owner B recurrence fixture failed: %v", err)
	}

	// A cancellation is persisted before the read. The adapter must preserve
	// the aggregate state; EffectiveSchedule remains an application concern.
	cancelDate := mustCivilDate(t, 2026, 9, 30)
	cancelUseCase, err := application.NewCancelInstallmentPlan(purchaseRepository, purchaseRepository, purchaseRepository, fixedCardPurchaseFinancialDate{date: cancelDate})
	if err != nil {
		t.Fatalf("NewCancelInstallmentPlan() error = %v", err)
	}
	if _, err := cancelUseCase.Execute(ctx, application.CancelInstallmentPlanInput{
		OwnerID: ownerA, PlanID: planA.ID(), ExpectedCancelledOn: cancelDate, IdempotencyKey: "scheduled-cancel-a",
	}); err != nil {
		t.Fatalf("CancelInstallmentPlan.Execute() error = %v", err)
	}

	before := countScheduledCommitmentWrites(t, ctx, pool)
	repository, err := adapter.NewScheduledCommitmentRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewScheduledCommitmentRepository() error = %v", err)
	}
	listScheduledCommitments, err := application.NewListScheduledCommitments(repository)
	if err != nil {
		t.Fatalf("NewListScheduledCommitments() error = %v", err)
	}
	sourcesA, err := repository.Read(ctx, ownerA)
	if err != nil {
		t.Fatalf("Read(owner A) error = %v", err)
	}
	if len(sourcesA.InstallmentPlans) != 1 || len(sourcesA.Recurrences) != 1 || sourcesA.InstallmentPlans[0].OwnerID() != ownerA || sourcesA.Recurrences[0].UserID() != ownerA {
		t.Fatalf("owner A sources = %+v, want one owner-bound plan and recurrence", sourcesA)
	}
	if sourcesA.InstallmentPlans[0].Status() != domain.InstallmentPlanStatusCancelled {
		t.Fatal("owner A plan did not preserve CANCELLED state")
	}
	effective, err := sourcesA.InstallmentPlans[0].EffectiveSchedule()
	if err != nil || len(effective) != 1 {
		t.Fatalf("cancelled plan effective schedule length = %d, error = %v, want cutoff schedule", len(effective), err)
	}
	if _, ok := sourcesA.Recurrences[0].CancelledAt(); ok {
		t.Fatal("owner A recurrence unexpectedly cancelled")
	}
	projectedA, err := listScheduledCommitments.Execute(ctx, application.ListScheduledCommitmentsInput{
		OwnerID: ownerA, EvaluationDate: mustCivilDate(t, 2026, 8, 25),
	})
	if err != nil {
		t.Fatalf("ListScheduledCommitments(owner A) error = %v", err)
	}
	if len(projectedA.Items) == 0 {
		t.Fatal("combined application snapshot produced no scheduled commitments")
	}
	if !containsScheduledCommitmentSource(projectedA.Items, application.ScheduledCommitmentSourceInstallmentPlan, planA.ID()) || !containsScheduledCommitmentSource(projectedA.Items, application.ScheduledCommitmentSourceRecurrence, recurrenceA.ID()) {
		t.Fatalf("combined application snapshot omitted owner A sources: %+v", projectedA.Items)
	}

	sourcesB, err := repository.Read(ctx, ownerB)
	if err != nil {
		t.Fatalf("Read(owner B) error = %v", err)
	}
	if len(sourcesB.InstallmentPlans) != 1 || len(sourcesB.Recurrences) != 1 || sourcesB.InstallmentPlans[0].OwnerID() != ownerB || sourcesB.Recurrences[0].UserID() != ownerB {
		t.Fatalf("owner B sources = %+v, want only owner B data", sourcesB)
	}
	activeSchedule, err := sourcesB.InstallmentPlans[0].Schedule()
	if err != nil || len(activeSchedule) != 3 {
		t.Fatalf("owner B ACTIVE schedule length = %d, error = %v, want complete schedule", len(activeSchedule), err)
	}
	lookupA, err := repository.FindInstallmentPlan(ctx, ownerA, planA.ID())
	if err != nil || !lookupA.Found || lookupA.InstallmentPlan.OwnerID() != ownerA {
		t.Fatalf("FindInstallmentPlan(owner A) = %+v, error = %v", lookupA, err)
	}
	lookupCrossOwner, err := repository.FindInstallmentPlan(ctx, ownerB, planA.ID())
	if err != nil || lookupCrossOwner.Found {
		t.Fatalf("FindInstallmentPlan(cross-owner) = %+v, error = %v, want not found", lookupCrossOwner, err)
	}
	if _, err := repository.Read(ctx, "usr_scheduled_unknown"); err != nil {
		t.Fatalf("unknown valid owner read error = %v, want stable empty source", err)
	}
	projectedB, err := listScheduledCommitments.Execute(ctx, application.ListScheduledCommitmentsInput{
		OwnerID: ownerB, EvaluationDate: mustCivilDate(t, 2026, 8, 25),
	})
	if err != nil {
		t.Fatalf("ListScheduledCommitments(owner B) error = %v", err)
	}
	if len(projectedB.Items) == 0 {
		t.Fatal("combined application snapshot omitted owner B sources")
	}
	if containsScheduledCommitmentSource(projectedB.Items, application.ScheduledCommitmentSourceInstallmentPlan, planA.ID()) || containsScheduledCommitmentSource(projectedB.Items, application.ScheduledCommitmentSourceRecurrence, recurrenceA.ID()) {
		t.Fatal("combined application snapshot leaked owner A sources to owner B")
	}
	if got := countScheduledCommitmentWrites(t, ctx, pool); got != before {
		t.Fatalf("read changed persisted counts from %v to %v", before, got)
	}

	plans, err := repository.ListInstallmentPlans(ctx, ownerA)
	if err != nil || len(plans) != 1 {
		t.Fatalf("ListInstallmentPlans() = %d plans, error = %v", len(plans), err)
	}
	recurrences, err := repository.ListRecurrences(ctx, ownerA)
	if err != nil || len(recurrences) != 1 {
		t.Fatalf("ListRecurrences() = %d recurrences, error = %v", len(recurrences), err)
	}
}

func containsScheduledCommitmentSource(items []application.ScheduledCommitmentLine, source application.ScheduledCommitmentSource, sourceID string) bool {
	for _, item := range items {
		if item.Source() == source && item.SourceID() == sourceID {
			return true
		}
	}
	return false
}

func TestScheduledCommitmentPostgresReaderPreservesContextCancellationAndRejectsInvalidOwner(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	repository, err := adapter.NewScheduledCommitmentRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewScheduledCommitmentRepository() error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.Read(cancelled, syntheticUserID); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read(cancelled context) error = %v, want context.Canceled", err)
	}
	if _, err := repository.Read(context.Background(), ""); !errors.Is(err, adapter.ErrInvalidStoredScheduledCommitment) {
		t.Fatalf("Read(invalid owner) error = %v, want invalid owner category", err)
	}
	if _, err := repository.ListInstallmentPlans(context.Background(), ""); !errors.Is(err, adapter.ErrInvalidStoredInstallmentPlan) {
		t.Fatalf("ListInstallmentPlans(invalid owner) error = %v, want invalid owner category", err)
	}
	if _, err := repository.ListRecurrences(context.Background(), ""); !errors.Is(err, adapter.ErrInvalidStoredRecurrence) {
		t.Fatalf("ListRecurrences(invalid owner) error = %v, want invalid owner category", err)
	}
}

func countScheduledCommitmentWrites(t testing.TB, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	// This helper is intentionally kept to read-only aggregate counts. The
	// concrete pool is supplied by the integration fixture and no mutation is
	// possible through this interface.
	var total int
	for _, table := range []string{"transactions", "audit_events", "installment_plans", "installment_plan_audit_events", "recurrences", "recurrence_audit_events", "card_purchase_idempotency_records", "installment_plan_idempotency_records"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
		total += count
	}
	return total
}
