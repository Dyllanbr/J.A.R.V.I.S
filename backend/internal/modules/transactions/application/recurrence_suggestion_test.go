package application_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"jarvis/backend/internal/modules/transactions/application"
	"jarvis/backend/internal/modules/transactions/domain"
)

func TestListRecurrenceSuggestionsBuildsOwnerScopedWindowAndStableIdentity(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	now := time.Date(2024, time.August, 24, 12, 30, 0, 0, location)
	expenses := suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.June, 10),
		localDate(t, 2024, time.July, 10),
		localDate(t, 2024, time.August, 10),
	)
	expenseReader := &suggestionExpenseReaderFake{expenses: expenses}
	recurrenceReader := &suggestionRecurrenceReaderFake{}
	suppressions := newSuggestionSuppressionFake()
	clock := &suggestionClockFake{now: now}
	useCase := mustListSuggestions(t, expenseReader, recurrenceReader, suppressions, clock)

	result, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	item := result.Items[0]
	if err := application.ValidateRecurrenceSuggestionID(item.ID); err != nil {
		t.Fatalf("suggestion ID invalid: %v", err)
	}
	if item.Suggestion.Description() != "Academia" || item.Suggestion.ExpectedAmount().MinorUnits() != 11900 || item.Suggestion.AnchorDay() != 10 {
		t.Fatalf("unexpected suggestion: %+v", item)
	}
	if got := item.Suggestion.ProposedStartsOn().String(); got != "2024-09-10" {
		t.Fatalf("proposed startsOn = %s", got)
	}
	if expenseReader.calls != 1 || clock.calls != 1 || recurrenceReader.calls != 1 || suppressions.calls != 1 {
		t.Fatalf("calls expense/clock/recurrence/suppression = %d/%d/%d/%d", expenseReader.calls, clock.calls, recurrenceReader.calls, suppressions.calls)
	}
	query := expenseReader.queries[0]
	if query.UserID != "owner-a" || query.FinancialTimezone != application.FinancialTimezone {
		t.Fatalf("query owner/timezone = %q/%q", query.UserID, query.FinancialTimezone)
	}
	wantStart := time.Date(2024, time.March, 1, 0, 0, 0, 0, location).UTC()
	wantEnd := time.Date(2024, time.August, 25, 0, 0, 0, 0, location).UTC()
	if !query.Start.Equal(wantStart) || !query.End.Equal(wantEnd) {
		t.Fatalf("query range = [%s,%s), want [%s,%s)", query.Start, query.End, wantStart, wantEnd)
	}

	expenseReader.expenses = []domain.Expense{expenses[2], expenses[0], expenses[1]}
	again, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if again.Items[0].ID != item.ID || again.Items[0].Fingerprint != item.Fingerprint {
		t.Fatal("same evidence in a different adapter order changed identity")
	}

	expenseReader.expenses = suggestionExpenses(t, "owner-b", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))
	otherOwner, err := useCase.Execute(context.Background(), "owner-b")
	if err != nil {
		t.Fatal(err)
	}
	if otherOwner.Items[0].ID == item.ID {
		t.Fatal("owner-scoped suggestion IDs collided")
	}
}

func TestListRecurrenceSuggestionsUsesSaoPauloCivilDateNotFixedOffset(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	// 02:30 UTC is still the previous civil day in America/Sao_Paulo.
	now := time.Date(2024, time.August, 24, 2, 30, 0, 0, time.UTC)
	expenseReader := &suggestionExpenseReaderFake{}
	useCase := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, newSuggestionSuppressionFake(), &suggestionClockFake{now: now})
	result, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if result.Items == nil || len(result.Items) != 0 {
		t.Fatalf("empty items = %#v, want non-nil empty slice", result.Items)
	}
	wantEnd := time.Date(2024, time.August, 24, 0, 0, 0, 0, location).UTC()
	if !expenseReader.queries[0].End.Equal(wantEnd) {
		t.Fatalf("window end = %s, want %s", expenseReader.queries[0].End, wantEnd)
	}
}

