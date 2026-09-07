package application

import (
	"context"
	"errors"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingSafeAvailableSnapshotReader = errors.New("safe available: snapshot reader is required")
	ErrInvalidSafeAvailableOwnerID        = errors.New("safe available: invalid owner id")
	ErrInvalidSafeAvailablePeriod         = errors.New("safe available: invalid period")
	ErrSafeAvailableSnapshotQuery         = errors.New("safe available: snapshot query failed")
	ErrSafeAvailableDependency            = errors.New("safe available: invalid dependency result")
)

// SafeAvailableSnapshotQuery is the explicit owner and civil-period boundary
// for one read. Implementations must return a snapshot without writing.
type SafeAvailableSnapshotQuery struct {
	OwnerID string
	Period  domain.SafeAvailablePeriod
}

// SafeAvailableSnapshotReader is the only dependency used by the use case.
// A future adapter can populate all confirmed inputs in one consistent read.
type SafeAvailableSnapshotReader interface {
	Read(context.Context, SafeAvailableSnapshotQuery) (domain.SafeAvailableSnapshot, error)
}

type CalculateSafeAvailableInput struct {
	OwnerID string
	Period  domain.SafeAvailablePeriod
}

type CalculateSafeAvailableResult struct {
	Available domain.SafeAvailableResult
}

// CalculateSafeAvailable validates its boundary, performs exactly one
// read-only snapshot call and delegates the deterministic arithmetic to the
// domain. It has no Clock, ID generator, persistence or idempotency dependency.
type CalculateSafeAvailable struct {
	reader SafeAvailableSnapshotReader
}

func NewCalculateSafeAvailable(reader SafeAvailableSnapshotReader) (*CalculateSafeAvailable, error) {
	if reader == nil {
		return nil, ErrMissingSafeAvailableSnapshotReader
	}
	return &CalculateSafeAvailable{reader: reader}, nil
}

func (useCase *CalculateSafeAvailable) Execute(ctx context.Context, input CalculateSafeAvailableInput) (CalculateSafeAvailableResult, error) {
	if err := ctx.Err(); err != nil {
		return CalculateSafeAvailableResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return CalculateSafeAvailableResult{}, ErrInvalidSafeAvailableOwnerID
	}
	if input.Period.StartOn().String() == "" || input.Period.EndOn().String() == "" || input.Period.EndOn().Before(input.Period.StartOn()) {
		return CalculateSafeAvailableResult{}, ErrInvalidSafeAvailablePeriod
	}

	snapshot, err := useCase.reader.Read(ctx, SafeAvailableSnapshotQuery{OwnerID: input.OwnerID, Period: input.Period})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CalculateSafeAvailableResult{}, err
		}
		return CalculateSafeAvailableResult{}, newSafeOperationError(ErrSafeAvailableSnapshotQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return CalculateSafeAvailableResult{}, err
	}
	if snapshot.OwnerID() != input.OwnerID || !snapshot.Period().StartOn().Equal(input.Period.StartOn()) || !snapshot.Period().EndOn().Equal(input.Period.EndOn()) {
		return CalculateSafeAvailableResult{}, newSafeOperationError(ErrSafeAvailableDependency, domain.ErrInvalidSafeAvailableSnapshot)
	}
	result, err := domain.CalculateSafeAvailable(snapshot)
	if err != nil {
		return CalculateSafeAvailableResult{}, newSafeOperationError(ErrSafeAvailableDependency, err)
	}
	return CalculateSafeAvailableResult{Available: result}, nil
}
