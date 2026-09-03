package application

import (
	"context"
	"errors"
	"sort"

	"jarvis/backend/internal/modules/transactions/domain"
)

var (
	ErrMissingInstallmentPlanReader     = errors.New("installment plan: reader is required")
	ErrInstallmentPlanLookup            = errors.New("installment plan: lookup failed")
	ErrInstallmentPlanNotFound          = errors.New("installment plan: not found")
	ErrInstallmentPlanDependency        = errors.New("installment plan: invalid dependency result")
	ErrMissingFinancialDateProvider     = errors.New("financial date: provider is required")
	ErrFinancialDateUnavailable         = errors.New("financial date: unavailable")
	ErrMissingInstallmentCancelStore    = errors.New("cancel installment plan: store is required")
	ErrMissingInstallmentCancelReplay   = errors.New("cancel installment plan: replay reader is required")
	ErrInstallmentCancelKeyRequired     = errors.New("cancel installment plan: idempotency key is required")
	ErrInstallmentCancelKeyInvalid      = errors.New("cancel installment plan: idempotency key is invalid")
	ErrInstallmentCancelConflict        = errors.New("cancel installment plan: idempotency key was reused")
	ErrInstallmentCancelReplayLookup    = errors.New("cancel installment plan: replay lookup failed")
	ErrInstallmentCancelPersistence     = errors.New("cancel installment plan: persistence failed")
	ErrInstallmentCancellationDateStale = errors.New("cancel installment plan: confirmation date is stale")
)

type InstallmentPlanIDGenerator interface {
	NewInstallmentPlanID() (string, error)
}

type InstallmentPlanLookup struct {
	InstallmentPlan domain.InstallmentPlan
	Found           bool
}

type InstallmentPlanReader interface {
	FindInstallmentPlan(context.Context, string, string) (InstallmentPlanLookup, error)
	ListInstallmentPlans(context.Context, string) ([]domain.InstallmentPlan, error)
}

type GetInstallmentPlanInput struct {
	OwnerID string
	PlanID  string
}

type GetInstallmentPlanResult struct{ InstallmentPlan domain.InstallmentPlan }

type GetInstallmentPlan struct{ reader InstallmentPlanReader }

func NewGetInstallmentPlan(reader InstallmentPlanReader) (*GetInstallmentPlan, error) {
	if reader == nil {
		return nil, ErrMissingInstallmentPlanReader
	}
	return &GetInstallmentPlan{reader: reader}, nil
}

func (useCase *GetInstallmentPlan) Execute(ctx context.Context, input GetInstallmentPlanInput) (GetInstallmentPlanResult, error) {
	if err := ctx.Err(); err != nil {
		return GetInstallmentPlanResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return GetInstallmentPlanResult{}, domain.ErrInvalidInstallmentPlanOwnerID
	}
	if err := domain.ValidateInstallmentPlanID(input.PlanID); err != nil {
		return GetInstallmentPlanResult{}, err
	}
	lookup, err := useCase.reader.FindInstallmentPlan(ctx, input.OwnerID, input.PlanID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return GetInstallmentPlanResult{}, err
		}
		return GetInstallmentPlanResult{}, newSafeOperationError(ErrInstallmentPlanLookup, err)
	}
	if err := ctx.Err(); err != nil {
		return GetInstallmentPlanResult{}, err
	}
	if !lookup.Found {
		return GetInstallmentPlanResult{}, ErrInstallmentPlanNotFound
	}
	if err := validateInstallmentPlanDependencySnapshot(lookup.InstallmentPlan, input.OwnerID, input.PlanID); err != nil {
		return GetInstallmentPlanResult{}, err
	}
	return GetInstallmentPlanResult{InstallmentPlan: lookup.InstallmentPlan}, nil
}

type ListInstallmentPlansResult struct{ Items []domain.InstallmentPlan }

type ListInstallmentPlans struct{ reader InstallmentPlanReader }

func NewListInstallmentPlans(reader InstallmentPlanReader) (*ListInstallmentPlans, error) {
	if reader == nil {
		return nil, ErrMissingInstallmentPlanReader
	}
	return &ListInstallmentPlans{reader: reader}, nil
}

