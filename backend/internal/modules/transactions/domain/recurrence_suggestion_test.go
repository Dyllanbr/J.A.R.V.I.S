package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/domain"
)

func TestRecurrenceSuggestionDescriptionNormalizationIsConservative(t *testing.T) {
	t.Parallel()

	equivalent := []string{"Academia", " academia ", "ACADEMIA", "Academia\t  ", "Academia\u00a0"}
	observations := make([]domain.RecurrenceSuggestionObservation, 0, len(equivalent))
	for index, description := range equivalent {
		observations = append(observations, suggestionObservation(t, index+1, description, 11900, civilDate(t, 2024, time.January, 10)))
	}
	for _, observation := range observations[1:] {
		if observation.NormalizedDescription() != observations[0].NormalizedDescription() {
			t.Fatalf("normalized description = %q, want %q", observation.NormalizedDescription(), observations[0].NormalizedDescription())
		}
	}
	if observations[1].Description() != "academia" || observations[3].Description() != "Academia" {
		t.Fatalf("display normalization did not trim/collapse whitespace: %q / %q", observations[1].Description(), observations[3].Description())
	}

	notEquivalent := []struct {
		left  string
		right string
	}{
		{left: "Netflix", right: "Netflix.com"},
		{left: "Academia XP", right: "Academia XP 2"},
		{left: "Cafe", right: "Café"},
		{left: "Spotify!", right: "Spotify"},
	}
	for _, testCase := range notEquivalent {
		left := suggestionObservation(t, 10, testCase.left, 100, civilDate(t, 2024, time.January, 1))
		right := suggestionObservation(t, 11, testCase.right, 100, civilDate(t, 2024, time.January, 1))
		if left.NormalizedDescription() == right.NormalizedDescription() {
			t.Errorf("%q and %q were grouped", testCase.left, testCase.right)
		}
	}

	composed := suggestionObservation(t, 12, "CAFÉ", 100, civilDate(t, 2024, time.January, 1))
	decomposed := suggestionObservation(t, 13, "cafe\u0301", 100, civilDate(t, 2024, time.January, 1))
	if composed.NormalizedDescription() != decomposed.NormalizedDescription() {
		t.Fatal("canonically equivalent Unicode descriptions did not normalize equally")
	}

	invalidDescriptions := []string{
		string([]byte{0xff}),
		"Academia\x00",
		strings.Repeat("a", domain.MaxExpenseDescriptionRunes+1),
	}
	for _, description := range invalidDescriptions {
		_, err := domain.NewRecurrenceSuggestionObservation(domain.RecurrenceSuggestionObservationParams{
			ExpenseID:   "expense-invalid-description",
			Description: description,
			Amount:      money(t, 100),
			OccurredOn:  civilDate(t, 2024, time.January, 1),
		})
		if !errors.Is(err, domain.ErrInvalidRecurrenceSuggestionObservation) {
			t.Errorf("invalid description error = %v", err)
		}
	}
}

