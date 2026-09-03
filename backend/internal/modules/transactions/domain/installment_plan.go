package domain

import (
	"errors"
	"time"
)

const (
	InstallmentPlanIDPrefix    = "ipl_"
	InstallmentPlanIDHexLength = 32
	MinInstallmentCount        = 2
	MaxInstallmentCount        = 120
)

var (
	ErrInvalidInstallmentPlanID           = errors.New("installment plan: invalid id")
	ErrInvalidInstallmentPlanOwnerID      = errors.New("installment plan: invalid owner id")
	ErrInvalidInstallmentPlanCardID       = errors.New("installment plan: invalid credit card id")
	ErrInvalidInstallmentPlanExpenseID    = errors.New("installment plan: invalid expense id")
	ErrInvalidInstallmentTotalAmount      = errors.New("installment plan: total amount must be positive")
	ErrInvalidInstallmentCount            = errors.New("installment plan: invalid installment count")
	ErrInvalidInstallmentFirstDueDate     = errors.New("installment plan: invalid first due date")
	ErrInvalidInstallmentCreatedAt        = errors.New("installment plan: invalid creation time")
	ErrInvalidInstallmentPlanStatus       = errors.New("installment plan: invalid status")
	ErrInvalidInstallmentPlanState        = errors.New("installment plan: invalid state")
	ErrInstallmentPlanAlreadyCancelled    = errors.New("installment plan: already cancelled")
	ErrInvalidInstallmentCancellationDate = errors.New("installment plan: invalid cancellation date")
	ErrInvalidInstallmentMonthOffset      = errors.New("installment plan: invalid month offset")
	ErrInvalidCardCycle                   = errors.New("card cycle: invalid cycle")
)

// CardCycle is the civil statement boundary derived from a card's anchors.
// It carries no instant or timezone information.
type CardCycle struct {
	statementClosingOn CivilDate
	statementDueOn     CivilDate
}

func (cycle CardCycle) StatementClosingOn() CivilDate { return cycle.statementClosingOn }
func (cycle CardCycle) StatementDueOn() CivilDate     { return cycle.statementDueOn }

// CalculateCardCycle derives the statement closing and due dates for a
// purchase. Equality with the closing date belongs to the following cycle.
func CalculateCardCycle(purchaseOn CivilDate, closingAnchor, dueAnchor DayOfMonthAnchor) (CardCycle, error) {
	if !purchaseOn.valid() || closingAnchor.day < 1 || closingAnchor.day > 31 || dueAnchor.day < 1 || dueAnchor.day > 31 {
		return CardCycle{}, ErrInvalidCardCycle
	}
	closing, err := AnchoredCivilDateAtMonthOffset(purchaseOn, 0, closingAnchor)
	if err != nil {
		return CardCycle{}, ErrInvalidCardCycle
	}
	closingOffset := 0
	if !purchaseOn.Before(closing) {
		closingOffset = 1
	}
	closing, err = AnchoredCivilDateAtMonthOffset(purchaseOn, closingOffset, closingAnchor)
	if err != nil {
		return CardCycle{}, ErrInvalidCardCycle
	}
	due, err := AnchoredCivilDateAtMonthOffset(closing, 0, dueAnchor)
	if err != nil {
		return CardCycle{}, ErrInvalidCardCycle
	}
	if !closing.Before(due) {
		due, err = AnchoredCivilDateAtMonthOffset(closing, 1, dueAnchor)
		if err != nil {
			return CardCycle{}, ErrInvalidCardCycle
		}
	}
	return CardCycle{statementClosingOn: closing, statementDueOn: due}, nil
}

type InstallmentPlanStatus string

const (
	InstallmentPlanStatusActive    InstallmentPlanStatus = "ACTIVE"
	InstallmentPlanStatusCancelled InstallmentPlanStatus = "CANCELLED"
)

