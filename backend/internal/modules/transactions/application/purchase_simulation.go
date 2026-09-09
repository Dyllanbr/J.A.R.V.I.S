package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingPurchaseSimulationSnapshotReader = errors.New("purchase simulation: snapshot reader is required")
	ErrInvalidPurchaseSimulationOwnerID        = errors.New("purchase simulation: invalid owner id")
	ErrInvalidPurchaseSimulationPeriod         = errors.New("purchase simulation: invalid period")
	ErrPurchaseSimulationCardNotFound          = errors.New("purchase simulation: credit card not found")
	ErrPurchaseSimulationSnapshotQuery         = errors.New("purchase simulation: snapshot query failed")
	ErrPurchaseSimulationDependency            = errors.New("purchase simulation: invalid dependency result")
)

// PurchaseSimulationSnapshotQuery is the complete owner-scoped boundary for
// one hypothetical purchase read. An adapter must return both the confirmed
// Safe Available inputs and the card metadata from one coherent snapshot.
type PurchaseSimulationSnapshotQuery struct {
	OwnerID      string
	Period       domain.SafeAvailablePeriod
	CreditCardID string
}

// PurchaseSimulationSnapshot is an adapter result, not a persisted model.
// Found is explicit so a missing card cannot be confused with a zero-valued
// card or a valid empty financial snapshot.
type PurchaseSimulationSnapshot struct {
	SafeAvailable domain.SafeAvailableSnapshot
	CreditCard    domain.CreditCard
	Found         bool
}

// PurchaseSimulationSnapshotReader is the only dependency of the use case.
// It deliberately combines the card and financial inputs to keep a future
// PostgreSQL implementation on one snapshot boundary.
type PurchaseSimulationSnapshotReader interface {
	Read(context.Context, PurchaseSimulationSnapshotQuery) (PurchaseSimulationSnapshot, error)
}

type SimulatePurchaseInput struct {
	OwnerID          string
	Period           domain.SafeAvailablePeriod
	CreditCardID     string
	AmountMinor      int64
	Currency         domain.Currency
	PurchaseOn       domain.CivilDate
	Mode             domain.PurchaseSimulationMode
	InstallmentCount int
}

type SimulatePurchaseResult struct {
	Simulation domain.PurchaseSimulationResult
}

// SimulatePurchase performs one explicit, read-only purchase simulation.
// There is no Clock, ID generator, persistence, payment or idempotency
// dependency in this use case.
type SimulatePurchase struct {
	reader PurchaseSimulationSnapshotReader
}

func NewSimulatePurchase(reader PurchaseSimulationSnapshotReader) (*SimulatePurchase, error) {
	if reader == nil {
		return nil, ErrMissingPurchaseSimulationSnapshotReader
	}
	return &SimulatePurchase{reader: reader}, nil
}

func (useCase *SimulatePurchase) Execute(ctx context.Context, input SimulatePurchaseInput) (SimulatePurchaseResult, error) {
	if err := ctx.Err(); err != nil {
		return SimulatePurchaseResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return SimulatePurchaseResult{}, ErrInvalidPurchaseSimulationOwnerID
	}
	if !validPurchaseSimulationPeriod(input.Period) {
		return SimulatePurchaseResult{}, ErrInvalidPurchaseSimulationPeriod
	}

	requestDate, err := domain.NewCivilDate(input.PurchaseOn.Year(), input.PurchaseOn.Month(), input.PurchaseOn.Day())
	if err != nil || !requestDate.Equal(input.PurchaseOn) {
		return SimulatePurchaseResult{}, domain.ErrInvalidPurchaseSimulationDate
	}
	amount, err := domain.NewMoney(input.AmountMinor, input.Currency)
	if err != nil {
		return SimulatePurchaseResult{}, domain.ErrInvalidPurchaseSimulationAmount
	}
	request, err := domain.NewPurchaseSimulationRequest(domain.PurchaseSimulationRequestParams{
		Amount:     amount,
		PurchaseOn: requestDate, Mode: input.Mode, InstallmentCount: input.InstallmentCount,
		CreditCardID: input.CreditCardID,
	})
	if err != nil {
		return SimulatePurchaseResult{}, err
	}

	snapshot, err := useCase.reader.Read(ctx, PurchaseSimulationSnapshotQuery{
		OwnerID: input.OwnerID, Period: input.Period, CreditCardID: input.CreditCardID,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return SimulatePurchaseResult{}, err
		}
		return SimulatePurchaseResult{}, newSafeOperationError(ErrPurchaseSimulationSnapshotQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return SimulatePurchaseResult{}, err
	}
	if !snapshot.Found {
		return SimulatePurchaseResult{}, ErrPurchaseSimulationCardNotFound
	}
	if snapshot.SafeAvailable.OwnerID() != input.OwnerID ||
		!snapshot.SafeAvailable.Period().StartOn().Equal(input.Period.StartOn()) ||
		!snapshot.SafeAvailable.Period().EndOn().Equal(input.Period.EndOn()) {
		return SimulatePurchaseResult{}, newSafeOperationError(ErrPurchaseSimulationDependency, domain.ErrInvalidSafeAvailableSnapshot)
	}
	if snapshot.CreditCard.ID() != input.CreditCardID || snapshot.CreditCard.UserID() != input.OwnerID {
		return SimulatePurchaseResult{}, newSafeOperationError(ErrPurchaseSimulationDependency, domain.ErrPurchaseSimulationCardIDMismatch)
	}

	simulation, err := domain.SimulatePurchase(snapshot.SafeAvailable, snapshot.CreditCard, request)
	if err != nil {
		return SimulatePurchaseResult{}, newSafeOperationError(ErrPurchaseSimulationDependency, err)
	}
	return SimulatePurchaseResult{Simulation: simulation}, nil
}

func validPurchaseSimulationPeriod(period domain.SafeAvailablePeriod) bool {
	start, end := period.StartOn(), period.EndOn()
	if start.String() == "" || end.String() == "" || end.Before(start) {
		return false
	}
	canonicalStart, startErr := domain.NewCivilDate(start.Year(), start.Month(), start.Day())
	canonicalEnd, endErr := domain.NewCivilDate(end.Year(), end.Month(), end.Day())
	return startErr == nil && endErr == nil && canonicalStart.Equal(start) && canonicalEnd.Equal(end)
}
