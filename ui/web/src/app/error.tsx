"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";

export default function ErrorPage({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  const [copied, setCopied] = useState(false);

  const details = [
    `digest: ${error.digest ?? "none"}`,
    `error: ${error.name}`,
    `page: ${typeof window === "undefined" ? "" : window.location.pathname}`,
  ].join("\n");

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-[420px] flex-col justify-center gap-4 px-6 py-8">
      <span
        aria-hidden
        className="font-mono text-[56px] leading-[56px] tracking-display text-destructive/45"
      >
        500
      </span>
      <h1 className="text-2xl font-semibold tracking-tight">Something broke on our side</h1>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 rounded-lg border border-border bg-card p-3 text-sm">
        <dt className="text-muted-foreground">Reference</dt>
        <dd className="truncate text-right font-mono text-xs leading-5">{error.digest ?? "—"}</dd>
        <dt className="text-muted-foreground">Error</dt>
        <dd className="truncate text-right font-mono text-xs leading-5">{error.name}</dd>
      </dl>
      <div className="flex flex-col gap-2 pt-2">
        <Button size="lg" className="w-full" onClick={reset}>
          Reload
        </Button>
        <Button
          variant="outline"
          size="lg"
          className="w-full"
          onClick={() => void navigator.clipboard.writeText(details).then(() => setCopied(true))}
        >
          {copied ? <Check strokeWidth={1.5} className="text-success" /> : <Copy strokeWidth={1.5} />}
          {copied ? "Copied" : "Copy details"}
        </Button>
      </div>
    </main>
  );
}