// AnchoredCivilDateAtMonthOffset advances a civil date by calendar months,
// preserving the requested anchor and clamping only in the target month.
func AnchoredCivilDateAtMonthOffset(base CivilDate, monthOffset int, anchor DayOfMonthAnchor) (CivilDate, error) {
	if !base.valid() || monthOffset < 0 || anchor.day < 1 || anchor.day > 31 {
		return CivilDate{}, ErrInvalidInstallmentMonthOffset
	}
	baseIndex := int64(base.year-1)*12 + int64(base.month-1)
	target := baseIndex + int64(monthOffset)
	if target < baseIndex || target > int64(9999*12-1) {
		return CivilDate{}, ErrInvalidInstallmentMonthOffset
	}
	date, ok := civilDateFromMonthIndex(target, anchor.Day())
	if !ok {
		return CivilDate{}, ErrInvalidInstallmentMonthOffset
	}
	return date, nil
}

// Installment is a derived schedule entry. It has no independent persistence
// identity and does not represent an occurred transaction.
type Installment struct {
	number     int
	totalCount int
	dueDate    CivilDate
	amount     Money
}

func (installment Installment) Number() int        { return installment.number }
func (installment Installment) TotalCount() int    { return installment.totalCount }
func (installment Installment) DueDate() CivilDate { return installment.dueDate }
func (installment Installment) Amount() Money      { return installment.amount }

type InstallmentPlanParams struct {
	ID               string
	OwnerID          string
	CreditCardID     string
	ExpenseID        string
	TotalAmount      Money
	InstallmentCount int
	FirstDueDate     CivilDate
	DueDayAnchor     DayOfMonthAnchor
	CreatedAt        time.Time
}

type InstallmentPlanRehydrationParams struct {
	ID               string
	OwnerID          string
	CreditCardID     string
	ExpenseID        string
	TotalAmount      Money
	InstallmentCount int
	FirstDueDate     CivilDate
	DueDayAnchor     DayOfMonthAnchor
	Status           InstallmentPlanStatus
	CreatedAt        time.Time
	CancelledOn      *CivilDate
}

// InstallmentPlan is a confirmed obligation derived from a card purchase.
// Its schedule remains deterministic and is never persisted as individual
// financial facts.
type InstallmentPlan struct {
	id               string
	ownerID          string
	creditCardID     string
	expenseID        string
	totalAmount      Money
	installmentCount int
	firstDueDate     CivilDate
	dueDayAnchor     DayOfMonthAnchor
	status           InstallmentPlanStatus
	createdAt        time.Time
	cancelledOn      CivilDate
}

func NewInstallmentPlan(params InstallmentPlanParams) (InstallmentPlan, error) {
	return RehydrateInstallmentPlan(InstallmentPlanRehydrationParams{
		ID:               params.ID,
		OwnerID:          params.OwnerID,
		CreditCardID:     params.CreditCardID,
		ExpenseID:        params.ExpenseID,
		TotalAmount:      params.TotalAmount,
		InstallmentCount: params.InstallmentCount,
		FirstDueDate:     params.FirstDueDate,
		DueDayAnchor:     params.DueDayAnchor,
		Status:           InstallmentPlanStatusActive,
		CreatedAt:        params.CreatedAt,
	})
}

