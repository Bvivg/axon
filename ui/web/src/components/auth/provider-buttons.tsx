"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { describe } from "@/lib/errors";
import { currentNext } from "@/lib/auth/guards";
import { enabledProviders, providerLabels, startOAuth } from "@/lib/auth/oauth";

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
    <div className="space-y-3">
      <div className="flex items-center gap-3">
        <span className="h-px flex-1 bg-border" />
        <span className="text-xs uppercase tracking-wide text-muted-foreground">
          or continue with
        </span>
        <span className="h-px flex-1 bg-border" />
      </div>

      {error ? <Alert>{error}</Alert> : null}

      <div className="grid gap-2">
        {providers.map((slug) => (
          <Button
            key={slug}
            type="button"
            variant="outline"
            disabled={pending !== null}
            onClick={() => void begin(slug)}
          >
            {pending === slug
              ? "Redirecting…"
              : `Continue with ${providerLabels[slug] ?? slug}`}
          </Button>
        ))}
      </div>
    </div>
  );
}
