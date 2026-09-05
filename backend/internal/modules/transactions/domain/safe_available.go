package domain

import (
	"errors"
	"math"
	"sort"
	"strconv"
)

// SafeAvailableEntryKind classifies a confirmed component of the
// conservative Safe Available calculation. These values are read-model
// semantics only; they do not introduce a persisted financial state.
type SafeAvailableEntryKind string

const (
	SafeAvailableEntryKindAvailableBalance SafeAvailableEntryKind = "AVAILABLE_BALANCE"
	SafeAvailableEntryKindIncome           SafeAvailableEntryKind = "INCOME"
	SafeAvailableEntryKindExpense          SafeAvailableEntryKind = "EXPENSE"
	SafeAvailableEntryKindCommitment       SafeAvailableEntryKind = "COMMITMENT"
)

// SafeAvailableMissingData identifies an input that is not available to this
// read model. Missing data is carried explicitly instead of being converted to
// zero or silently replaced by a heuristic.
type SafeAvailableMissingData string

const (
	SafeAvailableMissingBudget           SafeAvailableMissingData = "BUDGET"
	SafeAvailableMissingConfirmedIncome  SafeAvailableMissingData = "CONFIRMED_INCOME"
	SafeAvailableMissingConfirmedExpense SafeAvailableMissingData = "CONFIRMED_EXPENSE"
	SafeAvailableMissingCommitments      SafeAvailableMissingData = "COMMITMENTS"
)

var (
	ErrInvalidSafeAvailableOwnerID       = errors.New("safe available: invalid owner id")
	ErrInvalidSafeAvailablePeriod        = errors.New("safe available: invalid period")
	ErrInvalidSafeAvailableEntryKind     = errors.New("safe available: invalid entry kind")
	ErrInvalidSafeAvailableEntryOwnerID  = errors.New("safe available: invalid entry owner id")
	ErrInvalidSafeAvailableEntryID       = errors.New("safe available: invalid entry id")
	ErrInvalidSafeAvailableEntrySequence = errors.New("safe available: invalid entry sequence")
	ErrInvalidSafeAvailableEntryDate     = errors.New("safe available: invalid entry date")
	ErrInvalidSafeAvailableEntryAmount   = errors.New("safe available: invalid entry amount")
	ErrInvalidSafeAvailableMissingData   = errors.New("safe available: invalid missing data")
	ErrSafeAvailableDuplicateEntry       = errors.New("safe available: duplicate entry")
	ErrSafeAvailableEntryOutsidePeriod   = errors.New("safe available: entry outside period")
	ErrSafeAvailableOverflow             = errors.New("safe available: amount overflow")
	ErrInvalidSafeAvailableSnapshot      = errors.New("safe available: invalid snapshot")
)

// SafeAvailablePeriod is an explicit inclusive civil period. No timezone,
// Clock or implicit current month is involved.
type SafeAvailablePeriod struct {
	startOn CivilDate
	endOn   CivilDate
}

func NewSafeAvailablePeriod(startOn, endOn CivilDate) (SafeAvailablePeriod, error) {
	if !startOn.valid() || !endOn.valid() || endOn.Before(startOn) {
		return SafeAvailablePeriod{}, ErrInvalidSafeAvailablePeriod
	}
	return SafeAvailablePeriod{startOn: startOn, endOn: endOn}, nil
}

func (period SafeAvailablePeriod) StartOn() CivilDate { return period.startOn }
func (period SafeAvailablePeriod) EndOn() CivilDate   { return period.endOn }

func (period SafeAvailablePeriod) contains(date CivilDate) bool {
	return !date.Before(period.startOn) && !period.endOn.Before(date)
}

// SafeAvailableEntry is one confirmed, positive component. The owner is
// retained in the snapshot boundary so a malformed cross-owner dependency
// fails closed before calculation.
type SafeAvailableEntry struct {
	kind     SafeAvailableEntryKind
	ownerID  string
	sourceID string
	sequence int
	dueOn    CivilDate
	amount   Money
}

type SafeAvailableEntryParams struct {
	Kind     SafeAvailableEntryKind
	OwnerID  string
	SourceID string
	Sequence int
	DueOn    CivilDate
	Amount   Money
}

