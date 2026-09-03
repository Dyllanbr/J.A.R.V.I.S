package randomid

import (
	"errors"
	"strings"
	"testing"
)

func TestInstallmentPlanGeneratorProducesOpaqueLowercaseHexID(t *testing.T) {
	reader := &countingEntropyReader{}
	id, err := (InstallmentPlanGenerator{reader: reader}).NewInstallmentPlanID()
	if err != nil {
		t.Fatalf("NewInstallmentPlanID() error = %v", err)
	}
	if !strings.HasPrefix(id, "ipl_") || len(id) != len("ipl_")+32 {
		t.Fatalf("generated ID = %q, want ipl_ plus 32 hex characters", id)
	}
	for _, character := range id[len("ipl_"):] {
		if !strings.ContainsRune("0123456789abcdef", character) {
			t.Fatalf("generated ID contains non-lowercase-hex character %q", character)
		}
	}
	if reader.bytesRead != 16 {
		t.Fatalf("entropy bytes read = %d, want 16", reader.bytesRead)
	}
}

func TestInstallmentPlanGeneratorReturnsSafeErrorWhenEntropyFails(t *testing.T) {
	id, err := (InstallmentPlanGenerator{reader: failingEntropyReader{}}).NewInstallmentPlanID()
	if id != "" {
		t.Fatalf("NewInstallmentPlanID() ID = %q, want empty", id)
	}
	if !errors.Is(err, ErrInstallmentPlanGeneration) {
		t.Fatalf("NewInstallmentPlanID() error = %v, want ErrInstallmentPlanGeneration", err)
	}
}
