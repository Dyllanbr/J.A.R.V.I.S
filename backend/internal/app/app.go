package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"jarvis/backend/internal/config"
	"jarvis/backend/internal/platform/httpserver"
)

// App owns the process-level lifecycle of the backend.
type App struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

// New composes the application with explicit dependencies.
func New(cfg config.Config, logger *slog.Logger) *App {
	return &App{
		server:          httpserver.New(cfg.HTTPAddress, logger),
		logger:          logger,
		shutdownTimeout: cfg.ShutdownTimeout,
	}
}

// Run serves requests until the context is canceled or the server fails.
func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.server.Addr, err)
	}
	return a.serve(ctx, listener)
}

func (a *App) serve(ctx context.Context, listener net.Listener) error {
	serverErr := make(chan error, 1)

	go func() {
		a.logger.Info("http server started", "address", listener.Addr().String())
		serverErr <- a.server.Serve(listener)
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		_ = a.server.Close()
		return fmt.Errorf("shutdown http server: %w", err)
	}

	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("stop http server: %w", err)
	}

	a.logger.Info("http server stopped")
	return nil
}
