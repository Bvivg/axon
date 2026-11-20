import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const cells = ["Top left", "Top center", "Top right", "Middle left", "Middle center", "Middle right", "Bottom left", "Bottom center", "Bottom right"];
const board = (page: Page) => page.getByRole("grid", { name: "Tic-Tac-Toe board", includeHidden: true });
const cell = (page: Page, index: number, mark = "Empty") => board(page).getByRole("button", { name: `${cells[index]}, ${mark}`, exact: true, includeHidden: true });
const status = (page: Page) => page.locator("[data-game-status]");

async function person(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", {
      headers, data: { email: newEmail("tournament"), username: newUsername("tournament"), displayName: name, password },
    });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  return { context, page: await context.newPage() };
}

test("a two-player bracket is created, filled, started and shows the winner after the game", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(180_000);
  const origin = new URL(baseURL!).origin;
  const host = await person(browser, origin, "Bracket Host");
  const guest = await person(browser, origin, "Bracket Guest");
  const viewer = await person(browser, origin, "Bracket Viewer");
  try {
    const name = `Friday cup ${Date.now().toString(36)}`;
    await host.page.goto("/games");
    await host.page.getByRole("navigation", { name: "Competitions" }).getByRole("link", { name: /Tournaments/ }).click();
    await expect(host.page).toHaveURL(/\/games\/tournaments$/);
    await expect(host.page.getByRole("heading", { name: "Tournaments", level: 1 })).toBeVisible();

    await host.page.getByRole("button", { name: "New tournament" }).click();
    const dialog = host.page.getByRole("dialog");
    await dialog.getByLabel("Name").fill(name);
    await dialog.getByRole("group", { name: "Game" }).getByRole("button", { name: "Tic-Tac-Toe" }).click();
    await dialog.getByRole("group", { name: "Format" }).getByRole("button", { name: "Single elimination" }).click();
    await dialog.getByRole("group", { name: "Players" }).getByRole("button", { name: "4", exact: true }).click();
    await expect(dialog.getByRole("group", { name: "Players" }).getByRole("button", { name: "4", exact: true })).toHaveAttribute("aria-pressed", "true");
    await dialog.getByRole("button", { name: "Create" }).click();
    await expect(host.page).toHaveURL(/\/games\/tournaments\/[0-9a-f-]+$/);
    const url = host.page.url();
    await expect(host.page.getByRole("heading", { name, level: 1 })).toBeVisible();
    await expect(host.page.locator("[data-tournament-status]").first()).toHaveAttribute("data-tournament-status", "registration");
    await expect(host.page.getByRole("button", { name: "Start" })).toBeDisabled();

    await host.page.getByRole("button", { name: "Register" }).click();
    await expect(host.page.locator("[data-player-count]")).toHaveAttribute("data-player-count", "1");
    await expect(host.page.getByRole("list", { name: "Registered players" })).toContainText("Bracket Host");

    await guest.page.goto("/games/tournaments");
    await guest.page.getByRole("navigation", { name: "Tournaments" }).getByRole("link", { name: new RegExp(name) }).click();
    await expect(guest.page).toHaveURL(url);
    await expect(guest.page.getByRole("button", { name: "Start" })).toHaveCount(0);
    await guest.page.getByRole("button", { name: "Register" }).click();
    await expect(guest.page.getByRole("button", { name: "Withdraw" })).toBeVisible();
    await expect(host.page.locator("[data-player-count]")).toHaveAttribute("data-player-count", "2");

    await host.page.getByRole("button", { name: "Start" }).click();
    await expect(host.page.locator("[data-tournament-status]").first()).toHaveAttribute("data-tournament-status", "running", { timeout: 30_000 });
    const final = host.page.getByRole("list", { name: "Bracket" }).getByRole("listitem", { name: "Final" });
    const match = final.getByRole("article", { name: "Board 1: Bracket Host vs Bracket Guest" });
    await expect(match).toHaveAttribute("data-match-result", "pending");
    await expect(match.getByRole("link", { name: "Open game" })).toBeVisible({ timeout: 60_000 });
    await expect(host.page.getByRole("table", { name: "Standings" })).toBeVisible();
    await host.page.screenshot({ path: testInfo.outputPath("tournament-running.png"), animations: "disabled" });

    await viewer.page.goto(url);
    const watched = viewer.page.getByRole("article", { name: "Board 1: Bracket Host vs Bracket Guest" });
    await expect(watched).toHaveAttribute("data-match-result", "pending");
    await expect(viewer.page.getByRole("button", { name: "Register" })).toHaveCount(0);

    await match.getByRole("link", { name: "Open game" }).click();
    await expect(host.page).toHaveURL(/\/games\/tic-tac-toe\/[0-9a-f-]+$/);
    await guest.page.goto(host.page.url());
    await expect(board(host.page)).toBeVisible({ timeout: 30_000 });
    await expect(board(guest.page)).toBeVisible({ timeout: 30_000 });
    const people = [host.page, guest.page];
    for (const [ply, square] of [0, 3, 1, 4, 2].entries()) {
      const mover = people[ply % 2];
      const other = people[(ply + 1) % 2];
      await expect(cell(mover, square)).toBeEnabled({ timeout: 20_000 });
      await cell(mover, square).click();
      await expect(cell(other, square, ply % 2 === 0 ? "X" : "O")).toBeVisible();
    }
    await expect(status(host.page)).toHaveText("You won");
    await expect(status(guest.page)).toHaveText("You lost");

    await expect(watched).toHaveAttribute("data-match-result", "win", { timeout: 60_000 });
    await expect(watched.getByLabel("Winner")).toBeVisible();
    await expect(watched).toContainText("Played");
    await expect(watched.getByRole("link", { name: "View game" })).toBeVisible();
    await expect(viewer.page.locator("[data-tournament-status]").first()).toHaveAttribute("data-tournament-status", "finished");
    await expect(viewer.page.locator("[data-tournament-winner]")).toHaveText("Bracket Host");
    const standings = viewer.page.getByRole("table", { name: "Standings" });
    await expect(standings.locator("[data-standing-rank='1']")).toContainText("Bracket Host");
    await expect(standings.locator("[data-standing-rank='1']")).toContainText("Champion");
    await expect(standings.locator("[data-standing-rank='2']")).toContainText("Bracket Guest");
    await viewer.page.screenshot({ path: testInfo.outputPath("tournament-finished.png"), animations: "disabled", fullPage: true });

    await host.page.goto("/games/tournaments");
    const row = host.page.getByRole("navigation", { name: "Tournaments" }).getByRole("link", { name: new RegExp(name) });
    await expect(row.locator("[data-tournament-status]")).toHaveAttribute("data-tournament-status", "finished");
  } finally {
    await Promise.all([host.context.close(), guest.context.close(), viewer.context.close()]);
  }
});
