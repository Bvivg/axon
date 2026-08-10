import { expect, test } from "@playwright/test";

import {
  expectProfile,
  fakeAuthorizeUrl,
  gatewayHost,
  newEmail,
  open,
  password,
  refreshCookie,
  refreshCookieName,
  refreshCookiePath,
  register,
  submitCredentials,
} from "./support";

/**
 * The browser half of stage 1.
 *
 * Every scenario here is one a real person performs, driven through the client
 * the way they would drive it. Nothing reaches around the UI to call the
 * gateway directly: a suite that does that is the Go suite, which already
 * exists and covers the contract. What is left over — and what only a browser
 * can answer — is whether the cookie the gateway sets is one a browser keeps,
 * whether a cross-origin call survives the preflight, and whether a reload
 * finds its way back to a session.
 */

test("registration signs you in and lands on the profile", async ({ page }) => {
  const email = newEmail("register");

  await open(page, "/register");
  await submitCredentials(page, "Create account", email);

  await expectProfile(page);
  // The card is titled with the display name, and none was given — so the
  // address on screen is the one the account was created with, read back from
  // the server rather than echoed from the form.
  await expect(page.getByRole("heading", { name: email })).toBeVisible();
});

test("an existing account signs in with its password", async ({ page }) => {
  const email = newEmail("password");

  await register(page, email);
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);

  await open(page, "/login");
  await submitCredentials(page, "Sign in", email);

  await expectProfile(page);
  await expect(page.getByRole("heading", { name: email })).toBeVisible();
});

test("the fake provider carries the browser through to the profile", async ({
  page,
}) => {
  // Collected from responses rather than from navigations: the provider answers
  // with a 302, which Chromium follows inside a single navigation. Only the
  // response list shows that the browser really went to the auth service.
  const seen: string[] = [];
  page.on("response", (response) => seen.push(response.url()));

  await open(page, "/login");
  await page.getByRole("button", { name: "Continue with the test provider" }).click();

  // No consent screen to click: the fake provider's authorize endpoint is a
  // redirect with the identity baked into the code, so the browser comes
  // straight back to the callback route.
  await expectProfile(page);
  await expect(
    page.getByRole("heading", { name: /@fake\.axon\.test$/ }),
  ).toBeVisible();

  expect(
    seen.some((url) => url.startsWith(fakeAuthorizeUrl)),
    "the browser never reached the provider's authorize endpoint",
  ).toBe(true);
  expect(
    seen.some((url) => url.includes("/auth/callback/fake?")),
    "the provider never returned the browser to the callback route",
  ).toBe(true);
});

test("a reload keeps the session", async ({ page }) => {
  await register(page, newEmail("reload"));

  // The access token died with the previous page: everything below is rebuilt
  // from the refresh cookie alone. This is the single reason nothing has to be
  // stored in JavaScript.
  await page.reload();

  await expectProfile(page);
});

test("the refresh token is out of the page's reach", async ({ page }) => {
  const email = newEmail("cookie");
  await register(page, email);

  // Both halves matter. On its own, "document.cookie has no refresh token" also
  // passes when there is no cookie anywhere — the cookie belongs to the
  // gateway's origin, not the client's, so the page could never see it. What is
  // worth asserting is that the session genuinely rests on a cookie *and* that
  // the cookie is unreachable from script.
  const cookie = await refreshCookie(page);
  expect(cookie, "no refresh cookie was stored").toBeDefined();
  expect(cookie!.httpOnly).toBe(true);
  expect(cookie!.path).toBe(refreshCookiePath);
  // Lax, and therefore only stored at all because the client and the gateway
  // share a registrable domain — the arrangement production has and compose's
  // default hostnames do not. See the header of docker-compose.web-e2e.yml.
  expect(cookie!.sameSite).toBe("Lax");
  // Host-only: no leading dot, so a sibling host cannot read or overwrite it.
  expect(cookie!.domain).toBe(gatewayHost);
  // Off in this stack only because the compose network is plain HTTP and the
  // origin is not localhost. Asserted so the reason stays visible.
  expect(cookie!.secure).toBe(false);

  const visible = await page.evaluate(() => ({
    cookies: document.cookie,
    local: Object.entries(localStorage),
    session: Object.entries(sessionStorage),
  }));

  expect(visible.cookies).toBe("");
  expect(visible.cookies).not.toContain(refreshCookieName);
  expect(visible.cookies).not.toContain(cookie!.value);
  // Nothing is persisted at all: the access token lives in a module variable
  // and dies with the tab, and the OAuth flow removes its own key as it reads
  // it.
  expect(visible.local).toEqual([]);
  expect(visible.session).toEqual([]);
  // The password never lingers in storage either.
  expect(JSON.stringify(visible)).not.toContain(password);
  expect(JSON.stringify(visible)).not.toContain(email);
});

test("signing out puts the profile out of reach", async ({ page }) => {
  await register(page, newEmail("logout"));

  // Asserted before signing out, not only after. Without it this test also
  // passes when the session was never cookie-backed in the first place —
  // which is exactly the failure mode the stack's SameSite and Secure
  // configuration exists to avoid, and it would pass silently.
  expect(await refreshCookie(page), "no session to sign out of").toBeDefined();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);

  // The gateway expires the cookie on the way out, so there is nothing left to
  // restore a session from.
  expect(await refreshCookie(page)).toBeUndefined();

  await open(page, "/profile");

  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
});
