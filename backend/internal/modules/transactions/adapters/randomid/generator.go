package randomid

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

var ErrGeneration = errors.New("expense id: generation failed")

// Generator creates opaque 128-bit expense identifiers using the operating
// system cryptographic random source.
type Generator struct{}

// NewExpenseID returns an identifier compatible with the domain's bounded
// opaque-ID policy.
func (Generator) NewExpenseID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", ErrGeneration
	}
	return "exp_" + hex.EncodeToString(random[:]), nil
}
