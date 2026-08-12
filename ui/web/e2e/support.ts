import { expect, type Browser, type Page } from "@playwright/test";

export const refreshCookieName = "axon_refresh";

export const refreshCookiePath = "/axon.auth.v1.AuthService/";

export const gatewayUrl =
  process.env.GATEWAY_URL ?? "http://gateway.axon.test:8080";

export const gatewayHost = new URL(gatewayUrl).hostname;

export const fakeAuthorizeUrl =
  process.env.OAUTH_FAKE_AUTHORIZE_URL ??
  "http://auth.axon.test:8081/oauth/fake/authorize";

export const password = "correct-horse-battery-staple";

let counter = 0;

export function newEmail(label: string): string {
  counter += 1;
  return `${label}-${Date.now()}-${counter}@axon.test`;
}

export async function open(page: Page, path: string): Promise<void> {
  const restored = page.waitForResponse(
    (response) =>
      response.url().endsWith(`${refreshCookiePath}RefreshToken`) &&
      response.request().method() === "POST",
  );

  await page.goto(path);
  await restored;
}

export async function submitCredentials(
  page: Page,
  action: "Create account" | "Sign in",
  email: string,
  displayName?: string,
): Promise<void> {
  await page.getByLabel("Email").fill(email);
  if (displayName !== undefined) {
    await page.getByLabel("Display name (optional)").fill(displayName);
  }
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: action }).click();
}

export async function expectProfile(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/profile$/);
  await expect(page.getByRole("tab", { name: "My profile" })).toBeVisible();
}

export async function expectLobby(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/chat$/);
  await expect(page.getByRole("heading", { name: "Your rooms" })).toBeVisible();
}

export async function expectHome(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "Games are coming" })).toBeVisible();
}

export async function register(
  page: Page,
  email: string,
  displayName?: string,
): Promise<void> {
  await open(page, "/register");
  await submitCredentials(page, "Create account", email, displayName);
  await expectHome(page);
}

export async function newPerson(
  browser: Browser,
  label: string,
  displayName: string,
): Promise<{ page: Page; close: () => Promise<void> }> {
  const context = await browser.newContext();
  const page = await context.newPage();

  await register(page, newEmail(label), displayName);

  return { page, close: () => context.close() };
}

export async function goToProfile(page: Page): Promise<void> {
  await page.getByRole("link", { name: "Profile" }).click();
  await expectProfile(page);
}

export async function signOut(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
}

export async function refreshCookie(page: Page) {
  const cookies = await page.context().cookies();
  return cookies.find((cookie) => cookie.name === refreshCookieName);
}
