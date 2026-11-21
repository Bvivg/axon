import { randomUUID } from "node:crypto";
import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const status = (page: Page) => page.locator("[data-game-status]");
const square = (page: Page, name: string) => page.locator(`[data-square=${name}]`);
const scholar = [["e2", "e4"], ["e7", "e5"], ["f1", "c4"], ["b8", "c6"], ["d1", "h5"], ["g8", "f6"], ["h5", "f7"]];

async function person(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", {
      headers, data: { email: newEmail("analysis"), username: newUsername("analysis"), displayName: name, password },
    });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  const { tokens, user } = await registered!.json();
  return {
    id: user.id as string,
    context,
    page: await context.newPage(),
    rpc: (method: string, data: object) => context.request.post(gatewayUrl + "/axon.game.v1.GameService/" + method, {
      headers: { ...headers, Authorization: `Bearer ${tokens.accessToken}` }, data,
    }),
  };
}

type Person = Awaited<ReturnType<typeof person>>;

async function quickMatch(a: Person, b: Person) {
  for (const player of [a, b]) expect((await player.rpc("FindMatch", { game: "chess", requestId: randomUUID() })).status()).toBe(200);
  let id = "";
  await expect.poll(async () => {
    const { ticket } = await (await a.rpc("GetMatch", { game: "chess" })).json();
    id = ticket?.status === "MATCH_STATUS_MATCHED" ? ticket.sessionId ?? "" : "";
    return id;
  }, { intervals: [250], timeout: 30_000 }).not.toBe("");
  const { session } = await (await a.rpc("GetSession", { sessionId: id })).json();
  expect(session.rated).toBe(true);
  return { id, white: session.playerIds[0] === a.id ? a : b, black: session.playerIds[0] === a.id ? b : a };
}

async function move(page: Page, from: string, to: string) {
  await expect(status(page)).toContainText("Your move");
  await square(page, from).click();
  await square(page, to).click();
}

test("the chess replay shows the engine evaluation and marks the blunder", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(150_000);
  const origin = new URL(baseURL!).origin;
  const first = await person(browser, origin, "Scholar One");
  const second = await person(browser, origin, "Scholar Two");
  try {
    const { id, white, black } = await quickMatch(first, second);
    await white.page.goto(`/games/chess/${id}`);
    await black.page.goto(`/games/chess/${id}`);
    for (const [ply, [from, to]] of scholar.entries()) await move(ply % 2 === 0 ? white.page : black.page, from, to);
    await expect(status(white.page)).toHaveText("You won · Checkmate");

    const page = black.page;
    const analysis = "**/axon.analysis.v1.AnalysisService/GetAnalysis";
    await page.route(analysis, route => route.fulfill({ status: 404, json: { code: "not_found", message: "this game has no analysis" } }));
    await page.goto(`/games/chess/${id}/replay`);
    const panel = page.locator("[data-analysis]");
    const bar = page.locator("[data-eval-bar]");
    const ply = page.locator("[data-replay-ply]");
    await expect(panel).toHaveAttribute("data-analysis", "pending");
    await expect(panel.getByRole("status")).toHaveText("Analysis pending");
    await expect(bar).toHaveAttribute("data-eval", "pending");
    await page.screenshot({ path: testInfo.outputPath("analysis-pending.png"), animations: "disabled" });

    await page.unroute(analysis);
    await expect(panel).toHaveAttribute("data-analysis", "ready", { timeout: 90_000 });
    await expect(page.locator("[data-eval-chart]")).toHaveAttribute("data-eval-chart", "8");
    await expect(ply).toHaveAttribute("data-replay-ply", "7");
    await expect(bar).toHaveAttribute("data-eval", "1-0");
    await expect(bar).toHaveAccessibleName("Evaluation 1-0");
    const marked = page.getByTestId("move-list").locator("[data-move-mark]");
    await expect(marked.filter({ hasText: "Nf6" })).toHaveText("Nf6??");
    await expect(marked.filter({ hasText: "Nf6" })).toHaveAttribute("data-move-mark", "??");
    await expect(page.locator("[data-chart-mark='??']")).toHaveCount(1);
    await expect(panel.locator("[data-analysis-move]")).toContainText("Qxf7#");

    await page.keyboard.press("Home");
    await expect(ply).toHaveText("Start position");
    for (let step = 0; step < 6; step++) await page.keyboard.press("ArrowRight");
    await expect(ply).toHaveAttribute("data-replay-ply", "6");
    await expect(bar).toHaveAttribute("data-eval", "+M1");
    await expect(panel.locator("[data-analysis-eval]")).toHaveText("+M1");
    await expect(panel.locator("[data-analysis-move]")).toHaveText("Nf6?? · Blunder");
    await expect(panel).toContainText(/Best \S+/);
    await expect(page.getByTestId("move-list").locator("[data-move-mark='??']")).toHaveCount(1);
    await page.screenshot({ path: testInfo.outputPath("analysis-blunder.png"), animations: "disabled" });

    await page.getByRole("button", { name: "Previous move" }).click();
    await expect(page.getByTestId("move-list").locator("[data-move-mark='??']")).toHaveCount(0);
    await expect(bar).not.toHaveAttribute("data-eval", "+M1");
  } finally {
    await Promise.all([first.context.close(), second.context.close()]);
  }
});