func TestDetectRecurrenceSuggestionsThresholdWindowAndAmbiguity(t *testing.T) {
	t.Parallel()
	evaluated := civilDate(t, 2024, time.August, 24)

	tests := []struct {
		name    string
		dates   []domain.CivilDate
		amounts []int64
		want    int
		count   int
	}{
		{name: "exactly three", dates: dates(t, 2024, time.June, 10, time.July, 10, time.August, 10), want: 1, count: 3},
		{name: "four", dates: dates(t, 2024, time.May, 10, time.June, 10, time.July, 10, time.August, 10), want: 1, count: 4},
		{name: "five", dates: dates(t, 2024, time.April, 10, time.May, 10, time.June, 10, time.July, 10, time.August, 10), want: 1, count: 5},
		{name: "six", dates: dates(t, 2024, time.March, 10, time.April, 10, time.May, 10, time.June, 10, time.July, 10, time.August, 10), want: 1, count: 6},
		{name: "two", dates: dates(t, 2024, time.July, 10, time.August, 10)},
		{name: "gap", dates: dates(t, 2024, time.June, 10, time.August, 10, time.September, 10)},
		{name: "old run", dates: dates(t, 2024, time.March, 10, time.April, 10, time.May, 10)},
		{name: "different amount", dates: dates(t, 2024, time.June, 10, time.July, 10, time.August, 10), amounts: []int64{11900, 11900, 12000}},
		{name: "duplicate equivalent month", dates: dates(t, 2024, time.June, 9, time.June, 10, time.July, 10, time.August, 10)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			observations := make([]domain.RecurrenceSuggestionObservation, 0, len(testCase.dates))
			for index, date := range testCase.dates {
				amount := int64(11900)
				if len(testCase.amounts) > 0 {
					amount = testCase.amounts[index]
				}
				observations = append(observations, suggestionObservation(t, index+1, "Academia", amount, date))
			}
			result := detectSuggestions(t, evaluated, observations, nil)
			if len(result) != testCase.want {
				t.Fatalf("suggestions = %d, want %d", len(result), testCase.want)
			}
			if testCase.want == 1 && result[0].EvidenceCount() != testCase.count {
				t.Fatalf("evidence count = %d, want %d", result[0].EvidenceCount(), testCase.count)
			}
		})
	}
}

func TestDetectRecurrenceSuggestionsGroupsOnlyEquivalentDescriptions(t *testing.T) {
	t.Parallel()
	evaluated := civilDate(t, 2024, time.August, 24)
	equivalent := []domain.RecurrenceSuggestionObservation{
		suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.June, 10)),
		suggestionObservation(t, 2, " academia  ", 11900, civilDate(t, 2024, time.July, 10)),
		suggestionObservation(t, 3, "ACADEMIA", 11900, civilDate(t, 2024, time.August, 10)),
	}
	if result := detectSuggestions(t, evaluated, equivalent, nil); len(result) != 1 {
		t.Fatalf("equivalent descriptions produced %d suggestions", len(result))
	}

	for _, different := range []string{"Academia.com", "Academía", "Academia 2"} {
		observations := slices.Clone(equivalent)
		observations[2] = suggestionObservation(t, 4, different, 11900, civilDate(t, 2024, time.August, 10))
		if result := detectSuggestions(t, evaluated, observations, nil); len(result) != 0 {
			t.Errorf("different description %q was grouped", different)
		}
	}
}

func TestDetectRecurrenceSuggestionsUsesOnlySixCivilMonths(t *testing.T) {
	t.Parallel()
	observations := make([]domain.RecurrenceSuggestionObservation, 0, 7)
	for index, date := range dates(t, 2024,
		time.February, 10,
		time.March, 10,
		time.April, 10,
		time.May, 10,
		time.June, 10,
		time.July, 10,
		time.August, 10,
	) {
		observations = append(observations, suggestionObservation(t, index+1, "Academia", 11900, date))
	}
	result := detectSuggestions(t, civilDate(t, 2024, time.August, 24), observations, nil)
	if len(result) != 1 || result[0].EvidenceCount() != 6 || result[0].Evidence()[0].OccurredOn().String() != "2024-03-10" {
		t.Fatalf("six-month evidence = %+v", result)
	}
}

