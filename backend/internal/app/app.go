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
	previewExpense, err := application.NewPreviewExpenseWithCategoryCatalog(repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	previewIncome, err := application.NewPreviewIncomeWithCategoryCatalog(repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	recordExpense, err := application.NewRecordExpenseWithCategoryCatalog(repository, randomid.Generator{}, systemClock{}, repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	legacyCreditGuard, err := application.NewLegacyExpenseCreditGuard(repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	recordIncome, err := application.NewRecordIncomeWithCategoryCatalog(repository, randomid.Generator{}, systemClock{}, repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	listTransactions, err := application.NewListTransactionsByMonth(repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	listCategories, err := application.NewListCategories(repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	financialRoutes := httpapi.NewWithLegacyCreditGuard(
		cfg.OwnerID,
		previewExpense,
		previewIncome,
		recordExpense,
		recordIncome,
		listTransactions,
		listCategories,
		legacyCreditGuard,
	)
	recurrenceRepository, err := transactionspostgres.NewRecurrenceRepository(pool, postgresConfig.OperationTimeout)
	if err != nil {
		pool.Close()
		return nil, err
	}
	recordRecurrence, err := application.NewRecordRecurrence(
		recurrenceRepository,
		recurrenceRepository,
		randomid.Generator{},
		systemClock{},
	)
	if err != nil {
		pool.Close()
		return nil, err
	}
	listRecurrences, err := application.NewListRecurrences(recurrenceRepository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	cancelRecurrence, err := application.NewCancelRecurrence(recurrenceRepository, recurrenceRepository, systemClock{})
	if err != nil {
		pool.Close()
		return nil, err
	}
	recurrenceRoutes := httpapi.NewRecurrence(
		cfg.OwnerID,
		application.PreviewRecurrence{},
		recordRecurrence,
		listRecurrences,
		cancelRecurrence,
	)
	recurrenceSuggestionRepository, err := transactionspostgres.NewRecurrenceSuggestionRepository(pool, postgresConfig.OperationTimeout)
	if err != nil {
		pool.Close()
		return nil, err
	}
	listRecurrenceSuggestions, err := application.NewListRecurrenceSuggestions(
		recurrenceSuggestionRepository,
		recurrenceRepository,
		recurrenceSuggestionRepository,
		systemClock{},
	)
	if err != nil {
		pool.Close()
		return nil, err
	}
	dismissRecurrenceSuggestion, err := application.NewDismissRecurrenceSuggestion(
		listRecurrenceSuggestions,
		recurrenceSuggestionRepository,
		recurrenceSuggestionRepository,
		systemClock{},
	)
	if err != nil {
		pool.Close()
		return nil, err
	}
	prepareSuggestedRecurrence, err := application.NewPrepareSuggestedRecurrence(listRecurrenceSuggestions)
	if err != nil {
		pool.Close()
		return nil, err
	}
	recurrenceSuggestionRoutes := httpapi.NewRecurrenceSuggestion(
		cfg.OwnerID,
		listRecurrenceSuggestions,
		dismissRecurrenceSuggestion,
		prepareSuggestedRecurrence,
	)
	creditCardRepository, err := transactionspostgres.NewCreditCardRepository(pool, postgresConfig.OperationTimeout)
	if err != nil {
		pool.Close()
		return nil, err
	}
	recordCreditCard, err := application.NewRecordCreditCard(
		creditCardRepository,
		creditCardRepository,
		randomid.NewCreditCardGenerator(),
		systemClock{},
	)
	if err != nil {
		pool.Close()
		return nil, err
	}
	listCreditCards, err := application.NewListCreditCards(creditCardRepository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	getCreditCard, err := application.NewGetCreditCard(creditCardRepository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	archiveCreditCard, err := application.NewArchiveCreditCard(
		creditCardRepository,
		creditCardRepository,
		creditCardRepository,
		systemClock{},
	)
	if err != nil {
		pool.Close()
		return nil, err
	}
	creditCardRoutes := httpapi.NewCreditCard(
		cfg.OwnerID,
		application.PreviewCreditCard{},
		recordCreditCard,
		listCreditCards,
		getCreditCard,
		archiveCreditCard,
	)
	cardPurchaseRepository, err := transactionspostgres.NewCardPurchaseRepository(pool, postgresConfig.OperationTimeout)
	if err != nil {
		pool.Close()
		return nil, err
	}
	previewCardPurchase, err := application.NewPreviewCardPurchaseWithCategoryCatalog(creditCardRepository, repository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	recordCardPurchase, err := application.NewRecordCardPurchase(
		cardPurchaseRepository,
		cardPurchaseRepository,
		creditCardRepository,
		repository,
		randomid.Generator{},
		randomid.NewInstallmentPlanGenerator(),
		systemClock{},
	)
	if err != nil {
		pool.Close()
		return nil, err
	}
	listInstallmentPlans, err := application.NewListInstallmentPlans(cardPurchaseRepository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	getInstallmentPlan, err := application.NewGetInstallmentPlan(cardPurchaseRepository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	previewInstallmentCancellation, err := application.NewPreviewInstallmentPlanCancellation(cardPurchaseRepository, financialDateProvider{})
	if err != nil {
		pool.Close()
		return nil, err
	}
	cancelInstallmentPlan, err := application.NewCancelInstallmentPlan(cardPurchaseRepository, cardPurchaseRepository, cardPurchaseRepository, financialDateProvider{})
	if err != nil {
		pool.Close()
		return nil, err
	}
	scheduledCommitmentRepository, err := transactionspostgres.NewScheduledCommitmentRepository(pool, postgresConfig.OperationTimeout)
	if err != nil {
		pool.Close()
		return nil, err
	}
	listScheduledCommitments, err := application.NewListScheduledCommitments(scheduledCommitmentRepository)
	if err != nil {
		pool.Close()
		return nil, err
	}
	cardPurchaseRoutes := httpapi.NewCardPurchase(cfg.OwnerID, previewCardPurchase, recordCardPurchase)
	installmentPlanRoutes := httpapi.NewInstallmentPlan(cfg.OwnerID, listInstallmentPlans, getInstallmentPlan, previewInstallmentCancellation, cancelInstallmentPlan)
	scheduledCommitmentRoutes := httpapi.NewScheduledCommitments(cfg.OwnerID, listScheduledCommitments)
	applicationInstance.server = httpserver.New(
		cfg.HTTPAddress,
		logger,
		financialRoutes,
		recurrenceRoutes,
		recurrenceSuggestionRoutes,
		creditCardRoutes,
		cardPurchaseRoutes,
		installmentPlanRoutes,
		scheduledCommitmentRoutes,
	)
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