func (useCase *ListInstallmentPlans) Execute(ctx context.Context, ownerID string) (ListInstallmentPlansResult, error) {
	if err := ctx.Err(); err != nil {
		return ListInstallmentPlansResult{}, err
	}
	if err := domain.ValidateUserID(ownerID); err != nil {
		return ListInstallmentPlansResult{}, domain.ErrInvalidInstallmentPlanOwnerID
	}
	items, err := useCase.reader.ListInstallmentPlans(ctx, ownerID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ListInstallmentPlansResult{}, err
		}
		return ListInstallmentPlansResult{}, newSafeOperationError(ErrInstallmentPlanLookup, err)
	}
	if err := ctx.Err(); err != nil {
		return ListInstallmentPlansResult{}, err
	}
	if items == nil {
		items = []domain.InstallmentPlan{}
	} else {
		items = append([]domain.InstallmentPlan(nil), items...)
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if err := validateInstallmentPlanDependencySnapshot(item, ownerID, ""); err != nil {
			return ListInstallmentPlansResult{}, err
		}
		if _, exists := seen[item.ID()]; exists {
			return ListInstallmentPlansResult{}, ErrInstallmentPlanDependency
		}
		seen[item.ID()] = struct{}{}
	}
	sort.Slice(items, func(left, right int) bool {
		if !items[left].FirstDueDate().Equal(items[right].FirstDueDate()) {
			return items[left].FirstDueDate().Before(items[right].FirstDueDate())
		}
		if items[left].CreatedAt() != items[right].CreatedAt() {
			return items[left].CreatedAt().Before(items[right].CreatedAt())
		}
		return items[left].ID() < items[right].ID()
	})
	return ListInstallmentPlansResult{Items: items}, nil
}

func validateInstallmentPlanDependencySnapshot(plan domain.InstallmentPlan, ownerID, planID string) error {
	if plan.OwnerID() != ownerID || (planID != "" && plan.ID() != planID) {
		return ErrInstallmentPlanDependency
	}
	params := domain.InstallmentPlanRehydrationParams{
		ID: plan.ID(), OwnerID: plan.OwnerID(), CreditCardID: plan.CreditCardID(), ExpenseID: plan.ExpenseID(),
		TotalAmount: plan.TotalAmount(), InstallmentCount: plan.InstallmentCount(), FirstDueDate: plan.FirstDueDate(),
		DueDayAnchor: plan.DueDayAnchor(), Status: plan.Status(), CreatedAt: plan.CreatedAt(),
	}
	if cancelledOn, ok := plan.CancelledOn(); ok {
		params.CancelledOn = &cancelledOn
	}
	if _, err := domain.RehydrateInstallmentPlan(params); err != nil {
		return newSafeOperationError(ErrInstallmentPlanDependency, err)
	}
	return nil
}

// FinancialDateProvider supplies the current civil date in the configured
// financial timezone. The domain remains independent of time zones.
type FinancialDateProvider interface {
	CurrentFinancialDate(context.Context) (domain.CivilDate, error)
}

type PreviewInstallmentPlanCancellationInput struct{ OwnerID, PlanID string }
type PreviewInstallmentPlanCancellationResult struct {
	PlanID      string
	CancelledOn domain.CivilDate
}

type PreviewInstallmentPlanCancellation struct {
	reader       InstallmentPlanReader
	dateProvider FinancialDateProvider
}

func NewPreviewInstallmentPlanCancellation(reader InstallmentPlanReader, dateProvider FinancialDateProvider) (*PreviewInstallmentPlanCancellation, error) {
	if reader == nil {
		return nil, ErrMissingInstallmentPlanReader
	}
	if dateProvider == nil {
		return nil, ErrMissingFinancialDateProvider
	}
	return &PreviewInstallmentPlanCancellation{reader: reader, dateProvider: dateProvider}, nil
}

func (useCase *PreviewInstallmentPlanCancellation) Execute(ctx context.Context, input PreviewInstallmentPlanCancellationInput) (PreviewInstallmentPlanCancellationResult, error) {
	if err := ctx.Err(); err != nil {
		return PreviewInstallmentPlanCancellationResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return PreviewInstallmentPlanCancellationResult{}, domain.ErrInvalidInstallmentPlanOwnerID
	}
	if err := domain.ValidateInstallmentPlanID(input.PlanID); err != nil {
		return PreviewInstallmentPlanCancellationResult{}, err
	}
	lookup, err := useCase.reader.FindInstallmentPlan(ctx, input.OwnerID, input.PlanID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PreviewInstallmentPlanCancellationResult{}, err
		}
		return PreviewInstallmentPlanCancellationResult{}, newSafeOperationError(ErrInstallmentPlanLookup, err)
	}
	if !lookup.Found {
		return PreviewInstallmentPlanCancellationResult{}, ErrInstallmentPlanNotFound
	}
	if err := validateInstallmentPlanDependencySnapshot(lookup.InstallmentPlan, input.OwnerID, input.PlanID); err != nil {
		return PreviewInstallmentPlanCancellationResult{}, err
	}
	if lookup.InstallmentPlan.Status() == domain.InstallmentPlanStatusCancelled {
		return PreviewInstallmentPlanCancellationResult{}, domain.ErrInstallmentPlanAlreadyCancelled
	}
	current, err := useCase.dateProvider.CurrentFinancialDate(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PreviewInstallmentPlanCancellationResult{}, err
		}
		return PreviewInstallmentPlanCancellationResult{}, newSafeOperationError(ErrFinancialDateUnavailable, err)
	}
	if _, err := domain.NewCivilDate(current.Year(), current.Month(), current.Day()); err != nil {
		return PreviewInstallmentPlanCancellationResult{}, ErrFinancialDateUnavailable
	}
	return PreviewInstallmentPlanCancellationResult{PlanID: input.PlanID, CancelledOn: current}, nil
}

