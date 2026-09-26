"use client";

import { Code } from "@connectrpc/connect";
import { Eye, EyeOff } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { Alert, FieldError } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input, InputGroup, InputGroupInput } from "@/components/ui/input";
import { Field, Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/status";
import { currentNext } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { authClient } from "@/lib/connect/clients";
import { describe, isCode } from "@/lib/errors";

export enum Mode {
  Login = "login",
  Register = "register",
}

enum Problem {
  Credentials = "credentials",
  EmailTaken = "email-taken",
  RateLimited = "rate-limited",
  Other = "other",
}

interface Failure {
  problem: Problem;
  message: string;
}

function toFailure(err: unknown): Failure {
  if (isCode(err, Code.Unauthenticated)) {
    return { problem: Problem.Credentials, message: "Email or password is incorrect." };
  }
  if (isCode(err, Code.AlreadyExists)) {
    return { problem: Problem.EmailTaken, message: "That email is already registered." };
  }
  if (isCode(err, Code.ResourceExhausted)) {
    return { problem: Problem.RateLimited, message: "Sign-in is paused for a minute. Try again shortly." };
  }
  return { problem: Problem.Other, message: describe(err) };
}

export function CredentialsForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const { signedIn } = useSession();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [reveal, setReveal] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);
  const [pending, setPending] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setFailure(null);

    try {
      const result =
        mode === Mode.Login
          ? await authClient.login({ email, password })
          : await authClient.register({
              email,
              password,
              displayName: displayName.trim() || undefined,
            });

      if (!result.user) {
        throw new Error("the server returned no user");
      }

      signedIn(result.user, result.tokens);
      router.replace(currentNext());
    } catch (err) {
      setFailure(toFailure(err));
      setPending(false);
    }
  }

  const credentialsWrong = failure?.problem === Problem.Credentials;
  const emailTaken = failure?.problem === Problem.EmailTaken;

  return (
    <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
      {failure?.problem === Problem.RateLimited ? (
        <Alert variant="warning">
          <span className="font-medium">Too many attempts.</span> {failure.message}
        </Alert>
      ) : null}
      {failure?.problem === Problem.Other ? <Alert>{failure.message}</Alert> : null}

      <Field>
        <Label htmlFor="email">Email</Label>
        <Input
          id="email"
          type="email"
          autoComplete="email"
          required
          value={email}
          aria-invalid={emailTaken || undefined}
          aria-describedby={emailTaken ? "email-error" : undefined}
          onChange={(e) => setEmail(e.target.value)}
          disabled={pending}
        />
        {emailTaken ? <FieldError id="email-error">{failure.message}</FieldError> : null}
      </Field>

      {mode === Mode.Register ? (
        <Field>
          <Label htmlFor="displayName">
            Display name <span className="font-normal text-muted-foreground">· optional</span>
          </Label>
          <Input
            id="displayName"
            type="text"
            autoComplete="nickname"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            disabled={pending}
          />
        </Field>
      ) : null}

      <Field>
        <Label htmlFor="password">Password</Label>
        <InputGroup invalid={credentialsWrong} className="pr-1 md:pr-0.5">
          <InputGroupInput
            id="password"
            type={reveal ? "text" : "password"}
            autoComplete={mode === Mode.Login ? "current-password" : "new-password"}
            required
            value={password}
            aria-invalid={credentialsWrong || undefined}
            aria-describedby={credentialsWrong ? "password-error" : undefined}
            onChange={(e) => setPassword(e.target.value)}
            disabled={pending}
          />
          <Button
            variant="ghost"
            size="icon-sm"
            aria-pressed={reveal}
            aria-label={reveal ? "Hide password" : "Show password"}
            onClick={() => setReveal((shown) => !shown)}
          >
            {reveal ? <EyeOff strokeWidth={1.5} /> : <Eye strokeWidth={1.5} />}
          </Button>
        </InputGroup>
        {credentialsWrong ? <FieldError id="password-error">{failure.message}</FieldError> : null}
      </Field>

      <Button type="submit" size="lg" className="mt-2 w-full" disabled={pending} aria-busy={pending}>
        {pending ? <Spinner /> : null}
        {mode === Mode.Login ? (pending ? "Signing in" : "Sign in") : pending ? "Creating account" : "Create account"}
      </Button>
    </form>
  );
}
