const timeZone = "America/Sao_Paulo";
const evidenceAmount = Object.freeze({ minor: 6390, currency: "BRL" });
const formatter = new Intl.DateTimeFormat("en-US", {
  timeZone,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

function civilDateParts(instant) {
  const parts = new Map(
    formatter
      .formatToParts(instant)
      .filter(({ type }) => type !== "literal")
      .map(({ type, value }) => [type, Number(value)]),
  );
  return {
    year: parts.get("year"),
    month: parts.get("month"),
    day: parts.get("day"),
  };
}

function isLeapYear(year) {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

function daysInMonth(year, month) {
  const lengths = [
    31,
    isLeapYear(year) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31,
  ];
  return lengths[month - 1];
}

function assertValidCivilDate(value) {
  if (
    !Number.isInteger(value.year) ||
    !Number.isInteger(value.month) ||
    !Number.isInteger(value.day) ||
    value.year < 1 ||
    value.year > 9999 ||
    value.month < 1 ||
    value.month > 12 ||
    value.day < 1 ||
    value.day > daysInMonth(value.year, value.month)
  ) {
    throw new Error("Invalid CivilDate in the iOS suggestion E2E fixture");
  }
}

function parseCivilDate(value) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) {
    throw new Error("Invalid CivilDate fixture text");
  }
  const parsed = {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
  };
  assertValidCivilDate(parsed);
  return parsed;
}

function shiftedMonth({ year, month }, offset) {
  const index = year * 12 + (month - 1) + offset;
  return {
    year: Math.floor(index / 12),
    month: (((index % 12) + 12) % 12) + 1,
  };
}

function nextCivilDate(value) {
  assertValidCivilDate(value);
  if (value.day < daysInMonth(value.year, value.month)) {
    return { ...value, day: value.day + 1 };
  }
  const nextMonth = shiftedMonth(value, 1);
  return { ...nextMonth, day: 1 };
}

function formatCivilDate(value) {
  assertValidCivilDate(value);
  return `${String(value.year).padStart(4, "0")}-${String(value.month).padStart(2, "0")}-${String(value.day).padStart(2, "0")}`;
}

function civilDateInMonth(month, day) {
  return { ...month, day: Math.min(day, daysInMonth(month.year, month.month)) };
}

function compareCivilDates(left, right) {
  return (
    left.year - right.year || left.month - right.month || left.day - right.day
  );
}

function monthIndex(value) {
  return value.year * 12 + (value.month - 1);
}

function nextOccurrenceAfter(lastEvidence, anchorDay, evaluatedOn) {
  let candidateMonth = shiftedMonth(lastEvidence, 1);
  let candidate = civilDateInMonth(candidateMonth, anchorDay);
  while (compareCivilDates(candidate, evaluatedOn) <= 0) {
    candidateMonth = shiftedMonth(candidateMonth, 1);
    candidate = civilDateInMonth(candidateMonth, anchorDay);
  }
  return candidate;
}

function buildBoundarySafeRecurrenceFixture(evaluatedOn, description) {
  assertValidCivilDate(evaluatedOn);
  const followingEvaluation = nextCivilDate(evaluatedOn);
  const crossesMonth =
    evaluatedOn.year !== followingEvaluation.year ||
    evaluatedOn.month !== followingEvaluation.month;

  let anchorDay;
  let latestEvidenceMonthOffset;
  let proposedStartsOn;

  if (crossesMonth) {
    anchorDay = 2;
    latestEvidenceMonthOffset = 0;
    proposedStartsOn = civilDateInMonth(
      shiftedMonth(evaluatedOn, 1),
      anchorDay,
    );
  } else if (followingEvaluation.day < 28) {
    anchorDay = followingEvaluation.day + 1;
    latestEvidenceMonthOffset = -1;
    proposedStartsOn = civilDateInMonth(evaluatedOn, anchorDay);
  } else {
    anchorDay = 1;
    latestEvidenceMonthOffset = 0;
    proposedStartsOn = civilDateInMonth(
      shiftedMonth(evaluatedOn, 1),
      anchorDay,
    );
  }

  const evidenceDates = [-2, -1, 0].map((offset) =>
    civilDateInMonth(
      shiftedMonth(evaluatedOn, latestEvidenceMonthOffset + offset),
      anchorDay,
    ),
  );

  return {
    description,
    expectedAmount: evidenceAmount,
    anchorDay,
    evidenceDates,
    proposedStartsOn,
  };
}

function assertBoundarySafeFixture(evaluatedOn, expectedFollowingDate) {
  const description = "Boundary safe recurrence fixture";
  const followingEvaluation = nextCivilDate(evaluatedOn);
  const fixture = buildBoundarySafeRecurrenceFixture(evaluatedOn, description);

  if (formatCivilDate(followingEvaluation) !== expectedFollowingDate) {
    throw new Error("Synthetic boundary pair is not adjacent");
  }
  if (
    fixture.description !== description ||
    fixture.expectedAmount.minor !== 6390 ||
    fixture.expectedAmount.currency !== "BRL"
  ) {
    throw new Error("Synthetic fixture changed description or amount");
  }
  if (fixture.evidenceDates.length !== 3) {
    throw new Error(
      "Synthetic fixture must contain exactly three evidence dates",
    );
  }
  if (
    !Number.isInteger(fixture.anchorDay) ||
    fixture.anchorDay < 1 ||
    fixture.anchorDay > 31
  ) {
    throw new Error("Synthetic fixture has an invalid anchor");
  }
  fixture.evidenceDates.forEach((evidenceDate) => {
    assertValidCivilDate(evidenceDate);
    if (
      evidenceDate.day !==
      Math.min(
        fixture.anchorDay,
        daysInMonth(evidenceDate.year, evidenceDate.month),
      )
    ) {
      throw new Error("Synthetic evidence does not preserve its anchor");
    }
  });
  for (let index = 1; index < fixture.evidenceDates.length; index += 1) {
    if (
      monthIndex(fixture.evidenceDates[index]) -
        monthIndex(fixture.evidenceDates[index - 1]) !==
      1
    ) {
      throw new Error("Synthetic evidence months are not consecutive");
    }
  }

  const latestEvidence = fixture.evidenceDates.at(-1);
  for (const evaluation of [evaluatedOn, followingEvaluation]) {
    const freshness = monthIndex(evaluation) - monthIndex(latestEvidence);
    if (freshness < 0 || freshness > 1) {
      throw new Error(
        "Synthetic evidence is not fresh for both evaluation dates",
      );
    }
    if (compareCivilDates(latestEvidence, evaluation) > 0) {
      throw new Error("Synthetic evidence is later than its evaluation date");
    }
    if (compareCivilDates(fixture.proposedStartsOn, evaluation) <= 0) {
      throw new Error("Synthetic proposedStartsOn is not strictly future");
    }
    const calculated = nextOccurrenceAfter(
      latestEvidence,
      fixture.anchorDay,
      evaluation,
    );
    if (compareCivilDates(calculated, fixture.proposedStartsOn) !== 0) {
      throw new Error("Synthetic proposedStartsOn differs across the boundary");
    }
  }
}

function runBoundarySafeFixtureSelfTest() {
  const cases = [
    ["2026-01-09", "2026-01-10"],
    ["2026-01-10", "2026-01-11"],
    ["2026-01-31", "2026-02-01"],
    ["2026-02-28", "2026-03-01"],
    ["2028-02-29", "2028-03-01"],
    ["2026-12-31", "2027-01-01"],
    ["2026-06-15", "2026-06-16"],
  ];
  for (const [evaluatedOn, followingEvaluation] of cases) {
    assertBoundarySafeFixture(parseCivilDate(evaluatedOn), followingEvaluation);
  }
  process.stdout.write(
    `Boundary-safe recurrence fixture matrix passed (${cases.length} cases).\n`,
  );
}

async function request(baseURL, path, options = {}) {
  return fetch(new URL(path, baseURL), {
    headers: { Accept: "application/json", ...(options.headers ?? {}) },
    ...options,
  });
}

async function prepareRealFixture() {
  const baseURL = process.env.JARVIS_API_BASE_URL;
  const description = process.env.JARVIS_IOS_E2E_SUGGESTION_DESCRIPTION;
  if (!baseURL || !description) {
    throw new Error("The iOS suggestion E2E fixture is not configured");
  }

  const evaluationInstant = new Date();
  const fixture = buildBoundarySafeRecurrenceFixture(
    civilDateParts(evaluationInstant),
    description,
  );

  for (const [index, observedOn] of fixture.evidenceDates.entries()) {
    const response = await request(baseURL, "/v1/transactions", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": `ios-suggestion-evidence-${process.pid}-${index}`,
      },
      body: JSON.stringify({
        type: "EXPENSE",
        description: fixture.description,
        amount: fixture.expectedAmount,
        paymentMethod: "PIX",
        categoryId: "expense.subscriptions",
        occurredAt: `${formatCivilDate(observedOn)}T15:00:00Z`,
      }),
    });
    if (response.status !== 201) {
      throw new Error(
        `Failed to create synthetic suggestion evidence (status ${response.status})`,
      );
    }
  }

  const recurrencesResponse = await request(baseURL, "/v1/recurrences");
  if (recurrencesResponse.status !== 200) {
    throw new Error(
      `Failed to verify initial Recurrences (status ${recurrencesResponse.status})`,
    );
  }
  const recurrences = await recurrencesResponse.json();
  if (recurrences.items.some((item) => item.description === description)) {
    throw new Error("A Recurrence existed before explicit iOS confirmation");
  }

  const suggestionsResponse = await request(
    baseURL,
    "/v1/recurrence-suggestions",
  );
  if (suggestionsResponse.status !== 200) {
    throw new Error(
      `Failed to verify the synthetic suggestion (status ${suggestionsResponse.status})`,
    );
  }
  const suggestions = await suggestionsResponse.json();
  const matching = suggestions.items.filter(
    (item) => item.description === description,
  );
  const proposedStartsOn = formatCivilDate(fixture.proposedStartsOn);
  if (
    matching.length !== 1 ||
    matching[0].proposedStartsOn !== proposedStartsOn
  ) {
    throw new Error(
      "The synthetic evidence did not produce the expected unique suggestion",
    );
  }

  process.stdout.write(proposedStartsOn);
}

if (process.argv.includes("--self-test")) {
  runBoundarySafeFixtureSelfTest();
} else {
  await prepareRealFixture();
}
