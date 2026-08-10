import { expect, type Page } from "@playwright/test";

/** The cookie the gateway keeps the refresh token in. */
export const refreshCookieName = "axon_refresh";

/**
 * The Connect procedure prefix the cookie is scoped to, so it rides along with
 * sign-in traffic and with nothing else.
 */
export const refreshCookiePath = "/axon.auth.v1.AuthService/";

/**
 * Where the gateway and the fake provider are, as the browser reaches them.
 *
 * Read from the environment rather than written down here: the compose file
 * decides the topology, and a second copy of a hostname is a second thing to
 * keep in step with it.
 */
export const gatewayUrl =
  process.env.GATEWAY_URL ?? "http://gateway.axon.test:8080";

/** The host the refresh cookie belongs to — the gateway's, not the client's. */
export const gatewayHost = new URL(gatewayUrl).hostname;

/** The fake provider's authorize endpoint, on the auth service's own listener. */
export const fakeAuthorizeUrl =
  process.env.OAUTH_FAKE_AUTHORIZE_URL ??
  "http://auth.axon.test:8081/oauth/fake/authorize";

/** Comfortably over the eight bytes the domain asks for. */
export const password = "correct-horse-battery-staple";

let counter = 0;

/**
 * A fresh address per call.
 *
 * Registration is not idempotent and the database survives for the length of a
 * run, so a fixed address would make every test after the first depend on the
 * order they ran in.
 */
export function newEmail(label: string): string {
  counter += 1;
  return `${label}-${Date.now()}-${counter}@axon.test`;
}

/**
 * Opens a page and waits for the session restore to answer.
 *
 * This is a hydration gate, not a convenience. The restore lives in an effect,
 * so its request cannot be in flight until React has hydrated — and until then,
 * typing into an input updates the DOM without reaching any state, and the form
 * submits empty. Waiting for the response is the cheapest honest signal that
 * the page is live.
 *
 * The POST is matched rather than the URL alone: a cross-origin Connect call is
 * preceded by a preflight, and OPTIONS answers on the same address.
 */
export async function open(page: Page, path: string): Promise<void> {
  const restored = page.waitForResponse(
    (response) =>
      response.url().endsWith(`${refreshCookiePath}RefreshToken`) &&
      response.request().method() === "POST",
  );

  await page.goto(path);
  await restored;
}

/** Fills in the credentials form and submits it. */
export async function submitCredentials(
  page: Page,
  action: "Create account" | "Sign in",
  email: string,
): Promise<void> {
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: action }).click();
}

/**
 * Asserts the profile is on screen — the page that only renders once the
 * gateway has accepted the access token on a procedure its policy protects.
 */
export async function expectProfile(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/profile$/);
  await expect(page.getByText("You are signed in.")).toBeVisible();
}

/** Registers a new account and leaves the browser signed in on the profile. */
export async function register(page: Page, email: string): Promise<void> {
  await open(page, "/register");
  await submitCredentials(page, "Create account", email);
  await expectProfile(page);
}

/** Reads the refresh cookie out of the browser, or undefined if there is none. */
export async function refreshCookie(page: Page) {
  const cookies = await page.context().cookies();
  return cookies.find((cookie) => cookie.name === refreshCookieName);
}