type CancelInstallmentPlanInput struct {
	OwnerID             string
	PlanID              string
	ExpectedCancelledOn domain.CivilDate
	IdempotencyKey      string
}

type InstallmentPlanCancellationReplayQuery struct {
	OwnerID        string
	Operation      string
	IdempotencyKey string
	Fingerprint    RequestFingerprint
}

type InstallmentPlanCancellationReplayLookup struct {
	InstallmentPlan domain.InstallmentPlan
	Found           bool
}

type InstallmentPlanCancellationReplayReader interface {
	FindInstallmentPlanCancellationReplay(context.Context, InstallmentPlanCancellationReplayQuery) (InstallmentPlanCancellationReplayLookup, error)
}

type IdempotentInstallmentPlanCancellationCommand struct {
	Operation           string
	IdempotencyKey      string
	Fingerprint         RequestFingerprint
	OwnerID             string
	PlanID              string
	ExpectedCancelledOn domain.CivilDate
	InstallmentPlan     domain.InstallmentPlan
}

type IdempotentInstallmentPlanCancellationResult struct {
	InstallmentPlan domain.InstallmentPlan
	Replayed        bool
}

type InstallmentPlanCancellationStore interface {
	CancelInstallmentPlan(context.Context, IdempotentInstallmentPlanCancellationCommand) (IdempotentInstallmentPlanCancellationResult, error)
}

type CancelInstallmentPlanResult struct {
	InstallmentPlan domain.InstallmentPlan
	Replayed        bool
}

type CancelInstallmentPlan struct {
	store        InstallmentPlanCancellationStore
	replayReader InstallmentPlanCancellationReplayReader
	reader       InstallmentPlanReader
	dateProvider FinancialDateProvider
}

func NewCancelInstallmentPlan(store InstallmentPlanCancellationStore, replayReader InstallmentPlanCancellationReplayReader, reader InstallmentPlanReader, dateProvider FinancialDateProvider) (*CancelInstallmentPlan, error) {
	if store == nil {
		return nil, ErrMissingInstallmentCancelStore
	}
	if replayReader == nil {
		return nil, ErrMissingInstallmentCancelReplay
	}
	if reader == nil {
		return nil, ErrMissingInstallmentPlanReader
	}
	if dateProvider == nil {
		return nil, ErrMissingFinancialDateProvider
	}
	return &CancelInstallmentPlan{store: store, replayReader: replayReader, reader: reader, dateProvider: dateProvider}, nil
}

