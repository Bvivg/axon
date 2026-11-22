"use client";

import { useState } from "react";

import { ProviderMark } from "@/components/auth/provider-buttons";
import { ProfileSection } from "@/components/profile/profile-page";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/status";
import { OAuthIntent, enabledProviders, providerLabels, startOAuth, toProvider } from "@/lib/auth/oauth";
import { describe } from "@/lib/errors";
import { useLinkedProviders } from "@/lib/query/profile";

const rowNames: Record<string, string> = { ...providerLabels, fake: "Test provider" };

export function SignInMethods() {
  const providers = enabledProviders();
  const linked = useLinkedProviders();
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (providers.length === 0) {
    return null;
  }

  async function connect(slug: string) {
    setPending(slug);
    setError(null);
    try {
      await startOAuth(slug, "/profile/sessions", OAuthIntent.Link);
    } catch (err) {
      setError(describe(err));
      setPending(null);
    }
  }

  return (
    <ProfileSection id="sign-in-methods" label="Sign-in methods">
      {error ? <Alert className="mb-2">{error}</Alert> : null}
      {linked.isError ? <Alert className="mb-2">{describe(linked.error)}</Alert> : null}
      <ul className="flex flex-col overflow-hidden">
        {providers.map((slug) => {
          const name = rowNames[slug] ?? slug;
          const kind = toProvider(slug);
          const connected = kind !== undefined && (linked.data?.includes(kind) ?? false);
          return (
            <li key={slug} className="flex min-h-14 items-center gap-3 border-b border-border py-2 last:border-b-0">
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
                <ProviderMark slug={slug} />
              </span>
              <span className="min-w-0 flex-1 text-sm font-medium">{name}</span>
              {linked.isPending ? (
                <Skeleton className="h-6 w-20 rounded-full" />
              ) : connected ? (
                <Badge variant="success" dot>
                  Connected
                </Badge>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={pending !== null || linked.isError}
                  aria-busy={pending === slug}
                  aria-label={`Connect ${name}`}
                  onClick={() => void connect(slug)}
                >
                  {pending === slug ? <Spinner /> : null}
                  {pending === slug ? "Redirecting…" : "Connect"}
                </Button>
              )}
            </li>
          );
        })}
      </ul>
    </ProfileSection>
  );
}
