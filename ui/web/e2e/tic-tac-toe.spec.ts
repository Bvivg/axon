import { randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const service = "/axon.game.v1.GameService/";
const cells = ["Top left", "Top center", "Top right", "Middle left", "Middle center", "Middle right", "Bottom left", "Bottom center", "Bottom right"];
const status = (page: Page) => page.locator("[data-game-status]");
const board = (page: Page) => page.getByRole("grid", { name: "Tic-Tac-Toe board" });
const cell = (page: Page, index: number, mark = "Empty") => board(page).getByRole("button", { name: `${cells[index]}, ${mark}`, exact: true });

async function player(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  const data = { email: newEmail("game"), username: newUsername("game"), displayName: name, password };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", { headers, data });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  const { tokens } = await registered!.json();
  const page = await context.newPage();
  const events: string[] = [];
  page.on("websocket", socket => {
    if (!new URL(socket.url()).pathname.endsWith("/ws/game")) return;
    events.push("game socket created");
    socket.on("framesent", () => events.push("game command sent"));
    socket.on("framereceived", () => events.push("game update received"));
    socket.on("socketerror", error => events.push(`game socket error: ${error}`));
    socket.on("close", () => events.push("game socket closed"));
  });
  return {
    context,
    page,
    events,
    rpc: (method: string, data: object) => context.request.post(gatewayUrl + service + method, {
      headers: { ...headers, Authorization: `Bearer ${tokens.accessToken}` }, data,
    }),
  };
}

async function createGame(page: Page) {
  await page.getByRole("button", { name: "Create game", exact: true }).click();
  await expect(page).toHaveURL(/\/games\/tic-tac-toe\/[0-9a-f-]+$/);
  await expect(status(page)).toContainText("Waiting for opponent");
  await expect(cell(page, 0)).toBeDisabled();
  await expect(page.getByRole("button", { name: "Copy invite link" })).toBeVisible();
  return page.url();
}

async function joinGame(guest: Page, url: string) {
  await guest.goto(url);
  await guest.getByRole("button", { name: "Join game" }).click();
  await expect(board(guest)).toBeVisible();
  await expect(status(guest)).not.toContainText("Connecting…");
}

async function play(people: [Page, Page], moves: number[]) {
  for (const [ply, square] of moves.entries()) {
    const mover = people[ply % 2];
    const other = people[(ply + 1) % 2];
    await expect(cell(other, square)).toBeDisabled();
    if (ply === 0) await cell(mover, square).press("Enter");
    else await cell(mover, square).click();
    const mark = ply % 2 === 0 ? "X" : "O";
    await expect(cell(other, square, mark)).toBeVisible();
    await expect(cell(mover, square, mark)).toBeDisabled();
  }
}

test("two players invite, win, reload and draw; a third player cannot join", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Alexandra Montgomery-Willoughby");
  const b = await player(browser, origin, "Cameron Beaumont-Sutherland");
  const outsider = await player(browser, origin, "Third player");
  try {
    const retryID = randomUUID();
    const requests = await Promise.all([a.rpc("CreateSession", { game: "tictactoe", requestId: retryID }), a.rpc("CreateSession", { game: "tictactoe", requestId: retryID })]);
    for (const result of requests) expect(result.status()).toBe(200);
    const ids = await Promise.all(requests.map(async result => (await result.json()).session.id));
    expect(ids[0]).toBe(ids[1]);

    await a.page.goto(origin + "/games");
    await a.page.getByRole("navigation", { name: "Game catalogue", exact: true }).getByRole("link", { name: "Tic-Tac-Toe lobby", exact: true }).click();
    await expect(a.page).toHaveURL(/\/games\/tic-tac-toe$/);
    const first = await createGame(a.page);
    await joinGame(b.page, first);
    const sessionId = first.split("/").at(-1);
    expect((await outsider.rpc("GetSession", { sessionId })).status()).toBe(404);
    expect((await outsider.rpc("JoinSession", { sessionId })).status()).toBe(400);
    await expect(status(a.page)).toContainText("Your turn");
    await expect(status(b.page)).toContainText("Opponent’s turn");
    await cell(a.page, 0).focus();
    await a.page.keyboard.press("ArrowRight");
    await expect(cell(a.page, 1)).toBeFocused();
    await a.page.keyboard.press("ArrowDown");
    await expect(cell(a.page, 4)).toBeFocused();
    await expect(a.page.locator('[data-player="O"]')).toContainText("05:00");
    await expect.poll(async () => a.page.locator('[data-player="X"]').innerText()).not.toContain("05:00");
    await play([a.page, b.page], [0, 3]);
    await a.page.getByRole("button", { name: "Отменить", exact: true }).click();
    await expect(cell(a.page, 3)).toBeDisabled();
    await expect(cell(b.page, 3)).toBeEnabled();
    await cell(b.page, 3).click();
    await expect(cell(a.page, 3, "O")).toBeVisible();
    await b.page.reload();
    await expect(cell(b.page, 0, "X")).toBeVisible();
    await expect(cell(b.page, 3, "O")).toBeVisible();
    await play([a.page, b.page], [1, 4, 2]);
    await expect(status(a.page)).toHaveText("You won");
    await expect(status(b.page)).toHaveText("You lost");
    await b.page.getByRole("button", { name: "Отменить", exact: true }).click();
    await expect(status(a.page)).toHaveText("Your turn");
    await expect(cell(a.page, 2)).toBeEnabled();
    await expect(cell(b.page, 2)).toBeDisabled();
    await expect(b.page.getByRole("button", { name: "Посмотреть поле" })).toHaveCount(0);
    await cell(a.page, 2).click();
    await expect(status(a.page)).toHaveText("You won");
    await expect(status(b.page)).toHaveText("You lost");
    await expect(b.page.getByRole("button", { name: "Посмотреть поле" })).toBeVisible();
    await expect(board(a.page).locator("[data-winning]")).toHaveCount(3);
    await expect(board(a.page).getByRole("button", { disabled: true })).toHaveCount(9);
    await a.page.screenshot({ path: testInfo.outputPath("win-desktop.png"), animations: "disabled" });

    await a.page.getByRole("link", { name: "Return to lobby" }).click();
    const previous = a.page.getByRole("link", { name: /Tic-Tac-Toe · You won/ });
    await expect(previous).toContainText("Cameron Beaumont-Sutherland");
    await expect(previous.locator("time")).toHaveText(/\d/);
    await a.page.screenshot({ path: testInfo.outputPath("game-history.png"), animations: "disabled" });
    await previous.click();
    await a.page.getByRole("button", { name: "Play again" }).click();
    await expect(status(a.page)).toHaveText(/Waiting for opponent/);
    const second = a.page.url();
    expect(second).not.toBe(first);
    await joinGame(b.page, second);
    await play([a.page, b.page], [0, 1, 2, 4, 3, 5, 7, 6, 8]);
    await expect(status(a.page)).toHaveText("Draw");
    await expect(status(b.page)).toHaveText("Draw");
    await expect(board(b.page).locator("[data-winning]")).toHaveCount(0);
    for (const [width, height] of [[320, 568], [390, 844], [844, 390]]) {
      await b.page.setViewportSize({ width, height });
      const bounds = await board(b.page).boundingBox();
      expect(bounds?.width).toBeGreaterThan(132);
      expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    }
    await b.page.setViewportSize({ width: 390, height: 844 });
    await expect(b.page.getByRole("button", { name: "Play again" })).toBeInViewport({ ratio: 1 });
    await b.page.screenshot({ path: testInfo.outputPath("draw-mobile.png"), animations: "disabled" });
    await b.page.getByRole("button", { name: "Отменить", exact: true }).click();
    await expect(status(a.page)).toHaveText("Your turn");
    await expect(cell(a.page, 8)).toBeEnabled();
    await a.page.reload();
    await expect(cell(a.page, 8)).toBeEnabled();
    await cell(a.page, 8).click();
    await expect(status(b.page)).toHaveText("Draw");
  } finally {
    await testInfo.attach("game-sockets", { body: JSON.stringify([a.events, b.events]), contentType: "application/json" });
    await Promise.all([a.context.close(), b.context.close(), outsider.context.close()]);
  }
});

