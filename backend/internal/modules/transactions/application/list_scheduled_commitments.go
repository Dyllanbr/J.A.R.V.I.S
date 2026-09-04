package application

import (
	"context"
	"errors"
	"sort"

	"jarvis/backend/internal/modules/transactions/domain"
)

const ScheduledCommitmentsHorizonMonths = 12

var (
	ErrMissingScheduledCommitmentPlanReader       = errors.New("list scheduled commitments: installment plan reader is required")
	ErrMissingScheduledCommitmentRecurrenceReader = errors.New("list scheduled commitments: recurrence reader is required")
	ErrScheduledCommitmentEvaluationDate          = errors.New("list scheduled commitments: evaluation date is invalid")
	ErrScheduledCommitmentPlanQuery               = errors.New("list scheduled commitments: installment plan query failed")
	ErrScheduledCommitmentRecurrenceQuery         = errors.New("list scheduled commitments: recurrence query failed")
	ErrScheduledCommitmentDependency              = errors.New("list scheduled commitments: invalid dependency result")
	ErrScheduledCommitmentDuplicate               = errors.New("list scheduled commitments: duplicate source")
	ErrScheduledCommitmentHorizon                 = errors.New("list scheduled commitments: horizon is invalid")
)

// ScheduledCommitmentSource identifies the existing confirmed aggregate that
// produced a read-only schedule line. It is not a persisted aggregate.
type ScheduledCommitmentSource string

const (
	ScheduledCommitmentSourceInstallmentPlan ScheduledCommitmentSource = "INSTALLMENT_PLAN"
	ScheduledCommitmentSourceRecurrence      ScheduledCommitmentSource = "RECURRENCE"
)

// ScheduledCommitmentLine is an in-memory projection of one future occurrence
// from an InstallmentPlan or an active Recurrence. It has no persistence
// identity and never represents a new Expense or payment.
type ScheduledCommitmentLine struct {
	source   ScheduledCommitmentSource
	sourceID string
	sequence int
	dueOn    domain.CivilDate
	amount   domain.Money
}

func (line ScheduledCommitmentLine) Source() ScheduledCommitmentSource { return line.source }
func (line ScheduledCommitmentLine) SourceID() string                  { return line.sourceID }
func (line ScheduledCommitmentLine) Sequence() int                     { return line.sequence }
func (line ScheduledCommitmentLine) DueOn() domain.CivilDate           { return line.dueOn }
func (line ScheduledCommitmentLine) Amount() domain.Money              { return line.amount }

// ListScheduledCommitmentsInput fixes the civil evaluation boundary. Passing
// the date explicitly keeps the projection deterministic and timezone-free.
type ListScheduledCommitmentsInput struct {
	OwnerID        string
	EvaluationDate domain.CivilDate
}

type ListScheduledCommitmentsResult struct {
	Items []ScheduledCommitmentLine
}

// ListScheduledCommitments combines only the two commitment sources already
// supported by the domain. It computes the result in memory and performs no
// writes, idempotency operations or clock reads.
type ListScheduledCommitments struct {
	planReader       InstallmentPlanReader
	recurrenceReader RecurrenceReader
}

func NewListScheduledCommitments(planReader InstallmentPlanReader, recurrenceReader RecurrenceReader) (*ListScheduledCommitments, error) {
	if planReader == nil {
		return nil, ErrMissingScheduledCommitmentPlanReader
	}
	if recurrenceReader == nil {
		return nil, ErrMissingScheduledCommitmentRecurrenceReader
	}
	return &ListScheduledCommitments{planReader: planReader, recurrenceReader: recurrenceReader}, nil
}

