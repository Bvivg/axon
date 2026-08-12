"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { authClient } from "@/lib/connect/clients";
import { describe } from "@/lib/errors";
import { takePendingFlow, toProvider } from "@/lib/auth/oauth";
import { useSession } from "@/lib/auth/session";

export default function OAuthCallbackPage() {
  const params = useParams<{ provider: string }>();
  const search = useSearchParams();
  const router = useRouter();
  const { signedIn } = useSession();

  const [error, setError] = useState<string | null>(null);

  const started = useRef(false);

  useEffect(() => {
    if (started.current) {
      return;
    }
    started.current = true;

    const slug = params.provider;

    void (async () => {
      const provider = toProvider(slug);
      if (provider === undefined) {
        setError(`Unknown provider: ${slug}`);
        return;
      }

      const denied = search.get("error");
      if (denied) {
        setError(`${providerRefusal(denied)} (${denied})`);
        return;
      }

      const code = search.get("code");
      const state = search.get("state");
      if (!code || !state) {
        setError("The provider returned an incomplete callback.");
        return;
      }

      const pending = takePendingFlow(slug);
      if (!pending) {
        setError("No sign-in was in progress in this tab. Start again.");
        return;
      }
      if (pending.state !== state) {

        setError("This callback does not belong to the sign-in you started.");
        return;
      }

      try {

        const displayName = search.get("name") ?? undefined;

        const result = await authClient.completeOAuth({ provider, code, state, displayName });
        if (!result.user) {
          throw new Error("the server returned no user");
        }

        signedIn(result.user, result.tokens);
        router.replace(pending.returnTo || "/profile");
      } catch (err) {
        setError(describe(err));
      }
    })();
  }, [params.provider, search, router, signedIn]);

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-md flex-col justify-center px-6">
      <Card>
        <CardHeader>
          <CardTitle>{error ? "Sign-in failed" : "Finishing sign-in…"}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {error ? (
            <>
              <Alert>{error}</Alert>
              <Link className="text-sm underline underline-offset-4" href="/login">
                Back to sign-in
              </Link>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">
              Exchanging the provider&apos;s code for a session.
            </p>
          )}
        </CardContent>
      </Card>
    </main>
  );
}

function providerRefusal(code: string): string {
  return code === "access_denied"
    ? "You declined the request at the provider."
    : "The provider refused the request.";
}