test("practice modes, undo, results, clocks, keyboard and responsive themes", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(90_000);
  const person = await player(browser, new URL(baseURL!).origin, "Practice player");
  const page = person.page;
  try {
    await page.goto("/games/tic-tac-toe");
    await page.getByRole("link", { name: "Компьютер / Игра вдвоём" }).click();
    await expect(page).toHaveURL(/\/practice$/);
    const mode = page.getByRole("combobox", { name: "Режим игры" });
    const undo = page.getByRole("button", { name: "Отменить", exact: true });
    const reset = page.getByRole("button", { name: "Новая партия", exact: true });
    await expect(undo).toBeDisabled();
    await cell(page, 0).click();
    await undo.click();
    await page.waitForTimeout(500);
    await expect(board(page).getByRole("button", { name: /Empty$/ })).toHaveCount(9);
    await expect(undo).toBeDisabled();
    await cell(page, 0).click();
    await expect(board(page).getByRole("button", { name: /, O$/ })).toHaveCount(1);
    await undo.click();
    await expect(board(page).getByRole("button", { name: /Empty$/ })).toHaveCount(9);
    await mode.selectOption("hard");
    await cell(page, 0).click();
    await expect(cell(page, 4, "O")).toBeVisible();
    await expect(cell(page, 0, "X")).toBeVisible();
    await undo.click();
    await expect(board(page).getByRole("button", { name: /Empty$/ })).toHaveCount(9);
    await cell(page, 0).click();
    await mode.selectOption("local");
    await page.waitForTimeout(500);
    await expect(board(page).getByRole("button", { name: /Empty$/ })).toHaveCount(9);
    await cell(page, 0).focus();
    await page.keyboard.press("ArrowRight");
    await expect(cell(page, 1)).toBeFocused();
    for (const [ply, index] of [0, 3, 1, 4, 2].entries()) {
      await cell(page, index).click();
      await expect(cell(page, index, ply % 2 ? "O" : "X")).toBeVisible();
    }
    await expect(status(page)).toHaveText("Победа крестиков");
    await expect(board(page).locator("[data-winning]")).toHaveCount(3);
    await expect(page.getByRole("button", { name: "Посмотреть поле" })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("practice-win-desktop.png"), animations: "disabled" });
    await page.getByRole("button", { name: "Посмотреть поле" }).click();
    await undo.click();
    await expect(cell(page, 2)).toBeEnabled();
    await expect(status(page)).toHaveText("Ход крестиков");
    await reset.click();
    for (const index of [0, 1, 2, 4, 3, 5, 7, 6, 8]) await cell(page, index).click();
    await expect(status(page)).toHaveText("Ничья");
    await page.getByRole("button", { name: "Посмотреть поле" }).click();
    await reset.click();
    await page.clock.install();
    await page.clock.fastForward(300_100);
    await expect(status(page)).toHaveText("Победа ноликов");
    await expect(page.locator('[data-player="X"]')).toContainText("00:00");
    await expect(board(page).getByRole("button", { disabled: true })).toHaveCount(9);
    await page.getByRole("button", { name: "Новая партия", exact: true }).first().click();
    await expect(page.locator('[data-player="X"]')).toContainText("05:00");
    await expect(page.locator('[data-player="O"]')).toContainText("05:00");
    await mode.selectOption("hard");
    await page.clock.fastForward(300_100);
    await expect(status(page)).toHaveText("Поражение");
    await page.getByRole("button", { name: "Новая партия", exact: true }).first().click();
    await mode.selectOption("local");
    await cell(page, 0).click();
    await cell(page, 4).click();
    for (const theme of ["light", "dark"]) {
      await page.evaluate(value => document.documentElement.setAttribute("data-theme", value), theme);
      expect(await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)).toBe(theme);
      await expect(cell(page, 4, "O").locator("svg")).toHaveCSS("color", theme === "dark" ? "rgb(157, 184, 220)" : "rgb(80, 104, 139)");
      await page.mouse.move(0, 0);
      for (const [width, height] of [[1440, 1000], [390, 844], [320, 568], [844, 390]]) {
        await page.setViewportSize({ width, height });
        const bounds = await board(page).boundingBox();
        expect(bounds!.width).toBeGreaterThan(200);
        expect(Math.abs(bounds!.height - bounds!.width)).toBeLessThan(2);
        expect(bounds!.x).toBeGreaterThanOrEqual(0);
        expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        if (width === 390 || width === 1440) await page.screenshot({ path: testInfo.outputPath(`practice-${theme}-${width}.png`), animations: "disabled" });
      }
    }
    await page.reload();
    await expect(mode).toHaveValue("easy");
    await expect(board(page).getByRole("button", { name: /Empty$/ })).toHaveCount(9);
  } finally {
    await person.context.close();
  }
});

