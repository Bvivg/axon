import { expect, test } from "@playwright/test";

import {
  expectHome,
  fakeAuthorizeUrl,
  gatewayHost,
  goToProfile,
  goToSessions,
  newEmail,
  open,
  password,
  refreshCookie,
  refreshCookieName,
  refreshCookiePath,
  register,
  signOut,
  submitCredentials,
} from "./support";

test("registration signs you in and lands on Chats", async ({ page }) => {
  const email = newEmail("register");

  await open(page, "/register");
  await submitCredentials(page, "Create account", email);

  await expectHome(page);

  await goToProfile(page);
  await expect(page.getByLabel("Email")).toHaveValue(email);
});

test("an existing account signs in with its password", async ({ page }) => {
  const email = newEmail("password");

  await register(page, email);
  await goToProfile(page);
  await signOut(page);

  await open(page, "/login");
  await submitCredentials(page, "Sign in", email);

  await expectHome(page);
  await goToProfile(page);
  await expect(page.getByLabel("Email")).toHaveValue(email);
});

test("the fake provider carries the browser through to Chats", async ({
  page,
}) => {

  const seen: string[] = [];
  page.on("response", (response) => seen.push(response.url()));

  await open(page, "/login");
  await page.getByRole("button", { name: "Continue with the test provider" }).click();

  await expectHome(page);
  await goToProfile(page);
  await expect(page.getByLabel("Email")).toHaveValue(/@fake\.axon\.test$/);

  expect(
    seen.some((url) => url.startsWith(fakeAuthorizeUrl)),
    "the browser never reached the provider's authorize endpoint",
  ).toBe(true);
  expect(
    seen.some((url) => url.includes("/auth/callback/fake?")),
    "the provider never returned the browser to the callback route",
  ).toBe(true);
});

test("a password account connects the test provider and then signs in with it", async ({ page }) => {
  const email = newEmail("connect");
  const subject = `connected-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

  await page.route(`${fakeAuthorizeUrl}**`, (route) => {
    const url = new URL(route.request().url());
    url.searchParams.set("sub", subject);
    return route.continue({ url: url.toString() });
  });

  await register(page, email);
  await goToSessions(page);

  const methods = page.getByRole("region", { name: "Sign-in methods" });
  await methods.getByRole("button", { name: "Connect Test provider" }).click();

  await expect(page).toHaveURL(/\/profile\/sessions$/, { timeout: 30_000 });
  await expect(page.getByRole("region", { name: "Sign-in methods" }).getByText("Connected")).toBeVisible();

  await signOut(page);
  await page.getByRole("button", { name: "Continue with the test provider" }).click();

  await expectHome(page);
  await goToProfile(page);
  await expect(page.getByLabel("Email")).toHaveValue(email);
});

test("a reload keeps the session", async ({ page }) => {
  await register(page, newEmail("reload"));

  await page.reload();

  await expectHome(page);
});

test("the refresh token is out of the page's reach", async ({ page }) => {
  const email = newEmail("cookie");
  const registration = page.waitForResponse(response => response.url().endsWith("/axon.auth.v1.AuthService/Register") && response.ok());
  await register(page, email);
  const { tokens } = await (await registration).json();
  expect(tokens.accessToken).toBeTruthy();
  expect(tokens.refreshToken).toBeFalsy();

  const cookie = await refreshCookie(page);
  expect(cookie, "no refresh cookie was stored").toBeDefined();
  expect(cookie!.httpOnly).toBe(true);
  expect(cookie!.path).toBe(refreshCookiePath);

  expect(cookie!.sameSite).toBe("Lax");

  expect(cookie!.domain).toBe(gatewayHost);

  expect(cookie!.secure).toBe(false);

  const visible = await page.evaluate(() => ({
    cookies: document.cookie,
    local: Object.entries(localStorage),
    session: Object.entries(sessionStorage),
  }));

  expect(visible.cookies).toBe("");
  expect(visible.cookies).not.toContain(refreshCookieName);
  expect(visible.cookies).not.toContain(cookie!.value);

  expect(visible.local.filter(([key]) => key !== "axon.device")).toEqual([]);
  expect(JSON.stringify(visible.local)).not.toContain(cookie!.value);
  expect(JSON.stringify(visible.local)).not.toContain(tokens.accessToken);
  expect(JSON.stringify(visible.session)).not.toContain(cookie!.value);
  expect(JSON.stringify(visible.session)).not.toContain(tokens.accessToken);

  expect(JSON.stringify(visible)).not.toContain(password);
  expect(JSON.stringify(visible)).not.toContain(email);
});

test("signing out puts the profile out of reach", async ({ page }) => {
  await register(page, newEmail("logout"));
  await goToProfile(page);

  expect(await refreshCookie(page), "no session to sign out of").toBeDefined();

  await signOut(page);

  expect(await refreshCookie(page)).toBeUndefined();

  await open(page, "/profile");

  await expect(page).toHaveURL(/\/login\?next=%2Fprofile$/);
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
});

test("the sign-in pages bounce somebody who is already signed in", async ({
  page,
}) => {
  await register(page, newEmail("bounce"));

  await open(page, "/login");
  await expectHome(page);

  await page.goBack();
  await expect(page).not.toHaveURL(/\/login/);

  await open(page, "/register");
  await expectHome(page);
});

test("the root is Chats for a session, and sign-in for none", async ({ page }) => {
  await register(page, newEmail("root"));

  await open(page, "/");
  await expectHome(page);

  await goToProfile(page);
  await signOut(page);

  await open(page, "/");
  await expect(page).toHaveURL(/\/login$/);

  await expect(page).not.toHaveURL(/next=/);
});

test("Apple's form POST becomes a redirect carrying the code and the name", async ({
  request,
}) => {
  const response = await request.post("/auth/apple/callback", {
    form: {
      code: "apple-code-1",
      state: "a-state",
      user: JSON.stringify({ name: { firstName: "Ada", lastName: "Lovelace" } }),
    },
    maxRedirects: 0,
  });

  expect(response.status()).toBe(303);

  const location = new URL(
    response.headers()["location"],
    "http://web.axon.test:3000",
  );
  expect(location.pathname).toBe("/auth/callback/apple");
  expect(location.searchParams.get("code")).toBe("apple-code-1");
  expect(location.searchParams.get("state")).toBe("a-state");
  expect(location.searchParams.get("name")).toBe("Ada Lovelace");
});

test("a later Apple sign-in carries no name, and the redirect says so", async ({
  request,
}) => {
  const response = await request.post("/auth/apple/callback", {
    form: { code: "apple-code-2", state: "a-state" },
    maxRedirects: 0,
  });

  expect(response.status()).toBe(303);

  const location = new URL(
    response.headers()["location"],
    "http://web.axon.test:3000",
  );
  expect(location.searchParams.get("code")).toBe("apple-code-2");
  expect(location.searchParams.has("name")).toBe(false);
});
