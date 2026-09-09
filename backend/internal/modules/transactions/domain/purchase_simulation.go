package domain

import (
	"errors"
	"math"
)

// PurchaseSimulationMode identifies the two purchase shapes supported by the
// read-only simulator. It is intentionally separate from the command model:
// a simulation never records an Expense or an InstallmentPlan.
type PurchaseSimulationMode string

const (
	PurchaseSimulationModeOneTime     PurchaseSimulationMode = "ONE_TIME"
	PurchaseSimulationModeInstallment PurchaseSimulationMode = "INSTALLMENT"
)

const purchaseSimulationSourcePrefix = "simulation_purchase_"

var (
	ErrInvalidPurchaseSimulationMode         = errors.New("purchase simulation: invalid mode")
	ErrInvalidPurchaseSimulationAmount       = errors.New("purchase simulation: invalid amount")
	ErrInvalidPurchaseSimulationDate         = errors.New("purchase simulation: invalid purchase date")
	ErrInvalidPurchaseSimulationCardID       = errors.New("purchase simulation: invalid credit card id")
	ErrInvalidPurchaseSimulationInstallments = errors.New("purchase simulation: invalid installment count")
	ErrPurchaseSimulationCardOwnerMismatch   = errors.New("purchase simulation: card owner mismatch")
	ErrPurchaseSimulationCardIDMismatch      = errors.New("purchase simulation: card id mismatch")
	ErrPurchaseSimulationCardArchived        = errors.New("purchase simulation: archived card is not eligible")
	ErrPurchaseSimulationInvalidSnapshot     = errors.New("purchase simulation: invalid snapshot")
	ErrPurchaseSimulationDuplicateCommitment = errors.New("purchase simulation: duplicate commitment")
	ErrPurchaseSimulationArithmeticOverflow  = errors.New("purchase simulation: amount overflow")
)

// PurchaseSimulationRequest is an immutable, validated hypothetical purchase.
// It has no persistence identity and cannot authorize or execute a payment.
type PurchaseSimulationRequest struct {
	amount           Money
	purchaseOn       CivilDate
	mode             PurchaseSimulationMode
	installmentCount int
	creditCardID     string
}

type PurchaseSimulationRequestParams struct {
	Amount           Money
	PurchaseOn       CivilDate
	Mode             PurchaseSimulationMode
	InstallmentCount int
	CreditCardID     string
}

// NewPurchaseSimulationRequest validates all fields before any snapshot read
// can occur in the Application layer.
func NewPurchaseSimulationRequest(params PurchaseSimulationRequestParams) (PurchaseSimulationRequest, error) {
	if params.Amount.Currency() != CurrencyBRL || params.Amount.MinorUnits() <= 0 {
		return PurchaseSimulationRequest{}, ErrInvalidPurchaseSimulationAmount
	}
	if !params.PurchaseOn.valid() {
		return PurchaseSimulationRequest{}, ErrInvalidPurchaseSimulationDate
	}
	if err := ValidateCreditCardID(params.CreditCardID); err != nil {
		return PurchaseSimulationRequest{}, ErrInvalidPurchaseSimulationCardID
	}
	switch params.Mode {
	case PurchaseSimulationModeOneTime:
		if params.InstallmentCount != 0 {
			return PurchaseSimulationRequest{}, ErrInvalidPurchaseSimulationInstallments
		}
	case PurchaseSimulationModeInstallment:
		if params.InstallmentCount < MinInstallmentCount || params.InstallmentCount > MaxInstallmentCount {
			return PurchaseSimulationRequest{}, ErrInvalidPurchaseSimulationInstallments
		}
	default:
		return PurchaseSimulationRequest{}, ErrInvalidPurchaseSimulationMode
	}
	return PurchaseSimulationRequest{
		amount: params.Amount, purchaseOn: params.PurchaseOn, mode: params.Mode,
		installmentCount: params.InstallmentCount, creditCardID: params.CreditCardID,
	}, nil
}

