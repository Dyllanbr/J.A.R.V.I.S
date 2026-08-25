package domain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const RecurrenceSuggestionRuleVersion = "expense-monthly-v1"

var (
	ErrInvalidRecurrenceSuggestionObservation = errors.New("recurrence suggestion: invalid observation")
	ErrInvalidRecurrenceSuggestionExclusion   = errors.New("recurrence suggestion: invalid recurrence exclusion")
	ErrInvalidRecurrenceSuggestionEvaluation  = errors.New("recurrence suggestion: invalid evaluation date")
)

// RecurrenceSuggestionObservation is an immutable, expense-only observation
// prepared for deterministic detection. OccurredOn is a civil date; the
// detector has no timezone or infrastructure dependency.
type RecurrenceSuggestionObservation struct {
	expenseID             string
	description           string
	normalizedDescription string
	amount                Money
	occurredOn            CivilDate
}

type RecurrenceSuggestionObservationParams struct {
	ExpenseID   string
	Description string
	Amount      Money
	OccurredOn  CivilDate
}

func NewRecurrenceSuggestionObservation(params RecurrenceSuggestionObservationParams) (RecurrenceSuggestionObservation, error) {
	if !isValidIdentifier(params.ExpenseID) || params.Amount.MinorUnits() <= 0 || !params.OccurredOn.valid() {
		return RecurrenceSuggestionObservation{}, ErrInvalidRecurrenceSuggestionObservation
	}
	description, normalized, ok := normalizeRecurrenceSuggestionDescription(params.Description)
	if !ok {
		return RecurrenceSuggestionObservation{}, ErrInvalidRecurrenceSuggestionObservation
	}
	return RecurrenceSuggestionObservation{
		expenseID:             params.ExpenseID,
		description:           description,
		normalizedDescription: normalized,
		amount:                params.Amount,
		occurredOn:            params.OccurredOn,
	}, nil
}

func (observation RecurrenceSuggestionObservation) ExpenseID() string { return observation.expenseID }
func (observation RecurrenceSuggestionObservation) Description() string {
	return observation.description
}
func (observation RecurrenceSuggestionObservation) NormalizedDescription() string {
	return observation.normalizedDescription
}
func (observation RecurrenceSuggestionObservation) Amount() Money { return observation.amount }
func (observation RecurrenceSuggestionObservation) OccurredOn() CivilDate {
	return observation.occurredOn
}

func (observation RecurrenceSuggestionObservation) valid() bool {
	if !isValidIdentifier(observation.expenseID) || observation.amount.MinorUnits() <= 0 || !observation.occurredOn.valid() {
		return false
	}
	description, normalized, ok := normalizeRecurrenceSuggestionDescription(observation.description)
	return ok && description == observation.description && normalized == observation.normalizedDescription
}

// RecurrenceSuggestionExclusion carries only the confirmed recurrence data
// needed to suppress duplicate inference. A cancelled exclusion has a civil
// cutoff; only later observations may support a new suggestion.
type RecurrenceSuggestionExclusion struct {
	normalizedDescription string
	amount                Money
	status                RecurrenceStatus
	cancelledOn           CivilDate
}

type RecurrenceSuggestionExclusionParams struct {
	Description string
	Amount      Money
	Status      RecurrenceStatus
	CancelledOn *CivilDate
}

func NewRecurrenceSuggestionExclusion(params RecurrenceSuggestionExclusionParams) (RecurrenceSuggestionExclusion, error) {
	_, normalized, ok := normalizeRecurrenceSuggestionDescription(params.Description)
	if !ok || params.Amount.MinorUnits() <= 0 {
		return RecurrenceSuggestionExclusion{}, ErrInvalidRecurrenceSuggestionExclusion
	}
	exclusion := RecurrenceSuggestionExclusion{
		normalizedDescription: normalized,
		amount:                params.Amount,
		status:                params.Status,
	}
	switch params.Status {
	case RecurrenceStatusActive:
		if params.CancelledOn != nil {
			return RecurrenceSuggestionExclusion{}, ErrInvalidRecurrenceSuggestionExclusion
		}
	case RecurrenceStatusCancelled:
		if params.CancelledOn == nil || !params.CancelledOn.valid() {
			return RecurrenceSuggestionExclusion{}, ErrInvalidRecurrenceSuggestionExclusion
		}
		exclusion.cancelledOn = *params.CancelledOn
	default:
		return RecurrenceSuggestionExclusion{}, ErrInvalidRecurrenceSuggestionExclusion
	}
	return exclusion, nil
}

