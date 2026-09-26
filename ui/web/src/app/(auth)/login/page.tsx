"use client";

import { CredentialsForm, Mode } from "@/components/auth/credentials-form";
import { OrDivider, ProviderButtons } from "@/components/auth/provider-buttons";
import { enabledProviders } from "@/lib/auth/oauth";
import { useRedirectWhenSignedIn } from "@/lib/auth/guards";

export default function LoginPage() {
  useRedirectWhenSignedIn();

  return (
    <>
      <h1 className="text-2xl font-semibold tracking-tight">Sign in to Axon</h1>
      <ProviderButtons />
      {enabledProviders().length > 0 ? <OrDivider /> : null}
      <CredentialsForm mode={Mode.Login} />
    </>
  );
}
