package application_test

import (
	"context"
	"errors"
	"testing"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestReplaceFinancialGoalValidatesBeforeSingleStoreCall(t *testing.T) {
	store := &financialGoalStoreSpy{}
	useCase, err := application.NewReplaceFinancialGoal(store)
	if err != nil {
		t.Fatalf("NewReplaceFinancialGoal() error = %v", err)
	}
	amount, err := domain.NewMoney(25_000, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	goal, err := useCase.Execute(context.Background(), application.ReplaceFinancialGoalInput{
		OwnerID: "owner-1", ID: "goal-1", Title: "Viagem", TargetAmount: amount,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if store.calls != 1 || !store.goal.Equal(goal) {
		t.Fatalf("store calls/goal = %d/%#v, want one exact declaration", store.calls, store.goal)
	}

	if _, err := useCase.Execute(context.Background(), application.ReplaceFinancialGoalInput{
		OwnerID: "", ID: "goal-2", Title: "Inválida", TargetAmount: amount,
	}); !errors.Is(err, application.ErrInvalidFinancialGoalInput) {
		t.Fatalf("invalid input error = %v, want ErrInvalidFinancialGoalInput", err)
	}
	if store.calls != 1 {
		t.Fatalf("invalid input reached store: calls = %d", store.calls)
	}
}

func TestReplaceProtectedValuePreservesContextAndSanitizesDependencyErrors(t *testing.T) {
	dependencyError := errors.New("postgres://secret/select protected_values")
	store := &protectedValueStoreSpy{err: dependencyError}
	useCase, err := application.NewReplaceProtectedValue(store)
	if err != nil {
		t.Fatalf("NewReplaceProtectedValue() error = %v", err)
	}
	amount, err := domain.NewMoney(0, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = useCase.Execute(context.Background(), application.ReplaceProtectedValueInput{
		OwnerID: "owner-1", ID: "protected-1", Label: "Reserva", Amount: amount,
	})
	if err == nil || err.Error() != application.ErrProtectedValuePersistence.Error() || !errors.Is(err, application.ErrProtectedValuePersistence) {
		t.Fatalf("dependency error = %v, want sanitized persistence category", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := useCase.Execute(cancelled, application.ReplaceProtectedValueInput{
		OwnerID: "owner-1", ID: "protected-2", Label: "Reserva", Amount: amount,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v, want context.Canceled", err)
	}
	if store.calls != 1 {
		t.Fatalf("cancelled input reached store: calls = %d", store.calls)
	}
}

type financialGoalStoreSpy struct {
	calls int
	goal  domain.FinancialGoal
}

func (store *financialGoalStoreSpy) ReplaceFinancialGoal(_ context.Context, goal domain.FinancialGoal) error {
	store.calls++
	store.goal = goal
	return nil
}

type protectedValueStoreSpy struct {
	calls int
	err   error
}

func (store *protectedValueStoreSpy) ReplaceProtectedValue(_ context.Context, _ domain.ProtectedValue) error {
	store.calls++
	return store.err
}