func (exclusion RecurrenceSuggestionExclusion) valid() bool {
	if exclusion.normalizedDescription == "" || exclusion.amount.MinorUnits() <= 0 {
		return false
	}
	switch exclusion.status {
	case RecurrenceStatusActive:
		return !exclusion.cancelledOn.valid()
	case RecurrenceStatusCancelled:
		return exclusion.cancelledOn.valid()
	default:
		return false
	}
}

// RecurrenceSuggestionFingerprint is the canonical, unhashed evidence
// identity. Hashing and owner scoping belong to the application layer.
type RecurrenceSuggestionFingerprint struct {
	canonical string
}

func (fingerprint RecurrenceSuggestionFingerprint) CanonicalBytes() []byte {
	return append([]byte(nil), fingerprint.canonical...)
}

func (fingerprint RecurrenceSuggestionFingerprint) Equal(other RecurrenceSuggestionFingerprint) bool {
	return fingerprint.canonical == other.canonical
}

// RecurrenceSuggestion represents a deterministic inference, never a
// confirmed Recurrence and never an occurred financial transaction.
type RecurrenceSuggestion struct {
	description           string
	normalizedDescription string
	expectedAmount        Money
	anchorDay             int
	proposedStartsOn      CivilDate
	evidence              []RecurrenceSuggestionObservation
	fingerprint           RecurrenceSuggestionFingerprint
}

func (suggestion RecurrenceSuggestion) Description() string { return suggestion.description }
func (suggestion RecurrenceSuggestion) NormalizedDescription() string {
	return suggestion.normalizedDescription
}
func (suggestion RecurrenceSuggestion) ExpectedAmount() Money { return suggestion.expectedAmount }
func (suggestion RecurrenceSuggestion) AnchorDay() int        { return suggestion.anchorDay }
func (suggestion RecurrenceSuggestion) ProposedStartsOn() CivilDate {
	return suggestion.proposedStartsOn
}
func (suggestion RecurrenceSuggestion) EvidenceCount() int  { return len(suggestion.evidence) }
func (suggestion RecurrenceSuggestion) RuleVersion() string { return RecurrenceSuggestionRuleVersion }
func (suggestion RecurrenceSuggestion) Fingerprint() RecurrenceSuggestionFingerprint {
	return suggestion.fingerprint
}
func (suggestion RecurrenceSuggestion) Evidence() []RecurrenceSuggestionObservation {
	return append([]RecurrenceSuggestionObservation(nil), suggestion.evidence...)
}

type DetectRecurrenceSuggestionsParams struct {
	EvaluatedOn  CivilDate
	Observations []RecurrenceSuggestionObservation
	Exclusions   []RecurrenceSuggestionExclusion
}

