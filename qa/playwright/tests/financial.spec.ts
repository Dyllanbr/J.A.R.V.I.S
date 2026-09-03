import { expect, test } from "@playwright/test";

const contentType = "application/json; charset=utf-8";
const financialTimeZone = "America/Sao_Paulo";
const suggestionAnchorDay = 10;

type CivilDateParts = { year: number; month: number; day: number };

type SuggestionCalendarFixture = {
  evidenceDates: string[];
  additionalEvidenceDate: string;
  proposedStartsOn: string;
};

const financialDateFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: financialTimeZone,
  calendar: "gregory",
  numberingSystem: "latn",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

function financialCivilDate(instant: Date = new Date()): CivilDateParts {
  const parts = new Map(
    financialDateFormatter
      .formatToParts(instant)
      .filter(
        ({ type }) => type === "year" || type === "month" || type === "day",
      )
      .map(({ type, value }) => [type, Number(value)]),
  );
  const year = parts.get("year");
  const month = parts.get("month");
  const day = parts.get("day");
  if (year === undefined || month === undefined || day === undefined) {
    throw new Error("could not resolve the current financial civil date");
  }
  return { year, month, day };
}

function shiftedMonth(date: CivilDateParts, offset: number): CivilDateParts {
  const shifted = new Date(Date.UTC(date.year, date.month - 1 + offset, 1));
  return {
    year: shifted.getUTCFullYear(),
    month: shifted.getUTCMonth() + 1,
    day: 1,
  };
}

function civilDate(year: number, month: number, day: number): string {
  return `${year.toString().padStart(4, "0")}-${month.toString().padStart(2, "0")}-${day.toString().padStart(2, "0")}`;
}

function suggestionCalendarFixture(
  evaluatedOn: CivilDateParts,
): SuggestionCalendarFixture {
  const atAnchor = (offset: number) => {
    const month = shiftedMonth(evaluatedOn, offset);
    return civilDate(month.year, month.month, suggestionAnchorDay);
  };
  return {
    evidenceDates: [-3, -2, -1].map(atAnchor),
    additionalEvidenceDate: atAnchor(-4),
    proposedStartsOn: atAnchor(evaluatedOn.day < suggestionAnchorDay ? 0 : 1),
  };
}

function financialEvidenceInstant(date: string): string {
  const instant = `${date}T12:00:00Z`;
  const observed = financialCivilDate(new Date(instant));
  if (civilDate(observed.year, observed.month, observed.day) !== date) {
    throw new Error(
      "synthetic evidence does not map to its financial CivilDate",
    );
  }
  return instant;
}

function expenseRequest(description: string, occurredAt: string) {
  return {
    type: "EXPENSE",
    description,
    amount: {
      minor: 4250,
      currency: "BRL",
    },
    paymentMethod: "PIX",
    occurredAt,
  };
}

function incomeRequest(description: string, occurredAt: string) {
  return {
    type: "INCOME",
    description,
    amount: {
      minor: 725000,
      currency: "BRL",
    },
    occurredAt,
  };
}

function recurrenceRequest(description: string, startsOn: string) {
  return {
    type: "EXPENSE",
    description,
    expectedAmount: {
      minor: 11900,
      currency: "BRL",
    },
    frequency: "MONTHLY",
    startsOn,
  };
}

function creditCardRequest(name: string) {
  return {
    name,
    lastFour: "4242",
    brand: "VISA",
    closingDay: 31,
    dueDay: 10,
    creditLimit: {
      minor: 250000,
      currency: "BRL",
    },
  };
}

test("preview validates and normalizes without persistence", async ({
  request,
}) => {
  const description = "Preview sintético exclusivo";
  const preview = await request.post("/v1/transactions/preview", {
    data: expenseRequest(
      `  ${description}  `,
      "2026-08-14T12:00:00.123456789-03:00",
    ),
  });

  expect(preview.status()).toBe(200);
  expect(preview.headers()["content-type"]).toBe(contentType);
  expect(preview.headers()["cache-control"]).toBe("no-store");
  expect(await preview.json()).toEqual({
    type: "EXPENSE",
    description,
    amount: { minor: 4250, currency: "BRL" },
    paymentMethod: "PIX",
    occurredAt: "2026-08-14T15:00:00.123456Z",
    financialTimezone: "America/Sao_Paulo",
    origin: "IOS",
  });

  const month = await request.get("/v1/transactions?month=2026-08");
  expect(month.status()).toBe(200);
  const monthBody = (await month.json()) as {
    items: Array<{ description: string }>;
  };
  expect(monthBody.items.some((item) => item.description === description)).toBe(
    false,
  );
});

test("preview rejects invalid and unknown input", async ({ request }) => {
  const invalidPayload = expenseRequest(
    "Valor inválido sintético",
    "2026-08-14T15:00:00Z",
  );
  invalidPayload.amount.minor = 0;
  const invalidAmount = await request.post("/v1/transactions/preview", {
    data: invalidPayload,
  });
  expect(invalidAmount.status()).toBe(400);

  const unknown = await request.post("/v1/transactions/preview", {
    data: {
      ...expenseRequest("Campo desconhecido sintético", "2026-08-14T15:00:00Z"),
      userId: "spoofed-owner",
    },
  });
  expect(unknown.status()).toBe(400);
  expect(await unknown.json()).toEqual({
    error: { code: "INVALID_REQUEST", message: "request is invalid" },
  });
});

