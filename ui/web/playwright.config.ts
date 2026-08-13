import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",

  workers: 1,
  fullyParallel: false,

  retries: 0,
  forbidOnly: !!process.env.CI,

  timeout: 30_000,
  expect: { timeout: 10_000 },

  reporter: [["list"]],

  use: {
    baseURL: process.env.WEB_BASE_URL ?? "http://web:3000",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