func TestListRecurrenceSuggestionsObservationWindowCrossesCalendarYear(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	tests := []struct {
		name      string
		now       time.Time
		expenses  []domain.Expense
		wantEnd   time.Time
		wantItems int
	}{
		{
			name:    "first day of January",
			now:     time.Date(2025, time.January, 1, 0, 30, 0, 0, location),
			wantEnd: time.Date(2025, time.January, 2, 0, 0, 0, 0, location),
		},
		{
			name: "December to January evidence",
			now:  time.Date(2025, time.January, 24, 12, 0, 0, 0, location),
			expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
				localDate(t, 2024, time.November, 10),
				localDate(t, 2024, time.December, 10),
				localDate(t, 2025, time.January, 10),
			),
			wantEnd:   time.Date(2025, time.January, 25, 0, 0, 0, 0, location),
			wantItems: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			expenseReader := &suggestionExpenseReaderFake{expenses: testCase.expenses}
			useCase := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, newSuggestionSuppressionFake(), &suggestionClockFake{now: testCase.now})
			result, err := useCase.Execute(context.Background(), "owner-a")
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != testCase.wantItems {
				t.Fatalf("items = %d, want %d", len(result.Items), testCase.wantItems)
			}
			query := expenseReader.queries[0]
			wantStart := time.Date(2024, time.August, 1, 0, 0, 0, 0, location)
			if !query.Start.Equal(wantStart.UTC()) || !query.End.Equal(testCase.wantEnd.UTC()) {
				t.Fatalf("query range = [%s,%s), want [%s,%s)", query.Start, query.End, wantStart.UTC(), testCase.wantEnd.UTC())
			}
			if query.Start.Location() != time.UTC || query.End.Location() != time.UTC || query.FinancialTimezone != application.FinancialTimezone {
				t.Fatalf("query boundary/timezone = %s/%s/%q", query.Start.Location(), query.End.Location(), query.FinancialTimezone)
			}
			if testCase.wantItems == 1 && result.Items[0].Suggestion.ProposedStartsOn().String() != "2025-02-10" {
				t.Fatalf("proposed startsOn = %s, want 2025-02-10", result.Items[0].Suggestion.ProposedStartsOn())
			}
		})
	}
}

func TestListRecurrenceSuggestionsOrdersAndSuppressesDeterministically(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	expenses := append(
		suggestionExpenses(t, "owner-a", "Software", 5000,
			localDate(t, 2024, time.June, 20), localDate(t, 2024, time.July, 20), localDate(t, 2024, time.August, 20)),
		suggestionExpenses(t, "owner-a", "Internet", 10000,
			localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))...,
	)
	expenses = append(expenses, suggestionExpenses(t, "owner-a", "Academia", 20000,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))...)
	expenseReader := &suggestionExpenseReaderFake{expenses: []domain.Expense{expenses[5], expenses[1], expenses[7], expenses[3], expenses[0], expenses[8], expenses[4], expenses[2], expenses[6]}}
	suppressions := newSuggestionSuppressionFake()
	useCase := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, suppressions, &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)})

	result, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{result.Items[0].Suggestion.Description(), result.Items[1].Suggestion.Description(), result.Items[2].Suggestion.Description()}
	if !slices.Equal(got, []string{"Academia", "Internet", "Software"}) {
		t.Fatalf("order = %v", got)
	}

	suppressions.dismissed["owner-a"] = map[application.RecurrenceSuggestionID]bool{result.Items[1].ID: true}
	filtered, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Items) != 2 || filtered.Items[0].Suggestion.Description() != "Academia" || filtered.Items[1].Suggestion.Description() != "Software" {
		t.Fatalf("suppressed result = %+v", filtered.Items)
	}
}

func TestListRecurrenceSuggestionsRespectsActiveAndCancelledRecurrences(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	now := time.Date(2024, time.October, 24, 12, 0, 0, 0, location)
	expenseReader := &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.August, 10), localDate(t, 2024, time.September, 10), localDate(t, 2024, time.October, 10))}
	recurrenceReader := &suggestionRecurrenceReaderFake{recurrences: []domain.Recurrence{suggestionRecurrence(t, "owner-a", " academia ", 11900, nil)}}
	useCase := mustListSuggestions(t, expenseReader, recurrenceReader, newSuggestionSuppressionFake(), &suggestionClockFake{now: now})
	result, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 {
		t.Fatal("ACTIVE equivalent recurrence did not suppress inference")
	}

	cancelledAt := time.Date(2024, time.July, 15, 12, 0, 0, 0, location)
	recurrenceReader.recurrences = []domain.Recurrence{suggestionRecurrence(t, "owner-a", "Academia", 11900, &cancelledAt)}
	result, err = useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("post-cancellation evidence produced %d suggestions", len(result.Items))
	}

	cancelledAt = time.Date(2024, time.September, 15, 12, 0, 0, 0, location)
	recurrenceReader.recurrences = []domain.Recurrence{suggestionRecurrence(t, "owner-a", "Academia", 11900, &cancelledAt)}
	result, err = useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 0 {
		t.Fatal("evidence at/before cancellation was reused")
	}
}

