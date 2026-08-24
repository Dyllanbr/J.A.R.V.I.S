package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jarvis/backend/internal/config"
)

func TestRunReturnsBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	defer listener.Close()

	application := newTestApp(t, config.Config{
		HTTPAddress:     listener.Addr().String(),
		ShutdownTimeout: time.Second,
	})

	if err := application.Run(context.Background()); err == nil {
		t.Fatal("Run() returned nil error for an occupied address")
	}
}

func TestHealthOnlyCompositionDoesNotRegisterFinancialRoutes(t *testing.T) {
	application := newTestApp(t, config.Config{
		HTTPAddress:     "127.0.0.1:8080",
		ShutdownTimeout: time.Second,
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", nil)
	response := httptest.NewRecorder()
	application.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("financial route status = %d, want 404 in health-only mode", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/recurrences", nil)
	response = httptest.NewRecorder()
	application.server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("recurrence route status = %d, want 404 in health-only mode", response.Code)
	}
}

func TestRunHonorsPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("create test listener: %v", err)
	}

	application := newTestApp(t, config.Config{
		HTTPAddress:     listener.Addr().String(),
		ShutdownTimeout: time.Second,
	})

	if err := application.serve(ctx, listener); err != nil {
		t.Fatalf("serve() returned an error for canceled context: %v", err)
	}
}

func TestRunGracefullyShutsDown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("create test listener: %v", err)
	}
	address := listener.Addr().String()
	application := newTestApp(t, config.Config{
		HTTPAddress:     address,
		ShutdownTimeout: 2 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- application.serve(ctx, listener)
	}()

	waitForHealth(t, "http://"+address+"/healthz", result)
	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() returned an error during graceful shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not finish within the graceful shutdown bound")
	}

	connection, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
	if err == nil {
		connection.Close()
		t.Fatal("server still accepted connections after shutdown")
	}
}

func waitForHealth(t *testing.T, url string, result <-chan error) {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-result:
			t.Fatalf("server exited before readiness: %v", err)
		default:
		}

		response, err := client.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestApp(t *testing.T, cfg config.Config) *App {
	t.Helper()
	application, err := New(context.Background(), cfg, discardLogger(), func(string) string {
		t.Fatal("health-only composition read PostgreSQL configuration")
		return ""
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return application
}

func TestNewRejectsInvalidFinancialOwnerBeforeDatabaseConfiguration(t *testing.T) {
	configurationRead := false
	_, err := New(context.Background(), config.Config{
		HTTPAddress:         "127.0.0.1:8080",
		ShutdownTimeout:     time.Second,
		FinancialAPIEnabled: true,
		OwnerID:             " owner ",
	}, discardLogger(), func(string) string {
		configurationRead = true
		return ""
	})
	if err != ErrInvalidFinancialOwner {
		t.Fatalf("New() error = %v, want ErrInvalidFinancialOwner", err)
	}
	if configurationRead {
		t.Fatal("New() loaded database configuration for an invalid owner")
	}
}
