import { build } from "vite";

await build({
  configFile: false,
  publicDir: false,
  define: { "process.env.NEXT_PUBLIC_API_BASE_URL": JSON.stringify(process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:18080") },
  build: { emptyOutDir: false, outDir: "public", lib: { entry: "src/calls-push-worker.ts", name: "AxonPush", formats: ["iife"], fileName: () => "calls-sw.js" } },
});
