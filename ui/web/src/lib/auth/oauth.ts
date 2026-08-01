import { OauthProvider } from "@/gen/axon/auth/v1/auth_pb";
import { authClient } from "@/lib/connect/clients";

/**
 * providers maps the URL segment a provider returns to onto its enum value.
 *
 * The segment is what the auth service appends to OAUTH_REDIRECT_BASE_URL, so
 * these strings are half of a contract with the backend, not a display concern.
 */
const providers: Record<string, OauthProvider> = {
  google: OauthProvider.GOOGLE,
  github: OauthProvider.GITHUB,
  apple: OauthProvider.APPLE,
  fake: OauthProvider.FAKE,
};

export type ProviderSlug = keyof typeof providers;

/** Human-readable names for the buttons. */
export const providerLabels: Record<string, string> = {
  google: "Google",
  github: "GitHub",
  apple: "Apple",
  fake: "the test provider",
};

/**
 * enabledProviders is which buttons to show.
 *
 * The service decides which providers actually work — one missing half of its
 * credentials counts as absent — and the contract has no way to ask. So this is
 * configured on both sides for now; a provider listed here but not configured
 * there fails at StartOAuth with a clear message rather than silently.
 */
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

/** stateKey is where the pending flow is parked while the browser is away. */
function stateKey(slug: string): string {
  return `axon:oauth:${slug}`;
}

interface PendingFlow {
  state: string;
  returnTo: string;
}

/**
 * Begins a provider sign-in and sends the browser to the provider.
 *
 * `returnTo` is a path within this app. It is sent absolute because the server
 * checks it against an allow-list of exact origins — an open redirect there is
 * how an attacker harvests authorization codes — and a bare path has no origin
 * to check. It is kept relative locally, because that is what the router wants.
 *
 * The state is kept so the callback can check that it belongs to a flow this
 * tab started. The server treats state as single-use and provider-bound
 * already; this check is what stops a callback URL someone pasted into the
 * address bar from reaching the network at all.
 *
 * sessionStorage rather than a variable because the browser leaves the page
 * entirely — and per-tab, so two tabs signing in at once do not overwrite each
 * other's flow.
 */
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

/**
 * Reads back the flow this tab started, removing it as it goes: like the state
 * on the server, it is good for exactly one callback.
 */
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
