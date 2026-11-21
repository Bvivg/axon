import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const status = (page: Page) => page.locator("[data-game-status]");
const square = (page: Page, number: string | number) => page.locator(`[data-square="${number}"]`);
const shortWin = "9-14 23-18 14x23 26x19 11-16 30-26 16x23x30 21-17 30x21x14 31-26 10-15 24-19 15x24x31 29-25 12-16 22-17 14x21x30x23 32-27 31x24 28x19x12 5-9".split(" ");

async function player(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  const data = { email: newEmail("checkers"), username: newUsername("checkers"), displayName: name, password };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", { headers, data });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  return { context, page: await context.newPage() };
}

async function play(page: Page, move: string) {
  await expect(status(page)).toContainText("Your move");
  await expect(status(page)).not.toContainText("Connecting");
  for (const step of move.split(/[-x]/)) await square(page, step).click();
  await expect(status(page)).not.toContainText("Your move");
}

test("two players capture, crown and win by blockade, then replay it", async ({ browser, baseURL }) => {
  test.setTimeout(150_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Draughts Alpha");
  const b = await player(browser, origin, "Draughts Bravo");
  const watcher = await player(browser, origin, "Draughts Watcher");
  try {
    await a.page.goto(origin + "/games");
    await a.page.getByRole("navigation", { name: "Game catalogue", exact: true }).getByRole("link", { name: "Checkers lobby", exact: true }).click();
    await expect(a.page).toHaveURL(/\/games\/checkers$/);
    await a.page.getByRole("button", { name: "Start a game", exact: true }).click();
    await a.page.getByRole("button", { name: /Private game/ }).click();
    await expect(a.page).toHaveURL(/\/games\/checkers\/[0-9a-f-]+$/);
    await expect(status(a.page)).toContainText("Waiting for opponent");
    await b.page.goto(a.page.url());
    await b.page.getByRole("button", { name: "Join game" }).click();
    await expect(b.page.getByRole("group", { name: "Checkers board" })).toBeVisible();
    await expect(a.page.locator("[data-square]").first()).toHaveAttribute("data-square", "32");
    await expect(b.page.locator("[data-square]").first()).toHaveAttribute("data-square", "1");
    await expect(a.page.locator("[data-movable]")).toHaveCount(4);
    await expect(square(a.page, "9")).toHaveAccessibleName("9, black man, can move");

    await square(a.page, "9").focus();
    await a.page.keyboard.press("Enter");
    await expect(square(a.page, "14")).toHaveAttribute("data-target", "true");
    await a.page.keyboard.press("ArrowUp");
    await expect(square(a.page, "13")).toBeFocused();
    await a.page.keyboard.press("ArrowLeft");
    await expect(square(a.page, "14")).toBeFocused();
    await a.page.keyboard.press("Enter");
    await expect(status(a.page)).toContainText("Opponent’s move");

    await watcher.page.goto(a.page.url());
    await expect(watcher.page.locator("[data-watching]")).toBeVisible();
    await expect(watcher.page.locator("[data-spectator-status]")).toHaveText("White to move");

    for (let index = 1; index < shortWin.length; index++) {
      await play(index % 2 === 0 ? a.page : b.page, shortWin[index]!);
      if (index === 2) await expect(status(b.page)).toContainText("Capture · Your move");
      if (index === 6) {
        await expect(square(b.page, "30").locator("[data-piece=B]")).toBeVisible();
        await expect(square(watcher.page, "30")).toHaveAccessibleName("30, black king");
      }
    }
    await expect(status(a.page)).toHaveText("You won · No moves left");
    await expect(status(b.page)).toHaveText("You lost · No moves left");
    await expect(watcher.page.locator("[data-spectator-status]")).toHaveText("Draughts Alpha won · No moves left");
    await expect(a.page.locator("[data-pieces-left='9']")).toHaveCount(1);
    await expect(a.page.locator("[data-pieces-left='1']")).toHaveCount(1);
    await expect(a.page.getByTestId("move-list")).toContainText("14x21x30x23");

    await a.page.getByRole("button", { name: "View board" }).click();
    await a.page.getByRole("link", { name: "Replay" }).click();
    await expect(a.page).toHaveURL(/\/games\/checkers\/[0-9a-f-]+\/replay$/);
    const ply = a.page.locator("[data-replay-ply]");
    await expect(ply).toHaveAttribute("data-replay-ply", String(shortWin.length), { timeout: 20_000 });
    await expect(a.page.locator("[data-replay-result]")).toHaveText("Draughts Alpha won");
    await expect(a.page.locator("[data-piece]")).toHaveCount(10);
    await a.page.keyboard.press("Home");
    await expect(ply).toHaveText("Start position");
    await expect(a.page.locator("[data-piece]")).toHaveCount(24);
    await a.page.getByRole("slider", { name: "Move" }).fill("7");
    await expect(square(a.page, "30").locator("[data-piece=B]")).toBeVisible();
  } finally {
    await Promise.all([a.context.close(), b.context.close(), watcher.context.close()]);
  }
});

test("practice: captures are forced and the computer answers", async ({ browser, baseURL }) => {
  test.setTimeout(60_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Draughts Bot");
  try {
    await a.page.goto(origin + "/games/checkers/practice");
    await a.page.getByRole("button", { name: "Two players", exact: true }).click();
    for (const move of ["11-15", "22-18"]) for (const step of move.split("-")) await square(a.page, step).click();
    await expect(a.page.locator("[data-movable]")).toHaveCount(1);
    await expect(square(a.page, "15")).toHaveAttribute("data-movable", "true");
    await square(a.page, "9").click();
    await expect(square(a.page, "13")).not.toHaveAttribute("data-target", "true");
    await square(a.page, "15").click();
    await square(a.page, "22").click();
    await expect(a.page.getByTestId("move-list")).toContainText("15x22");

    await a.page.getByRole("button", { name: "Computer", exact: true }).click();
    await square(a.page, "11").click();
    await square(a.page, "15").click();
    const reply = a.page.getByTestId("move-list").locator("li").first().locator("span").nth(2);
    await expect(reply).not.toHaveText("", { timeout: 15_000 });
    await expect(status(a.page)).toHaveText("Black to move", { timeout: 15_000 });
  } finally {
    await a.context.close();
  }
});