func RehydrateInstallmentPlan(params InstallmentPlanRehydrationParams) (InstallmentPlan, error) {
	if err := ValidateInstallmentPlanID(params.ID); err != nil {
		return InstallmentPlan{}, err
	}
	if err := ValidateUserID(params.OwnerID); err != nil {
		return InstallmentPlan{}, ErrInvalidInstallmentPlanOwnerID
	}
	if err := ValidateCreditCardID(params.CreditCardID); err != nil {
		return InstallmentPlan{}, ErrInvalidInstallmentPlanCardID
	}
	if !isValidIdentifier(params.ExpenseID) {
		return InstallmentPlan{}, ErrInvalidInstallmentPlanExpenseID
	}
	if params.InstallmentCount < MinInstallmentCount || params.InstallmentCount > MaxInstallmentCount {
		return InstallmentPlan{}, ErrInvalidInstallmentCount
	}
	if params.TotalAmount.Currency() != CurrencyBRL || params.TotalAmount.MinorUnits() <= 0 || params.TotalAmount.MinorUnits() < int64(params.InstallmentCount) {
		return InstallmentPlan{}, ErrInvalidInstallmentTotalAmount
	}
	if !params.FirstDueDate.valid() || params.DueDayAnchor.day < 1 || params.DueDayAnchor.day > 31 {
		return InstallmentPlan{}, ErrInvalidInstallmentFirstDueDate
	}
	canonicalFirst, err := AnchoredCivilDateAtMonthOffset(params.FirstDueDate, 0, params.DueDayAnchor)
	if err != nil || !canonicalFirst.Equal(params.FirstDueDate) {
		return InstallmentPlan{}, ErrInvalidInstallmentFirstDueDate
	}
	if params.CreatedAt.IsZero() {
		return InstallmentPlan{}, ErrInvalidInstallmentCreatedAt
	}
	createdAt := normalizeInstant(params.CreatedAt)
	plan := InstallmentPlan{
		id:               params.ID,
		ownerID:          params.OwnerID,
		creditCardID:     params.CreditCardID,
		expenseID:        params.ExpenseID,
		totalAmount:      params.TotalAmount,
		installmentCount: params.InstallmentCount,
		firstDueDate:     params.FirstDueDate,
		dueDayAnchor:     params.DueDayAnchor,
		status:           params.Status,
		createdAt:        createdAt,
	}
	switch params.Status {
	case InstallmentPlanStatusActive:
		if params.CancelledOn != nil {
			return InstallmentPlan{}, ErrInvalidInstallmentPlanState
		}
	case InstallmentPlanStatusCancelled:
		if params.CancelledOn == nil || !params.CancelledOn.valid() {
			return InstallmentPlan{}, ErrInvalidInstallmentPlanState
		}
		plan.cancelledOn = *params.CancelledOn
	default:
		return InstallmentPlan{}, ErrInvalidInstallmentPlanStatus
	}
	return plan, nil
}

func (plan InstallmentPlan) ID() string                     { return plan.id }
func (plan InstallmentPlan) OwnerID() string                { return plan.ownerID }
func (plan InstallmentPlan) UserID() string                 { return plan.ownerID }
func (plan InstallmentPlan) CreditCardID() string           { return plan.creditCardID }
func (plan InstallmentPlan) ExpenseID() string              { return plan.expenseID }
func (plan InstallmentPlan) TotalAmount() Money             { return plan.totalAmount }
func (plan InstallmentPlan) InstallmentCount() int          { return plan.installmentCount }
func (plan InstallmentPlan) FirstDueDate() CivilDate        { return plan.firstDueDate }
func (plan InstallmentPlan) DueDayAnchor() DayOfMonthAnchor { return plan.dueDayAnchor }
func (plan InstallmentPlan) Status() InstallmentPlanStatus  { return plan.status }
func (plan InstallmentPlan) CreatedAt() time.Time           { return plan.createdAt }
func (plan InstallmentPlan) CancelledOn() (CivilDate, bool) {
	if plan.status != InstallmentPlanStatusCancelled {
		return CivilDate{}, false
	}
	return plan.cancelledOn, true
}

func (plan InstallmentPlan) structurallyValid() bool {
	params := InstallmentPlanRehydrationParams{
		ID: plan.id, OwnerID: plan.ownerID, CreditCardID: plan.creditCardID,
		ExpenseID: plan.expenseID, TotalAmount: plan.totalAmount,
		InstallmentCount: plan.installmentCount, FirstDueDate: plan.firstDueDate,
		DueDayAnchor: plan.dueDayAnchor, Status: plan.status, CreatedAt: plan.createdAt,
	}
	if plan.status == InstallmentPlanStatusCancelled {
		params.CancelledOn = &plan.cancelledOn
	}
	_, err := RehydrateInstallmentPlan(params)
	return err == nil
}

