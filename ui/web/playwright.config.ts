import { defineConfig, devices } from "@playwright/test";

const baseURL = process.env.WEB_BASE_URL ?? "http://web:3000";

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
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    permissions: ["microphone"],
  },

  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        channel: "chromium",
        launchOptions: {
          args: [
            "--use-fake-ui-for-media-stream",
            "--use-fake-device-for-media-stream",
            `--unsafely-treat-insecure-origin-as-secure=${new URL(baseURL).origin}`,
          ],
        },
      },
    },
  ],
});
