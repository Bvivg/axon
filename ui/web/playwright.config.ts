import { defineConfig, devices } from "@playwright/test";

/**
 * The browser suite runs only inside Docker (see .claude/rules/testing.md), so
 * the defaults here are the compose network's names rather than localhost. A
 * host run would need the whole stack published, which is exactly what the
 * stack file refuses to do.
 *
 * There is no `webServer`: compose starts the client and holds this container
 * until its healthcheck passes. Letting Playwright start a server as well would
 * be a second, differently configured copy of the thing under test.
 */
export default defineConfig({
  testDir: "./e2e",

  // One worker, and therefore one browser. The gateway rate-limits per address
  // and a browser cannot forge one, so every test in a run shares a bucket;
  // serial keeps that predictable. Six short scenarios do not need the
  // parallelism anyway.
  workers: 1,
  fullyParallel: false,

  // The stack is built from empty on every run, so a failure is a failure —
  // retrying would only hide a race in the client.
  retries: 0,
  forbidOnly: !!process.env.CI,

  timeout: 30_000,
  expect: { timeout: 10_000 },

  // The run's output is the report: artifacts stay inside a container that is
  // torn down straight afterwards, and the Makefile prints the stack's logs
  // when the run is red.
  reporter: [["list"]],

  use: {
    baseURL: process.env.WEB_BASE_URL ?? "http://web:3000",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  // Chromium alone. What is under test is cookie attributes, CORS and session
  // restore — server-side behaviour a second engine would exercise identically,
  // for twice the image and twice the run.
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