func NewSafeAvailableEntry(params SafeAvailableEntryParams) (SafeAvailableEntry, error) {
	if !validSafeAvailableEntryKind(params.Kind) {
		return SafeAvailableEntry{}, ErrInvalidSafeAvailableEntryKind
	}
	if err := ValidateUserID(params.OwnerID); err != nil {
		return SafeAvailableEntry{}, ErrInvalidSafeAvailableEntryOwnerID
	}
	if !isValidIdentifier(params.SourceID) {
		return SafeAvailableEntry{}, ErrInvalidSafeAvailableEntryID
	}
	if params.Sequence < 0 || params.Kind == SafeAvailableEntryKindCommitment && params.Sequence == 0 {
		return SafeAvailableEntry{}, ErrInvalidSafeAvailableEntrySequence
	}
	if !params.DueOn.valid() {
		return SafeAvailableEntry{}, ErrInvalidSafeAvailableEntryDate
	}
	if params.Amount.Currency() != CurrencyBRL || params.Amount.MinorUnits() <= 0 {
		return SafeAvailableEntry{}, ErrInvalidSafeAvailableEntryAmount
	}
	return SafeAvailableEntry{
		kind: params.Kind, ownerID: params.OwnerID, sourceID: params.SourceID,
		sequence: params.Sequence, dueOn: params.DueOn, amount: params.Amount,
	}, nil
}

func (entry SafeAvailableEntry) Kind() SafeAvailableEntryKind { return entry.kind }
func (entry SafeAvailableEntry) OwnerID() string              { return entry.ownerID }
func (entry SafeAvailableEntry) SourceID() string             { return entry.sourceID }
func (entry SafeAvailableEntry) Sequence() int                { return entry.sequence }
func (entry SafeAvailableEntry) DueOn() CivilDate             { return entry.dueOn }
func (entry SafeAvailableEntry) Amount() Money                { return entry.amount }

// SafeAvailableSnapshot contains all explicitly supplied inputs for one
// calculation. It is an in-memory read model, never a persisted aggregate.
type SafeAvailableSnapshot struct {
	ownerID          string
	period           SafeAvailablePeriod
	availableBalance Money
	incomes          []SafeAvailableEntry
	expenses         []SafeAvailableEntry
	commitments      []SafeAvailableEntry
	missingData      []SafeAvailableMissingData
}

type SafeAvailableSnapshotParams struct {
	OwnerID          string
	Period           SafeAvailablePeriod
	AvailableBalance Money
	Incomes          []SafeAvailableEntry
	Expenses         []SafeAvailableEntry
	Commitments      []SafeAvailableEntry
	MissingData      []SafeAvailableMissingData
}

// NewSafeAvailableSnapshot validates and defensively copies a reader result.
// This Stage 1 has no confirmed budget source, so BUDGET is always carried as
// an explicit missing-data marker unless a future stage supplies one.
func NewSafeAvailableSnapshot(params SafeAvailableSnapshotParams) (SafeAvailableSnapshot, error) {
	if err := ValidateUserID(params.OwnerID); err != nil {
		return SafeAvailableSnapshot{}, ErrInvalidSafeAvailableOwnerID
	}
	if !params.Period.startOn.valid() || !params.Period.endOn.valid() || params.Period.endOn.Before(params.Period.startOn) {
		return SafeAvailableSnapshot{}, ErrInvalidSafeAvailablePeriod
	}
	if params.AvailableBalance.Currency() != CurrencyBRL {
		return SafeAvailableSnapshot{}, ErrInvalidSafeAvailableSnapshot
	}

	missingData, err := normalizeSafeAvailableMissingData(params.MissingData)
	if err != nil {
		return SafeAvailableSnapshot{}, err
	}
	if !containsSafeAvailableMissingData(missingData, SafeAvailableMissingBudget) {
		missingData = append(missingData, SafeAvailableMissingBudget)
		sort.Slice(missingData, func(left, right int) bool { return missingData[left] < missingData[right] })
	}
	seen := make(map[string]struct{}, len(params.Incomes)+len(params.Expenses)+len(params.Commitments))
	groups := []struct {
		entries []SafeAvailableEntry
		kind    SafeAvailableEntryKind
	}{
		{entries: params.Incomes, kind: SafeAvailableEntryKindIncome},
		{entries: params.Expenses, kind: SafeAvailableEntryKindExpense},
		{entries: params.Commitments, kind: SafeAvailableEntryKindCommitment},
	}
	for _, group := range groups {
		for _, entry := range group.entries {
			if entry.kind != group.kind {
				return SafeAvailableSnapshot{}, ErrInvalidSafeAvailableEntryKind
			}
			if err := validateSafeAvailableEntryForSnapshot(entry, params.OwnerID, params.Period); err != nil {
				return SafeAvailableSnapshot{}, err
			}
			key := safeAvailableEntryKey(entry)
			if _, exists := seen[key]; exists {
				return SafeAvailableSnapshot{}, ErrSafeAvailableDuplicateEntry
			}
			seen[key] = struct{}{}
		}
	}

	return SafeAvailableSnapshot{
		ownerID: params.OwnerID, period: params.Period, availableBalance: params.AvailableBalance,
		incomes:     append([]SafeAvailableEntry(nil), params.Incomes...),
		expenses:    append([]SafeAvailableEntry(nil), params.Expenses...),
		commitments: append([]SafeAvailableEntry(nil), params.Commitments...),
		missingData: append([]SafeAvailableMissingData(nil), missingData...),
	}, nil
}