func (request PurchaseSimulationRequest) Amount() Money                { return request.amount }
func (request PurchaseSimulationRequest) PurchaseOn() CivilDate        { return request.purchaseOn }
func (request PurchaseSimulationRequest) Mode() PurchaseSimulationMode { return request.mode }
func (request PurchaseSimulationRequest) InstallmentCount() int        { return request.installmentCount }
func (request PurchaseSimulationRequest) CreditCardID() string         { return request.creditCardID }

// PurchaseSimulationAssumption records a material property of a simulation
// instead of hiding it in prose or silently treating it as a financial fact.
type PurchaseSimulationAssumption string

const (
	PurchaseSimulationAssumptionNotPersisted     PurchaseSimulationAssumption = "NOT_PERSISTED"
	PurchaseSimulationAssumptionNoExpenseCreated PurchaseSimulationAssumption = "NO_EXPENSE_CREATED"
)

// PurchaseSimulationResult is an immutable comparison between the confirmed
// baseline and the hypothetical projection. HypotheticalCommitments are
// read-model lines only; they are never persisted as future Expenses.
type PurchaseSimulationResult struct {
	request                 PurchaseSimulationRequest
	baseline                SafeAvailableResult
	projected               SafeAvailableResult
	impact                  Money
	hypotheticalCommitments []SafeAvailableEntry
	assumptions             []PurchaseSimulationAssumption
}

func (result PurchaseSimulationResult) Request() PurchaseSimulationRequest { return result.request }
func (result PurchaseSimulationResult) Baseline() SafeAvailableResult      { return result.baseline }
func (result PurchaseSimulationResult) Projected() SafeAvailableResult     { return result.projected }
func (result PurchaseSimulationResult) Impact() Money                      { return result.impact }
func (result PurchaseSimulationResult) HypotheticalCommitments() []SafeAvailableEntry {
	return append([]SafeAvailableEntry(nil), result.hypotheticalCommitments...)
}
func (result PurchaseSimulationResult) Assumptions() []PurchaseSimulationAssumption {
	return append([]PurchaseSimulationAssumption(nil), result.assumptions...)
}

