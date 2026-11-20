import { defineConfig } from "@playwright/test";
import spike from "./playwright.calls-spike.config";

export default defineConfig({
  ...spike,
  testMatch: "calls-ws.spec.ts",
  projects: spike.projects?.filter((project) => project.name === "chromium"),
  reporter: [["list"], ["json", { outputFile: "/evidence/playwright-results.json" }]],
});