func (snapshot SafeAvailableSnapshot) OwnerID() string             { return snapshot.ownerID }
func (snapshot SafeAvailableSnapshot) Period() SafeAvailablePeriod { return snapshot.period }
func (snapshot SafeAvailableSnapshot) AvailableBalance() Money     { return snapshot.availableBalance }
func (snapshot SafeAvailableSnapshot) Incomes() []SafeAvailableEntry {
	return append([]SafeAvailableEntry(nil), snapshot.incomes...)
}
func (snapshot SafeAvailableSnapshot) Expenses() []SafeAvailableEntry {
	return append([]SafeAvailableEntry(nil), snapshot.expenses...)
}
func (snapshot SafeAvailableSnapshot) Commitments() []SafeAvailableEntry {
	return append([]SafeAvailableEntry(nil), snapshot.commitments...)
}
func (snapshot SafeAvailableSnapshot) MissingData() []SafeAvailableMissingData {
	return append([]SafeAvailableMissingData(nil), snapshot.missingData...)
}

// SafeAvailableBreakdownLine is an immutable explanation of one calculation
// component. The balance line is synthetic and uses the period start date.
type SafeAvailableBreakdownLine struct {
	kind     SafeAvailableEntryKind
	sourceID string
	sequence int
	dueOn    CivilDate
	amount   Money
}

func (line SafeAvailableBreakdownLine) Kind() SafeAvailableEntryKind { return line.kind }
func (line SafeAvailableBreakdownLine) SourceID() string             { return line.sourceID }
func (line SafeAvailableBreakdownLine) Sequence() int                { return line.sequence }
func (line SafeAvailableBreakdownLine) DueOn() CivilDate             { return line.dueOn }
func (line SafeAvailableBreakdownLine) Amount() Money                { return line.amount }

// SafeAvailableResult is the deterministic, decomposable read model returned
// by CalculateSafeAvailable.
type SafeAvailableResult struct {
	ownerID               string
	period                SafeAvailablePeriod
	availableBalance      Money
	totalConfirmedIncome  Money
	totalConfirmedExpense Money
	totalCommitment       Money
	finalAmount           Money
	breakdown             []SafeAvailableBreakdownLine
	missingData           []SafeAvailableMissingData
}

func (result SafeAvailableResult) OwnerID() string                  { return result.ownerID }
func (result SafeAvailableResult) Period() SafeAvailablePeriod      { return result.period }
func (result SafeAvailableResult) AvailableBalance() Money          { return result.availableBalance }
func (result SafeAvailableResult) TotalConfirmedIncome() Money      { return result.totalConfirmedIncome }
func (result SafeAvailableResult) TotalConfirmedExpense() Money     { return result.totalConfirmedExpense }
func (result SafeAvailableResult) TotalConfirmedCommitments() Money { return result.totalCommitment }
func (result SafeAvailableResult) FinalAmount() Money               { return result.finalAmount }
func (result SafeAvailableResult) Breakdown() []SafeAvailableBreakdownLine {
	return append([]SafeAvailableBreakdownLine(nil), result.breakdown...)
}
func (result SafeAvailableResult) MissingData() []SafeAvailableMissingData {
	return append([]SafeAvailableMissingData(nil), result.missingData...)
}