test("income preview is canonical, zero-write, and rejects ambiguous shapes", async ({
  request,
}) => {
  const description = "Preview receita Playwright sintética";
  const preview = await request.post("/v1/transactions/preview", {
    data: incomeRequest(
      `  ${description}  `,
      "2026-06-14T12:00:00.123456789-03:00",
    ),
  });
  expect(preview.status()).toBe(200);
  expect(preview.headers()["cache-control"]).toBe("no-store");
  expect(await preview.json()).toEqual({
    type: "INCOME",
    description,
    amount: { minor: 725000, currency: "BRL" },
    occurredAt: "2026-06-14T15:00:00.123456Z",
    financialTimezone: "America/Sao_Paulo",
    origin: "IOS",
  });

  const month = await request.get("/v1/transactions?month=2026-06");
  const monthBody = (await month.json()) as {
    items: Array<{ description: string }>;
  };
  expect(monthBody.items.some((item) => item.description === description)).toBe(
    false,
  );

  const base = incomeRequest(
    "Shape inválido sintético",
    "2026-06-14T15:00:00Z",
  );
  const invalidShapes: Array<Record<string, unknown>> = [
    { ...base, paymentMethod: null },
    { ...base, paymentMethod: "PIX" },
    { ...base, type: "income" },
    { ...base, type: "TRANSFER" },
    { ...base, amount: { minor: 0, currency: "BRL" } },
    { ...base, amount: { minor: -1, currency: "BRL" } },
    { ...base, amount: { minor: 72.5, currency: "BRL" } },
    { ...base, userId: "spoofed-owner" },
    { ...base, ownerId: "spoofed-owner" },
    { ...base, origin: "WHATSAPP" },
    { ...base, financialTimezone: "UTC" },
    { ...base, status: "RECORDED" },
    { ...base, employer: "synthetic" },
    { ...base, category: "synthetic" },
    { ...base, recurring: true },
  ];
  for (const data of invalidShapes) {
    const response = await request.post("/v1/transactions/preview", { data });
    expect(response.status()).toBe(400);
  }
});

test("create requires a key and safely replays the original resource", async ({
  request,
}) => {
  const payload = expenseRequest(
    "Mercado Playwright sintético",
    "2026-08-14T15:00:00.000000123Z",
  );
  const missing = await request.post("/v1/transactions", { data: payload });
  expect(missing.status()).toBe(400);
  expect(await missing.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REQUIRED",
      message: "idempotency key is required",
    },
  });

  const headers = { "Idempotency-Key": "pw-synthetic-create-replay-001" };
  const created = await request.post("/v1/transactions", {
    data: payload,
    headers,
  });
  expect(created.status()).toBe(201);
  expect(created.headers()["content-type"]).toBe(contentType);
  expect(created.headers()["cache-control"]).toBe("no-store");
  const createdBody = (await created.json()) as Record<string, unknown>;
  expect(createdBody).toMatchObject({
    type: "EXPENSE",
    description: payload.description,
    amount: payload.amount,
    paymentMethod: "PIX",
    occurredAt: "2026-08-14T15:00:00Z",
    financialTimezone: "America/Sao_Paulo",
    origin: "IOS",
    status: "RECORDED",
    version: 1,
  });
  expect("userId" in createdBody).toBe(false);

  const reorderedSemanticReplay = {
    occurredAt: "2026-08-14T15:00:00.000000999Z",
    paymentMethod: payload.paymentMethod,
    amount: { currency: payload.amount.currency, minor: payload.amount.minor },
    description: `  ${payload.description}  `,
    type: payload.type,
  };
  const replay = await request.post("/v1/transactions", {
    data: reorderedSemanticReplay,
    headers,
  });
  expect(replay.status()).toBe(201);
  expect(replay.headers()["idempotency-replayed"]).toBe("true");
  expect(await replay.json()).toEqual(createdBody);

  const conflict = await request.post("/v1/transactions", {
    data: { ...payload, description: "Payload diferente sintético" },
    headers,
  });
  expect(conflict.status()).toBe(409);
  expect(await conflict.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REUSED",
      message: "idempotency key was reused with a different request",
    },
  });
});

test("income create requires a key and replays the persisted body", async ({
  request,
}) => {
  const payload = incomeRequest(
    "Salário Playwright sintético",
    "2026-07-14T15:00:00.123456789Z",
  );
  const missing = await request.post("/v1/transactions", { data: payload });
  expect(missing.status()).toBe(400);
  expect(await missing.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REQUIRED",
      message: "idempotency key is required",
    },
  });

  const headers = { "Idempotency-Key": "pw-synthetic-income-replay-001" };
  const created = await request.post("/v1/transactions", {
    data: payload,
    headers,
  });
  expect(created.status()).toBe(201);
  expect(created.headers()["cache-control"]).toBe("no-store");
  const createdBytes = await created.body();
  const createdBody = JSON.parse(createdBytes.toString()) as Record<
    string,
    unknown
  >;
  expect(createdBody).toMatchObject({
    type: "INCOME",
    description: payload.description,
    amount: payload.amount,
    occurredAt: "2026-07-14T15:00:00.123456Z",
    financialTimezone: "America/Sao_Paulo",
    origin: "IOS",
    status: "RECORDED",
    version: 1,
  });
  expect("paymentMethod" in createdBody).toBe(false);
  expect("userId" in createdBody).toBe(false);

  const replay = await request.post("/v1/transactions", {
    data: {
      occurredAt: "2026-07-14T15:00:00.123456999Z",
      amount: { currency: "BRL", minor: 725000 },
      description: `  ${payload.description}  `,
      type: "INCOME",
    },
    headers,
  });
  expect(replay.status()).toBe(201);
  expect(replay.headers()["idempotency-replayed"]).toBe("true");
  expect(await replay.body()).toEqual(createdBytes);

  const conflict = await request.post("/v1/transactions", {
    data: { ...payload, description: "Outra receita sintética" },
    headers,
  });
  expect(conflict.status()).toBe(409);
  expect(await conflict.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REUSED",
      message: "idempotency key was reused with a different request",
    },
  });
});

