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

type scheduledPlanReaderFake struct {
	items    []domain.InstallmentPlan
	ownerID  string
	listCall int
	err      error
}

func (reader *scheduledPlanReaderFake) FindInstallmentPlan(context.Context, string, string) (application.InstallmentPlanLookup, error) {
	return application.InstallmentPlanLookup{}, nil
}

func (reader *scheduledPlanReaderFake) ListInstallmentPlans(_ context.Context, ownerID string) ([]domain.InstallmentPlan, error) {
	reader.listCall++
	reader.ownerID = ownerID
	return reader.items, reader.err
}

type scheduledRecurrenceReaderFake struct {
	items    []domain.Recurrence
	ownerID  string
	listCall int
	err      error
}

func TestNewListScheduledCommitmentsRequiresBothOwnerScopedReaders(t *testing.T) {
	recurrences := &scheduledRecurrenceReaderFake{}
	plans := &scheduledPlanReaderFake{}
	if _, err := application.NewListScheduledCommitments(nil, recurrences); !errors.Is(err, application.ErrMissingScheduledCommitmentPlanReader) {
		t.Fatalf("nil plan reader error = %v", err)
	}
	if _, err := application.NewListScheduledCommitments(plans, nil); !errors.Is(err, application.ErrMissingScheduledCommitmentRecurrenceReader) {
		t.Fatalf("nil recurrence reader error = %v", err)
	}
}

func (reader *scheduledRecurrenceReaderFake) ListRecurrences(_ context.Context, ownerID string) ([]domain.Recurrence, error) {
	reader.listCall++
	reader.ownerID = ownerID
	return reader.items, reader.err
}

func TestListScheduledCommitmentsUsesActivePlansAndRecurrencesWithinCivilHorizon(t *testing.T) {
	owner := "owner-scheduled"
	plan := scheduledPlan(t, owner, "ipl_0123456789abcdef0123456789abcdef", 2026, time.January, 31, 13, 1300)
	recurrence := scheduledRecurrence(t, owner, "recurrence-scheduled-001", 2025, time.September, 3, 250)
	planReader := &scheduledPlanReaderFake{items: []domain.InstallmentPlan{plan}}
	recurrenceReader := &scheduledRecurrenceReaderFake{items: []domain.Recurrence{recurrence}}
	useCase := mustListScheduledCommitments(t, planReader, recurrenceReader)

	result, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{
		OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.January, 31),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if planReader.ownerID != owner || recurrenceReader.ownerID != owner || planReader.listCall != 1 || recurrenceReader.listCall != 1 {
		t.Fatalf("readers owner/calls = %q/%q %d/%d", planReader.ownerID, recurrenceReader.ownerID, planReader.listCall, recurrenceReader.listCall)
	}
	if len(result.Items) != 24 {
		t.Fatalf("items = %d, want 24 (12 plan dates after evaluation plus 12 recurrence dates)", len(result.Items))
	}
	if result.Items[0].DueOn().String() != "2026-02-03" || result.Items[len(result.Items)-1].DueOn().String() != "2027-01-31" {
		t.Fatalf("boundary dates = %s/%s", result.Items[0].DueOn(), result.Items[len(result.Items)-1].DueOn())
	}
	for _, item := range result.Items {
		if item.DueOn().Equal(scheduledDate(t, 2026, time.January, 31)) || item.DueOn().Before(scheduledDate(t, 2026, time.January, 31)) {
			t.Fatalf("evaluation date leaked into result: %s", item.DueOn())
		}
		if item.DueOn().Before(scheduledDate(t, 2026, time.January, 31)) || scheduledDate(t, 2027, time.January, 31).Before(item.DueOn()) {
			t.Fatalf("item outside horizon: %s", item.DueOn())
		}
	}
}

func TestListScheduledCommitmentsUsesEffectiveScheduleForCancelledPlan(t *testing.T) {
	owner := "owner-cancelled"
	plan := scheduledPlan(t, owner, "ipl_fedcba9876543210fedcba9876543210", 2026, time.January, 31, 4, 1000)
	cancelled, err := plan.Cancel(scheduledDate(t, 2026, time.February, 28))
	if err != nil {
		t.Fatal(err)
	}
	useCase := mustListScheduledCommitments(t, &scheduledPlanReaderFake{items: []domain.InstallmentPlan{cancelled}}, &scheduledRecurrenceReaderFake{})
	result, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.January, 1)})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].DueOn().String() != "2026-01-31" || result.Items[1].DueOn().String() != "2026-02-28" {
		t.Fatalf("effective schedule = %#v", result.Items)
	}
}

