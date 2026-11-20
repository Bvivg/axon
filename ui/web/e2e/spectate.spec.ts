import { randomUUID } from "node:crypto";
import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const cells = ["Top left", "Top center", "Top right", "Middle left", "Middle center", "Middle right", "Bottom left", "Bottom center", "Bottom right"];
const board = (page: Page) => page.getByRole("grid", { name: "Tic-Tac-Toe board", includeHidden: true });
const cell = (page: Page, index: number, mark = "Empty") => board(page).getByRole("button", { name: `${cells[index]}, ${mark}`, exact: true, includeHidden: true });

async function person(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", {
      headers, data: { email: newEmail("watch"), username: newUsername("watch"), displayName: name, password },
    });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  const { tokens } = await registered!.json();
  return {
    context,
    page: await context.newPage(),
    rpc: (method: string, data: object) => context.request.post(gatewayUrl + "/axon.game.v1.GameService/" + method, {
      headers: { ...headers, Authorization: `Bearer ${tokens.accessToken}` }, data,
    }),
  };
}

test("a spectator watches a live game from the lobby and replays it move by move", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const x = await person(browser, origin, "Xavier Spectated");
  const o = await person(browser, origin, "Olivia Spectated");
  const watcher = await person(browser, origin, "Watching Walter");
  try {
    const created = await x.rpc("CreateSession", { game: "tictactoe", requestId: randomUUID() });
    expect(created.status()).toBe(200);
    const id: string = (await created.json()).session.id;
    expect((await o.rpc("JoinSession", { sessionId: id })).status()).toBe(200);
    await x.page.goto(`/games/tic-tac-toe/${id}`);
    await o.page.goto(`/games/tic-tac-toe/${id}`);
    await expect(cell(x.page, 0)).toBeEnabled();

    await watcher.page.goto("/games/tic-tac-toe");
    const live = watcher.page.locator(`[data-live-game="${id}"]`);
    await expect(live).toContainText("Xavier Spectated vs Olivia Spectated");
    await live.click();
    await expect(watcher.page.locator("[data-watching]")).toBeVisible();
    await expect(watcher.page.locator("[data-spectator-status]")).toHaveText("X to move");
    await expect(watcher.page.getByRole("button", { name: "Resign" })).toHaveCount(0);
    await expect(cell(watcher.page, 4)).toBeDisabled();
    await expect(x.page.locator("[data-spectators]")).toHaveText("1 watching");

    for (const [ply, square] of [0, 3, 1, 4, 2].entries()) {
      const mover = ply % 2 === 0 ? x.page : o.page;
      const mark = ply % 2 === 0 ? "X" : "O";
      await cell(mover, square).click();
      await expect(cell(watcher.page, square, mark)).toBeVisible();
    }
    await expect(watcher.page.locator("[data-spectator-status]")).toHaveText("Xavier Spectated won");
    await watcher.page.screenshot({ path: testInfo.outputPath("spectator.png"), animations: "disabled" });

    await watcher.page.getByRole("link", { name: "Replay" }).click();
    await expect(watcher.page).toHaveURL(new RegExp(`/games/tic-tac-toe/${id}/replay$`));
    const ply = watcher.page.locator("[data-replay-ply]");
    await expect(ply).toHaveAttribute("data-replay-ply", "5");
    await expect(watcher.page.locator("[data-replay-result]")).toHaveText("Xavier Spectated won");
    await watcher.page.keyboard.press("Home");
    await expect(ply).toHaveText("Start position");
    await expect(board(watcher.page).getByRole("button", { name: /, Empty$/, includeHidden: true })).toHaveCount(9);
    await watcher.page.keyboard.press("ArrowRight");
    await watcher.page.keyboard.press("ArrowRight");
    await expect(ply).toContainText("Move 2 of 5");
    await expect(cell(watcher.page, 0, "X")).toBeVisible();
    await expect(cell(watcher.page, 3, "O")).toBeVisible();
    await expect(cell(watcher.page, 1)).toBeVisible();
    await watcher.page.getByRole("slider", { name: "Move" }).fill("4");
    await expect(ply).toHaveAttribute("data-replay-ply", "4");
    await expect(cell(watcher.page, 4, "O")).toBeVisible();
    await expect(cell(watcher.page, 2)).toBeVisible();
    await watcher.page.getByRole("button", { name: "Last move" }).click();
    await expect(cell(watcher.page, 2, "X")).toBeVisible();
    await watcher.page.screenshot({ path: testInfo.outputPath("replay.png"), animations: "disabled" });
  } finally {
    await Promise.all([x.context.close(), o.context.close(), watcher.context.close()]);
  }
});
