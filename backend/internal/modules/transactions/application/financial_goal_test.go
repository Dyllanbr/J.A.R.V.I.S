package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

type financialGoalsReaderFake struct {
	snapshot application.FinancialGoalsSnapshot
	err      error
	calls    int
	owner    string
}

func (reader *financialGoalsReaderFake) Read(_ context.Context, ownerID string) (application.FinancialGoalsSnapshot, error) {
	reader.calls++
	reader.owner = ownerID
	if reader.err != nil {
		return application.FinancialGoalsSnapshot{}, reader.err
	}
	return reader.snapshot, nil
}

func TestListFinancialGoalsReadsOneSnapshotAndSortsDefensively(t *testing.T) {
	goalA := financialGoal(t, "goal_a", "owner-1", "Viagem", 20_000)
	goalB := financialGoal(t, "goal_b", "owner-1", "Reserva", 10_000)
	protected := protectedValue(t, "protected_a", "owner-1", "Reserva mínima", 5_000)
	reader := &financialGoalsReaderFake{snapshot: application.FinancialGoalsSnapshot{Goals: []domain.FinancialGoal{goalB, goalA}, ProtectedValues: []domain.ProtectedValue{protected}}}
	useCase, err := application.NewListFinancialGoals(reader)
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Execute(context.Background(), "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 || reader.owner != "owner-1" || len(result.Goals()) != 2 || result.Goals()[0].ID() != "goal_a" || result.Goals()[1].ID() != "goal_b" || len(result.ProtectedValues()) != 1 {
		t.Fatalf("reader/result = %#v/%#v", reader, result)
	}
	goals := result.Goals()
	goals[0] = domain.FinancialGoal{}
	if result.Goals()[0].ID() != "goal_a" {
		t.Fatal("result exposed mutable goal slice")
	}
}

func TestListFinancialGoalsValidatesBeforeReadAndRejectsCrossOwnerOrDuplicate(t *testing.T) {
	reader := &financialGoalsReaderFake{}
	useCase, _ := application.NewListFinancialGoals(reader)
	if _, err := useCase.Execute(context.Background(), ""); !errors.Is(err, application.ErrInvalidFinancialGoalsOwnerID) {
		t.Fatalf("invalid owner error = %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls = %d, want 0", reader.calls)
	}
	foreign := financialGoal(t, "goal_foreign", "owner-2", "Outra", 1)
	reader.snapshot = application.FinancialGoalsSnapshot{Goals: []domain.FinancialGoal{foreign}}
	if _, err := useCase.Execute(context.Background(), "owner-1"); !errors.Is(err, application.ErrFinancialGoalsDependency) || err.Error() != application.ErrFinancialGoalsDependency.Error() {
		t.Fatalf("cross-owner error = %v", err)
	}
	duplicate := financialGoal(t, "goal_dup", "owner-1", "Dup", 1)
	reader.snapshot = application.FinancialGoalsSnapshot{Goals: []domain.FinancialGoal{duplicate, duplicate}}
	if _, err := useCase.Execute(context.Background(), "owner-1"); !errors.Is(err, application.ErrFinancialGoalDuplicate) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestListFinancialGoalsPreservesContextAndSanitizesReaderErrors(t *testing.T) {
	reader := &financialGoalsReaderFake{err: errors.New("SELECT goals owner=owner-1 DSN=secret")}
	useCase, _ := application.NewListFinancialGoals(reader)
	_, err := useCase.Execute(context.Background(), "owner-1")
	if !errors.Is(err, application.ErrFinancialGoalsSnapshotQuery) || err.Error() != application.ErrFinancialGoalsSnapshotQuery.Error() {
		t.Fatalf("sanitized error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(cancelled, "owner-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v", err)
	}
	deadline, deadlineCancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer deadlineCancel()
	if _, err := useCase.Execute(deadline, "owner-1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
	reader.err = context.DeadlineExceeded
	if _, err := useCase.Execute(context.Background(), "owner-1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reader deadline error = %v", err)
	}
}

func TestListFinancialGoalsEmptySnapshotIsStable(t *testing.T) {
	reader := &financialGoalsReaderFake{snapshot: application.FinancialGoalsSnapshot{}}
	useCase, _ := application.NewListFinancialGoals(reader)
	result, err := useCase.Execute(context.Background(), "owner-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Goals() == nil || result.ProtectedValues() == nil || len(result.Goals()) != 0 || len(result.ProtectedValues()) != 0 {
		t.Fatalf("empty result = %#v", result)
	}
}

func financialGoal(t *testing.T, id, owner, title string, amount int64) domain.FinancialGoal {
	t.Helper()
	goal, err := domain.NewFinancialGoal(domain.FinancialGoalParams{ID: id, OwnerID: owner, Title: title, TargetAmount: money(t, amount)})
	if err != nil {
		t.Fatal(err)
	}
	return goal
}

func protectedValue(t *testing.T, id, owner, label string, amount int64) domain.ProtectedValue {
	t.Helper()
	value, err := domain.NewProtectedValue(domain.ProtectedValueParams{ID: id, OwnerID: owner, Label: label, Amount: money(t, amount)})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func money(t *testing.T, minor int64) domain.Money {
	t.Helper()
	value, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