test("monthly history returns a discriminated Expense and Income collection", async ({
  request,
}) => {
  const expenseDescription = "Despesa mista Playwright sintética";
  const incomeDescription = "Receita mista Playwright sintética";
  const sharedKey = "pw-synthetic-mixed-operation-key";

  const expense = await request.post("/v1/transactions", {
    data: expenseRequest(expenseDescription, "2026-05-10T15:00:00Z"),
    headers: { "Idempotency-Key": sharedKey },
  });
  const income = await request.post("/v1/transactions", {
    data: incomeRequest(incomeDescription, "2026-05-10T16:00:00Z"),
    headers: { "Idempotency-Key": sharedKey },
  });
  expect(expense.status()).toBe(201);
  expect(income.status()).toBe(201);

  const response = await request.get("/v1/transactions?month=2026-05");
  expect(response.status()).toBe(200);
  const body = (await response.json()) as {
    month: string;
    items: Array<Record<string, unknown>>;
  };
  expect(body.month).toBe("2026-05");
  const relevant = body.items.filter((item) =>
    [expenseDescription, incomeDescription].includes(
      item.description as string,
    ),
  );
  expect(relevant).toHaveLength(2);
  const incomeItem = relevant[0];
  const expenseItem = relevant[1];
  if (!incomeItem || !expenseItem) {
    throw new Error("mixed history fixtures are missing");
  }
  expect(incomeItem).toMatchObject({
    type: "INCOME",
    description: incomeDescription,
  });
  expect("paymentMethod" in incomeItem).toBe(false);
  expect(expenseItem).toMatchObject({
    type: "EXPENSE",
    description: expenseDescription,
    paymentMethod: "PIX",
  });

  const empty = await request.get("/v1/transactions?month=2040-01");
  expect(empty.status()).toBe(200);
  expect(await empty.json()).toEqual({ month: "2040-01", items: [] });
});

test("monthly history lists only the requested financial month", async ({
  request,
}) => {
  const description = "Boundary mensal sintético";
  const created = await request.post("/v1/transactions", {
    data: expenseRequest(description, "2026-08-01T03:00:00Z"),
    headers: { "Idempotency-Key": "pw-synthetic-month-001" },
  });
  expect(created.status()).toBe(201);

  const august = await request.get("/v1/transactions?month=2026-08");
  expect(august.status()).toBe(200);
  expect(august.headers()["content-type"]).toBe(contentType);
  expect(august.headers()["cache-control"]).toBe("no-store");
  const augustBody = (await august.json()) as {
    month: string;
    items: Array<{ description: string }>;
  };
  expect(augustBody.month).toBe("2026-08");
  expect(
    augustBody.items.some((item) => item.description === description),
  ).toBe(true);

  const september = await request.get("/v1/transactions?month=2026-09");
  expect(september.status()).toBe(200);
  const septemberBody = (await september.json()) as {
    items: Array<{ description: string }>;
  };
  expect(
    septemberBody.items.some((item) => item.description === description),
  ).toBe(false);

  const invalid = await request.get("/v1/transactions?month=2026-8");
  expect(invalid.status()).toBe(400);
});

test("system category catalog is complete and deterministically ordered", async ({
  request,
}) => {
  const response = await request.get("/v1/categories");
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toBe(contentType);
  expect(response.headers()["cache-control"]).toBe("no-store");
  const categories = (await response.json()) as Array<Record<string, unknown>>;
  expect(categories).toHaveLength(17);
  expect(categories.map((category) => category.id)).toEqual([
    "expense.food",
    "expense.transport",
    "expense.housing",
    "expense.health",
    "expense.leisure",
    "expense.education",
    "expense.subscriptions",
    "expense.shopping",
    "expense.taxes_fees",
    "expense.other",
    "income.salary",
    "income.freelance",
    "income.refund",
    "income.sale",
    "income.investment_return",
    "income.benefits",
    "income.other",
  ]);
  expect(categories[0]).toEqual({
    id: "expense.food",
    type: "EXPENSE",
    displayName: "Alimentação",
  });
  expect(categories[10]).toEqual({
    id: "income.salary",
    type: "INCOME",
    displayName: "Salário",
  });
  expect(categories.every((category) => !("sortOrder" in category))).toBe(true);
});

test("categorized previews validate applicability and reject ambiguous category shapes", async ({
  request,
}) => {
  const expense = expenseRequest(
    "Preview despesa categorizada sintética",
    "2026-04-10T15:00:00Z",
  );
  const expensePreview = await request.post("/v1/transactions/preview", {
    data: { ...expense, categoryId: "expense.food" },
  });
  expect(expensePreview.status()).toBe(200);
  expect(await expensePreview.json()).toMatchObject({
    type: "EXPENSE",
    categoryId: "expense.food",
  });

  const income = incomeRequest(
    "Preview receita categorizada sintética",
    "2026-04-10T16:00:00Z",
  );
  const incomePreview = await request.post("/v1/transactions/preview", {
    data: { ...income, categoryId: "income.salary" },
  });
  expect(incomePreview.status()).toBe(200);
  expect(await incomePreview.json()).toMatchObject({
    type: "INCOME",
    categoryId: "income.salary",
  });

  for (const data of [
    { ...expense, categoryId: null },
    { ...expense, categoryId: "" },
    { ...expense, categoryId: "income.salary" },
    { ...expense, categoryId: "expense.unknown" },
    { ...expense, categoryName: "Alimentação" },
    { ...income, categoryId: "expense.food" },
  ]) {
    const invalid = await request.post("/v1/transactions/preview", { data });
    expect(invalid.status()).toBe(400);
    expect(await invalid.json()).toEqual({
      error: { code: "INVALID_REQUEST", message: "request is invalid" },
    });
  }
});

