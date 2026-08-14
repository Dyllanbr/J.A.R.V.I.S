package main

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
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

func TestInvalidStartupConfigurationIsRedactedFromLogs(t *testing.T) {
	tests := []struct {
		name          string
		address       string
		timeout       string
		sensitiveRaw  string
		expectedField string
	}{
		{
			name:          "address",
			address:       "synthetic-sensitive-address-log-marker",
			timeout:       "10s",
			sensitiveRaw:  "synthetic-sensitive-address-log-marker",
			expectedField: "JARVIS_HTTP_ADDRESS",
		},
		{
			name:          "shutdown timeout",
			address:       "127.0.0.1:8080",
			timeout:       "synthetic-sensitive-duration-log-marker",
			sensitiveRaw:  "synthetic-sensitive-duration-log-marker",
			expectedField: "JARVIS_SHUTDOWN_TIMEOUT",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("JARVIS_HTTP_ADDRESS", test.address)
			t.Setenv("JARVIS_SHUTDOWN_TIMEOUT", test.timeout)

			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			err := run(logger)
			if err == nil {
				t.Fatal("run() returned nil error for invalid configuration")
			}

			logger.Error("application stopped", "error", err)
			logOutput := output.String()
			if strings.Contains(logOutput, test.sensitiveRaw) {
				t.Fatal("application log exposed the raw invalid value")
			}
			if !strings.Contains(logOutput, test.expectedField) {
				t.Fatalf("application log does not identify %s", test.expectedField)
			}
		})
	}
}