func TestListRecurrenceSuggestionsNewEvidenceEscapesOnlyOldSuppression(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	expenseReader := &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))}
	suppressions := newSuggestionSuppressionFake()
	clock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)}
	useCase := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, suppressions, clock)
	initial, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	oldID := initial.Items[0].ID
	suppressions.dismissed["owner-a"] = map[application.RecurrenceSuggestionID]bool{oldID: true}
	if hidden, err := useCase.Execute(context.Background(), "owner-a"); err != nil || len(hidden.Items) != 0 {
		t.Fatalf("dismissed evidence remained visible: %v / %+v", err, hidden.Items)
	}

	expenseReader.expenses = append(expenseReader.expenses, suggestionExpenses(t, "owner-a", "Academia", 11900, localDate(t, 2024, time.September, 10))[0])
	clock.now = time.Date(2024, time.September, 24, 12, 0, 0, 0, location)
	withNewEvidence, err := useCase.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(withNewEvidence.Items) != 1 || withNewEvidence.Items[0].ID == oldID {
		t.Fatalf("new evidence result = %+v", withNewEvidence.Items)
	}
}

func TestListRecurrenceSuggestionsFailsClosedOnOwnerViolationAndSafeDependencyErrors(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	now := time.Date(2024, time.August, 24, 12, 0, 0, 0, location)
	foreignExpenses := suggestionExpenses(t, "owner-b", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))
	useCase := mustListSuggestions(t, &suggestionExpenseReaderFake{expenses: foreignExpenses}, &suggestionRecurrenceReaderFake{}, newSuggestionSuppressionFake(), &suggestionClockFake{now: now})
	if _, err := useCase.Execute(context.Background(), "owner-a"); !errors.Is(err, application.ErrRecurrenceSuggestionOwnerIsolation) {
		t.Fatalf("foreign expense error = %v", err)
	}

	ownedExpenses := suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))
	foreignRecurrence := suggestionRecurrence(t, "owner-b", "Academia", 11900, nil)
	useCase = mustListSuggestions(t, &suggestionExpenseReaderFake{expenses: ownedExpenses}, &suggestionRecurrenceReaderFake{recurrences: []domain.Recurrence{foreignRecurrence}}, newSuggestionSuppressionFake(), &suggestionClockFake{now: now})
	if _, err := useCase.Execute(context.Background(), "owner-a"); !errors.Is(err, application.ErrRecurrenceSuggestionOwnerIsolation) {
		t.Fatalf("foreign recurrence error = %v", err)
	}

	foreignID := application.RecurrenceSuggestionID("rsg_" + strings.Repeat("b", 64))
	badSuppressions := newSuggestionSuppressionFake()
	badSuppressions.forced = []application.RecurrenceSuggestionID{foreignID}
	useCase = mustListSuggestions(t, &suggestionExpenseReaderFake{expenses: ownedExpenses}, &suggestionRecurrenceReaderFake{}, badSuppressions, &suggestionClockFake{now: now})
	if _, err := useCase.Execute(context.Background(), "owner-a"); !errors.Is(err, application.ErrRecurrenceSuggestionOwnerIsolation) {
		t.Fatalf("foreign suppression error = %v", err)
	}

	marker := errors.New("postgres address and sensitive marker")
	expenseReader := &suggestionExpenseReaderFake{err: marker}
	useCase = mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, newSuggestionSuppressionFake(), &suggestionClockFake{now: now})
	_, err := useCase.Execute(context.Background(), "owner-a")
	if !errors.Is(err, application.ErrRecurrenceSuggestionExpenseQuery) || !errors.Is(err, marker) || strings.Contains(err.Error(), marker.Error()) {
		t.Fatalf("safe dependency error = %v", err)
	}

	expenseReader.err = context.Canceled
	if _, err := useCase.Execute(context.Background(), "owner-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
}

