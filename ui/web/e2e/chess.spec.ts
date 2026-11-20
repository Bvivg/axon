import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const status = (page: Page) => page.locator("[data-game-status]");
const square = (page: Page, name: string) => page.locator(`[data-square=${name}]`);

async function player(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  const data = { email: newEmail("chess"), username: newUsername("chess"), displayName: name, password };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", { headers, data });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  return { context, page: await context.newPage() };
}

async function move(page: Page, from: string, to: string) {
  await expect(status(page)).toContainText("Your move");
  await square(page, from).click();
  await square(page, to).click();
}

test("two players invite each other and finish with checkmate", async ({ browser, baseURL }) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Chess Alpha");
  const b = await player(browser, origin, "Chess Bravo");
  try {
    await a.page.goto(origin + "/games");
    await a.page.getByRole("navigation", { name: "Game catalogue", exact: true }).getByRole("link", { name: "Chess lobby", exact: true }).click();
    await expect(a.page).toHaveURL(/\/games\/chess$/);
    await a.page.getByRole("button", { name: "Start a game", exact: true }).click();
    await a.page.getByRole("button", { name: /Private game/ }).click();
    await expect(a.page).toHaveURL(/\/games\/chess\/[0-9a-f-]+$/);
    await expect(status(a.page)).toContainText("Waiting for opponent");
    await b.page.goto(a.page.url());
    await b.page.getByRole("button", { name: "Join game" }).click();
    await expect(b.page.getByRole("grid", { name: "Chess board" })).toBeVisible();
    await expect(b.page.locator("[role=grid] [role=row]").first().locator("button").first()).toHaveAttribute("data-square", "h1");
    await move(a.page, "f2", "f3");
    await move(b.page, "e7", "e5");
    await move(a.page, "g2", "g4");
    await expect(status(b.page)).toContainText("Your move");
    const from = (await square(b.page, "d8").boundingBox())!;
    const to = (await square(b.page, "h4").boundingBox())!;
    await b.page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
    await b.page.mouse.down();
    await b.page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
    await b.page.mouse.up();
    await expect(status(a.page)).toHaveText("You lost · Checkmate");
    await expect(status(b.page)).toHaveText("You won · Checkmate");
    await expect(a.page.getByTestId("move-list")).toContainText("Qh4#");
    await expect(b.page.locator("[data-rating-delta]")).toHaveText("Checkmate");
  } finally {
    await a.context.close();
    await b.context.close();
  }
});

test("quick match pairs two chess players who agree a draw", async ({ browser, baseURL }) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Draw Alpha");
  const b = await player(browser, origin, "Draw Bravo");
  try {
    for (const person of [a, b]) {
      await person.page.goto(origin + "/games/chess");
      await expect(person.page.getByTestId("my-rating")).toHaveText("1200");
      await person.page.getByRole("button", { name: "Start a game", exact: true }).click();
      await person.page.getByRole("button", { name: /Quick match/ }).click();
    }
    await expect(a.page).toHaveURL(/\/games\/chess\/[0-9a-f-]+$/, { timeout: 20_000 });
    await expect(b.page).toHaveURL(a.page.url(), { timeout: 20_000 });
    await expect(a.page.getByRole("button", { name: "Undo move", exact: true })).toHaveCount(0);
    const [white, black] = (await a.page.locator("[role=grid] [role=row]").first().locator("button").first().getAttribute("data-square")) === "a8" ? [a, b] : [b, a];
    await move(white.page, "e2", "e4");
    await expect(status(black.page)).toContainText("Your move");
    await black.page.getByRole("button", { name: "Offer draw", exact: true }).click();
    await expect(black.page.getByRole("button", { name: "Draw offered", exact: true })).toBeDisabled();
    await white.page.getByRole("button", { name: "Accept draw", exact: true }).click();
    await expect(status(white.page)).toHaveText("Draw · Draw agreed");
    await expect(white.page.locator("[data-rating-delta]")).toHaveText("Draw agreed · Rating ±0");
    await white.page.getByRole("button", { name: "View board" }).click();
    await white.page.getByRole("link", { name: "Back to lobby" }).first().click();
    await expect(white.page.getByTestId("my-rating")).toHaveText("1200");
    await expect(white.page.locator("#game-history")).toContainText("Draw");
    await expect(white.page.locator("#game-history")).toContainText("±0");
  } finally {
    await a.context.close();
    await b.context.close();
  }
});

test("practice: the computer answers and a pawn promotes through the piece picker", async ({ browser, baseURL }) => {
  test.setTimeout(60_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Practice Alpha");
  try {
    await a.page.goto(origin + "/games/chess/practice");
    await square(a.page, "e2").click();
    await square(a.page, "e4").click();
    await expect(a.page.getByTestId("move-list").locator("li").first().locator("span").nth(2)).not.toHaveText("", { timeout: 15_000 });
    await a.page.getByRole("button", { name: "Two players", exact: true }).click();
    await expect(a.page.getByText("No moves yet")).toBeVisible();
    for (const [from, to] of [["h2", "h4"], ["g7", "g5"], ["h4", "g5"], ["g8", "f6"], ["g5", "f6"], ["a7", "a6"], ["f6", "e7"], ["a6", "a5"]]) {
      await square(a.page, from!).click();
      await square(a.page, to!).click();
    }
    await square(a.page, "e7").click();
    await square(a.page, "d8").click();
    await a.page.getByRole("button", { name: "Promote to knight", exact: true }).click();
    await expect(square(a.page, "d8")).toHaveAttribute("aria-label", "d8, white knight");
    await expect(a.page.getByTestId("move-list")).toContainText("exd8=N");
  } finally {
    await a.context.close();
  }
});
