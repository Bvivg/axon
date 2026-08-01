"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { authClient } from "@/lib/connect/clients";
import { describe } from "@/lib/errors";
import { useSession } from "@/lib/auth/session";

export type Mode = "login" | "register";

/**
 * The email-and-password form for both signing in and signing up.
 *
 * One component because the two differ only in which procedure they call and
 * whether a display name is asked for. Splitting them would duplicate the
 * error handling, which is the part with actual behaviour in it.
 *
 * No client-side validation beyond what the browser does for free. The gateway
 * is the trust boundary and validates on arrival; a second set of rules here
 * would drift from it and start rejecting things the server accepts.
 */
export function CredentialsForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const { signedIn } = useSession();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setError(null);

    try {
      const result =
        mode === "login"
          ? await authClient.login({ email, password })
          : await authClient.register({
              email,
              password,
              // Optional in the contract, so an empty box means "not given"
              // rather than an empty name.
              displayName: displayName.trim() || undefined,
            });

      if (!result.user) {
        throw new Error("the server returned no user");
      }

      // Only the access token is in the response. The refresh token was moved
      // into an HttpOnly cookie by the gateway on the way here, which is why
      // there is nothing to store and nothing to accidentally log.
      signedIn(result.user, result.tokens);
      router.replace("/profile");
    } catch (err) {
      setError(describe(err));
      setPending(false);
    }
  }

  return (
    <form className="space-y-4" onSubmit={(e) => void submit(e)}>
      {error ? <Alert>{error}</Alert> : null}

      <div className="space-y-2">
        <Label htmlFor="email">Email</Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
      </div>

      {mode === "register" ? (
        <div className="space-y-2">
          <Label htmlFor="displayName">Display name (optional)</Label>
          <Input
            id="displayName"
            type="text"
            autoComplete="nickname"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
          />
        </div>
      ) : null}

      <div className="space-y-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          type="password"
          // Tells a password manager whether to offer a saved password or a
          // generated one.
          autoComplete={mode === "login" ? "current-password" : "new-password"}
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>

      <Button type="submit" className="w-full" disabled={pending}>
        {pending
          ? "Working…"
          : mode === "login"
            ? "Sign in"
            : "Create account"}
      </Button>
    </form>
  );
}
