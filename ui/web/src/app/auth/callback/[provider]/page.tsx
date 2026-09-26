"use client";

import { X } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { ProviderMark } from "@/components/auth/provider-buttons";
import { BrandMark } from "@/components/nav/brand";
import { Button, buttonVariants } from "@/components/ui/button";
import { LoadingDots } from "@/components/ui/status";
import { authClient } from "@/lib/connect/clients";
import { describe, errorCode } from "@/lib/errors";
import { providerLabels, startOAuth, takePendingFlow, toProvider } from "@/lib/auth/oauth";
import { useSession } from "@/lib/auth/session";

interface Failure {
  title: string;
  detail: string;
}

export default function OAuthCallbackPage() {
  const params = useParams<{ provider: string }>();
  const search = useSearchParams();
  const router = useRouter();
  const { signedIn } = useSession();

  const [failure, setFailure] = useState<Failure | null>(null);
  const [retrying, setRetrying] = useState(false);

  const started = useRef(false);

  const slug = params.provider;
  const provider = providerLabels[slug] ?? slug;

  useEffect(() => {
    if (started.current) {
      return;
    }
    started.current = true;

    void (async () => {
      const kind = toProvider(slug);
      if (kind === undefined) {
        setFailure({ title: "Unknown sign-in provider", detail: `provider=${slug}` });
        return;
      }

      const denied = search.get("error");
      if (denied) {
        setFailure({
          title: denied === "access_denied" ? `You declined access at ${provider}` : `${provider} refused the request`,
          detail: `error=${denied}`,
        });
        return;
      }

      const code = search.get("code");
      const state = search.get("state");
      if (!code || !state) {
        setFailure({ title: `${provider} sent back an incomplete answer`, detail: "missing code or state" });
        return;
      }

      const pending = takePendingFlow(slug);
      if (!pending) {
        setFailure({ title: "No sign-in was in progress in this tab", detail: "state not found in this tab" });
        return;
      }
      if (pending.state !== state) {
        setFailure({ title: "This answer belongs to another sign-in", detail: "state mismatch" });
        return;
      }

      try {
        const displayName = search.get("name") ?? undefined;
        const result = await authClient.completeOAuth({ provider: kind, code, state, displayName });
        if (!result.user) {
          throw new Error("the server returned no user");
        }

        signedIn(result.user, result.tokens);
        router.replace(pending.returnTo || "/profile");
      } catch (err) {
        setFailure({ title: "Sign-in failed", detail: `[${errorCode(err)}] ${describe(err)}` });
      }
    })();
  }, [slug, provider, search, router, signedIn]);

  const retry = async () => {
    setRetrying(true);
    try {
      await startOAuth(slug, "/");
    } catch (err) {
      setFailure({ title: "Sign-in failed", detail: `[${errorCode(err)}] ${describe(err)}` });
      setRetrying(false);
    }
  };

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-[400px] flex-col items-center justify-center gap-6 px-7 py-8 text-center">
      <div className="flex items-center gap-3.5">
        <ProviderMark slug={slug} className="size-[52px] rounded-xl text-sm" />
        {failure ? (
          <span className="flex size-7 items-center justify-center rounded-full bg-destructive/15 text-destructive">
            <X aria-hidden className="size-4" strokeWidth={2} />
          </span>
        ) : (
          <LoadingDots className="text-signal" label={`Signing in with ${provider}`} />
        )}
        <BrandMark className="size-[52px] rounded-xl" />
      </div>

      <h1 role={failure ? "alert" : undefined} className="text-xl font-semibold tracking-snug">
        {failure ? failure.title : `Signing in with ${provider}`}
      </h1>

      {failure ? (
        <>
          <code className="block w-full rounded-lg border border-border bg-muted px-3 py-2.5 text-left font-mono text-xs text-muted-foreground">
            {failure.detail}
          </code>
          <div className="flex w-full flex-col gap-2">
            {toProvider(slug) !== undefined ? (
              <Button size="lg" className="w-full" onClick={() => void retry()} disabled={retrying} aria-busy={retrying}>
                Try {provider} again
              </Button>
            ) : null}
            <Link href="/login" className={buttonVariants({ variant: "outline", size: "lg", className: "w-full" })}>
              Back to sign in
            </Link>
          </div>
        </>
      ) : null}
    </main>
  );
}
