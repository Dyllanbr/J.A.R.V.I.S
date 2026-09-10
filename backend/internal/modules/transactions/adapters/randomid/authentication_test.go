package randomid

import (
	"errors"
	"strings"
	"testing"
)

func TestSessionGeneratorsProduceOpaqueDistinctValues(t *testing.T) {
	idGenerator := NewSessionIDGenerator()
	firstID, err := idGenerator.NewSessionID()
	if err != nil {
		t.Fatalf("first NewSessionID() error = %v", err)
	}
	secondID, err := idGenerator.NewSessionID()
	if err != nil {
		t.Fatalf("second NewSessionID() error = %v", err)
	}
	if firstID == secondID || !strings.HasPrefix(firstID, "ses_") || len(firstID) != len("ses_")+32 {
		t.Fatalf("session IDs = %q/%q", firstID, secondID)
	}

	credentialGenerator := NewSessionCredentialGenerator()
	firstCredential, err := credentialGenerator.NewSessionCredential()
	if err != nil {
		t.Fatalf("first NewSessionCredential() error = %v", err)
	}
	secondCredential, err := credentialGenerator.NewSessionCredential()
	if err != nil {
		t.Fatalf("second NewSessionCredential() error = %v", err)
	}
	if firstCredential == secondCredential || !strings.HasPrefix(firstCredential, "tok_") || len(firstCredential) != len("tok_")+43 {
		t.Fatalf("session credentials are not opaque URL-safe values: %q/%q", firstCredential, secondCredential)
	}
	for _, credential := range []string{firstCredential, secondCredential} {
		for index := 0; index < len(credential); index++ {
			if credential[index] < '!' || credential[index] > '~' {
				t.Fatalf("credential contains non-header-safe byte %d", credential[index])
			}
		}
	}
}

func TestSessionGeneratorsReturnSafeErrorsWhenEntropyFails(t *testing.T) {
	if id, err := (SessionIDGenerator{reader: failingEntropyReader{}}).NewSessionID(); id != "" || !errors.Is(err, ErrSessionIDGeneration) {
		t.Fatalf("NewSessionID() = %q/%v, want safe generation error", id, err)
	}
	if credential, err := (SessionCredentialGenerator{reader: failingEntropyReader{}}).NewSessionCredential(); credential != "" || !errors.Is(err, ErrSessionCredentialGeneration) {
		t.Fatalf("NewSessionCredential() = %q/%v, want safe generation error", credential, err)
	}
}
