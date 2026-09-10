package randomid

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

var (
	ErrSessionIDGeneration         = errors.New("session id: generation failed")
	ErrSessionCredentialGeneration = errors.New("session credential: generation failed")
)

// SessionIDGenerator creates opaque session identifiers using cryptographic
// randomness. The prefix makes operational inspection unambiguous without
// embedding owner or credential data.
type SessionIDGenerator struct{ reader io.Reader }

func NewSessionIDGenerator() SessionIDGenerator { return SessionIDGenerator{reader: rand.Reader} }

func (generator SessionIDGenerator) NewSessionID() (string, error) {
	var random [16]byte
	if generator.reader == nil {
		return "", ErrSessionIDGeneration
	}
	if _, err := io.ReadFull(generator.reader, random[:]); err != nil {
		return "", ErrSessionIDGeneration
	}
	return "ses_" + hex.EncodeToString(random[:]), nil
}

// SessionCredentialGenerator creates a URL-safe opaque bearer credential.
// The raw value is returned only to the caller; persistence receives its
// digest through the application boundary.
type SessionCredentialGenerator struct{ reader io.Reader }

func NewSessionCredentialGenerator() SessionCredentialGenerator {
	return SessionCredentialGenerator{reader: rand.Reader}
}

func (generator SessionCredentialGenerator) NewSessionCredential() (string, error) {
	var random [32]byte
	if generator.reader == nil {
		return "", ErrSessionCredentialGeneration
	}
	if _, err := io.ReadFull(generator.reader, random[:]); err != nil {
		return "", ErrSessionCredentialGeneration
	}
	return "tok_" + base64.RawURLEncoding.EncodeToString(random[:]), nil
}
