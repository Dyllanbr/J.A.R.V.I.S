import { expect, test } from "@playwright/test";

const contentType = "application/json; charset=utf-8";

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
