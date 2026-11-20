import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const status = (page: Page) => page.locator("[data-game-status]");

async function player(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  const data = { email: newEmail("quoridor"), username: newUsername("quoridor"), displayName: name, password };
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
  if (move.length === 3) await page.locator(`[data-wall=${move}]`).first().click();
  else await page.locator(`[data-cell=${move}]`).click();
}

test("two players race across the board around walls", async ({ browser, baseURL }) => {
  test.setTimeout(120_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Wall Alpha");
  const b = await player(browser, origin, "Wall Bravo");
  try {
    await a.page.goto(origin + "/games");
    await a.page.getByRole("navigation", { name: "Game catalogue", exact: true }).getByRole("link", { name: "Quoridor lobby", exact: true }).click();
    await expect(a.page).toHaveURL(/\/games\/quoridor$/);
    await a.page.getByRole("button", { name: "Start a game", exact: true }).click();
    await a.page.getByRole("button", { name: /Private game/ }).click();
    await expect(a.page).toHaveURL(/\/games\/quoridor\/[0-9a-f-]+$/);
    await expect(status(a.page)).toContainText("Waiting for opponent");
    await b.page.goto(a.page.url());
    await b.page.getByRole("button", { name: "Join game" }).click();
    await expect(b.page.getByRole("group", { name: "Quoridor board" })).toBeVisible();
    await expect(b.page.locator("[data-cell]").first()).toHaveAttribute("data-cell", "i1");
    await expect(a.page.locator("[data-cell]").first()).toHaveAttribute("data-cell", "a9");
    const white = ["h5v", "e2", "e3", "e4", "e5", "e6", "e7", "e8", "d9"];
    const black = ["a7h", "c7h", "g7h", "a3h", "c3h", "g3h", "a5h", "c5h"];
    for (let index = 0; index < white.length; index++) {
      await play(a.page, white[index]!);
      if (black[index]) await play(b.page, black[index]!);
    }
    await expect(status(a.page)).toHaveText("You won · Reached the far side");
    await expect(status(b.page)).toHaveText("You lost · Reached the far side");
    await expect(b.page.locator("[data-placed-wall]")).toHaveCount(9);
    await expect(a.page.locator("[data-walls-left='2']")).toHaveCount(1);
    await expect(a.page.locator("[data-walls-left='9']")).toHaveCount(1);
    await expect(a.page.getByTestId("move-list")).toContainText("h5v");
  } finally {
    await a.context.close();
    await b.context.close();
  }
});

test("a wall that would seal the path is refused", async ({ browser, baseURL }) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Seal Alpha");
  try {
    await a.page.goto(origin + "/games/quoridor/practice");
    await a.page.getByRole("button", { name: "Two players", exact: true }).click();
    for (const move of ["a1h", "a8h", "c1h", "c8h", "e1h", "e8h", "g1h"]) await a.page.locator(`[data-wall=${move}]`).first().click();
    await expect(a.page.locator("[data-placed-wall]")).toHaveCount(7);
    await expect(a.page.locator("[data-wall=h1v]").first()).toBeDisabled();
    await expect(a.page.locator("[data-wall=h2v]").first()).toBeEnabled();
  } finally {
    await a.context.close();
  }
});

test("quick match pairs two players and resignation moves the rating", async ({ browser, baseURL }) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Quick Alpha");
  const b = await player(browser, origin, "Quick Bravo");
  try {
    for (const person of [a, b]) {
      await person.page.goto(origin + "/games/quoridor");
      await expect(person.page.getByTestId("my-rating")).toHaveText("1200");
      await person.page.getByRole("button", { name: "Start a game", exact: true }).click();
      await person.page.getByRole("button", { name: /Quick match/ }).click();
    }
    await expect(a.page).toHaveURL(/\/games\/quoridor\/[0-9a-f-]+$/, { timeout: 20_000 });
    await expect(b.page).toHaveURL(a.page.url(), { timeout: 20_000 });
    const aFirst = (await a.page.locator("[data-cell]").first().getAttribute("data-cell")) === "a9";
    const [first, second] = aFirst ? [a, b] : [b, a];
    await play(first.page, "e2");
    await expect(status(second.page)).toContainText("Your move");
    await second.page.getByRole("button", { name: "Resign", exact: true }).click();
    await second.page.getByRole("button", { name: "Resign", exact: true }).click();
    await expect(status(first.page)).toHaveText("You won · Resignation");
    await expect(second.page.locator("[data-rating-delta]")).toHaveText("Resignation · Rating −20");
    await second.page.getByRole("button", { name: "View board" }).click();
    await second.page.getByRole("link", { name: "Back to lobby" }).first().click();
    await expect(second.page.getByTestId("my-rating")).toHaveText("1180");
  } finally {
    await a.context.close();
    await b.context.close();
  }
});

test("practice: the computer answers moves and walls", async ({ browser, baseURL }) => {
  test.setTimeout(60_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Bot Alpha");
  try {
    await a.page.goto(origin + "/games/quoridor/practice");
    await a.page.locator("[data-cell=e2]").click();
    const reply = a.page.getByTestId("move-list").locator("li").first().locator("span").nth(2);
    await expect(reply).not.toHaveText("", { timeout: 15_000 });
    await expect(status(a.page)).toHaveText("Violet to move", { timeout: 15_000 });
    await a.page.locator("[data-wall=a1h]").first().click();
    await expect(a.page.locator("[data-placed-wall=a1h]")).toHaveCount(1);
    await expect(a.page.getByTestId("move-list").locator("li").nth(1).locator("span").nth(2)).not.toHaveText("", { timeout: 15_000 });
  } finally {
    await a.context.close();
  }
});
