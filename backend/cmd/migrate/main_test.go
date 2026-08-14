package main

import (
	"context"
	"errors"
	"testing"
)

func TestRunRejectsInvalidCommandsBeforeLoadingConfiguration(t *testing.T) {
	tests := [][]string{nil, {}, {"status"}, {"up", "down"}}
	for _, arguments := range tests {
		configurationRead := false
		err := run(context.Background(), arguments, func(string) string {
			configurationRead = true
			return ""
		})
		if !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("run(%v) error = %v, want %v", arguments, err, ErrInvalidCommand)
		}
		if configurationRead {
			t.Fatalf("run(%v) read configuration for an invalid command", arguments)
		}
	}
}