func TestListScheduledCommitmentsFiltersCancelledRecurrence(t *testing.T) {
	owner := "owner-recurrence"
	recurrence := scheduledRecurrence(t, owner, "recurrence-cancelled-001", 2026, time.January, 10, 500)
	cancelled, err := recurrence.Cancel(time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	useCase := mustListScheduledCommitments(t, &scheduledPlanReaderFake{}, &scheduledRecurrenceReaderFake{items: []domain.Recurrence{cancelled}})
	result, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.January, 1)})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("cancelled recurrence items = %#v, want stable empty slice", result.Items)
	}
}

func TestListScheduledCommitmentsOrderingIsStableAcrossReaderOrder(t *testing.T) {
	owner := "owner-order"
	plan := scheduledPlan(t, owner, "ipl_11111111111111111111111111111111", 2026, time.March, 10, 2, 200)
	otherPlan := scheduledPlan(t, owner, "ipl_00000000000000000000000000000000", 2026, time.March, 10, 2, 200)
	recurrence := scheduledRecurrence(t, owner, "recurrence-order-001", 2026, time.March, 10, 200)
	otherRecurrence := scheduledRecurrence(t, owner, "recurrence-order-000", 2026, time.March, 10, 200)
	input := application.ListScheduledCommitmentsInput{OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.February, 1)}
	first := mustListScheduledCommitments(t, &scheduledPlanReaderFake{items: []domain.InstallmentPlan{plan, otherPlan}}, &scheduledRecurrenceReaderFake{items: []domain.Recurrence{recurrence, otherRecurrence}})
	firstResult, err := first.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second := mustListScheduledCommitments(t, &scheduledPlanReaderFake{items: []domain.InstallmentPlan{otherPlan, plan}}, &scheduledRecurrenceReaderFake{items: []domain.Recurrence{otherRecurrence, recurrence}})
	secondResult, err := second.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstResult.Items) != 26 || len(secondResult.Items) != 26 {
		t.Fatalf("items = %d/%d, want 26/26", len(firstResult.Items), len(secondResult.Items))
	}
	for index := range firstResult.Items {
		left, right := firstResult.Items[index], secondResult.Items[index]
		if left.Source() != right.Source() || left.SourceID() != right.SourceID() || left.Sequence() != right.Sequence() || !left.DueOn().Equal(right.DueOn()) || !left.Amount().Equal(right.Amount()) {
			t.Fatalf("unstable item %d = %#v/%#v", index, left, right)
		}
	}
	if firstResult.Items[0].Source() != application.ScheduledCommitmentSourceInstallmentPlan || firstResult.Items[1].Source() != application.ScheduledCommitmentSourceInstallmentPlan || firstResult.Items[2].Source() != application.ScheduledCommitmentSourceRecurrence {
		t.Fatalf("same-date ordering = %q, %q, %q", firstResult.Items[0].Source(), firstResult.Items[1].Source(), firstResult.Items[2].Source())
	}
	if firstResult.Items[0].SourceID() != otherPlan.ID() || firstResult.Items[1].SourceID() != plan.ID() || firstResult.Items[2].SourceID() != otherRecurrence.ID() {
		t.Fatalf("same-date tie-break = %q, %q, %q", firstResult.Items[0].SourceID(), firstResult.Items[1].SourceID(), firstResult.Items[2].SourceID())
	}
}

func TestListScheduledCommitmentsRejectsDuplicateSourcesAndDependencyOwnerMismatch(t *testing.T) {
	owner := "owner-validation"
	plan := scheduledPlan(t, owner, "ipl_22222222222222222222222222222222", 2026, time.April, 10, 2, 200)
	useCase := mustListScheduledCommitments(t, &scheduledPlanReaderFake{items: []domain.InstallmentPlan{plan, plan}}, &scheduledRecurrenceReaderFake{})
	if _, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.January, 1)}); !errors.Is(err, application.ErrScheduledCommitmentDuplicate) {
		t.Fatalf("duplicate plan error = %v", err)
	}

	otherPlan := scheduledPlan(t, "owner-other", "ipl_33333333333333333333333333333333", 2026, time.April, 10, 2, 200)
	useCase = mustListScheduledCommitments(t, &scheduledPlanReaderFake{items: []domain.InstallmentPlan{otherPlan}}, &scheduledRecurrenceReaderFake{})
	if _, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.January, 1)}); !errors.Is(err, application.ErrScheduledCommitmentDependency) {
		t.Fatalf("owner mismatch error = %v", err)
	}
}

func TestListScheduledCommitmentsRejectsDuplicateRecurrencesAndOwnerMismatch(t *testing.T) {
	owner := "owner-recurrence-validation"
	recurrence := scheduledRecurrence(t, owner, "recurrence-duplicate-001", 2026, time.April, 10, 200)
	useCase := mustListScheduledCommitments(t, &scheduledPlanReaderFake{}, &scheduledRecurrenceReaderFake{items: []domain.Recurrence{recurrence, recurrence}})
	if _, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.January, 1)}); !errors.Is(err, application.ErrScheduledCommitmentDuplicate) {
		t.Fatalf("duplicate recurrence error = %v", err)
	}

	other := scheduledRecurrence(t, "owner-other", "recurrence-owner-other", 2026, time.April, 10, 200)
	useCase = mustListScheduledCommitments(t, &scheduledPlanReaderFake{}, &scheduledRecurrenceReaderFake{items: []domain.Recurrence{other}})
	if _, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: owner, EvaluationDate: scheduledDate(t, 2026, time.January, 1)}); !errors.Is(err, application.ErrScheduledCommitmentDependency) {
		t.Fatalf("recurrence owner mismatch error = %v", err)
	}
}

