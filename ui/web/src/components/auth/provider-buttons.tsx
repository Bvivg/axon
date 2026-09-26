"use client";

import { useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/status";
import { currentNext } from "@/lib/auth/guards";
import { enabledProviders, providerLabels, startOAuth } from "@/lib/auth/oauth";
import { describe } from "@/lib/errors";
import { cn } from "@/lib/utils";

export const providerMarks: Record<string, string> = {
  github: "GH",
  google: "G",
  apple: "A",
  fake: "T",
};

export function ProviderMark({ slug, className }: { slug: string; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "flex size-5 shrink-0 items-center justify-center rounded-full border border-dashed border-muted-foreground font-mono text-[10px] text-muted-foreground",
        className,
      )}
    >
      {providerMarks[slug] ?? slug.slice(0, 1).toUpperCase()}
    </span>
  );
}

export function ProviderButtons() {
  const providers = enabledProviders();
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (providers.length === 0) {
    return null;
  }

  async function begin(slug: string) {
    setPending(slug);
    setError(null);
    try {
      await startOAuth(slug, currentNext());
    } catch (err) {
      setError(describe(err));
      setPending(null);
    }
  }

  return (
    <div className="flex flex-col gap-2">
      {error ? <Alert>{error}</Alert> : null}

      {providers.map((slug) => (
        <Button
          key={slug}
          variant="outline"
          size="lg"
          className="w-full"
          disabled={pending !== null}
          aria-busy={pending === slug}
          onClick={() => void begin(slug)}
        >
          {pending === slug ? <Spinner /> : <ProviderMark slug={slug} />}
          {pending === slug ? "Redirecting…" : `Continue with ${providerLabels[slug] ?? slug}`}
        </Button>
      ))}
    </div>
  );
}

export function OrDivider() {
  return (
    <div className="flex items-center gap-3" role="separator" aria-label="or">
      <span className="h-px flex-1 bg-border" />
      <span aria-hidden className="font-mono text-xs text-muted-foreground">
        or
      </span>
      <span className="h-px flex-1 bg-border" />
    </div>
  );
}