test("categorized Expense and Income replay and remain classified in mixed history", async ({
  request,
}) => {
  const expenseDescription = "Despesa categorizada Playwright sintética";
  const expense = {
    ...expenseRequest(expenseDescription, "2026-04-11T15:00:00Z"),
    categoryId: "expense.food",
  };
  const expenseHeaders = {
    "Idempotency-Key": "pw-synthetic-category-expense-001",
  };
  const createdExpense = await request.post("/v1/transactions", {
    data: expense,
    headers: expenseHeaders,
  });
  expect(createdExpense.status()).toBe(201);
  const expenseBytes = await createdExpense.body();
  expect(JSON.parse(expenseBytes.toString())).toMatchObject({
    type: "EXPENSE",
    categoryId: "expense.food",
  });
  const replayedExpense = await request.post("/v1/transactions", {
    data: expense,
    headers: expenseHeaders,
  });
  expect(replayedExpense.status()).toBe(201);
  expect(replayedExpense.headers()["idempotency-replayed"]).toBe("true");
  expect(await replayedExpense.body()).toEqual(expenseBytes);
  const categoryConflict = await request.post("/v1/transactions", {
    data: { ...expense, categoryId: "expense.transport" },
    headers: expenseHeaders,
  });
  expect(categoryConflict.status()).toBe(409);

  const incomeDescription = "Receita categorizada Playwright sintética";
  const income = {
    ...incomeRequest(incomeDescription, "2026-04-11T16:00:00Z"),
    categoryId: "income.salary",
  };
  const createdIncome = await request.post("/v1/transactions", {
    data: income,
    headers: { "Idempotency-Key": "pw-synthetic-category-income-001" },
  });
  expect(createdIncome.status()).toBe(201);
  const incomeBody = (await createdIncome.json()) as Record<string, unknown>;
  expect(incomeBody).toMatchObject({
    type: "INCOME",
    categoryId: "income.salary",
  });
  expect("paymentMethod" in incomeBody).toBe(false);

  const history = await request.get("/v1/transactions?month=2026-04");
  expect(history.status()).toBe(200);
  const historyBody = (await history.json()) as {
    items: Array<Record<string, unknown>>;
  };
  const relevant = historyBody.items.filter((item) =>
    [expenseDescription, incomeDescription].includes(
      item.description as string,
    ),
  );
  expect(relevant).toHaveLength(2);
  expect(relevant[0]).toMatchObject({
    type: "INCOME",
    categoryId: "income.salary",
  });
  expect(relevant[1]).toMatchObject({
    type: "EXPENSE",
    categoryId: "expense.food",
    paymentMethod: "PIX",
  });
});

test("recurrence preview is canonical, strict, and write-free", async ({
  request,
}) => {
  const description = "Preview recorrência Playwright sintética";
  const payload = recurrenceRequest(`  ${description}  `, "2024-02-29");
  const preview = await request.post("/v1/recurrences/preview", {
    data: payload,
  });
  expect(preview.status()).toBe(200);
  expect(preview.headers()["content-type"]).toBe(contentType);
  expect(preview.headers()["cache-control"]).toBe("no-store");
  expect(await preview.json()).toEqual({
    type: "EXPENSE",
    description,
    expectedAmount: { minor: 11900, currency: "BRL" },
    frequency: "MONTHLY",
    startsOn: "2024-02-29",
  });

  const list = await request.get("/v1/recurrences");
  expect(list.status()).toBe(200);
  const listBody = (await list.json()) as {
    items: Array<{ description: string }>;
  };
  expect(listBody.items.some((item) => item.description === description)).toBe(
    false,
  );

  for (const data of [
    { ...payload, userId: "spoofed-owner" },
    { ...payload, type: "INCOME" },
    { ...payload, startsOn: "2026-02-29" },
    { ...payload, startsOn: "2026-08-31T00:00:00Z" },
    { ...payload, expectedAmount: null },
    { ...payload, categoryId: "expense.subscriptions" },
    { ...payload, paymentMethod: "CREDIT" },
    { ...payload, recurring: true },
  ]) {
    const invalid = await request.post("/v1/recurrences/preview", { data });
    expect(invalid.status()).toBe(400);
  }
});

test("recurrence create replays safely, conflicts, and appears in owner list", async ({
  request,
}) => {
  const description = "Recorrência create Playwright sintética";
  const payload = recurrenceRequest(description, "2026-08-31");
  const headers = { "Idempotency-Key": "pw-recurrence-create-replay-001" };

  const missingKey = await request.post("/v1/recurrences", {
    data: payload,
  });
  expect(missingKey.status()).toBe(400);
  expect(await missingKey.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REQUIRED",
      message: "idempotency key is required",
    },
  });

  const created = await request.post("/v1/recurrences", {
    data: payload,
    headers,
  });
  expect(created.status()).toBe(201);
  expect(created.headers()["idempotency-replayed"]).toBeUndefined();
  const createdBytes = await created.body();
  const createdBody = JSON.parse(createdBytes.toString()) as Record<
    string,
    unknown
  >;
  expect(createdBody).toMatchObject({
    type: "EXPENSE",
    description,
    expectedAmount: payload.expectedAmount,
    frequency: "MONTHLY",
    startsOn: "2026-08-31",
    status: "ACTIVE",
  });
  expect("cancelledAt" in createdBody).toBe(false);
  expect("userId" in createdBody).toBe(false);

  const replay = await request.post("/v1/recurrences", {
    data: { ...payload, description: `  ${description}  ` },
    headers,
  });
  expect(replay.status()).toBe(201);
  expect(replay.headers()["idempotency-replayed"]).toBe("true");
  expect(await replay.body()).toEqual(createdBytes);

  const conflict = await request.post("/v1/recurrences", {
    data: {
      ...payload,
      expectedAmount: { minor: 12900, currency: "BRL" },
    },
    headers,
  });
  expect(conflict.status()).toBe(409);
  expect(await conflict.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REUSED",
      message: "idempotency key was reused with a different request",
    },
  });

  const list = await request.get("/v1/recurrences");
  const listBody = (await list.json()) as {
    items: Array<Record<string, unknown>>;
  };
  expect(
    listBody.items.filter((item) => item.description === description),
  ).toHaveLength(1);
});

