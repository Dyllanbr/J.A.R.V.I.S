//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	adapter "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestFinancialGoalsPostgresSnapshotAndOwnerIsolation(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const ownerA = "usr_financial_goal_owner_a"
	const ownerB = "usr_financial_goal_owner_b"
	insertSyntheticUser(t, ctx, pool, ownerA)
	insertSyntheticUser(t, ctx, pool, ownerB)

	withConnection(t, ctx, pool, func(connection *pgx.Conn) {
		assertMigrationVersion(t, ctx, connection, 10)
	})
	assertTableExists(t, ctx, pool, "financial_goals", true)
	assertTableExists(t, ctx, pool, "protected_values", true)

	repository, err := adapter.NewFinancialGoalsRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewFinancialGoalsRepository() error = %v", err)
	}
	setGoal, err := application.NewReplaceFinancialGoal(repository)
	if err != nil {
		t.Fatalf("NewReplaceFinancialGoal() error = %v", err)
	}
	setValue, err := application.NewReplaceProtectedValue(repository)
	if err != nil {
		t.Fatalf("NewReplaceProtectedValue() error = %v", err)
	}
	goalAmount, err := domain.NewMoney(125000, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	valueAmount, err := domain.NewMoney(0, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setGoal.Execute(ctx, application.ReplaceFinancialGoalInput{OwnerID: ownerA, ID: "goal_a", Title: "Viagem", TargetAmount: goalAmount}); err != nil {
		t.Fatalf("goal replacement failed: %v", err)
	}
	if _, err := setValue.Execute(ctx, application.ReplaceProtectedValueInput{OwnerID: ownerA, ID: "protected_a", Label: "Reserva", Amount: valueAmount}); err != nil {
		t.Fatalf("protected value replacement failed: %v", err)
	}

	list, err := application.NewListFinancialGoals(repository)
	if err != nil {
		t.Fatalf("NewListFinancialGoals() error = %v", err)
	}
	resultA, err := list.Execute(ctx, ownerA)
	if err != nil {
		t.Fatalf("owner A list failed: %v", err)
	}
	if len(resultA.Goals()) != 1 || resultA.Goals()[0].ID() != "goal_a" || resultA.Goals()[0].TargetAmount().MinorUnits() != 125000 {
		t.Fatalf("owner A goals = %#v", resultA.Goals())
	}
	if len(resultA.ProtectedValues()) != 1 || resultA.ProtectedValues()[0].ID() != "protected_a" || resultA.ProtectedValues()[0].Amount().MinorUnits() != 0 {
		t.Fatalf("owner A protected values = %#v", resultA.ProtectedValues())
	}
	resultB, err := list.Execute(ctx, ownerB)
	if err != nil {
		t.Fatalf("owner B list failed: %v", err)
	}
	if len(resultB.Goals()) != 0 || len(resultB.ProtectedValues()) != 0 {
		t.Fatalf("owner B leaked owner A data: %#v %#v", resultB.Goals(), resultB.ProtectedValues())
	}

	beforeGoals := countRows(t, ctx, pool, "financial_goals")
	beforeValues := countRows(t, ctx, pool, "protected_values")
	if _, err := list.Execute(ctx, ownerA); err != nil {
		t.Fatalf("replay list failed: %v", err)
	}
	if countRows(t, ctx, pool, "financial_goals") != beforeGoals || countRows(t, ctx, pool, "protected_values") != beforeValues {
		t.Fatal("snapshot reads changed declaration row counts")
	}
}

func TestFinancialGoalsPostgresPreservesContextAndSanitizesErrors(t *testing.T) {
	pool := newMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	repository, err := adapter.NewFinancialGoalsRepository(pool, 5*time.Second)
	if err != nil {
		t.Fatalf("NewFinancialGoalsRepository() error = %v", err)
	}
	owner := "usr_financial_goal_context_owner"
	insertSyntheticUser(t, ctx, pool, owner)
	cancelled, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	if _, err := repository.Read(cancelled, owner); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v, want context.Canceled", err)
	}
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if _, err := repository.Read(deadline, owner); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline read error = %v, want context.DeadlineExceeded", err)
	}
	payload := "postgres://jarvis:jarvis@localhost:55432/jarvis_test"
	pool.Close()
	_, err = repository.Read(context.Background(), owner)
	if err == nil || strings.Contains(err.Error(), payload) || strings.Contains(strings.ToLower(err.Error()), "select") {
		t.Fatalf("database error was not sanitized: %v", err)
	}
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count %s failed: %v", table, err)
	}
	return count
}