// DetectRecurrenceSuggestions applies the frozen, conservative monthly rules.
// Input order is irrelevant and the returned order is deterministic.
func DetectRecurrenceSuggestions(params DetectRecurrenceSuggestionsParams) ([]RecurrenceSuggestion, error) {
	if !params.EvaluatedOn.valid() {
		return nil, ErrInvalidRecurrenceSuggestionEvaluation
	}
	for _, observation := range params.Observations {
		if !observation.valid() {
			return nil, ErrInvalidRecurrenceSuggestionObservation
		}
	}
	for _, exclusion := range params.Exclusions {
		if !exclusion.valid() {
			return nil, ErrInvalidRecurrenceSuggestionExclusion
		}
	}

	evaluationMonth := civilMonthIndex(params.EvaluatedOn)
	windowStart := evaluationMonth - 5
	groups := make(map[recurrenceSuggestionGroupKey][]RecurrenceSuggestionObservation)
	for _, observation := range params.Observations {
		month := civilMonthIndex(observation.occurredOn)
		if month < windowStart || month > evaluationMonth || params.EvaluatedOn.Before(observation.occurredOn) {
			continue
		}
		key := recurrenceSuggestionGroupKey{
			normalizedDescription: observation.normalizedDescription,
			amountMinor:           observation.amount.MinorUnits(),
		}
		groups[key] = append(groups[key], observation)
	}

	suggestions := make([]RecurrenceSuggestion, 0, len(groups))
	for key, observations := range groups {
		active, cancelledOn := recurrenceSuggestionExclusionFor(key, params.Exclusions)
		if active {
			continue
		}
		filtered := observations[:0]
		for _, observation := range observations {
			if cancelledOn.valid() && !cancelledOn.Before(observation.occurredOn) {
				continue
			}
			filtered = append(filtered, observation)
		}
		if len(filtered) < 3 || hasDuplicateSuggestionMonth(filtered) {
			continue
		}
		sort.Slice(filtered, func(left, right int) bool {
			if !filtered[left].occurredOn.Equal(filtered[right].occurredOn) {
				return filtered[left].occurredOn.Before(filtered[right].occurredOn)
			}
			return filtered[left].expenseID < filtered[right].expenseID
		})
		evidence, anchor, ok := mostRecentEligibleSuggestionRun(filtered, evaluationMonth)
		if !ok {
			continue
		}
		proposed, ok := nextSuggestedDate(evidence[len(evidence)-1].occurredOn, params.EvaluatedOn, anchor)
		if !ok {
			continue
		}
		display := evidence[len(evidence)-1].description
		fingerprint := buildRecurrenceSuggestionFingerprint(key, anchor, evidence)
		suggestions = append(suggestions, RecurrenceSuggestion{
			description:           display,
			normalizedDescription: key.normalizedDescription,
			expectedAmount:        evidence[0].amount,
			anchorDay:             anchor,
			proposedStartsOn:      proposed,
			evidence:              append([]RecurrenceSuggestionObservation(nil), evidence...),
			fingerprint:           fingerprint,
		})
	}

	sort.Slice(suggestions, func(left, right int) bool {
		if suggestions[left].normalizedDescription != suggestions[right].normalizedDescription {
			return suggestions[left].normalizedDescription < suggestions[right].normalizedDescription
		}
		if suggestions[left].expectedAmount.MinorUnits() != suggestions[right].expectedAmount.MinorUnits() {
			return suggestions[left].expectedAmount.MinorUnits() < suggestions[right].expectedAmount.MinorUnits()
		}
		return bytes.Compare(suggestions[left].fingerprint.CanonicalBytes(), suggestions[right].fingerprint.CanonicalBytes()) < 0
	})
	return suggestions, nil
}

type recurrenceSuggestionGroupKey struct {
	normalizedDescription string
	amountMinor           int64
}

func recurrenceSuggestionExclusionFor(key recurrenceSuggestionGroupKey, exclusions []RecurrenceSuggestionExclusion) (bool, CivilDate) {
	var latestCancellation CivilDate
	for _, exclusion := range exclusions {
		if exclusion.normalizedDescription != key.normalizedDescription || exclusion.amount.MinorUnits() != key.amountMinor {
			continue
		}
		if exclusion.status == RecurrenceStatusActive {
			return true, CivilDate{}
		}
		if !latestCancellation.valid() || latestCancellation.Before(exclusion.cancelledOn) {
			latestCancellation = exclusion.cancelledOn
		}
	}
	return false, latestCancellation
}

func hasDuplicateSuggestionMonth(observations []RecurrenceSuggestionObservation) bool {
	seen := make(map[int64]struct{}, len(observations))
	for _, observation := range observations {
		month := civilMonthIndex(observation.occurredOn)
		if _, exists := seen[month]; exists {
			return true
		}
		seen[month] = struct{}{}
	}
	return false
}

func mostRecentEligibleSuggestionRun(observations []RecurrenceSuggestionObservation, evaluationMonth int64) ([]RecurrenceSuggestionObservation, int, bool) {
	for end := len(observations) - 1; end >= 2; end-- {
		endMonth := civilMonthIndex(observations[end].occurredOn)
		if evaluationMonth-endMonth > 1 {
			break
		}
		startOfConsecutiveRun := end
		for startOfConsecutiveRun > 0 && civilMonthIndex(observations[startOfConsecutiveRun-1].occurredOn)+1 == civilMonthIndex(observations[startOfConsecutiveRun].occurredOn) {
			startOfConsecutiveRun--
		}
		for start := startOfConsecutiveRun; start <= end-2; start++ {
			candidate := observations[start : end+1]
			if anchor, ok := inferRecurrenceSuggestionAnchor(candidate); ok {
				return append([]RecurrenceSuggestionObservation(nil), candidate...), anchor, true
			}
		}
		end = startOfConsecutiveRun
	}
	return nil, 0, false
}