func TestDetectRecurrenceSuggestionsDayAnchorAndNextDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		evaluated  domain.CivilDate
		dates      []domain.CivilDate
		want       bool
		anchor     int
		proposedOn domain.CivilDate
	}{
		{name: "same day", evaluated: civilDate(t, 2024, time.August, 24), dates: dates(t, 2024, time.June, 10, time.July, 10, time.August, 10), want: true, anchor: 10, proposedOn: civilDate(t, 2024, time.September, 10)},
		{name: "plus and minus one", evaluated: civilDate(t, 2024, time.August, 24), dates: dates(t, 2024, time.June, 10, time.July, 11, time.August, 9), want: true, anchor: 10, proposedOn: civilDate(t, 2024, time.September, 10)},
		{name: "plus and minus two", evaluated: civilDate(t, 2024, time.August, 24), dates: dates(t, 2024, time.June, 8, time.July, 12, time.August, 10), want: true, anchor: 10, proposedOn: civilDate(t, 2024, time.September, 10)},
		{name: "deviation above two", evaluated: civilDate(t, 2024, time.August, 24), dates: dates(t, 2024, time.June, 7, time.July, 10, time.August, 13)},
		{name: "leap year month end", evaluated: civilDate(t, 2024, time.March, 31), dates: dates(t, 2024, time.January, 31, time.February, 29, time.March, 31), want: true, anchor: 31, proposedOn: civilDate(t, 2024, time.April, 30)},
		{name: "non leap month end", evaluated: civilDate(t, 2023, time.March, 31), dates: dates(t, 2023, time.January, 31, time.February, 28, time.March, 31), want: true, anchor: 31, proposedOn: civilDate(t, 2023, time.April, 30)},
		{name: "anchor twenty eight", evaluated: civilDate(t, 2024, time.April, 28), dates: dates(t, 2024, time.February, 28, time.March, 28, time.April, 28), want: true, anchor: 28, proposedOn: civilDate(t, 2024, time.May, 28)},
		{name: "anchor twenty nine", evaluated: civilDate(t, 2024, time.April, 29), dates: dates(t, 2024, time.February, 29, time.March, 29, time.April, 29), want: true, anchor: 29, proposedOn: civilDate(t, 2024, time.May, 29)},
		{name: "anchor thirty", evaluated: civilDate(t, 2024, time.March, 30), dates: dates(t, 2024, time.January, 30, time.February, 29, time.March, 30), want: true, anchor: 30, proposedOn: civilDate(t, 2024, time.April, 30)},
		{name: "current occurrence upcoming", evaluated: civilDate(t, 2024, time.August, 5), dates: dates(t, 2024, time.May, 10, time.June, 10, time.July, 10), want: true, anchor: 10, proposedOn: civilDate(t, 2024, time.August, 10)},
		{name: "current occurrence passed", evaluated: civilDate(t, 2024, time.August, 24), dates: dates(t, 2024, time.May, 10, time.June, 10, time.July, 10), want: true, anchor: 10, proposedOn: civilDate(t, 2024, time.September, 10)},
		{name: "year transition", evaluated: civilDate(t, 2024, time.December, 24), dates: dates(t, 2024, time.October, 10, time.November, 10, time.December, 10), want: true, anchor: 10, proposedOn: civilDate(t, 2025, time.January, 10)},
		{name: "supported calendar upper bound", evaluated: civilDate(t, 9999, time.December, 31), dates: dates(t, 9999, time.October, 10, time.November, 10, time.December, 10)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			observations := make([]domain.RecurrenceSuggestionObservation, 0, len(testCase.dates))
			for index, date := range testCase.dates {
				observations = append(observations, suggestionObservation(t, index+1, "Academia", 11900, date))
			}
			result := detectSuggestions(t, testCase.evaluated, observations, nil)
			if !testCase.want {
				if len(result) != 0 {
					t.Fatalf("suggestions = %d, want zero", len(result))
				}
				return
			}
			if len(result) != 1 {
				t.Fatalf("suggestions = %d, want one", len(result))
			}
			if result[0].AnchorDay() != testCase.anchor {
				t.Errorf("anchor = %d, want %d", result[0].AnchorDay(), testCase.anchor)
			}
			if !result[0].ProposedStartsOn().Equal(testCase.proposedOn) {
				t.Errorf("proposed = %s, want %s", result[0].ProposedStartsOn(), testCase.proposedOn)
			}
		})
	}
}

