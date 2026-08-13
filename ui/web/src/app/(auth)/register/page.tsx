"use client";

import Link from "next/link";

import { CredentialsForm } from "@/components/auth/credentials-form";
import { useRedirectWhenSignedIn } from "@/lib/auth/guards";
import { ProviderButtons } from "@/components/auth/provider-buttons";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default function RegisterPage() {

  useRedirectWhenSignedIn();

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create account</CardTitle>
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