test("recurrence cancel replays its timestamp and create replay remains historical", async ({
  request,
}) => {
  const payload = recurrenceRequest(
    "Recorrência cancel Playwright sintética",
    "2026-12-31",
  );
  const createHeaders = {
    "Idempotency-Key": "pw-recurrence-historical-create-001",
  };
  const created = await request.post("/v1/recurrences", {
    data: payload,
    headers: createHeaders,
  });
  expect(created.status()).toBe(201);
  const createdBytes = await created.body();
  const createdBody = JSON.parse(createdBytes.toString()) as { id: string };

  const cancelHeaders = {
    "Idempotency-Key": "pw-recurrence-cancel-replay-001",
  };
  const cancelled = await request.post(
    `/v1/recurrences/${createdBody.id}/cancel`,
    { headers: cancelHeaders },
  );
  expect(cancelled.status()).toBe(200);
  const cancelledBytes = await cancelled.body();
  expect(JSON.parse(cancelledBytes.toString())).toMatchObject({
    id: createdBody.id,
    status: "CANCELLED",
    startsOn: "2026-12-31",
  });

  const cancelReplay = await request.post(
    `/v1/recurrences/${createdBody.id}/cancel`,
    { headers: cancelHeaders },
  );
  expect(cancelReplay.status()).toBe(200);
  expect(cancelReplay.headers()["idempotency-replayed"]).toBe("true");
  expect(await cancelReplay.body()).toEqual(cancelledBytes);

  const createReplay = await request.post("/v1/recurrences", {
    data: payload,
    headers: createHeaders,
  });
  expect(createReplay.status()).toBe(201);
  expect(createReplay.headers()["idempotency-replayed"]).toBe("true");
  expect(await createReplay.body()).toEqual(createdBytes);

  const newCancelKey = await request.post(
    `/v1/recurrences/${createdBody.id}/cancel`,
    { headers: { "Idempotency-Key": "pw-recurrence-cancel-new-key-001" } },
  );
  expect(newCancelKey.status()).toBe(409);
  expect(await newCancelKey.json()).toEqual({
    error: {
      code: "RECURRENCE_ALREADY_CANCELLED",
      message: "recurrence is already cancelled",
    },
  });

  const unknown = await request.post("/v1/recurrences/rec_unknown/cancel", {
    headers: { "Idempotency-Key": "pw-recurrence-unknown-001" },
  });
  expect(unknown.status()).toBe(404);
  expect(await unknown.json()).toEqual({
    error: {
      code: "RECURRENCE_NOT_FOUND",
      message: "recurrence was not found",
    },
  });
});

test("expense.subscriptions remains classification and never creates recurrence", async ({
  request,
}) => {
  const description = "Assinatura pontual Playwright sintética";
  const transaction = await request.post("/v1/transactions", {
    data: {
      ...expenseRequest(description, "2026-10-10T15:00:00Z"),
      categoryId: "expense.subscriptions",
    },
    headers: {
      "Idempotency-Key": "pw-subscription-category-is-not-recurrence-001",
    },
  });
  expect(transaction.status()).toBe(201);
  expect(await transaction.json()).toMatchObject({
    type: "EXPENSE",
    categoryId: "expense.subscriptions",
  });

  const recurrences = await request.get("/v1/recurrences");
  expect(recurrences.status()).toBe(200);
  const body = (await recurrences.json()) as {
    items: Array<{ description: string }>;
  };
  expect(body.items.some((item) => item.description === description)).toBe(
    false,
  );
});

test("recurrence suggestion calendar fixtures handle year and anchor boundaries", () => {
  const cases: Array<{
    evaluatedOn: CivilDateParts;
    evidenceDates: string[];
    additionalEvidenceDate: string;
    proposedStartsOn: string;
  }> = [
    {
      evaluatedOn: { year: 2026, month: 1, day: 5 },
      evidenceDates: ["2025-10-10", "2025-11-10", "2025-12-10"],
      additionalEvidenceDate: "2025-09-10",
      proposedStartsOn: "2026-01-10",
    },
    {
      evaluatedOn: { year: 2026, month: 1, day: 10 },
      evidenceDates: ["2025-10-10", "2025-11-10", "2025-12-10"],
      additionalEvidenceDate: "2025-09-10",
      proposedStartsOn: "2026-02-10",
    },
    {
      evaluatedOn: { year: 2026, month: 1, day: 25 },
      evidenceDates: ["2025-10-10", "2025-11-10", "2025-12-10"],
      additionalEvidenceDate: "2025-09-10",
      proposedStartsOn: "2026-02-10",
    },
    {
      evaluatedOn: { year: 2026, month: 2, day: 28 },
      evidenceDates: ["2025-11-10", "2025-12-10", "2026-01-10"],
      additionalEvidenceDate: "2025-10-10",
      proposedStartsOn: "2026-03-10",
    },
    {
      evaluatedOn: { year: 2026, month: 12, day: 31 },
      evidenceDates: ["2026-09-10", "2026-10-10", "2026-11-10"],
      additionalEvidenceDate: "2026-08-10",
      proposedStartsOn: "2027-01-10",
    },
  ];

  for (const expectation of cases) {
    expect(suggestionCalendarFixture(expectation.evaluatedOn)).toEqual({
      evidenceDates: expectation.evidenceDates,
      additionalEvidenceDate: expectation.additionalEvidenceDate,
      proposedStartsOn: expectation.proposedStartsOn,
    });
    for (const date of [
      ...expectation.evidenceDates,
      expectation.additionalEvidenceDate,
    ]) {
      expect(financialEvidenceInstant(date)).toBe(`${date}T12:00:00Z`);
    }
  }
});