func TestListScheduledCommitmentsRejectsInvalidInputBeforeReadersAndKeepsEmptyResultStable(t *testing.T) {
	plans := &scheduledPlanReaderFake{}
	recurrences := &scheduledRecurrenceReaderFake{}
	useCase := mustListScheduledCommitments(t, plans, recurrences)
	for _, input := range []application.ListScheduledCommitmentsInput{
		{OwnerID: "", EvaluationDate: scheduledDate(t, 2026, time.January, 1)},
		{OwnerID: "owner-valid", EvaluationDate: domain.CivilDate{}},
	} {
		if _, err := useCase.Execute(context.Background(), input); err == nil {
			t.Fatal("invalid input unexpectedly succeeded")
		}
	}
	if plans.listCall != 0 || recurrences.listCall != 0 {
		t.Fatalf("invalid input reached readers: %d/%d", plans.listCall, recurrences.listCall)
	}

	result, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: "owner-valid", EvaluationDate: scheduledDate(t, 2026, time.January, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty sources = %#v", result.Items)
	}

	if _, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: "owner-valid", EvaluationDate: scheduledDate(t, 9999, time.December, 31)}); !errors.Is(err, application.ErrScheduledCommitmentHorizon) {
		t.Fatalf("horizon overflow error = %v", err)
	}
	if plans.listCall != 1 || recurrences.listCall != 1 {
		t.Fatal("horizon overflow reached a reader")
	}
}

func TestListScheduledCommitmentsSafelyWrapsReaderFailuresAndPreservesCancellation(t *testing.T) {
	marker := "SQL_SECRET_SCHEDULE"
	useCase := mustListScheduledCommitments(t, &scheduledPlanReaderFake{err: errors.New(marker)}, &scheduledRecurrenceReaderFake{})
	_, err := useCase.Execute(context.Background(), application.ListScheduledCommitmentsInput{OwnerID: "owner-errors", EvaluationDate: scheduledDate(t, 2026, time.January, 1)})
	if !errors.Is(err, application.ErrScheduledCommitmentPlanQuery) || strings.Contains(err.Error(), marker) {
		t.Fatalf("reader error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plans := &scheduledPlanReaderFake{}
	recurrences := &scheduledRecurrenceReaderFake{}
	useCase = mustListScheduledCommitments(t, plans, recurrences)
	if _, err := useCase.Execute(ctx, application.ListScheduledCommitmentsInput{OwnerID: "owner-errors", EvaluationDate: scheduledDate(t, 2026, time.January, 1)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	if plans.listCall != 0 || recurrences.listCall != 0 {
		t.Fatal("cancelled request reached readers")
	}
}

func mustListScheduledCommitments(t *testing.T, planReader application.InstallmentPlanReader, recurrenceReader application.RecurrenceReader) *application.ListScheduledCommitments {
	t.Helper()
	useCase, err := application.NewListScheduledCommitments(planReader, recurrenceReader)
	if err != nil {
		t.Fatal(err)
	}
	return useCase
}

func scheduledPlan(t *testing.T, owner, id string, year int, month time.Month, day, count int, totalMinor int64) domain.InstallmentPlan {
	t.Helper()
	amount, err := domain.NewMoney(totalMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	firstDue := scheduledDate(t, year, month, day)
	anchor, err := domain.NewDayOfMonthAnchor(day)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewInstallmentPlan(domain.InstallmentPlanParams{
		ID: id, OwnerID: owner, CreditCardID: "card_0123456789abcdef0123456789abcdef", ExpenseID: "expense-" + id,
		TotalAmount: amount, InstallmentCount: count, FirstDueDate: firstDue, DueDayAnchor: anchor,
		CreatedAt: time.Date(2025, time.December, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func scheduledRecurrence(t *testing.T, owner, id string, year int, month time.Month, day int, amountMinor int64) domain.Recurrence {
	t.Helper()
	amount, err := domain.NewMoney(amountMinor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID: id,
		Details: domain.RecurrenceDetails{
			UserID: owner, TransactionType: domain.TransactionTypeExpense, Description: "Compromisso sintético",
			ExpectedAmount: amount, Frequency: domain.RecurrenceFrequencyMonthly, StartsOn: scheduledDate(t, year, month, day),
		},
		CreatedAt: time.Date(2025, time.November, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return recurrence
}

func scheduledDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	date, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return date
}
