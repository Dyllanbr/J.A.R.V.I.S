package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	newHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	if contentTypeOptions := response.Header().Get("X-Content-Type-Options"); contentTypeOptions != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", contentTypeOptions)
	}
	if body := response.Body.String(); body != "{\"status\":\"ok\"}" {
		t.Fatalf("body = %q, want exact health response", body)
	}
}

func TestHealthRejectsUnsupportedMethods(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/healthz", strings.NewReader("sensitive-test-marker"))
	response := httptest.NewRecorder()

	newHandler().ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if body := response.Body.String(); body != "Method Not Allowed\n" {
		t.Fatalf("body = %q, want generic method error", body)
	}
	if strings.Contains(response.Body.String(), "sensitive-test-marker") {
		t.Fatal("response body exposed request data")
	}
}

func TestNewConfiguresResourceLimits(t *testing.T) {
	server := New("127.0.0.1:8080", testLogger())

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{name: "read header", got: server.ReadHeaderTimeout, want: 5 * time.Second},
		{name: "read", got: server.ReadTimeout, want: 10 * time.Second},
		{name: "write", got: server.WriteTimeout, want: 10 * time.Second},
		{name: "idle", got: server.IdleTimeout, want: 60 * time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("timeout = %v, want %v", test.got, test.want)
			}
		})
	}

	if server.MaxHeaderBytes != maxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, maxHeaderBytes)
	}
}