function fixtureSQL(id: string, sql: string) {
  const container = process.env.GAME_E2E_POSTGRES_CONTAINER;
  if (!container || !/^axon-postgres-\d+$/.test(container) || !/^[0-9a-f-]{36}$/.test(id)) throw new Error("Requires an Axon dev fixture session");
  return execFileSync("docker", ["exec", container, "sh", "-c", 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -tAc "$1"', "--", sql], { encoding: "utf8" }).trim();
}

function shortenFixtureClock(id: string) {
  fixtureSQL(id, `UPDATE game.sessions SET clock=jsonb_build_object('remaining_ms',jsonb_build_array(1500,1500),'started_at',clock_timestamp()) WHERE id='${id}'; SELECT pg_notify('game_session_changed','${id}');`);
}

test("server clocks survive offline clients, timeout, undo and an empty room", async ({ browser, baseURL }, testInfo) => {
  test.skip(!process.env.GAME_E2E_POSTGRES_CONTAINER, "Requires fixture clock control on the dev PostgreSQL");
  test.setTimeout(90_000);
  const a = await player(browser, new URL(baseURL!).origin, "Clock player X");
  const b = await player(browser, new URL(baseURL!).origin, "Clock player O");
  try {
    await a.page.goto("/games/tic-tac-toe");
    const first = await createGame(a.page);
    await joinGame(b.page, first);
    await expect(cell(a.page, 0)).toBeEnabled();
    await cell(a.page, 0).click();
    await expect(cell(b.page, 0, "X")).toBeVisible();
    await b.context.setOffline(true);
    shortenFixtureClock(first.split("/").at(-1)!);
    await expect(status(a.page)).toHaveText("You won");
    await expect(a.page.getByText("Время вышло", { exact: true })).toBeVisible();
    await expect(a.page.locator('[data-player="O"]')).toContainText("00:00");
    await expect(a.page.locator('[data-player="X"]')).toContainText("00:02");
    await b.context.setOffline(false);
    await b.page.reload();
    await expect(status(b.page)).toHaveText("You lost");
    await expect(b.page.locator('[data-player="O"]')).toContainText("00:00");
    await b.page.getByRole("button", { name: "Отменить", exact: true }).click();
    await expect(status(a.page)).toHaveText("Your turn");
    await expect(status(b.page)).toHaveText("Opponent’s turn");
    await expect(board(a.page).getByRole("button", { name: /Empty$/ })).toHaveCount(9);
    await expect(a.page.getByText("Время вышло", { exact: true })).toHaveCount(0);
    await expect(a.page.locator('[data-player="O"]')).toContainText("05:00");
    await b.page.setViewportSize({ width: 390, height: 844 });
    await b.page.screenshot({ path: testInfo.outputPath("network-clock-undo-mobile.png"), animations: "disabled" });
    await a.page.getByRole("button", { name: "Новая партия", exact: true }).click();
    await expect(status(a.page)).toContainText("Waiting for opponent");
    const second = a.page.url();
    expect(second).not.toBe(first);
    await joinGame(b.page, second);
    const id = second.split("/").at(-1)!;
    shortenFixtureClock(id);
    await Promise.all([a.page.goto("/games/tic-tac-toe"), b.page.goto("/games/tic-tac-toe")]);
    await expect.poll(() => fixtureSQL(id, `SELECT timed_out FROM game.sessions WHERE id='${id}'`)).toBe("t");
    const response = await a.rpc("GetSession", { sessionId: id });
    const { session } = await response.json();
    expect(session.status).toBe("SESSION_STATUS_WON");
    expect(session.timedOut).toBe(true);
    await a.page.goto(second);
    await expect(status(a.page)).toHaveText("You lost");
    await expect(a.page.getByRole("button", { name: "Отменить", exact: true })).toBeDisabled();
  } finally {
    await Promise.all([a.context.close(), b.context.close()]);
  }
});
