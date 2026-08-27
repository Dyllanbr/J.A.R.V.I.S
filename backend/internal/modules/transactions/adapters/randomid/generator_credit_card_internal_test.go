package randomid

import (
	"errors"
	"io"
	"testing"
)

type failingEntropyReader struct{}

func (failingEntropyReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestCreditCardGeneratorReturnsSafeErrorWhenEntropyFails(t *testing.T) {
	generator := CreditCardGenerator{reader: failingEntropyReader{}}

	id, err := generator.NewCreditCardID()
	if id != "" {
		t.Fatalf("NewCreditCardID() ID = %q, want empty", id)
	}
	if !errors.Is(err, ErrCreditCardGeneration) {
		t.Fatalf("NewCreditCardID() error = %v, want ErrCreditCardGeneration", err)
	}
}

func TestCreditCardGeneratorConsumesExactly128Bits(t *testing.T) {
	reader := &countingEntropyReader{}
	generator := CreditCardGenerator{reader: reader}

	if _, err := generator.NewCreditCardID(); err != nil {
		t.Fatalf("NewCreditCardID() error = %v", err)
	}
	if reader.bytesRead != 16 {
		t.Fatalf("entropy bytes read = %d, want 16", reader.bytesRead)
	}
}

type countingEntropyReader struct {
	bytesRead int
}

func (reader *countingEntropyReader) Read(destination []byte) (int, error) {
	reader.bytesRead += len(destination)
	for index := range destination {
		destination[index] = byte(index)
	}
	return len(destination), nil
}