test("recurrence suggestions preview server data, dismiss idempotently, and refresh with new evidence", async ({
  request,
}) => {
  const description = "Detecção recorrente Playwright sintética";
  const calendar = suggestionCalendarFixture(financialCivilDate());
  for (const observedOn of calendar.evidenceDates) {
    const occurredAt = financialEvidenceInstant(observedOn);
    const payload = expenseRequest(description, occurredAt);
    payload.amount.minor = 11900;
    const created = await request.post("/v1/transactions", {
      data: payload,
      headers: {
        "Idempotency-Key": `pw-suggestion-evidence-${observedOn}-001`,
      },
    });
    expect(created.status()).toBe(201);
  }

  const listed = await request.get("/v1/recurrence-suggestions");
  expect(listed.status()).toBe(200);
  expect(listed.headers()["cache-control"]).toBe("no-store");
  const listBody = (await listed.json()) as {
    items: Array<Record<string, unknown>>;
  };
  const matches = listBody.items.filter(
    (item) => item.description === description,
  );
  expect(matches).toHaveLength(1);
  const suggestion = matches[0];
  if (!suggestion) {
    throw new Error("expected recurrence suggestion was not returned");
  }
  expect(suggestion).toMatchObject({
    description,
    expectedAmount: { minor: 11900, currency: "BRL" },
    anchorDay: 10,
    proposedStartsOn: calendar.proposedStartsOn,
    evidenceCount: 3,
    observedDates: calendar.evidenceDates,
  });
  expect(Object.keys(suggestion).sort()).toEqual([
    "anchorDay",
    "description",
    "evidenceCount",
    "expectedAmount",
    "id",
    "observedDates",
    "proposedStartsOn",
  ]);
  const firstID = suggestion.id as string;

  const preview = await request.post(
    `/v1/recurrence-suggestions/${firstID}/preview`,
  );
  expect(preview.status()).toBe(200);
  expect(await preview.json()).toEqual({
    type: "EXPENSE",
    description,
    expectedAmount: { minor: 11900, currency: "BRL" },
    frequency: "MONTHLY",
    startsOn: calendar.proposedStartsOn,
  });
  const recurrences = await request.get("/v1/recurrences");
  const recurrenceBody = (await recurrences.json()) as {
    items: Array<{ description: string }>;
  };
  expect(
    recurrenceBody.items.some((item) => item.description === description),
  ).toBe(false);

  const dismissed = await request.post(
    `/v1/recurrence-suggestions/${firstID}/dismiss`,
  );
  expect(dismissed.status()).toBe(204);
  expect(dismissed.headers()["idempotency-replayed"]).toBeUndefined();
  expect(await dismissed.body()).toHaveLength(0);
  const replay = await request.post(
    `/v1/recurrence-suggestions/${firstID}/dismiss`,
  );
  expect(replay.status()).toBe(204);
  expect(replay.headers()["idempotency-replayed"]).toBe("true");
  expect(await replay.body()).toHaveLength(0);

  const hidden = (await (
    await request.get("/v1/recurrence-suggestions")
  ).json()) as { items: Array<Record<string, unknown>> };
  expect(hidden.items.some((item) => item.id === firstID)).toBe(false);

  const additionalEvidence = expenseRequest(
    description,
    financialEvidenceInstant(calendar.additionalEvidenceDate),
  );
  additionalEvidence.amount.minor = 11900;
  const added = await request.post("/v1/transactions", {
    data: additionalEvidence,
    headers: {
      "Idempotency-Key": `pw-suggestion-evidence-${calendar.additionalEvidenceDate}-001`,
    },
  });
  expect(added.status()).toBe(201);
  const refreshed = (await (
    await request.get("/v1/recurrence-suggestions")
  ).json()) as { items: Array<Record<string, unknown>> };
  const refreshedMatch = refreshed.items.find(
    (item) => item.description === description,
  );
  expect(refreshedMatch).toBeDefined();
  expect(refreshedMatch?.id).not.toBe(firstID);
  expect(refreshedMatch?.evidenceCount).toBe(4);
});

test("recurrence suggestion endpoints reject client authority and malformed identities", async ({
  request,
}) => {
  const query = await request.get(
    "/v1/recurrence-suggestions?userId=spoofed-owner",
  );
  expect(query.status()).toBe(400);
  expect(await query.json()).toEqual({
    error: { code: "INVALID_REQUEST", message: "request is invalid" },
  });

  const malformed = await request.post(
    "/v1/recurrence-suggestions/rsg_NOT_HEX/preview",
  );
  expect(malformed.status()).toBe(400);

  const validUnknown = `rsg_${"f".repeat(64)}`;
  const stale = await request.post(
    `/v1/recurrence-suggestions/${validUnknown}/preview`,
  );
  expect(stale.status()).toBe(404);
  expect(await stale.json()).toEqual({
    error: {
      code: "RECURRENCE_SUGGESTION_NOT_FOUND",
      message: "recurrence suggestion was not found",
    },
  });

  const injected = await request.post(
    `/v1/recurrence-suggestions/${validUnknown}/dismiss`,
    { data: { userId: "spoofed-owner", amount: 1 } },
  );
  expect(injected.status()).toBe(400);
});

