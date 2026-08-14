import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: process.env.CI ? "github" : "list",
  timeout: 10_000,
  use: {
    baseURL: process.env.JARVIS_API_BASE_URL ?? "http://127.0.0.1:8080",
    extraHTTPHeaders: {
      Accept: "application/json",
    },
  },
});