func TestDetectRecurrenceSuggestionsBreaksRealAnchorScoreTieWithHigherAnchor(t *testing.T) {
	t.Parallel()
	observations := []domain.RecurrenceSuggestionObservation{
		suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.May, 10)),
		suggestionObservation(t, 2, "Academia", 11900, civilDate(t, 2024, time.June, 11)),
		suggestionObservation(t, 3, "Academia", 11900, civilDate(t, 2024, time.July, 10)),
		suggestionObservation(t, 4, "Academia", 11900, civilDate(t, 2024, time.August, 11)),
	}

	// Anchors 10 and 11 both score sum=2 and max=1; the frozen final
	// tie-break therefore requires the higher anchor, 11.
	result := detectSuggestions(t, civilDate(t, 2024, time.August, 24), observations, nil)
	if len(result) != 1 {
		t.Fatalf("suggestions = %d, want one", len(result))
	}
	if result[0].AnchorDay() != 11 {
		t.Fatalf("anchor = %d, want higher tied anchor 11", result[0].AnchorDay())
	}
	if got := result[0].ProposedStartsOn().String(); got != "2024-09-11" {
		t.Fatalf("proposed startsOn = %s, want 2024-09-11", got)
	}
}

func TestDetectRecurrenceSuggestionsUsesMostRecentEligibleRun(t *testing.T) {
	t.Parallel()
	observations := []domain.RecurrenceSuggestionObservation{
		suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.March, 10)),
		suggestionObservation(t, 2, "Academia", 11900, civilDate(t, 2024, time.April, 10)),
		suggestionObservation(t, 3, "Academia", 11900, civilDate(t, 2024, time.May, 10)),
		suggestionObservation(t, 4, "Academia", 11900, civilDate(t, 2024, time.June, 20)),
		suggestionObservation(t, 5, "Academia", 11900, civilDate(t, 2024, time.July, 10)),
		suggestionObservation(t, 6, "Academia", 11900, civilDate(t, 2024, time.August, 10)),
	}
	if result := detectSuggestions(t, civilDate(t, 2024, time.August, 24), observations, nil); len(result) != 0 {
		t.Fatalf("stale old run produced %d suggestion(s)", len(result))
	}
}

func TestDetectRecurrenceSuggestionsSelectsLatestRunAvailableAroundGap(t *testing.T) {
	t.Parallel()
	evaluated := civilDate(t, 2024, time.August, 24)

	t.Run("recent eligible run excludes older pre-gap evidence", func(t *testing.T) {
		observations := []domain.RecurrenceSuggestionObservation{
			suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.March, 10)),
			suggestionObservation(t, 2, "Academia", 11900, civilDate(t, 2024, time.April, 10)),
			// May is the gap. Only June through August form the eligible run.
			suggestionObservation(t, 3, "Academia", 11900, civilDate(t, 2024, time.June, 11)),
			suggestionObservation(t, 4, "Academia", 11900, civilDate(t, 2024, time.July, 10)),
			suggestionObservation(t, 5, "Academia", 11900, civilDate(t, 2024, time.August, 11)),
		}
		result := detectSuggestions(t, evaluated, observations, nil)
		if len(result) != 1 {
			t.Fatalf("suggestions = %d, want one", len(result))
		}
		evidence := result[0].Evidence()
		if len(evidence) != 3 || evidence[0].OccurredOn().String() != "2024-06-11" || evidence[2].OccurredOn().String() != "2024-08-11" {
			t.Fatalf("selected evidence = %+v, want June-August run", evidence)
		}
	})

	t.Run("old eligible run cannot combine across gap with recent pair", func(t *testing.T) {
		observations := []domain.RecurrenceSuggestionObservation{
			suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.March, 10)),
			suggestionObservation(t, 2, "Academia", 11900, civilDate(t, 2024, time.April, 10)),
			suggestionObservation(t, 3, "Academia", 11900, civilDate(t, 2024, time.May, 10)),
			// June is the gap. July-August alone are insufficient and the old
			// run is not recent enough to escape freshness.
			suggestionObservation(t, 4, "Academia", 11900, civilDate(t, 2024, time.July, 10)),
			suggestionObservation(t, 5, "Academia", 11900, civilDate(t, 2024, time.August, 10)),
		}
		if result := detectSuggestions(t, evaluated, observations, nil); len(result) != 0 {
			t.Fatalf("gap-crossing evidence produced %d suggestion(s)", len(result))
		}
	})
}

