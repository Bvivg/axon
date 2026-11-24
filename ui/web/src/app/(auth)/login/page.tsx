"use client";

import { Suspense } from "react";

import { AuthSwitch } from "@/components/auth/auth-switch";
import { CredentialsForm, Mode } from "@/components/auth/credentials-form";
import { OrDivider, ProviderButtons } from "@/components/auth/provider-buttons";
import { enabledProviders } from "@/lib/auth/oauth";
import { useRedirectWhenSignedIn } from "@/lib/auth/guards";

export default function LoginPage() {
  useRedirectWhenSignedIn();

  return (
    <>
      <h1>Sign in to Axon</h1>
      <CredentialsForm mode={Mode.Login} />
      {enabledProviders().length > 0 ? <OrDivider /> : null}
      <ProviderButtons />
      <Suspense><AuthSwitch prompt="Don't have an account?" href="/register" label="Create one" /></Suspense>
    </>
  );
}
