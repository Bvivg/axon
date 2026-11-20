import { defineConfig } from "@playwright/test";
import sfu from "./playwright.calls-sfu.config";

export default defineConfig({ ...sfu, testMatch: "calls-migration.spec.ts" });
