package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
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

	application := New(config.Config{
		HTTPAddress:     listener.Addr().String(),
		ShutdownTimeout: time.Second,
	}, discardLogger())

	if err := application.Run(context.Background()); err == nil {
		t.Fatal("Run() returned nil error for an occupied address")
	}
}

func TestRunHonorsPreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("create test listener: %v", err)
	}

	application := New(config.Config{
		HTTPAddress:     listener.Addr().String(),
		ShutdownTimeout: time.Second,
	}, discardLogger())

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
	application := New(config.Config{
		HTTPAddress:     address,
		ShutdownTimeout: 2 * time.Second,
	}, discardLogger())

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