func inferRecurrenceSuggestionAnchor(observations []RecurrenceSuggestionObservation) (int, bool) {
	bestAnchor, bestSum, bestMaximum := 0, 0, 0
	for anchor := 1; anchor <= 31; anchor++ {
		sum, maximum, valid := 0, 0, true
		for _, observation := range observations {
			expected := anchor
			if last := daysInMonth(observation.occurredOn.year, observation.occurredOn.month); expected > last {
				expected = last
			}
			deviation := observation.occurredOn.day - expected
			if deviation < 0 {
				deviation = -deviation
			}
			if deviation > 2 {
				valid = false
				break
			}
			sum += deviation
			if deviation > maximum {
				maximum = deviation
			}
		}
		if !valid {
			continue
		}
		if bestAnchor == 0 || sum < bestSum || sum == bestSum && maximum < bestMaximum || sum == bestSum && maximum == bestMaximum && anchor > bestAnchor {
			bestAnchor, bestSum, bestMaximum = anchor, sum, maximum
		}
	}
	return bestAnchor, bestAnchor != 0
}

func nextSuggestedDate(latest, evaluated CivilDate, anchor int) (CivilDate, bool) {
	month := civilMonthIndex(latest) + 1
	for month <= int64(9999*12-1) {
		candidate, ok := civilDateFromMonthIndex(month, anchor)
		if !ok {
			return CivilDate{}, false
		}
		if evaluated.Before(candidate) {
			return candidate, true
		}
		month++
	}
	return CivilDate{}, false
}

func civilMonthIndex(date CivilDate) int64 {
	return int64(date.year-1)*12 + int64(date.month-1)
}

func civilDateFromMonthIndex(monthIndex int64, anchor int) (CivilDate, bool) {
	if monthIndex < 0 || monthIndex > int64(9999*12-1) || anchor < 1 || anchor > 31 {
		return CivilDate{}, false
	}
	year := int(monthIndex/12) + 1
	month := time.Month(monthIndex%12 + 1)
	day := anchor
	if last := daysInMonth(year, month); day > last {
		day = last
	}
	date, err := NewCivilDate(year, month, day)
	return date, err == nil
}

func normalizeRecurrenceSuggestionDescription(value string) (string, string, bool) {
	if !utf8.ValidString(value) {
		return "", "", false
	}
	fields := strings.FieldsFunc(value, unicode.IsSpace)
	if len(fields) == 0 {
		return "", "", false
	}
	display := norm.NFC.String(strings.Join(fields, " "))
	if utf8.RuneCountInString(display) > maxFinancialDescriptionRunes {
		return "", "", false
	}
	for _, character := range display {
		if unicode.IsControl(character) {
			return "", "", false
		}
	}
	normalized := norm.NFC.String(cases.Fold().String(display))
	return display, normalized, true
}

func buildRecurrenceSuggestionFingerprint(key recurrenceSuggestionGroupKey, anchor int, evidence []RecurrenceSuggestionObservation) RecurrenceSuggestionFingerprint {
	var buffer bytes.Buffer
	writeSuggestionFingerprintString(&buffer, RecurrenceSuggestionRuleVersion)
	writeSuggestionFingerprintString(&buffer, key.normalizedDescription)
	writeSuggestionFingerprintInt64(&buffer, key.amountMinor)
	writeSuggestionFingerprintString(&buffer, string(CurrencyBRL))
	writeSuggestionFingerprintInt64(&buffer, int64(anchor))
	writeSuggestionFingerprintInt64(&buffer, int64(len(evidence)))
	for _, observation := range evidence {
		writeSuggestionFingerprintString(&buffer, observation.expenseID)
		writeSuggestionFingerprintString(&buffer, observation.occurredOn.String())
	}
	return RecurrenceSuggestionFingerprint{canonical: buffer.String()}
}

func writeSuggestionFingerprintString(buffer *bytes.Buffer, value string) {
	writeSuggestionFingerprintInt64(buffer, int64(len(value)))
	_, _ = buffer.WriteString(value)
}

func writeSuggestionFingerprintInt64(buffer *bytes.Buffer, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = buffer.Write(encoded[:])
}
