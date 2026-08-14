import { expect, test } from "@playwright/test";

test("foundation health endpoint is operational", async ({ request }) => {
  const response = await request.get("/healthz");

  expect(response.status()).toBe(200);
  const headers = response.headers();
  expect(headers["content-type"]).toBe("application/json; charset=utf-8");
  expect(headers["cache-control"]).toBe("no-store");
  expect(headers["x-content-type-options"]).toBe("nosniff");
  expect(await response.text()).toBe('{"status":"ok"}');
});

test("foundation health endpoint rejects unsupported methods", async ({
  request,
}) => {
  const response = await request.post("/healthz", { data: {} });

  expect(response.status()).toBe(405);
  expect(response.headers()["content-type"]).toBe("text/plain; charset=utf-8");
  expect(await response.text()).toBe("Method Not Allowed\n");
});
