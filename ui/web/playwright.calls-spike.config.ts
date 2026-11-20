import { defineConfig, devices } from "@playwright/test";
import base from "./playwright.config";

export default defineConfig({
  ...base,
  testIgnore: [],
  testMatch: ["calls-media-spike.spec.ts", "calls-permissions-spike.spec.ts"],
  outputDir: "/evidence/test-results",
  reporter: [["list"], ["json", { outputFile: "/evidence/playwright-results.json" }]],
  use: {
    ...base.use,
    baseURL: "http://localhost:3000",
    permissions: [],
    screenshot: "off",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        channel: "chromium",
        permissions: ["microphone", "camera"],
        launchOptions: {
          args: ["--use-fake-device-for-media-stream", "--use-file-for-fake-audio-capture=/evidence/.secrets/tone.wav"],
        },
      },
    },
    {
      name: "firefox",
      testMatch: "calls-media-spike.spec.ts",
      use: {
        ...devices["Desktop Firefox"],
        launchOptions: {
          firefoxUserPrefs: {
            "media.navigator.streams.fake": true,
            "media.navigator.permission.disabled": true,
          },
        },
      },
    },
  ],
});
