package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewExpenseRepositoryValidatesDependencies(t *testing.T) {
	if _, err := NewExpenseRepository(nil, time.Second); !errors.Is(err, ErrMissingPool) {
		t.Fatalf("NewExpenseRepository(nil) error = %v, want %v", err, ErrMissingPool)
	}

	if _, err := NewExpenseRepository(&pgxpool.Pool{}, 0); !errors.Is(err, ErrInvalidTimeout) {
		t.Fatalf("NewExpenseRepository(pool, 0) error = %v, want %v", err, ErrInvalidTimeout)
	}
	if _, err := NewExpenseRepository(&pgxpool.Pool{}, 31*time.Second); !errors.Is(err, ErrInvalidTimeout) {
		t.Fatalf("NewExpenseRepository(pool, 31s) error = %v, want %v", err, ErrInvalidTimeout)
	}
}

func TestRepositoryErrorRedactsCauseMessage(t *testing.T) {
	const marker = "SUPER_SECRET_MARKER_98431"
	cause := errors.New(marker)
	err := newRepositoryError(ErrInsertExpense, cause)

	if !errors.Is(err, ErrInsertExpense) || !errors.Is(err, cause) {
		t.Fatal("repository error did not preserve categorical matching")
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatal("repository error exposed the underlying message")
	}
}