func (plan InstallmentPlan) Cancel(cancelledOn CivilDate) (InstallmentPlan, error) {
	if !plan.structurallyValid() {
		return InstallmentPlan{}, ErrInvalidInstallmentPlanState
	}
	if plan.status == InstallmentPlanStatusCancelled {
		return InstallmentPlan{}, ErrInstallmentPlanAlreadyCancelled
	}
	if !cancelledOn.valid() {
		return InstallmentPlan{}, ErrInvalidInstallmentCancellationDate
	}
	plan.status = InstallmentPlanStatusCancelled
	plan.cancelledOn = cancelledOn
	return plan, nil
}

func (plan InstallmentPlan) Schedule() ([]Installment, error) {
	if !plan.structurallyValid() {
		return nil, ErrInvalidInstallmentPlanState
	}
	return BuildInstallmentSchedule(plan.totalAmount, plan.installmentCount, plan.firstDueDate, plan.dueDayAnchor)
}

// BuildInstallmentSchedule derives an exact, independent schedule without
// requiring an aggregate identifier or creation timestamp. It is suitable for
// previews and is shared by InstallmentPlan.Schedule.
func BuildInstallmentSchedule(totalAmount Money, installmentCount int, firstDueDate CivilDate, dueDayAnchor DayOfMonthAnchor) ([]Installment, error) {
	if installmentCount < MinInstallmentCount || installmentCount > MaxInstallmentCount {
		return nil, ErrInvalidInstallmentCount
	}
	if totalAmount.Currency() != CurrencyBRL || totalAmount.MinorUnits() <= 0 || totalAmount.MinorUnits() < int64(installmentCount) {
		return nil, ErrInvalidInstallmentTotalAmount
	}
	if !firstDueDate.valid() || dueDayAnchor.day < 1 || dueDayAnchor.day > 31 {
		return nil, ErrInvalidInstallmentFirstDueDate
	}
	canonicalFirst, err := AnchoredCivilDateAtMonthOffset(firstDueDate, 0, dueDayAnchor)
	if err != nil || !canonicalFirst.Equal(firstDueDate) {
		return nil, ErrInvalidInstallmentFirstDueDate
	}
	base := totalAmount.MinorUnits() / int64(installmentCount)
	remainder := totalAmount.MinorUnits() % int64(installmentCount)
	schedule := make([]Installment, installmentCount)
	for index := range schedule {
		dueDate, err := AnchoredCivilDateAtMonthOffset(firstDueDate, index, dueDayAnchor)
		if err != nil {
			return nil, err
		}
		amountMinor := base
		if index == len(schedule)-1 {
			amountMinor += remainder
		}
		amount, err := NewMoney(amountMinor, CurrencyBRL)
		if err != nil {
			return nil, err
		}
		schedule[index] = Installment{number: index + 1, totalCount: installmentCount, dueDate: dueDate, amount: amount}
	}
	return schedule, nil
}

func (plan InstallmentPlan) EffectiveSchedule() ([]Installment, error) {
	schedule, err := plan.Schedule()
	if err != nil {
		return nil, err
	}
	if plan.status != InstallmentPlanStatusCancelled {
		return schedule, nil
	}
	effective := schedule[:0]
	for _, installment := range schedule {
		if !plan.cancelledOn.Before(installment.dueDate) {
			effective = append(effective, installment)
		}
	}
	return append([]Installment(nil), effective...), nil
}

func ValidateInstallmentPlanID(value string) error {
	if len(value) != len(InstallmentPlanIDPrefix)+InstallmentPlanIDHexLength || value[:len(InstallmentPlanIDPrefix)] != InstallmentPlanIDPrefix {
		return ErrInvalidInstallmentPlanID
	}
	for index := len(InstallmentPlanIDPrefix); index < len(value); index++ {
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return ErrInvalidInstallmentPlanID
		}
	}
	return nil
}
