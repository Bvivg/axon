"use client";

import { Suspense } from "react";

import { AuthSwitch } from "@/components/auth/auth-switch";
import { CredentialsForm, Mode } from "@/components/auth/credentials-form";
import { OrDivider, ProviderButtons } from "@/components/auth/provider-buttons";
import { enabledProviders } from "@/lib/auth/oauth";
import { useRedirectWhenSignedIn } from "@/lib/auth/guards";

export default function RegisterPage() {
  useRedirectWhenSignedIn();

  return (
    <>
      <h1>Create your account</h1>
      <CredentialsForm mode={Mode.Register} />
      {enabledProviders().length > 0 ? <OrDivider /> : null}
      <ProviderButtons />
      <Suspense><AuthSwitch prompt="Already have an account?" href="/login" label="Sign in" /></Suspense>
    </>
  );
}