func TestRecurrenceSuggestionFingerprintIsEvidenceStable(t *testing.T) {
	t.Parallel()
	base := []domain.RecurrenceSuggestionObservation{
		suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.June, 10)),
		suggestionObservation(t, 2, "ACADEMIA", 11900, civilDate(t, 2024, time.July, 10)),
		suggestionObservation(t, 3, " academia ", 11900, civilDate(t, 2024, time.August, 10)),
	}
	shuffled := []domain.RecurrenceSuggestionObservation{base[2], base[0], base[1]}
	first := detectSuggestions(t, civilDate(t, 2024, time.August, 24), base, nil)[0]
	second := detectSuggestions(t, civilDate(t, 2024, time.August, 25), shuffled, nil)[0]
	if !first.Fingerprint().Equal(second.Fingerprint()) {
		t.Fatal("same evidence produced a different fingerprint")
	}
	withNewEvidence := append(slices.Clone(base), suggestionObservation(t, 4, "Academia", 11900, civilDate(t, 2024, time.September, 10)))
	third := detectSuggestions(t, civilDate(t, 2024, time.September, 24), withNewEvidence, nil)[0]
	if first.Fingerprint().Equal(third.Fingerprint()) {
		t.Fatal("new evidence did not change fingerprint")
	}
	canonical := first.Fingerprint().CanonicalBytes()
	canonical[0] ^= 0xff
	if slices.Equal(canonical, first.Fingerprint().CanonicalBytes()) {
		t.Fatal("fingerprint exposed mutable byte alias")
	}
	evidence := first.Evidence()
	evidence[0] = domain.RecurrenceSuggestionObservation{}
	if first.Evidence()[0].ExpenseID() == "" {
		t.Fatal("evidence getter exposed mutable slice alias")
	}
}