func TestDismissRecurrenceSuggestionIsIdempotentAndUsesFinalStore(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	listClock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)}
	expenseReader := &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))}
	suppressions := newSuggestionSuppressionFake()
	list := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, suppressions, listClock)
	listed, err := list.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	id := listed.Items[0].ID
	listClock.calls = 0
	suppressions.calls = 0
	dismissClock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 1, 2, 345678900, location)}
	store := &suggestionSuppressionStoreFake{}
	store.fn = func(command application.DismissRecurrenceSuggestionCommand) (application.DismissRecurrenceSuggestionStoreResult, error) {
		if suppressions.dismissed[command.UserID] == nil {
			suppressions.dismissed[command.UserID] = make(map[application.RecurrenceSuggestionID]bool)
		}
		suppressions.dismissed[command.UserID][command.SuggestionID] = true
		return application.DismissRecurrenceSuggestionStoreResult{}, nil
	}
	useCase, err := application.NewDismissRecurrenceSuggestion(list, suppressions, store, dismissClock)
	if err != nil {
		t.Fatal(err)
	}

	result, err := useCase.Execute(context.Background(), application.DismissRecurrenceSuggestionInput{UserID: "owner-a", SuggestionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.SuggestionID != id || store.calls != 1 || listClock.calls != 1 || dismissClock.calls != 1 {
		t.Fatalf("first dismiss result/calls = %+v store=%d listClock=%d dismissClock=%d", result, store.calls, listClock.calls, dismissClock.calls)
	}
	command := store.commands[0]
	if command.Operation != application.DismissRecurrenceSuggestionOperation || command.UserID != "owner-a" || command.SuggestionID != id || command.Fingerprint == (application.RecurrenceSuggestionFingerprint{}) {
		t.Fatalf("dismiss command = %+v", command)
	}
	derivedID, err := application.DeriveRecurrenceSuggestionID(command.UserID, command.Fingerprint)
	if err != nil || derivedID != command.SuggestionID {
		t.Fatalf("persisted identity derivation = %q, err %v", derivedID, err)
	}
	wantDismissedAt := dismissClock.now.UTC().Truncate(time.Microsecond)
	if !command.DismissedAt.Equal(wantDismissedAt) {
		t.Fatalf("dismissedAt = %s, want %s", command.DismissedAt, wantDismissedAt)
	}

	replayed, err := useCase.Execute(context.Background(), application.DismissRecurrenceSuggestionInput{UserID: "owner-a", SuggestionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.Replayed || store.calls != 1 || listClock.calls != 1 || dismissClock.calls != 1 {
		t.Fatalf("replay/calls = %+v store=%d listClock=%d dismissClock=%d", replayed, store.calls, listClock.calls, dismissClock.calls)
	}
}

func TestDismissRecurrenceSuggestionRejectsStaleAndPreservesCancellation(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	listClock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)}
	expenseReader := &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))}
	suppressions := newSuggestionSuppressionFake()
	list := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, suppressions, listClock)
	listed, err := list.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	staleID := listed.Items[0].ID
	expenseReader.expenses = append(expenseReader.expenses, suggestionExpenses(t, "owner-a", "Academia", 11900, localDate(t, 2024, time.September, 10))[0])
	listClock.now = time.Date(2024, time.September, 24, 12, 0, 0, 0, location)
	dismissClock := &suggestionClockFake{now: listClock.now}
	store := &suggestionSuppressionStoreFake{}
	useCase, err := application.NewDismissRecurrenceSuggestion(list, suppressions, store, dismissClock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := useCase.Execute(context.Background(), application.DismissRecurrenceSuggestionInput{UserID: "owner-a", SuggestionID: staleID}); !errors.Is(err, application.ErrRecurrenceSuggestionNotFound) {
		t.Fatalf("stale error = %v", err)
	}
	if store.calls != 0 || dismissClock.calls != 0 {
		t.Fatalf("stale dismiss consumed store/clock = %d/%d", store.calls, dismissClock.calls)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	beforeExpenseCalls := expenseReader.calls
	if _, err := useCase.Execute(cancelled, application.DismissRecurrenceSuggestionInput{UserID: "owner-a", SuggestionID: staleID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v", err)
	}
	if expenseReader.calls != beforeExpenseCalls || store.calls != 0 || dismissClock.calls != 0 {
		t.Fatal("cancelled caller reached dependencies")
	}
}

func TestDismissRecurrenceSuggestionPreflightFailureHasNoEffects(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	validID := application.RecurrenceSuggestionID("rsg_" + strings.Repeat("a", 64))
	suppressions := newSuggestionSuppressionFake()
	suppressions.err = errors.New("private persistence detail")
	listClock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)}
	list := mustListSuggestions(t, &suggestionExpenseReaderFake{}, &suggestionRecurrenceReaderFake{}, suppressions, listClock)
	dismissClock := &suggestionClockFake{now: listClock.now}
	store := &suggestionSuppressionStoreFake{}
	useCase, err := application.NewDismissRecurrenceSuggestion(list, suppressions, store, dismissClock)
	if err != nil {
		t.Fatal(err)
	}
	_, err = useCase.Execute(context.Background(), application.DismissRecurrenceSuggestionInput{UserID: "owner-a", SuggestionID: validID})
	if !errors.Is(err, application.ErrRecurrenceSuggestionSuppressionQuery) || strings.Contains(err.Error(), "private") {
		t.Fatalf("preflight error = %v", err)
	}
	if listClock.calls != 0 || dismissClock.calls != 0 || store.calls != 0 {
		t.Fatalf("preflight failure effects listClock/dismissClock/store = %d/%d/%d", listClock.calls, dismissClock.calls, store.calls)
	}
}

