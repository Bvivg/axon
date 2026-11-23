import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const status = (page: Page) => page.locator("[data-game-status]");

async function person(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  const data = { email: newEmail("table"), username: newUsername("table"), displayName: name, password };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", { headers, data });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  const page = await context.newPage();
  return { context, page };
}

async function openDialog(page: Page, origin: string) {
  await page.goto(origin + "/games/durak");
  await page.getByRole("button", { name: "Start a game", exact: true }).click();
  return page.getByRole("dialog");
}

async function choose(dialog: ReturnType<Page["getByRole"]>, group: string, option: string) {
  await dialog.getByRole("group", { name: group, exact: true }).getByRole("button", { name: option, exact: true }).click();
}

async function playOneMove(page: Page) {
  await expect(status(page)).toContainText("Your move", { timeout: 30_000 });
  const moves = page.getByTestId("move-list").locator("li");
  const before = await moves.count().catch(() => 0);
  const legal = page.locator("[data-hand-card][data-legal]");
  if (await legal.count()) await legal.first().click({ position: { x: 8, y: 24 } });
  else if (await page.getByRole("button", { name: "Take", exact: true }).count()) await page.getByRole("button", { name: "Take", exact: true }).click();
  else await page.getByRole("button", { name: "Pass", exact: true }).click();
  await expect(moves).not.toHaveCount(before);
}

test("a three-seat transfer table with bots finishes after the player leaves", async ({ browser, baseURL }) => {
  test.setTimeout(300_000);
  const origin = new URL(baseURL!).origin;
  const host = await person(browser, origin, "Table Host");
  try {
    const { page } = host;
    const dialog = await openDialog(page, origin);
    await choose(dialog, "Players", "3");
    await choose(dialog, "Mode", "Transfer");
    await choose(dialog, "Bots", "2");
    await expect(dialog.getByRole("button", { name: /Quick match/ })).toBeDisabled();
    await dialog.getByRole("button", { name: /Play the bots/ }).click();
    await expect(page).toHaveURL(/\/games\/durak\/[0-9a-f-]+$/);
    const table = page.getByRole("group", { name: "Durak table" });
    await expect(table).toHaveAttribute("data-seats", "3");
    await expect(page.locator("[data-seat-label]")).toHaveCount(2);
    await expect(page.locator("[data-seat-label]").first()).toContainText("Bot");
    await expect(page.getByTestId("durak-hand").locator("[data-hand-card]")).not.toHaveCount(0);
    await expect(page.locator("[data-table-settings]")).toHaveText("3 players · transfer · 36 cards");
    await expect(page.getByTestId("durak-seat-1")).toHaveAttribute("data-hidden", "true");
    await expect(page.getByTestId("durak-seat-1").locator("[data-card]")).toHaveCount(0);

    await playOneMove(page);
    await page.getByRole("button", { name: "Leave table" }).click();
    await page.getByRole("button", { name: "Leave", exact: true }).click();
    await expect(status(page)).toContainText(/You left|You are the durak/);
    await expect(status(page)).toContainText("You are the durak", { timeout: 240_000 });
    await expect(page.locator("[data-place]")).toHaveCount(2);
    await expect(page.getByRole("button", { name: "View board" })).toBeVisible();
  } finally {
    await host.context.close();
  }
});

test("a guest joins an open four-seat table from the lobby and the host fills it with bots", async ({ browser, baseURL }) => {
  test.setTimeout(120_000);
  const origin = new URL(baseURL!).origin;
  const host = await person(browser, origin, "Room Host");
  const guest = await person(browser, origin, "Room Guest");
  try {
    const dialog = await openDialog(host.page, origin);
    await choose(dialog, "Players", "5");
    await expect(dialog.getByRole("group", { name: "Deck", exact: true }).getByRole("button", { name: "24", exact: true })).toBeDisabled();
    await choose(dialog, "Players", "4");
    await dialog.getByRole("checkbox", { name: "Cheating allowed (unrated)" }).check();
    await dialog.getByRole("button", { name: /Private game/ }).click();
    await expect(host.page).toHaveURL(/\/games\/durak\/[0-9a-f-]+$/);
    const id = host.page.url().split("/").pop()!;
    await expect(status(host.page)).toHaveText("Waiting for players · 1 of 4");
    await expect(host.page.getByTestId("durak-seats").locator("[data-taken]")).toHaveCount(1);

    await guest.page.goto(origin + "/games/durak");
    await guest.page.locator(`[data-open-table="${id}"]`).click();
    await expect(guest.page).toHaveURL(new RegExp(`/games/durak/${id}$`));
    await guest.page.getByRole("button", { name: "Join game" }).click();
    await expect(status(guest.page)).toHaveText("Waiting for players · 2 of 4");
    await expect(host.page.getByTestId("durak-seats").locator("[data-taken]")).toHaveCount(2);
    await expect(guest.page.getByRole("button", { name: "Fill with bots" })).toHaveCount(0);

    await host.page.getByRole("button", { name: "Fill with bots" }).click();
    for (const page of [host.page, guest.page]) {
      await expect(page.getByRole("group", { name: "Durak table" })).toHaveAttribute("data-seats", "4");
      await expect(page.getByTestId("durak-hand").locator("[data-hand-card]")).toHaveCount(6);
      await expect(page.locator("[data-seat-label]")).toHaveCount(3);
      await expect(page.locator("[data-table-settings]")).toHaveText("4 players · throw-in · 36 cards · cheating");
    }
    await expect(host.page.locator("[data-seat-label]").filter({ hasText: "Bot" })).toHaveCount(2);
    await expect.poll(async () => {
      const [a, b] = await Promise.all([status(host.page).textContent(), status(guest.page).textContent()]);
      return [a, b].some(text => text?.startsWith("Your move")) || await host.page.getByTestId("move-list").locator("li").count() > 0;
    }, { timeout: 20_000 }).toBe(true);
  } finally {
    await host.context.close();
    await guest.context.close();
  }
});