// CalculateSafeAvailable applies the frozen conservative formula exactly:
// available balance + confirmed income - confirmed expenses - commitments.
// All arithmetic is integer-only and rejects overflow instead of wrapping.
func CalculateSafeAvailable(snapshot SafeAvailableSnapshot) (SafeAvailableResult, error) {
	if err := validateSafeAvailableSnapshotState(snapshot); err != nil {
		return SafeAvailableResult{}, err
	}

	incomeTotal, err := sumSafeAvailableEntries(snapshot.incomes)
	if err != nil {
		return SafeAvailableResult{}, err
	}
	expenseTotal, err := sumSafeAvailableEntries(snapshot.expenses)
	if err != nil {
		return SafeAvailableResult{}, err
	}
	commitmentTotal, err := sumSafeAvailableEntries(snapshot.commitments)
	if err != nil {
		return SafeAvailableResult{}, err
	}
	finalAmount, err := addSafeAvailable(snapshot.availableBalance.MinorUnits(), incomeTotal.MinorUnits())
	if err != nil {
		return SafeAvailableResult{}, err
	}
	finalAmount, err = subtractSafeAvailable(finalAmount, expenseTotal.MinorUnits())
	if err != nil {
		return SafeAvailableResult{}, err
	}
	finalAmount, err = subtractSafeAvailable(finalAmount, commitmentTotal.MinorUnits())
	if err != nil {
		return SafeAvailableResult{}, err
	}
	finalMoney, err := NewMoney(finalAmount, CurrencyBRL)
	if err != nil {
		return SafeAvailableResult{}, ErrSafeAvailableOverflow
	}

	breakdown := make([]SafeAvailableBreakdownLine, 0, 1+len(snapshot.incomes)+len(snapshot.expenses)+len(snapshot.commitments))
	breakdown = append(breakdown, SafeAvailableBreakdownLine{
		kind: SafeAvailableEntryKindAvailableBalance, sourceID: "available-balance", dueOn: snapshot.period.startOn, amount: snapshot.availableBalance,
	})
	appendBreakdown := func(entries []SafeAvailableEntry) {
		for _, entry := range entries {
			breakdown = append(breakdown, SafeAvailableBreakdownLine{
				kind: entry.kind, sourceID: entry.sourceID, sequence: entry.sequence, dueOn: entry.dueOn, amount: entry.amount,
			})
		}
	}
	appendBreakdown(snapshot.incomes)
	appendBreakdown(snapshot.expenses)
	appendBreakdown(snapshot.commitments)
	sort.Slice(breakdown, func(left, right int) bool { return safeAvailableBreakdownBefore(breakdown[left], breakdown[right]) })

	return SafeAvailableResult{
		ownerID: snapshot.ownerID, period: snapshot.period, availableBalance: snapshot.availableBalance,
		totalConfirmedIncome: incomeTotal, totalConfirmedExpense: expenseTotal, totalCommitment: commitmentTotal,
		finalAmount: finalMoney, breakdown: breakdown, missingData: append([]SafeAvailableMissingData(nil), snapshot.missingData...),
	}, nil
}

func validSafeAvailableEntryKind(kind SafeAvailableEntryKind) bool {
	switch kind {
	case SafeAvailableEntryKindIncome, SafeAvailableEntryKindExpense, SafeAvailableEntryKindCommitment:
		return true
	default:
		return false
	}
}

func validateSafeAvailableEntryForSnapshot(entry SafeAvailableEntry, ownerID string, period SafeAvailablePeriod) error {
	if !validSafeAvailableEntryKind(entry.kind) {
		return ErrInvalidSafeAvailableEntryKind
	}
	if entry.ownerID != ownerID || ValidateUserID(entry.ownerID) != nil {
		return ErrInvalidSafeAvailableEntryOwnerID
	}
	if !isValidIdentifier(entry.sourceID) {
		return ErrInvalidSafeAvailableEntryID
	}
	if entry.sequence < 0 || entry.kind == SafeAvailableEntryKindCommitment && entry.sequence == 0 {
		return ErrInvalidSafeAvailableEntrySequence
	}
	if !entry.dueOn.valid() {
		return ErrInvalidSafeAvailableEntryDate
	}
	if !period.contains(entry.dueOn) {
		return ErrSafeAvailableEntryOutsidePeriod
	}
	if entry.amount.Currency() != CurrencyBRL || entry.amount.MinorUnits() <= 0 {
		return ErrInvalidSafeAvailableEntryAmount
	}
	return nil
}