func TestDismissRecurrenceSuggestionFinalStoreRemainsRaceAuthority(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	listClock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)}
	expenseReader := &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))}
	suppressions := newSuggestionSuppressionFake()
	list := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, suppressions, listClock)
	listed, err := list.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	id := listed.Items[0].ID
	dismissClock := &suggestionClockFake{now: listClock.now}
	store := &suggestionSuppressionStoreFake{fn: func(application.DismissRecurrenceSuggestionCommand) (application.DismissRecurrenceSuggestionStoreResult, error) {
		return application.DismissRecurrenceSuggestionStoreResult{Replayed: true}, nil
	}}
	useCase, err := application.NewDismissRecurrenceSuggestion(list, suppressions, store, dismissClock)
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Execute(context.Background(), application.DismissRecurrenceSuggestionInput{UserID: "owner-a", SuggestionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replayed || store.calls != 1 || dismissClock.calls != 1 {
		t.Fatalf("final race result/calls = %+v store=%d clock=%d", result, store.calls, dismissClock.calls)
	}
}

func TestDismissRecurrenceSuggestionStoreErrorsAreTypedAndSafe(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	listClock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)}
	suppressions := newSuggestionSuppressionFake()
	list := mustListSuggestions(t, &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 10), localDate(t, 2024, time.August, 10))}, &suggestionRecurrenceReaderFake{}, suppressions, listClock)
	listed, err := list.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New("private suppression database detail")
	store := &suggestionSuppressionStoreFake{fn: func(application.DismissRecurrenceSuggestionCommand) (application.DismissRecurrenceSuggestionStoreResult, error) {
		return application.DismissRecurrenceSuggestionStoreResult{}, marker
	}}
	useCase, err := application.NewDismissRecurrenceSuggestion(list, suppressions, store, &suggestionClockFake{now: listClock.now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = useCase.Execute(context.Background(), application.DismissRecurrenceSuggestionInput{UserID: "owner-a", SuggestionID: listed.Items[0].ID})
	if !errors.Is(err, application.ErrRecurrenceSuggestionSuppressionWrite) || !errors.Is(err, marker) || strings.Contains(err.Error(), marker.Error()) {
		t.Fatalf("store error = %v", err)
	}
}

func TestPrepareSuggestedRecurrenceUsesCurrentServerDerivedDataOnly(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	listClock := &suggestionClockFake{now: time.Date(2024, time.August, 24, 12, 0, 0, 0, location)}
	expenseReader := &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "  Academia   Premium ", 15900,
		localDate(t, 2024, time.June, 10), localDate(t, 2024, time.July, 11), localDate(t, 2024, time.August, 9))}
	suppressions := newSuggestionSuppressionFake()
	list := mustListSuggestions(t, expenseReader, &suggestionRecurrenceReaderFake{}, suppressions, listClock)
	listed, err := list.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	id := listed.Items[0].ID
	prepare, err := application.NewPrepareSuggestedRecurrence(list)
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepare.Execute(context.Background(), application.PrepareSuggestedRecurrenceInput{UserID: "owner-a", SuggestionID: id})
	if err != nil {
		t.Fatal(err)
	}
	details := result.Preview.Details
	if details.UserID != "owner-a" || details.TransactionType != domain.TransactionTypeExpense || details.Description != "Academia Premium" || details.ExpectedAmount.MinorUnits() != 15900 || details.Frequency != domain.RecurrenceFrequencyMonthly || details.StartsOn.String() != "2024-09-10" {
		t.Fatalf("preview details = %+v", details)
	}

	expenseReader.expenses = append(expenseReader.expenses, suggestionExpenses(t, "owner-a", "Academia Premium", 15900, localDate(t, 2024, time.September, 10))[0])
	listClock.now = time.Date(2024, time.September, 24, 12, 0, 0, 0, location)
	if _, err := prepare.Execute(context.Background(), application.PrepareSuggestedRecurrenceInput{UserID: "owner-a", SuggestionID: id}); !errors.Is(err, application.ErrRecurrenceSuggestionNotFound) {
		t.Fatalf("stale prepare error = %v", err)
	}
	current, err := list.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	currentID := current.Items[0].ID
	suppressions.dismissed["owner-a"] = map[application.RecurrenceSuggestionID]bool{currentID: true}
	if _, err := prepare.Execute(context.Background(), application.PrepareSuggestedRecurrenceInput{UserID: "owner-a", SuggestionID: currentID}); !errors.Is(err, application.ErrRecurrenceSuggestionSuppressed) {
		t.Fatalf("suppressed prepare error = %v", err)
	}
}