test("credit card preview, create, replay, list, detail, archive, and historical replay stay metadata-only", async ({
  request,
}) => {
  const name = "Cartão Playwright sintético exclusivo";
  const payload = creditCardRequest(name);

  const rejectedPreviewQuery = await request.post(
    "/v1/cards/preview?foo;bar=baz",
    { data: payload },
  );
  expect(rejectedPreviewQuery.status()).toBe(400);
  expect(await rejectedPreviewQuery.json()).toEqual({
    error: { code: "INVALID_REQUEST", message: "request is invalid" },
  });

  const preview = await request.post("/v1/cards/preview", { data: payload });
  expect(preview.status()).toBe(200);
  expect(preview.headers()["cache-control"]).toBe("no-store");
  expect(await preview.json()).toEqual(payload);

  const injected = await request.post("/v1/cards/preview", {
    data: { ...payload, userId: "spoofed-owner" },
  });
  expect(injected.status()).toBe(400);
  expect(await injected.json()).toEqual({
    error: { code: "INVALID_REQUEST", message: "request is invalid" },
  });

  const missingKey = await request.post("/v1/cards", { data: payload });
  expect(missingKey.status()).toBe(400);
  expect(await missingKey.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REQUIRED",
      message: "idempotency key is required",
    },
  });

  const createHeaders = {
    "Idempotency-Key": "pw-credit-card-create-exclusive-001",
  };
  const rejectedCreateQuery = await request.post("/v1/cards?foo;bar=baz", {
    data: payload,
    headers: createHeaders,
  });
  expect(rejectedCreateQuery.status()).toBe(400);
  expect(await rejectedCreateQuery.json()).toEqual({
    error: { code: "INVALID_REQUEST", message: "request is invalid" },
  });

  const created = await request.post("/v1/cards", {
    data: payload,
    headers: createHeaders,
  });
  expect(created.status()).toBe(201);
  expect(created.headers()["idempotency-replayed"]).toBeUndefined();
  const createdBytes = await created.body();
  const createdBody = JSON.parse(createdBytes.toString()) as Record<
    string,
    unknown
  >;
  expect(createdBody).toMatchObject({ ...payload, status: "ACTIVE" });
  expect(Object.keys(createdBody).sort()).toEqual([
    "brand",
    "closingDay",
    "createdAt",
    "creditLimit",
    "dueDay",
    "id",
    "lastFour",
    "name",
    "status",
  ]);
  const cardID = createdBody.id as string;
  expect(cardID).toMatch(/^card_[0-9a-f]{32}$/);

  const createReplay = await request.post("/v1/cards", {
    data: payload,
    headers: createHeaders,
  });
  expect(createReplay.status()).toBe(201);
  expect(createReplay.headers()["idempotency-replayed"]).toBe("true");
  expect(await createReplay.body()).toEqual(createdBytes);

  const conflict = await request.post("/v1/cards", {
    data: { ...payload, name: `${name} alterado` },
    headers: createHeaders,
  });
  expect(conflict.status()).toBe(409);
  expect(await conflict.json()).toEqual({
    error: {
      code: "IDEMPOTENCY_KEY_REUSED",
      message: "idempotency key was reused with a different request",
    },
  });

  const rejectedListQuery = await request.get("/v1/cards?foo;bar=baz");
  expect(rejectedListQuery.status()).toBe(400);
  expect(await rejectedListQuery.json()).toEqual({
    error: { code: "INVALID_REQUEST", message: "request is invalid" },
  });

  const listed = await request.get("/v1/cards");
  expect(listed.status()).toBe(200);
  const listBody = (await listed.json()) as {
    items: Array<Record<string, unknown>>;
  };
  expect(listBody.items.filter((item) => item.id === cardID)).toEqual([
    createdBody,
  ]);
  const detail = await request.get(`/v1/cards/${cardID}`);
  expect(detail.status()).toBe(200);
  expect(await detail.body()).toEqual(createdBytes);

  const archiveHeaders = {
    "Idempotency-Key": "pw-credit-card-archive-exclusive-001",
  };
  const archived = await request.post(`/v1/cards/${cardID}/archive`, {
    headers: archiveHeaders,
  });
  expect(archived.status()).toBe(200);
  expect(archived.headers()["idempotency-replayed"]).toBeUndefined();
  const archivedBytes = await archived.body();
  expect(JSON.parse(archivedBytes.toString())).toMatchObject({
    id: cardID,
    status: "ARCHIVED",
  });
  const archiveReplay = await request.post(`/v1/cards/${cardID}/archive`, {
    headers: archiveHeaders,
  });
  expect(archiveReplay.status()).toBe(200);
  expect(archiveReplay.headers()["idempotency-replayed"]).toBe("true");
  expect(await archiveReplay.body()).toEqual(archivedBytes);

  const historicalCreate = await request.post("/v1/cards", {
    data: payload,
    headers: createHeaders,
  });
  expect(historicalCreate.status()).toBe(201);
  expect(historicalCreate.headers()["idempotency-replayed"]).toBe("true");
  expect(await historicalCreate.body()).toEqual(createdBytes);
  const current = await request.get(`/v1/cards/${cardID}`);
  expect((await current.json()) as Record<string, unknown>).toMatchObject({
    id: cardID,
    status: "ARCHIVED",
  });

  const alreadyArchived = await request.post(`/v1/cards/${cardID}/archive`, {
    headers: { "Idempotency-Key": "pw-credit-card-archive-new-001" },
  });
  expect(alreadyArchived.status()).toBe(409);
  expect(await alreadyArchived.json()).toEqual({
    error: {
      code: "CREDIT_CARD_ALREADY_ARCHIVED",
      message: "credit card is already archived",
    },
  });

  for (const target of [
    "/v1/cards",
    "/v1/cards/preview",
    `/v1/cards/${cardID}`,
    `/v1/cards/${cardID}/archive`,
  ]) {
    const head = await request.head(target);
    expect(head.status()).toBe(405);
    expect(head.headers()["cache-control"]).toBe("no-store");
  }
});