// SimulatePurchase overlays only the hypothetical lines that fall inside the
// explicit Safe Available period and recalculates the existing read model.
// The confirmed snapshot itself is never mutated.
func SimulatePurchase(snapshot SafeAvailableSnapshot, card CreditCard, request PurchaseSimulationRequest) (PurchaseSimulationResult, error) {
	validatedRequest, err := NewPurchaseSimulationRequest(PurchaseSimulationRequestParams{
		Amount: request.amount, PurchaseOn: request.purchaseOn, Mode: request.mode,
		InstallmentCount: request.installmentCount, CreditCardID: request.creditCardID,
	})
	if err != nil {
		return PurchaseSimulationResult{}, err
	}
	request = validatedRequest
	if err := validateSafeAvailableSnapshotState(snapshot); err != nil {
		return PurchaseSimulationResult{}, ErrPurchaseSimulationInvalidSnapshot
	}
	if !card.structurallyValid() {
		return PurchaseSimulationResult{}, ErrPurchaseSimulationInvalidSnapshot
	}
	if card.UserID() != snapshot.OwnerID() {
		return PurchaseSimulationResult{}, ErrPurchaseSimulationCardOwnerMismatch
	}
	if card.ID() != request.CreditCardID() {
		return PurchaseSimulationResult{}, ErrPurchaseSimulationCardIDMismatch
	}
	if card.Status() != CreditCardStatusActive {
		return PurchaseSimulationResult{}, ErrPurchaseSimulationCardArchived
	}
	baseline, err := CalculateSafeAvailable(snapshot)
	if err != nil {
		return PurchaseSimulationResult{}, err
	}

	cycle, err := CalculateCardCycle(request.PurchaseOn(), card.ClosingDayAnchor(), card.DueDayAnchor())
	if err != nil {
		return PurchaseSimulationResult{}, err
	}

	schedule := make([]Installment, 0, 1)
	switch request.Mode() {
	case PurchaseSimulationModeOneTime:
		amount := request.Amount()
		schedule = append(schedule, Installment{number: 1, totalCount: 1, dueDate: cycle.StatementDueOn(), amount: amount})
	case PurchaseSimulationModeInstallment:
		schedule, err = BuildInstallmentSchedule(request.Amount(), request.InstallmentCount(), cycle.StatementDueOn(), card.DueDayAnchor())
		if err != nil {
			return PurchaseSimulationResult{}, err
		}
	default:
		return PurchaseSimulationResult{}, ErrInvalidPurchaseSimulationMode
	}

	sourceID := purchaseSimulationSourcePrefix + request.CreditCardID()
	hypothetical := make([]SafeAvailableEntry, 0, len(schedule))
	for _, installment := range schedule {
		if !snapshot.period.contains(installment.DueDate()) {
			continue
		}
		entry, entryErr := NewSafeAvailableEntry(SafeAvailableEntryParams{
			Kind: SafeAvailableEntryKindCommitment, OwnerID: snapshot.OwnerID(), SourceID: sourceID,
			Sequence: installment.Number(), DueOn: installment.DueDate(), Amount: installment.Amount(),
		})
		if entryErr != nil {
			return PurchaseSimulationResult{}, entryErr
		}
		hypothetical = append(hypothetical, entry)
	}

	commitments := append([]SafeAvailableEntry(nil), snapshot.Commitments()...)
	commitments = append(commitments, hypothetical...)
	budget, hasBudget := snapshot.Budget()
	var budgetPtr *MonthlyBudget
	if hasBudget {
		budgetCopy := budget
		budgetPtr = &budgetCopy
	}
	projectedSnapshot, err := NewSafeAvailableSnapshot(SafeAvailableSnapshotParams{
		OwnerID: snapshot.OwnerID(), Period: snapshot.Period(), AvailableBalance: snapshot.AvailableBalance(),
		Budget: budgetPtr, Incomes: snapshot.Incomes(), Expenses: snapshot.Expenses(), Commitments: commitments,
		MissingData: snapshot.MissingData(),
	})
	if err != nil {
		if errors.Is(err, ErrSafeAvailableDuplicateEntry) {
			return PurchaseSimulationResult{}, ErrPurchaseSimulationDuplicateCommitment
		}
		return PurchaseSimulationResult{}, ErrPurchaseSimulationInvalidSnapshot
	}
	projected, err := CalculateSafeAvailable(projectedSnapshot)
	if err != nil {
		return PurchaseSimulationResult{}, err
	}
	impactMinor, err := subtractSimulationAmounts(projected.FinalAmount().MinorUnits(), baseline.FinalAmount().MinorUnits())
	if err != nil {
		return PurchaseSimulationResult{}, err
	}
	impact, err := NewMoney(impactMinor, CurrencyBRL)
	if err != nil {
		return PurchaseSimulationResult{}, ErrPurchaseSimulationArithmeticOverflow
	}

	return PurchaseSimulationResult{
		request: request, baseline: baseline, projected: projected, impact: impact,
		hypotheticalCommitments: hypothetical,
		assumptions: []PurchaseSimulationAssumption{
			PurchaseSimulationAssumptionNotPersisted,
			PurchaseSimulationAssumptionNoExpenseCreated,
		},
	}, nil
}

func subtractSimulationAmounts(left, right int64) (int64, error) {
	if right > 0 && left < math.MinInt64+right {
		return 0, ErrPurchaseSimulationArithmeticOverflow
	}
	if right < 0 && left > math.MaxInt64+right {
		return 0, ErrPurchaseSimulationArithmeticOverflow
	}
	return left - right, nil
}