func TestPrepareSuggestedRecurrenceReevaluatesCancellationObservedAfterExpenseSnapshot(t *testing.T) {
	t.Parallel()
	location := financialLocation(t)
	listClock := &suggestionClockFake{now: time.Date(2024, time.October, 24, 12, 0, 0, 0, location)}
	expenseReader := &suggestionExpenseReaderFake{expenses: suggestionExpenses(t, "owner-a", "Academia", 11900,
		localDate(t, 2024, time.August, 10),
		localDate(t, 2024, time.September, 10),
		localDate(t, 2024, time.October, 10),
	)}
	recurrenceReader := &suggestionRecurrenceReaderFake{}
	suppressions := newSuggestionSuppressionFake()
	list := mustListSuggestions(t, expenseReader, recurrenceReader, suppressions, listClock)

	listed, err := list.Execute(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("initial suggestions = %d, want one", len(listed.Items))
	}
	listedID := listed.Items[0].ID
	prepare, err := application.NewPrepareSuggestedRecurrence(list)
	if err != nil {
		t.Fatal(err)
	}

	cancelledAt := time.Date(2024, time.September, 15, 12, 0, 0, 0, location)
	hookCalls := 0
	expenseReader.afterSnapshot = func() {
		hookCalls++
		recurrenceReader.recurrences = []domain.Recurrence{
			suggestionRecurrence(t, "owner-a", "Academia", 11900, &cancelledAt),
		}
	}

	_, err = prepare.Execute(context.Background(), application.PrepareSuggestedRecurrenceInput{
		UserID:       "owner-a",
		SuggestionID: listedID,
	})
	if !errors.Is(err, application.ErrRecurrenceSuggestionNotFound) {
		t.Fatalf("prepare after authoritative cancellation error = %v", err)
	}
	if hookCalls != 1 || expenseReader.calls != 2 || recurrenceReader.calls != 2 {
		t.Fatalf("snapshot/order calls hook/expense/recurrence = %d/%d/%d, want 1/2/2", hookCalls, expenseReader.calls, recurrenceReader.calls)
	}
	if suppressions.calls != 1 {
		t.Fatalf("suppression calls = %d, want only initial listing call", suppressions.calls)
	}
}