test("card purchase one-time and installment flows remain explicit and replayable", async ({
  request,
}) => {
  const legacyCredit = await request.post("/v1/transactions", {
    data: {
      type: "EXPENSE",
      description: "CREDIT legado 4B rejeitado",
      amount: { minor: 10000, currency: "BRL" },
      paymentMethod: "CREDIT",
      occurredAt: "2026-08-25T14:00:00Z",
    },
    headers: { "Idempotency-Key": "pw-4b-legacy-credit-rejected-001" },
  });
  expect(legacyCredit.status()).toBe(400);
  expect(await legacyCredit.json()).toEqual({
    error: {
      code: "CREDIT_CARD_REQUIRED",
      message: "credit card is required for new CREDIT expenses",
    },
  });

  const card = await request.post("/v1/cards", {
    data: creditCardRequest("Cartão Playwright 4B sintético"),
    headers: { "Idempotency-Key": "pw-4b-card-create-001" },
  });
  expect(card.status()).toBe(201);
  const cardBody = (await card.json()) as { id: string };
  expect(cardBody.id).toMatch(/^card_[0-9a-f]{32}$/);

  const oneTimePayload = {
    description: "Compra Playwright 4B à vista",
    amount: { minor: 10000, currency: "BRL" },
    occurredAt: "2026-08-25T14:00:00Z",
    creditCardId: cardBody.id,
  };
  const oneTimePreview = await request.post("/v1/card-purchases/preview", {
    data: oneTimePayload,
  });
  expect(oneTimePreview.status()).toBe(200);
  expect(await oneTimePreview.json()).toMatchObject({
    description: oneTimePayload.description,
    amount: oneTimePayload.amount,
    purchaseMode: "ONE_TIME",
    statementClosingOn: "2026-08-31",
    statementDueOn: "2026-09-10",
  });

  const oneTime = await request.post("/v1/card-purchases", {
    data: oneTimePayload,
    headers: { "Idempotency-Key": "pw-4b-purchase-one-time-001" },
  });
  expect(oneTime.status()).toBe(201);
  expect(oneTime.headers()["idempotency-replayed"]).toBeUndefined();
  const oneTimeBytes = await oneTime.body();
  const oneTimeBody = JSON.parse(oneTimeBytes.toString()) as Record<
    string,
    unknown
  >;
  expect(oneTimeBody).toMatchObject({ purchaseMode: "ONE_TIME" });
  expect(oneTimeBody.installmentPlan).toBeUndefined();
  expect((oneTimeBody.expense as Record<string, unknown>).paymentMethod).toBe(
    "CREDIT",
  );

  const oneTimeReplay = await request.post("/v1/card-purchases", {
    data: oneTimePayload,
    headers: { "Idempotency-Key": "pw-4b-purchase-one-time-001" },
  });
  expect(oneTimeReplay.status()).toBe(201);
  expect(oneTimeReplay.headers()["idempotency-replayed"]).toBe("true");
  expect(await oneTimeReplay.body()).toEqual(oneTimeBytes);

  const installmentPayload = {
    description: "Compra Playwright 4B parcelada",
    amount: { minor: 10100, currency: "BRL" },
    occurredAt: "2026-08-25T14:00:00Z",
    creditCardId: cardBody.id,
    installmentCount: 3,
  };
  const installmentPreview = await request.post("/v1/card-purchases/preview", {
    data: installmentPayload,
  });
  expect(installmentPreview.status()).toBe(200);
  expect(await installmentPreview.json()).toMatchObject({
    purchaseMode: "INSTALLMENT",
    statementDueOn: "2026-09-10",
    installmentSummary: {
      installmentCount: 3,
      firstDueDate: "2026-09-10",
      lastDueDate: "2026-11-10",
      dueDayAnchor: 10,
      regularInstallmentAmount: { minor: 3366, currency: "BRL" },
      lastInstallmentAmount: { minor: 3368, currency: "BRL" },
    },
  });

  const installment = await request.post("/v1/card-purchases", {
    data: installmentPayload,
    headers: { "Idempotency-Key": "pw-4b-purchase-installment-001" },
  });
  expect(installment.status()).toBe(201);
  expect(installment.headers()["idempotency-replayed"]).toBeUndefined();
  const installmentBody = (await installment.json()) as {
    purchaseMode: string;
    installmentPlan: { id: string; status: string; schedule: unknown[] };
  };
  expect(installmentBody.purchaseMode).toBe("INSTALLMENT");
  expect(installmentBody.installmentPlan.id).toMatch(/^ipl_[0-9a-f]{32}$/);
  expect(installmentBody.installmentPlan.status).toBe("ACTIVE");
  expect(installmentBody.installmentPlan.schedule).toHaveLength(3);

  const planID = installmentBody.installmentPlan.id;
  const plans = await request.get("/v1/installment-plans");
  expect(plans.status()).toBe(200);
  expect((await plans.json()).items).toEqual([
    expect.objectContaining({ id: planID, status: "ACTIVE" }),
  ]);
  const detail = await request.get(`/v1/installment-plans/${planID}`);
  expect(detail.status()).toBe(200);
  expect(await detail.json()).toMatchObject({ id: planID, status: "ACTIVE" });

  const cancellationPreview = await request.post(
    `/v1/installment-plans/${planID}/cancellation-preview`,
  );
  expect(cancellationPreview.status()).toBe(200);
  const cancellationPreviewBody = (await cancellationPreview.json()) as {
    expectedCancelledOn: string;
  };
  expect(cancellationPreviewBody.expectedCancelledOn).toMatch(
    /^\d{4}-\d{2}-\d{2}$/,
  );

  const cancellation = await request.post(
    `/v1/installment-plans/${planID}/cancel`,
    {
      data: {
        expectedCancelledOn: cancellationPreviewBody.expectedCancelledOn,
      },
      headers: { "Idempotency-Key": "pw-4b-plan-cancel-001" },
    },
  );
  expect(cancellation.status()).toBe(200);
  expect(cancellation.headers()["idempotency-replayed"]).toBeUndefined();
  const cancellationBytes = await cancellation.body();
  expect(JSON.parse(cancellationBytes.toString())).toMatchObject({
    id: planID,
    status: "CANCELLED",
    cancelledOn: cancellationPreviewBody.expectedCancelledOn,
  });

  const cancellationReplay = await request.post(
    `/v1/installment-plans/${planID}/cancel`,
    {
      data: {
        expectedCancelledOn: cancellationPreviewBody.expectedCancelledOn,
      },
      headers: { "Idempotency-Key": "pw-4b-plan-cancel-001" },
    },
  );
  expect(cancellationReplay.status()).toBe(200);
  expect(cancellationReplay.headers()["idempotency-replayed"]).toBe("true");
  expect(await cancellationReplay.body()).toEqual(cancellationBytes);

  const alreadyCancelled = await request.post(
    `/v1/installment-plans/${planID}/cancel`,
    {
      data: {
        expectedCancelledOn: cancellationPreviewBody.expectedCancelledOn,
      },
      headers: { "Idempotency-Key": "pw-4b-plan-cancel-002" },
    },
  );
  expect(alreadyCancelled.status()).toBe(409);
  expect(await alreadyCancelled.json()).toEqual({
    error: {
      code: "INSTALLMENT_PLAN_ALREADY_CANCELLED",
      message: "installment plan is already cancelled",
    },
  });

  const augustTransactions = await request.get(
    "/v1/transactions?month=2026-08",
  );
  expect(augustTransactions.status()).toBe(200);
  const transactionItems = (await augustTransactions.json()).items as Array<{
    description: string;
  }>;
  expect(
    transactionItems.filter(({ description }) =>
      description.startsWith("Compra Playwright 4B"),
    ),
  ).toHaveLength(2);
});
