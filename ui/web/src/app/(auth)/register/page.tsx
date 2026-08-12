"use client";

import Link from "next/link";

import { CredentialsForm } from "@/components/auth/credentials-form";
import { useRedirectWhenSignedIn } from "@/lib/auth/guards";
import { ProviderButtons } from "@/components/auth/provider-buttons";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function RegisterPage() {
  // Somebody who is already signed in has no business on this page; the guard
  // takes them where they were going, without leaving this page behind them.
  useRedirectWhenSignedIn();

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create account</CardTitle>
        <CardDescription>
          Registering signs you in straight away — there is no email to confirm
          first.
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-6">
        <CredentialsForm mode="register" />
        <ProviderButtons />
      </CardContent>

      <CardFooter className="text-muted-foreground">
        Already registered?&nbsp;
        <Link className="underline underline-offset-4" href="/login">
          Sign in
        </Link>
      </CardFooter>
    </Card>
  );
}