func (useCase *ListScheduledCommitments) Execute(ctx context.Context, input ListScheduledCommitmentsInput) (ListScheduledCommitmentsResult, error) {
	if err := ctx.Err(); err != nil {
		return ListScheduledCommitmentsResult{}, err
	}
	if err := domain.ValidateUserID(input.OwnerID); err != nil {
		return ListScheduledCommitmentsResult{}, domain.ErrInvalidInstallmentPlanOwnerID
	}
	if !validScheduledCommitmentDate(input.EvaluationDate) {
		return ListScheduledCommitmentsResult{}, ErrScheduledCommitmentEvaluationDate
	}

	horizonEnd, err := scheduledCommitmentHorizonEnd(input.EvaluationDate)
	if err != nil {
		return ListScheduledCommitmentsResult{}, err
	}
	items := make([]ScheduledCommitmentLine, 0)
	if err := useCase.appendInstallmentPlans(ctx, input.OwnerID, input.EvaluationDate, horizonEnd, &items); err != nil {
		return ListScheduledCommitmentsResult{}, err
	}
	if err := useCase.appendRecurrences(ctx, input.OwnerID, input.EvaluationDate, horizonEnd, &items); err != nil {
		return ListScheduledCommitmentsResult{}, err
	}
	sort.Slice(items, func(left, right int) bool {
		return scheduledCommitmentComesBefore(items[left], items[right])
	})
	return ListScheduledCommitmentsResult{Items: items}, nil
}

func (useCase *ListScheduledCommitments) appendInstallmentPlans(ctx context.Context, ownerID string, evaluationDate, horizonEnd domain.CivilDate, items *[]ScheduledCommitmentLine) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plans, err := useCase.planReader.ListInstallmentPlans(ctx, ownerID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return newSafeOperationError(ErrScheduledCommitmentPlanQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if err := validateInstallmentPlanDependencySnapshot(plan, ownerID, ""); err != nil {
			return newSafeOperationError(ErrScheduledCommitmentDependency, err)
		}
		if _, exists := seen[plan.ID()]; exists {
			return ErrScheduledCommitmentDuplicate
		}
		seen[plan.ID()] = struct{}{}
		schedule, err := plan.EffectiveSchedule()
		if err != nil {
			return newSafeOperationError(ErrScheduledCommitmentDependency, err)
		}
		for _, installment := range schedule {
			if !scheduledCommitmentInWindow(installment.DueDate(), evaluationDate, horizonEnd) {
				continue
			}
			*items = append(*items, ScheduledCommitmentLine{
				source: ScheduledCommitmentSourceInstallmentPlan, sourceID: plan.ID(),
				sequence: installment.Number(), dueOn: installment.DueDate(), amount: installment.Amount(),
			})
		}
	}
	return nil
}

func (useCase *ListScheduledCommitments) appendRecurrences(ctx context.Context, ownerID string, evaluationDate, horizonEnd domain.CivilDate, items *[]ScheduledCommitmentLine) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	recurrences, err := useCase.recurrenceReader.ListRecurrences(ctx, ownerID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return newSafeOperationError(ErrScheduledCommitmentRecurrenceQuery, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(recurrences))
	for _, recurrence := range recurrences {
		if err := validateScheduledRecurrenceSnapshot(recurrence, ownerID); err != nil {
			return newSafeOperationError(ErrScheduledCommitmentDependency, err)
		}
		if _, exists := seen[recurrence.ID()]; exists {
			return ErrScheduledCommitmentDuplicate
		}
		seen[recurrence.ID()] = struct{}{}
		if recurrence.Status() != domain.RecurrenceStatusActive {
			continue
		}
		startOffset := recurrenceMonthOffset(recurrence.StartsOn(), evaluationDate)
		lastOffset := recurrenceMonthOffset(recurrence.StartsOn(), horizonEnd)
		for offset := startOffset; offset <= lastOffset; offset++ {
			occurrence, err := recurrence.OccurrenceDate(offset)
			if err != nil {
				return newSafeOperationError(ErrScheduledCommitmentDependency, err)
			}
			if !scheduledCommitmentInWindow(occurrence, evaluationDate, horizonEnd) {
				continue
			}
			*items = append(*items, ScheduledCommitmentLine{
				source: ScheduledCommitmentSourceRecurrence, sourceID: recurrence.ID(),
				sequence: offset + 1, dueOn: occurrence, amount: recurrence.ExpectedAmount(),
			})
		}
	}
	return nil
}