func TestRecurrenceSuggestionConstructorsAndInputValidation(t *testing.T) {
	t.Parallel()
	clock := &suggestionClockFake{now: time.Now()}
	expenses := &suggestionExpenseReaderFake{}
	recurrences := &suggestionRecurrenceReaderFake{}
	suppressions := newSuggestionSuppressionFake()
	if _, err := application.NewListRecurrenceSuggestions(nil, recurrences, suppressions, clock); !errors.Is(err, application.ErrMissingRecurrenceSuggestionExpenseReader) {
		t.Errorf("missing expense reader error = %v", err)
	}
	if _, err := application.NewListRecurrenceSuggestions(expenses, nil, suppressions, clock); !errors.Is(err, application.ErrMissingRecurrenceSuggestionRecurrenceReader) {
		t.Errorf("missing recurrence reader error = %v", err)
	}
	if _, err := application.NewListRecurrenceSuggestions(expenses, recurrences, nil, clock); !errors.Is(err, application.ErrMissingRecurrenceSuggestionSuppressionReader) {
		t.Errorf("missing suppression reader error = %v", err)
	}
	if _, err := application.NewListRecurrenceSuggestions(expenses, recurrences, suppressions, nil); !errors.Is(err, application.ErrMissingRecurrenceSuggestionClock) {
		t.Errorf("missing clock error = %v", err)
	}
	list := mustListSuggestions(t, expenses, recurrences, suppressions, clock)
	store := &suggestionSuppressionStoreFake{}
	if _, err := application.NewDismissRecurrenceSuggestion(nil, suppressions, store, clock); !errors.Is(err, application.ErrMissingDismissRecurrenceSuggestionList) {
		t.Errorf("missing dismiss list error = %v", err)
	}
	if _, err := application.NewDismissRecurrenceSuggestion(list, nil, store, clock); !errors.Is(err, application.ErrMissingRecurrenceSuggestionSuppressionReader) {
		t.Errorf("missing dismiss reader error = %v", err)
	}
	if _, err := application.NewDismissRecurrenceSuggestion(list, suppressions, nil, clock); !errors.Is(err, application.ErrMissingRecurrenceSuggestionStore) {
		t.Errorf("missing dismiss store error = %v", err)
	}
	if _, err := application.NewDismissRecurrenceSuggestion(list, suppressions, store, nil); !errors.Is(err, application.ErrMissingDismissRecurrenceSuggestionClock) {
		t.Errorf("missing dismiss clock error = %v", err)
	}
	if _, err := application.NewPrepareSuggestedRecurrence(nil); !errors.Is(err, application.ErrMissingPrepareSuggestedRecurrenceList) {
		t.Errorf("missing prepare list error = %v", err)
	}
	invalidIDs := []application.RecurrenceSuggestionID{"", "rsg_abc", application.RecurrenceSuggestionID("rsg_" + strings.Repeat("A", 64))}
	for _, id := range invalidIDs {
		if err := application.ValidateRecurrenceSuggestionID(id); !errors.Is(err, application.ErrInvalidRecurrenceSuggestionID) {
			t.Errorf("ValidateRecurrenceSuggestionID(%q) = %v", id, err)
		}
	}
	if _, err := application.DeriveRecurrenceSuggestionID(" owner-a ", application.RecurrenceSuggestionFingerprint{}); !errors.Is(err, domain.ErrInvalidUserID) {
		t.Fatalf("DeriveRecurrenceSuggestionID(invalid owner) error = %v", err)
	}
}

type suggestionClockFake struct {
	now   time.Time
	calls int
}

func (clock *suggestionClockFake) Now() time.Time {
	clock.calls++
	return clock.now
}

type suggestionExpenseReaderFake struct {
	expenses      []domain.Expense
	err           error
	calls         int
	queries       []application.RecurrenceSuggestionExpenseQuery
	afterSnapshot func()
}

func (reader *suggestionExpenseReaderFake) ListRecurrenceSuggestionExpenses(_ context.Context, query application.RecurrenceSuggestionExpenseQuery) ([]domain.Expense, error) {
	reader.calls++
	reader.queries = append(reader.queries, query)
	snapshot := slices.Clone(reader.expenses)
	if reader.afterSnapshot != nil {
		reader.afterSnapshot()
	}
	return snapshot, reader.err
}

type suggestionRecurrenceReaderFake struct {
	recurrences []domain.Recurrence
	err         error
	calls       int
}

func (reader *suggestionRecurrenceReaderFake) ListRecurrences(context.Context, string) ([]domain.Recurrence, error) {
	reader.calls++
	return slices.Clone(reader.recurrences), reader.err
}

