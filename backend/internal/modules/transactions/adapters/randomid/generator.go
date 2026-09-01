package randomid

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
)

var ErrGeneration = errors.New("expense id: generation failed")

var ErrIncomeGeneration = errors.New("income id: generation failed")

var ErrRecurrenceGeneration = errors.New("recurrence id: generation failed")

var ErrCreditCardGeneration = errors.New("credit card id: generation failed")

// Generator creates opaque 128-bit financial aggregate identifiers using the
// operating system cryptographic random source.
type Generator struct{}

// CreditCardGenerator keeps its entropy source private so production always
// uses crypto/rand while the package can prove failure handling deterministically.
type CreditCardGenerator struct {
	reader io.Reader
}

func NewCreditCardGenerator() CreditCardGenerator {
	return CreditCardGenerator{reader: rand.Reader}
}

// NewExpenseID returns an identifier compatible with the domain's bounded
// opaque-ID policy.
func (Generator) NewExpenseID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", ErrGeneration
	}
	return "exp_" + hex.EncodeToString(random[:]), nil
}

// NewIncomeID returns an opaque identifier with an Income-specific prefix.
func (Generator) NewIncomeID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", ErrIncomeGeneration
	}
	return "inc_" + hex.EncodeToString(random[:]), nil
}

// NewRecurrenceID returns an opaque identifier with a Recurrence-specific prefix.
func (Generator) NewRecurrenceID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", ErrRecurrenceGeneration
	}
	return "rec_" + hex.EncodeToString(random[:]), nil
}

// NewCreditCardID returns an opaque 128-bit identifier with a card-specific prefix.
func (generator CreditCardGenerator) NewCreditCardID() (string, error) {
	var random [16]byte
	if generator.reader == nil {
		return "", ErrCreditCardGeneration
	}
	if _, err := io.ReadFull(generator.reader, random[:]); err != nil {
		return "", ErrCreditCardGeneration
	}
	return "card_" + hex.EncodeToString(random[:]), nil
}
