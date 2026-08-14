package main

import (
	"io"
	"log/slog"
	"testing"
)

func TestRunRejectsInvalidStartupConfiguration(t *testing.T) {
	t.Setenv("JARVIS_HTTP_ADDRESS", "127.0.0.1:0")
	t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", "10s")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := run(logger); err == nil {
		t.Fatal("run() returned nil error for invalid startup configuration")
	}
}
