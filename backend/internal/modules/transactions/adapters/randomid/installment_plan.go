package randomid

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

var ErrInstallmentPlanGeneration = errors.New("installment plan id: generation failed")

// InstallmentPlanGenerator creates opaque 128-bit plan identifiers using the
// operating system's cryptographically secure random source.
type InstallmentPlanGenerator struct {
	reader io.Reader
}

func NewInstallmentPlanGenerator() InstallmentPlanGenerator {
	return InstallmentPlanGenerator{reader: rand.Reader}
}

func (generator InstallmentPlanGenerator) NewInstallmentPlanID() (string, error) {
	var random [16]byte
	if generator.reader == nil {
		return "", ErrInstallmentPlanGeneration
	}
	if _, err := io.ReadFull(generator.reader, random[:]); err != nil {
		return "", ErrInstallmentPlanGeneration
	}
	return "ipl_" + hex.EncodeToString(random[:]), nil
}
