package migrations

import (
	"errors"
	"strings"
	"testing"
)

func TestMigrationErrorRedactsCauseMessage(t *testing.T) {
	const marker = "SUPER_SECRET_MARKER_98431"
	cause := errors.New(marker)
	err := newMigrationError(ErrApply, cause)

	if !errors.Is(err, ErrApply) || !errors.Is(err, cause) {
		t.Fatal("migration error did not preserve categorical matching")
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatal("migration error exposed the underlying message")
	}
}