type suggestionSuppressionFake struct {
	dismissed map[string]map[application.RecurrenceSuggestionID]bool
	forced    []application.RecurrenceSuggestionID
	err       error
	calls     int
}

func newSuggestionSuppressionFake() *suggestionSuppressionFake {
	return &suggestionSuppressionFake{dismissed: make(map[string]map[application.RecurrenceSuggestionID]bool)}
}

func (reader *suggestionSuppressionFake) ListDismissedRecurrenceSuggestionIDs(_ context.Context, userID string, ids []application.RecurrenceSuggestionID) ([]application.RecurrenceSuggestionID, error) {
	reader.calls++
	if reader.err != nil {
		return nil, reader.err
	}
	if reader.forced != nil {
		return slices.Clone(reader.forced), nil
	}
	result := make([]application.RecurrenceSuggestionID, 0, len(ids))
	for _, id := range ids {
		if reader.dismissed[userID][id] {
			result = append(result, id)
		}
	}
	return result, nil
}

type suggestionSuppressionStoreFake struct {
	fn       func(application.DismissRecurrenceSuggestionCommand) (application.DismissRecurrenceSuggestionStoreResult, error)
	calls    int
	commands []application.DismissRecurrenceSuggestionCommand
}

func (store *suggestionSuppressionStoreFake) DismissRecurrenceSuggestion(_ context.Context, command application.DismissRecurrenceSuggestionCommand) (application.DismissRecurrenceSuggestionStoreResult, error) {
	store.calls++
	store.commands = append(store.commands, command)
	if store.fn != nil {
		return store.fn(command)
	}
	return application.DismissRecurrenceSuggestionStoreResult{}, nil
}

func mustListSuggestions(t *testing.T, expenseReader application.RecurrenceSuggestionExpenseReader, recurrenceReader application.RecurrenceReader, suppressionReader application.RecurrenceSuggestionSuppressionReader, clock application.Clock) *application.ListRecurrenceSuggestions {
	t.Helper()
	useCase, err := application.NewListRecurrenceSuggestions(expenseReader, recurrenceReader, suppressionReader, clock)
	if err != nil {
		t.Fatal(err)
	}
	return useCase
}

func suggestionExpenses(t *testing.T, userID, description string, amount int64, dates ...time.Time) []domain.Expense {
	t.Helper()
	result := make([]domain.Expense, 0, len(dates))
	for index, occurredAt := range dates {
		money, err := domain.NewMoney(amount, domain.CurrencyBRL)
		if err != nil {
			t.Fatal(err)
		}
		expense, err := domain.NewExpense(domain.ExpenseParams{
			ID: "expense-" + userID + "-" + description + "-" + occurredAt.Format("20060102") + "-" + string(rune('a'+index)),
			Details: domain.ExpenseDetails{
				UserID: userID, Description: description, Amount: money, PaymentMethod: domain.PaymentMethodPIX,
				OccurredAt: occurredAt, FinancialTimezone: application.FinancialTimezone, Origin: domain.OriginIOS,
			},
			CreatedAt: occurredAt.Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, expense)
	}
	return result
}

func suggestionRecurrence(t *testing.T, userID, description string, amount int64, cancelledAt *time.Time) domain.Recurrence {
	t.Helper()
	money, err := domain.NewMoney(amount, domain.CurrencyBRL)
	if err != nil {
		t.Fatal(err)
	}
	startsOn, err := domain.NewCivilDate(2024, time.January, 10)
	if err != nil {
		t.Fatal(err)
	}
	recurrence, err := domain.NewRecurrence(domain.RecurrenceParams{
		ID: "recurrence-" + userID,
		Details: domain.RecurrenceDetails{
			UserID: userID, TransactionType: domain.TransactionTypeExpense, Description: description,
			ExpectedAmount: money, Frequency: domain.RecurrenceFrequencyMonthly, StartsOn: startsOn,
		},
		CreatedAt: time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cancelledAt != nil {
		recurrence, err = recurrence.Cancel(*cancelledAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	return recurrence
}

func financialLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(application.FinancialTimezone)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func localDate(t *testing.T, year int, month time.Month, day int) time.Time {
	t.Helper()
	return time.Date(year, month, day, 12, 0, 0, 0, financialLocation(t))
}