func validateSafeAvailableSnapshotState(snapshot SafeAvailableSnapshot) error {
	if ValidateUserID(snapshot.ownerID) != nil || !snapshot.period.startOn.valid() || !snapshot.period.endOn.valid() || snapshot.period.endOn.Before(snapshot.period.startOn) || snapshot.availableBalance.Currency() != CurrencyBRL {
		return ErrInvalidSafeAvailableSnapshot
	}
	normalizedMissingData, err := normalizeSafeAvailableMissingData(snapshot.missingData)
	if err != nil {
		return err
	}
	if !containsSafeAvailableMissingData(normalizedMissingData, SafeAvailableMissingBudget) {
		return ErrInvalidSafeAvailableSnapshot
	}
	seen := make(map[string]struct{}, len(snapshot.incomes)+len(snapshot.expenses)+len(snapshot.commitments))
	groups := []struct {
		entries []SafeAvailableEntry
		kind    SafeAvailableEntryKind
	}{
		{entries: snapshot.incomes, kind: SafeAvailableEntryKindIncome},
		{entries: snapshot.expenses, kind: SafeAvailableEntryKindExpense},
		{entries: snapshot.commitments, kind: SafeAvailableEntryKindCommitment},
	}
	for _, group := range groups {
		for _, entry := range group.entries {
			if entry.kind != group.kind {
				return ErrInvalidSafeAvailableEntryKind
			}
			if err := validateSafeAvailableEntryForSnapshot(entry, snapshot.ownerID, snapshot.period); err != nil {
				return err
			}
			key := safeAvailableEntryKey(entry)
			if _, exists := seen[key]; exists {
				return ErrSafeAvailableDuplicateEntry
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func normalizeSafeAvailableMissingData(values []SafeAvailableMissingData) ([]SafeAvailableMissingData, error) {
	seen := make(map[SafeAvailableMissingData]struct{}, len(values))
	result := make([]SafeAvailableMissingData, 0, len(values))
	for _, value := range values {
		switch value {
		case SafeAvailableMissingBudget, SafeAvailableMissingConfirmedIncome, SafeAvailableMissingConfirmedExpense, SafeAvailableMissingCommitments:
		default:
			return nil, ErrInvalidSafeAvailableMissingData
		}
		if _, exists := seen[value]; exists {
			return nil, ErrSafeAvailableDuplicateEntry
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func containsSafeAvailableMissingData(values []SafeAvailableMissingData, wanted SafeAvailableMissingData) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func safeAvailableEntryKey(entry SafeAvailableEntry) string {
	return string(entry.kind) + "\x00" + entry.sourceID + "\x00" + strconv.Itoa(entry.sequence)
}

func sumSafeAvailableEntries(entries []SafeAvailableEntry) (Money, error) {
	var total int64
	for _, entry := range entries {
		if entry.amount.MinorUnits() > 0 && total > math.MaxInt64-entry.amount.MinorUnits() {
			return Money{}, ErrSafeAvailableOverflow
		}
		total += entry.amount.MinorUnits()
	}
	return NewMoney(total, CurrencyBRL)
}

func addSafeAvailable(left, right int64) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, ErrSafeAvailableOverflow
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, ErrSafeAvailableOverflow
	}
	return left + right, nil
}

func subtractSafeAvailable(left, right int64) (int64, error) {
	if right > 0 && left < math.MinInt64+right {
		return 0, ErrSafeAvailableOverflow
	}
	return left - right, nil
}

func safeAvailableBreakdownBefore(left, right SafeAvailableBreakdownLine) bool {
	if !left.dueOn.Equal(right.dueOn) {
		return left.dueOn.Before(right.dueOn)
	}
	if safeAvailableKindRank(left.kind) != safeAvailableKindRank(right.kind) {
		return safeAvailableKindRank(left.kind) < safeAvailableKindRank(right.kind)
	}
	if left.sourceID != right.sourceID {
		return left.sourceID < right.sourceID
	}
	if left.sequence != right.sequence {
		return left.sequence < right.sequence
	}
	return left.amount.MinorUnits() < right.amount.MinorUnits()
}

func safeAvailableKindRank(kind SafeAvailableEntryKind) int {
	switch kind {
	case SafeAvailableEntryKindAvailableBalance:
		return 0
	case SafeAvailableEntryKindIncome:
		return 1
	case SafeAvailableEntryKindExpense:
		return 2
	case SafeAvailableEntryKindCommitment:
		return 3
	default:
		return 4
	}
}
