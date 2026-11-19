import { defineConfig } from "@playwright/test";
import spike from "./playwright.calls-spike.config";
export default defineConfig({...spike,testMatch:"calls-admission.spec.ts",projects:spike.projects?.filter(p=>p.name==="chromium"),reporter:[["list"],["json",{outputFile:"/evidence/playwright-results.json"}]]});