func (useCase *CancelInstallmentPlan) Execute(ctx context.Context, input CancelInstallmentPlanInput) (CancelInstallmentPlanResult, error) {
	if err := ctx.Err(); err != nil {
		return CancelInstallmentPlanResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return CancelInstallmentPlanResult{}, domain.ErrInvalidInstallmentPlanOwnerID
	}
	if err := domain.ValidateInstallmentPlanID(input.PlanID); err != nil {
		return CancelInstallmentPlanResult{}, err
	}
	if _, err := domain.NewCivilDate(input.ExpectedCancelledOn.Year(), input.ExpectedCancelledOn.Month(), input.ExpectedCancelledOn.Day()); err != nil {
		return CancelInstallmentPlanResult{}, domain.ErrInvalidInstallmentCancellationDate
	}
	if err := validateInstallmentCancelKey(input.IdempotencyKey); err != nil {
		return CancelInstallmentPlanResult{}, err
	}
	fingerprint := fingerprintInstallmentPlanCancellation(input.OwnerID, input.PlanID, input.ExpectedCancelledOn)
	replay, found, err := findInstallmentPlanCancellationReplay(ctx, useCase.replayReader, InstallmentPlanCancellationReplayQuery{OwnerID: input.OwnerID, Operation: CancelInstallmentPlanOperation, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint})
	if err != nil {
		return CancelInstallmentPlanResult{}, err
	}
	if found {
		if err := validateInstallmentPlanDependencySnapshot(replay.InstallmentPlan, input.OwnerID, input.PlanID); err != nil {
			return CancelInstallmentPlanResult{}, err
		}
		cancelledOn, ok := replay.InstallmentPlan.CancelledOn()
		if !ok || !cancelledOn.Equal(input.ExpectedCancelledOn) {
			return CancelInstallmentPlanResult{}, ErrInstallmentPlanDependency
		}
		return CancelInstallmentPlanResult{InstallmentPlan: replay.InstallmentPlan, Replayed: true}, nil
	}
	lookup, err := useCase.reader.FindInstallmentPlan(ctx, input.OwnerID, input.PlanID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CancelInstallmentPlanResult{}, err
		}
		return CancelInstallmentPlanResult{}, newSafeOperationError(ErrInstallmentPlanLookup, err)
	}
	if !lookup.Found {
		return CancelInstallmentPlanResult{}, ErrInstallmentPlanNotFound
	}
	if err := validateInstallmentPlanDependencySnapshot(lookup.InstallmentPlan, input.OwnerID, input.PlanID); err != nil {
		return CancelInstallmentPlanResult{}, err
	}
	if lookup.InstallmentPlan.Status() == domain.InstallmentPlanStatusCancelled {
		return CancelInstallmentPlanResult{}, domain.ErrInstallmentPlanAlreadyCancelled
	}
	current, err := useCase.dateProvider.CurrentFinancialDate(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CancelInstallmentPlanResult{}, err
		}
		return CancelInstallmentPlanResult{}, newSafeOperationError(ErrFinancialDateUnavailable, err)
	}
	if !current.Equal(input.ExpectedCancelledOn) {
		return CancelInstallmentPlanResult{}, ErrInstallmentCancellationDateStale
	}
	cancelled, err := lookup.InstallmentPlan.Cancel(input.ExpectedCancelledOn)
	if err != nil {
		return CancelInstallmentPlanResult{}, err
	}
	stored, err := useCase.store.CancelInstallmentPlan(ctx, IdempotentInstallmentPlanCancellationCommand{Operation: CancelInstallmentPlanOperation, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint, OwnerID: input.OwnerID, PlanID: input.PlanID, ExpectedCancelledOn: input.ExpectedCancelledOn, InstallmentPlan: cancelled})
	if err != nil {
		if errors.Is(err, ErrInstallmentCancelConflict) {
			return CancelInstallmentPlanResult{}, ErrInstallmentCancelConflict
		}
		if errors.Is(err, domain.ErrInstallmentPlanAlreadyCancelled) {
			return CancelInstallmentPlanResult{}, domain.ErrInstallmentPlanAlreadyCancelled
		}
		return CancelInstallmentPlanResult{}, newSafeOperationError(ErrInstallmentCancelPersistence, err)
	}
	if err := validateInstallmentPlanDependencySnapshot(stored.InstallmentPlan, input.OwnerID, input.PlanID); err != nil {
		return CancelInstallmentPlanResult{}, err
	}
	if cancelledOn, ok := stored.InstallmentPlan.CancelledOn(); !ok || !cancelledOn.Equal(input.ExpectedCancelledOn) {
		return CancelInstallmentPlanResult{}, ErrInstallmentPlanDependency
	}
	return CancelInstallmentPlanResult{InstallmentPlan: stored.InstallmentPlan, Replayed: stored.Replayed}, nil
}

const CancelInstallmentPlanOperation = "CANCEL_INSTALLMENT_PLAN"

func validateInstallmentCancelKey(key string) error {
	if key == "" {
		return ErrInstallmentCancelKeyRequired
	}
	if !isValidIdempotencyKey(key) {
		return ErrInstallmentCancelKeyInvalid
	}
	return nil
}

func fingerprintInstallmentPlanCancellation(ownerID, planID string, date domain.CivilDate) RequestFingerprint {
	digest := newRequestFingerprintDigest()
	writeFingerprintString(digest, CancelInstallmentPlanOperation)
	writeFingerprintString(digest, ownerID)
	writeFingerprintString(digest, planID)
	writeFingerprintString(digest, date.String())
	var fingerprint RequestFingerprint
	copy(fingerprint[:], digest.Sum(nil))
	return fingerprint
}

func findInstallmentPlanCancellationReplay(ctx context.Context, reader InstallmentPlanCancellationReplayReader, query InstallmentPlanCancellationReplayQuery) (InstallmentPlanCancellationReplayLookup, bool, error) {
	lookup, err := reader.FindInstallmentPlanCancellationReplay(ctx, query)
	if err == nil {
		return lookup, lookup.Found, nil
	}
	switch {
	case errors.Is(err, ErrInstallmentCancelConflict):
		return InstallmentPlanCancellationReplayLookup{}, false, ErrInstallmentCancelConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return InstallmentPlanCancellationReplayLookup{}, false, err
	default:
		return InstallmentPlanCancellationReplayLookup{}, false, newSafeOperationError(ErrInstallmentCancelReplayLookup, err)
	}
}