func validScheduledCommitmentDate(value domain.CivilDate) bool {
	_, err := domain.NewCivilDate(value.Year(), value.Month(), value.Day())
	return err == nil
}

func scheduledCommitmentHorizonEnd(evaluationDate domain.CivilDate) (domain.CivilDate, error) {
	anchor, err := domain.NewDayOfMonthAnchor(evaluationDate.Day())
	if err != nil {
		return domain.CivilDate{}, ErrScheduledCommitmentHorizon
	}
	end, err := domain.AnchoredCivilDateAtMonthOffset(evaluationDate, ScheduledCommitmentsHorizonMonths, anchor)
	if err != nil {
		return domain.CivilDate{}, ErrScheduledCommitmentHorizon
	}
	return end, nil
}

func scheduledCommitmentInWindow(value, evaluationDate, horizonEnd domain.CivilDate) bool {
	return evaluationDate.Before(value) && !horizonEnd.Before(value)
}

func recurrenceMonthOffset(startsOn, target domain.CivilDate) int {
	difference := (target.Year()-startsOn.Year())*12 + int(target.Month()) - int(startsOn.Month())
	if difference < 0 {
		return 0
	}
	return difference
}

func scheduledCommitmentComesBefore(left, right ScheduledCommitmentLine) bool {
	if !left.dueOn.Equal(right.dueOn) {
		return left.dueOn.Before(right.dueOn)
	}
	if left.source != right.source {
		return left.source < right.source
	}
	if left.sourceID != right.sourceID {
		return left.sourceID < right.sourceID
	}
	if left.sequence != right.sequence {
		return left.sequence < right.sequence
	}
	return left.amount.MinorUnits() < right.amount.MinorUnits()
}

func validateScheduledRecurrenceSnapshot(recurrence domain.Recurrence, ownerID string) error {
	if recurrence.UserID() != ownerID || domain.ValidateRecurrenceUserID(recurrence.UserID()) != nil || domain.ValidateRecurrenceID(recurrence.ID()) != nil {
		return ErrScheduledCommitmentDependency
	}
	if recurrence.Type() != domain.TransactionTypeExpense || recurrence.Frequency() != domain.RecurrenceFrequencyMonthly || recurrence.ExpectedAmount().Currency() != domain.CurrencyBRL || recurrence.ExpectedAmount().MinorUnits() <= 0 || !validScheduledCommitmentDate(recurrence.StartsOn()) || recurrence.CreatedAt().IsZero() {
		return ErrScheduledCommitmentDependency
	}
	canonical, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID: recurrence.ID(),
		Details: domain.RecurrenceDetails{
			UserID: recurrence.UserID(), TransactionType: recurrence.Type(), Description: recurrence.Description(),
			ExpectedAmount: recurrence.ExpectedAmount(), Frequency: recurrence.Frequency(), StartsOn: recurrence.StartsOn(),
		},
		CreatedAt: recurrence.CreatedAt(),
	})
	if err != nil || canonical.Description() != recurrence.Description() || !canonical.CreatedAt().Equal(recurrence.CreatedAt()) {
		return ErrScheduledCommitmentDependency
	}
	switch recurrence.Status() {
	case domain.RecurrenceStatusActive:
		if _, ok := recurrence.CancelledAt(); ok {
			return ErrScheduledCommitmentDependency
		}
	case domain.RecurrenceStatusCancelled:
		cancelledAt, ok := recurrence.CancelledAt()
		if !ok {
			return ErrScheduledCommitmentDependency
		}
		cancelled, err := canonical.Cancel(cancelledAt)
		if err != nil || cancelled.Status() != recurrence.Status() {
			return ErrScheduledCommitmentDependency
		}
	default:
		return ErrScheduledCommitmentDependency
	}
	return nil
}
