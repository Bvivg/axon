"use client";

import { X } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";

import { ProviderMark } from "@/components/auth/provider-buttons";
import { BrandMark } from "@/components/nav/brand";
import { Button, buttonVariants } from "@/components/ui/button";
import { LoadingDots } from "@/components/ui/status";
import { apiClient, authClient } from "@/lib/connect/clients";
import { OAuthIntent, providerLabels, startOAuth, takePendingFlow, toProvider } from "@/lib/auth/oauth";
import { linkFailure, signInFailure } from "@/lib/auth/oauth-failure";
import { refreshSession } from "@/lib/auth/refresh";
import { profileKeys } from "@/lib/query/profile";
import { useSession } from "@/lib/auth/session";
import { HistoryMode, navigateTo } from "@/lib/chat/address";

interface Failure {
  title: string;
  detail: string;
  retryable?: boolean;
}

export default function OAuthCallbackPage() {
  const params = useParams<{ provider: string }>();
  const search = useSearchParams();
  const router = useRouter();
  const { beginSignIn, signedIn } = useSession();
  const queryClient = useQueryClient();

  const [failure, setFailure] = useState<Failure | null>(null);
  const [intent, setIntent] = useState(OAuthIntent.SignIn);
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

      if (pending.intent === OAuthIntent.Link) {
        setIntent(OAuthIntent.Link);
        try {
          if (!(await refreshSession())) {
            setFailure({ title: `Sign in to connect ${provider}`, detail: "no active session", retryable: false });
            return;
          }
          const result = await apiClient.completeOAuth({ provider: kind, code, state });
          if (!result.linked) {
            throw new Error("the server did not connect the provider");
          }
          await queryClient.invalidateQueries({ queryKey: profileKeys.linkedProviders });
          navigateTo(router, pending.returnTo || "/profile/sessions", HistoryMode.Replace);
        } catch (err) {
          setFailure(linkFailure(err, provider));
        }
        return;
      }

      try {
        beginSignIn();
        const displayName = search.get("name") ?? undefined;
        const result = await authClient.completeOAuth({ provider: kind, code, state, displayName });
        if (!result.user) {
          throw new Error("the server returned no user");
        }

        signedIn(result.user, result.tokens);
        navigateTo(router, pending.returnTo || "/profile", HistoryMode.Replace);
      } catch (err) {
        setFailure(signInFailure(err));
      }
    })();
  }, [slug, provider, search, router, beginSignIn, signedIn, queryClient]);

  const linking = intent === OAuthIntent.Link;

  const retry = async () => {
    setRetrying(true);
    try {
      await (linking ? startOAuth(slug, "/profile/sessions", OAuthIntent.Link) : startOAuth(slug, "/"));
    } catch (err) {
      setFailure(linking ? linkFailure(err, provider) : signInFailure(err));
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
          <LoadingDots className="text-signal" label={linking ? `Connecting ${provider}` : `Signing in with ${provider}`} />
        )}
        <BrandMark className="size-[52px] rounded-xl" />
      </div>

      <h1 role={failure ? "alert" : undefined} className="text-xl font-semibold tracking-snug">
        {failure ? failure.title : linking ? `Connecting ${provider}` : `Signing in with ${provider}`}
      </h1>

      {failure ? (
        <>
          <code className="block w-full rounded-lg border border-border bg-muted px-3 py-2.5 text-left font-mono text-xs text-muted-foreground">
            {failure.detail}
          </code>
          <div className="flex w-full flex-col gap-2">
            {toProvider(slug) !== undefined && failure.retryable !== false ? (
              <Button size="lg" className="w-full" onClick={() => void retry()} disabled={retrying} aria-busy={retrying}>
                Try {provider} again
              </Button>
            ) : null}
            <Link
              href={linking ? "/profile/sessions" : "/login"}
              className={buttonVariants({ variant: "outline", size: "lg", className: "w-full" })}
            >
              {linking ? "Back to profile" : "Back to sign in"}
            </Link>
          </div>
        </>
      ) : null}
    </main>
  );
}
