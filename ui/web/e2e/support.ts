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

export function newUsername(label: string): string {
  counter += 1;
  const stem = label.toLowerCase().replace(/[^a-z0-9]+/g, "_").slice(0, 14);
  const salt = Math.random().toString(36).slice(2, 6);
  return `${stem}_${Date.now().toString(36)}${salt}${counter}`;
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
  username?: string,
): Promise<void> {
  await page.getByLabel("Email").fill(email);
  if (action === "Create account") {
    await page.getByLabel("Username").fill(username ?? newUsername("user"));
  }
  if (displayName !== undefined) {
    await page.getByLabel("Display name").fill(displayName);
  }
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: action }).click();
}

export async function expectProfile(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/profile\/my$/);
  await expect(page.getByRole("heading", { name: "Personal info" })).toBeVisible();
}

export async function expectSessions(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/profile\/sessions$/);
  await expect(page.getByRole("heading", { name: "Sessions" })).toBeVisible();
}

export async function expectLobby(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/chat$/);
  await expect(page.getByRole("heading", { name: "Chats", exact: true })).toBeVisible();
}

export async function expectHome(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/chat$/, { timeout: 30_000 });
  await expect(page.getByRole("heading", { name: "Chats", exact: true })).toBeVisible();
}

export async function register(
  page: Page,
  email: string,
  displayName?: string,
  username = newUsername("user"),
): Promise<string> {
  await open(page, "/register");
  await submitCredentials(page, "Create account", email, displayName, username);
  await expectHome(page);
  return username;
}

export async function newPerson(
  browser: Browser,
  label: string,
  displayName: string,
): Promise<{ page: Page; email: string; username: string; close: () => Promise<void> }> {
  const context = await browser.newContext();
  const page = await context.newPage();
  const email = newEmail(label);
  const username = await register(page, email, displayName, newUsername(label));

  return { page, email, username, close: () => context.close() };
}

export async function goToProfile(page: Page): Promise<void> {
  await page.getByRole("navigation", { name: "Primary", exact: true }).getByRole("link", { name: "Profile", exact: true }).click();
  await page.getByRole("navigation", { name: "Profile", exact: true }).getByRole("link", { name: "Personal info", exact: true }).click();
  await expectProfile(page);
  await expect(page.getByRole("menu")).toHaveCount(0);
}

export async function goToSessions(page: Page): Promise<void> {
  await page.getByRole("navigation", { name: "Primary", exact: true }).getByRole("link", { name: "Profile", exact: true }).click();
  await page.getByRole("navigation", { name: "Profile", exact: true }).getByRole("link", { name: "Sessions", exact: true }).click();
  await expectSessions(page);
}

export async function signOut(page: Page): Promise<void> {
  await page.getByRole("navigation", { name: "Primary", exact: true }).getByRole("link", { name: "Profile", exact: true }).click();
  await page.getByRole("navigation", { name: "Profile", exact: true }).getByRole("link", { name: "My profile", exact: true }).click();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
}

export async function refreshCookie(page: Page) {
  const cookies = await page.context().cookies();
  return cookies.find((cookie) => cookie.name === refreshCookieName);
}
