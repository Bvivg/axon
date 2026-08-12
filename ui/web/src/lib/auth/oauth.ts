import { OauthProvider } from "@/gen/axon/auth/v1/auth_pb";
import { authClient } from "@/lib/connect/clients";

const providers: Record<string, OauthProvider> = {
  google: OauthProvider.GOOGLE,
  github: OauthProvider.GITHUB,
  apple: OauthProvider.APPLE,
  fake: OauthProvider.FAKE,
};

export type ProviderSlug = keyof typeof providers;

export const providerLabels: Record<string, string> = {
  google: "Google",
  github: "GitHub",
  apple: "Apple",
  fake: "the test provider",
};

export function enabledProviders(): string[] {
  const configured = process.env.NEXT_PUBLIC_OAUTH_PROVIDERS ?? "fake";
  return configured
    .split(",")
    .map((name) => name.trim().toLowerCase())
    .filter((name) => name in providers);
}

export function toProvider(slug: string): OauthProvider | undefined {
  return providers[slug.toLowerCase()];
}

function stateKey(slug: string): string {
  return `axon:oauth:${slug}`;
}

interface PendingFlow {
  state: string;
  returnTo: string;
}

export async function startOAuth(slug: string, returnTo: string): Promise<void> {
  const provider = toProvider(slug);
  if (provider === undefined) {
    throw new Error(`unknown provider: ${slug}`);
  }

  const started = await authClient.startOAuth({
    provider,
    returnTo: new URL(returnTo, window.location.origin).toString(),
  });

  const pending: PendingFlow = { state: started.state, returnTo };
  sessionStorage.setItem(stateKey(slug), JSON.stringify(pending));

  window.location.assign(started.authorizationUrl);
}

export function takePendingFlow(slug: string): PendingFlow | null {
  const raw = sessionStorage.getItem(stateKey(slug));
  sessionStorage.removeItem(stateKey(slug));

  if (!raw) {
    return null;
  }

  try {
    return JSON.parse(raw) as PendingFlow;
  } catch {
    return null;
  }
}
