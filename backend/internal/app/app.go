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
	"jarvis/backend/internal/modules/transactions/adapters/httpapi"
	transactionspostgres "jarvis/backend/internal/modules/transactions/adapters/postgres"
	"jarvis/backend/internal/modules/transactions/adapters/randomid"
	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
	"jarvis/backend/internal/platform/httpserver"
	platformpostgres "jarvis/backend/internal/platform/postgres"
)

var ErrInvalidFinancialOwner = errors.New("application: JARVIS_OWNER_ID is invalid")

// App owns the process-level lifecycle of the backend.
type App struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	closeResources  func()
}

// New composes the application with explicit dependencies.
func New(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	getenv func(string) string,
) (*App, error) {
	applicationInstance := &App{
		logger:          logger,
		shutdownTimeout: cfg.ShutdownTimeout,
		closeResources:  func() {},
	}
	if !cfg.FinancialAPIEnabled {
		applicationInstance.server = httpserver.New(cfg.HTTPAddress, logger)
		return applicationInstance, nil
	}
	if err := domain.ValidateUserID(cfg.OwnerID); err != nil {
		return nil, ErrInvalidFinancialOwner
	}

	postgresConfig, err := platformpostgres.LoadConfig(getenv)
	if err != nil {
		return nil, err
	}
	pool, err := platformpostgres.Open(ctx, postgresConfig)
	if err != nil {
		return nil, err
	}
	applicationInstance.closeResources = pool.Close

	repository, err := transactionspostgres.NewExpenseRepository(pool, postgresConfig.OperationTimeout)
	if err != nil {
		pool.Close()
		return nil, err
	}
	recordExpense, err := application.NewRecordExpense(repository, randomid.Generator{}, systemClock{})
	if err != nil {
		pool.Close()
		return nil, err
	}
	recordIncome, err := application.NewRecordIncome(repository, randomid.Generator{}, systemClock{})
	if err != nil {
		pool.Close()
		return nil, err
	}
	listTransactions, err := application.NewListTransactionsByMonth(repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	financialRoutes := httpapi.New(
		cfg.OwnerID,
		application.PreviewExpense{},
		application.PreviewIncome{},
		recordExpense,
		recordIncome,
		listTransactions,
	)
	applicationInstance.server = httpserver.New(cfg.HTTPAddress, logger, financialRoutes)
	return applicationInstance, nil
}

// Run serves requests until the context is canceled or the server fails.
func (a *App) Run(ctx context.Context) error {
	defer a.closeResources()
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.server.Addr, err)
	}
	return a.serve(ctx, listener)
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
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