func TestRecurrenceSuggestionExclusionsRespectLifecycle(t *testing.T) {
	t.Parallel()
	amount := money(t, 11900)
	observations := []domain.RecurrenceSuggestionObservation{
		suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.June, 10)),
		suggestionObservation(t, 2, "Academia", 11900, civilDate(t, 2024, time.July, 10)),
		suggestionObservation(t, 3, "Academia", 11900, civilDate(t, 2024, time.August, 10)),
	}
	active, err := domain.NewRecurrenceSuggestionExclusion(domain.RecurrenceSuggestionExclusionParams{
		Description: " academia ", Amount: amount, Status: domain.RecurrenceStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := detectSuggestions(t, civilDate(t, 2024, time.August, 24), observations, []domain.RecurrenceSuggestionExclusion{active}); len(result) != 0 {
		t.Fatal("ACTIVE equivalent recurrence did not suppress suggestion")
	}

	cancelledOn := civilDate(t, 2024, time.July, 15)
	cancelled, err := domain.NewRecurrenceSuggestionExclusion(domain.RecurrenceSuggestionExclusionParams{
		Description: "Academia", Amount: amount, Status: domain.RecurrenceStatusCancelled, CancelledOn: &cancelledOn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result := detectSuggestions(t, civilDate(t, 2024, time.August, 24), observations, []domain.RecurrenceSuggestionExclusion{cancelled}); len(result) != 0 {
		t.Fatal("pre-cancellation evidence was sufficient to resuggest")
	}
	newEvidence := []domain.RecurrenceSuggestionObservation{
		suggestionObservation(t, 4, "Academia", 11900, civilDate(t, 2024, time.August, 10)),
		suggestionObservation(t, 5, "Academia", 11900, civilDate(t, 2024, time.September, 10)),
		suggestionObservation(t, 6, "Academia", 11900, civilDate(t, 2024, time.October, 10)),
	}
	if result := detectSuggestions(t, civilDate(t, 2024, time.October, 24), newEvidence, []domain.RecurrenceSuggestionExclusion{cancelled}); len(result) != 1 {
		t.Fatalf("post-cancellation evidence produced %d suggestions, want one", len(result))
	}
}

func TestRecurrenceSuggestionExclusionsAreOrderIndependent(t *testing.T) {
	t.Parallel()
	observations := []domain.RecurrenceSuggestionObservation{
		suggestionObservation(t, 1, "Academia", 11900, civilDate(t, 2024, time.August, 10)),
		suggestionObservation(t, 2, "Academia", 11900, civilDate(t, 2024, time.September, 10)),
		suggestionObservation(t, 3, "Academia", 11900, civilDate(t, 2024, time.October, 10)),
	}
	olderDate := civilDate(t, 2024, time.July, 15)
	newerDate := civilDate(t, 2024, time.August, 15)
	older := suggestionExclusion(t, domain.RecurrenceStatusCancelled, &olderDate)
	newer := suggestionExclusion(t, domain.RecurrenceStatusCancelled, &newerDate)
	active := suggestionExclusion(t, domain.RecurrenceStatusActive, nil)
	evaluated := civilDate(t, 2024, time.October, 24)

	if result := detectSuggestions(t, evaluated, observations, []domain.RecurrenceSuggestionExclusion{older}); len(result) != 1 {
		t.Fatalf("older cutoff sanity check produced %d suggestions, want one", len(result))
	}

	activeOrders := []struct {
		name       string
		exclusions []domain.RecurrenceSuggestionExclusion
	}{
		{name: "older newer active", exclusions: []domain.RecurrenceSuggestionExclusion{older, newer, active}},
		{name: "older active newer", exclusions: []domain.RecurrenceSuggestionExclusion{older, active, newer}},
		{name: "newer older active", exclusions: []domain.RecurrenceSuggestionExclusion{newer, older, active}},
		{name: "newer active older", exclusions: []domain.RecurrenceSuggestionExclusion{newer, active, older}},
		{name: "active older newer", exclusions: []domain.RecurrenceSuggestionExclusion{active, older, newer}},
		{name: "active newer older", exclusions: []domain.RecurrenceSuggestionExclusion{active, newer, older}},
	}
	for _, testCase := range activeOrders {
		t.Run(testCase.name, func(t *testing.T) {
			if result := detectSuggestions(t, evaluated, observations, testCase.exclusions); len(result) != 0 {
				t.Fatalf("ACTIVE order produced %d suggestion(s)", len(result))
			}
		})
	}

	for _, testCase := range []struct {
		name       string
		exclusions []domain.RecurrenceSuggestionExclusion
	}{
		{name: "cancelled old then new", exclusions: []domain.RecurrenceSuggestionExclusion{older, newer}},
		{name: "cancelled new then old", exclusions: []domain.RecurrenceSuggestionExclusion{newer, older}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if result := detectSuggestions(t, evaluated, observations, testCase.exclusions); len(result) != 0 {
				t.Fatalf("latest cancellation cutoff order produced %d suggestion(s)", len(result))
			}
		})
	}
}

func TestRecurrenceSuggestionRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	validDate := civilDate(t, 2024, time.January, 10)
	validMoney := money(t, 100)
	tests := []domain.RecurrenceSuggestionObservationParams{
		{ExpenseID: "", Description: "Academia", Amount: validMoney, OccurredOn: validDate},
		{ExpenseID: "exp-1", Description: " ", Amount: validMoney, OccurredOn: validDate},
		{ExpenseID: "exp-1", Description: "Academia", Amount: money(t, 0), OccurredOn: validDate},
		{ExpenseID: "exp-1", Description: "Academia", Amount: validMoney},
	}
	for _, params := range tests {
		if _, err := domain.NewRecurrenceSuggestionObservation(params); !errors.Is(err, domain.ErrInvalidRecurrenceSuggestionObservation) {
			t.Errorf("NewRecurrenceSuggestionObservation(%+v) error = %v", params, err)
		}
	}
	if _, err := domain.DetectRecurrenceSuggestions(domain.DetectRecurrenceSuggestionsParams{}); !errors.Is(err, domain.ErrInvalidRecurrenceSuggestionEvaluation) {
		t.Fatalf("zero evaluation error = %v", err)
	}
	if _, err := domain.DetectRecurrenceSuggestions(domain.DetectRecurrenceSuggestionsParams{EvaluatedOn: validDate, Observations: []domain.RecurrenceSuggestionObservation{{}}}); !errors.Is(err, domain.ErrInvalidRecurrenceSuggestionObservation) {
		t.Fatalf("zero observation error = %v", err)
	}

	zeroDate := domain.CivilDate{}
	invalidExclusions := []domain.RecurrenceSuggestionExclusionParams{
		{Description: "", Amount: validMoney, Status: domain.RecurrenceStatusActive},
		{Description: "Academia", Amount: money(t, 0), Status: domain.RecurrenceStatusActive},
		{Description: "Academia", Amount: validMoney, Status: domain.RecurrenceStatusActive, CancelledOn: &validDate},
		{Description: "Academia", Amount: validMoney, Status: domain.RecurrenceStatusCancelled},
		{Description: "Academia", Amount: validMoney, Status: domain.RecurrenceStatusCancelled, CancelledOn: &zeroDate},
		{Description: "Academia", Amount: validMoney, Status: domain.RecurrenceStatus("UNKNOWN")},
	}
	for _, params := range invalidExclusions {
		if _, err := domain.NewRecurrenceSuggestionExclusion(params); !errors.Is(err, domain.ErrInvalidRecurrenceSuggestionExclusion) {
			t.Errorf("NewRecurrenceSuggestionExclusion(%+v) error = %v", params, err)
		}
	}
}

func detectSuggestions(t *testing.T, evaluated domain.CivilDate, observations []domain.RecurrenceSuggestionObservation, exclusions []domain.RecurrenceSuggestionExclusion) []domain.RecurrenceSuggestion {
	t.Helper()
	result, err := domain.DetectRecurrenceSuggestions(domain.DetectRecurrenceSuggestionsParams{
		EvaluatedOn: evaluated, Observations: observations, Exclusions: exclusions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func suggestionObservation(t *testing.T, id int, description string, amountMinor int64, occurredOn domain.CivilDate) domain.RecurrenceSuggestionObservation {
	t.Helper()
	observation, err := domain.NewRecurrenceSuggestionObservation(domain.RecurrenceSuggestionObservationParams{
		ExpenseID:   "expense-" + time.Date(2000, 1, id, 0, 0, 0, 0, time.UTC).Format("20060102"),
		Description: description,
		Amount:      money(t, amountMinor),
		OccurredOn:  occurredOn,
	})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func suggestionExclusion(t *testing.T, status domain.RecurrenceStatus, cancelledOn *domain.CivilDate) domain.RecurrenceSuggestionExclusion {
	t.Helper()
	exclusion, err := domain.NewRecurrenceSuggestionExclusion(domain.RecurrenceSuggestionExclusionParams{
		Description: "Academia",
		Amount:      money(t, 11900),
		Status:      status,
		CancelledOn: cancelledOn,
	})
	if err != nil {
		t.Fatal(err)
	}
	return exclusion
}

func money(t *testing.T, minor int64) domain.Money {
	t.Helper()
	value, err := domain.NewMoney(minor, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func civilDate(t *testing.T, year int, month time.Month, day int) domain.CivilDate {
	t.Helper()
	value, err := domain.NewCivilDate(year, month, day)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func dates(t *testing.T, year int, values ...any) []domain.CivilDate {
	t.Helper()
	result := make([]domain.CivilDate, 0, len(values)/2)
	for index := 0; index < len(values); index += 2 {
		result = append(result, civilDate(t, year, values[index].(time.Month), values[index+1].(int)))
	}
	return result
}
