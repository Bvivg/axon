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
  const [copyFailed, setCopyFailed] = useState(false);

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
      <div className="flex flex-col gap-2 pt-2">
        <Button size="lg" className="w-full" onClick={reset}>
          Try again
        </Button>
        <details className="rounded-lg border border-border bg-card p-3 text-sm">
          <summary className="cursor-pointer font-medium">Technical details</summary>
          <pre className="my-3 whitespace-pre-wrap break-all text-xs">{details}</pre>
        <Button
          variant="outline"
          size="lg"
          className="w-full"
          onClick={() => {
            setCopyFailed(false);
            if (!navigator.clipboard) { setCopyFailed(true); return; }
            void navigator.clipboard.writeText(details).then(() => setCopied(true), () => setCopyFailed(true));
          }}
        >
          {copied ? <Check strokeWidth={1.5} className="text-success" /> : <Copy strokeWidth={1.5} />}
          {copied ? "Copied" : "Copy details"}
        </Button>
          {copyFailed ? <p role="status" className="mt-2 text-sm">Couldn’t copy. Select the details to copy them manually.</p> : null}
        </details>
      </div>
    </main>
  );
}
