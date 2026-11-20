import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const service = "/axon.game.v1.GameService/";
const cells = ["Top left", "Top center", "Top right", "Middle left", "Middle center", "Middle right", "Bottom left", "Bottom center", "Bottom right"];
const status = (page: Page) => page.locator("[data-game-status]");
const board = (page: Page) => page.getByRole("grid", { name: "Tic-Tac-Toe board", includeHidden: true });
const cell = (page: Page, index: number, mark = "Empty") => board(page).getByRole("button", { name: `${cells[index]}, ${mark}`, exact: true, includeHidden: true });

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
  await page.getByRole("button", { name: "Start a game", exact: true }).click();
  await page.getByRole("button", { name: /Private game/ }).click();
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
    await expect(a.page.locator('[data-player="O"]')).toContainText("5:00");
    await expect.poll(async () => a.page.locator('[data-player="X"]').innerText()).not.toContain("5:00");
    await play([a.page, b.page], [0, 3]);
    await a.page.getByRole("button", { name: "Undo move", exact: true }).click();
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
    await a.page.getByRole("button", { name: "View board" }).click();
    await expect(a.page.getByRole("button", { name: "Undo move", exact: true })).toBeDisabled();
    await expect(b.page.getByRole("button", { name: "View board" })).toBeVisible();
    await expect(board(a.page).locator("[data-winning]")).toHaveCount(3);
    await expect(board(a.page).getByRole("button", { disabled: true, includeHidden: true })).toHaveCount(9);
    await a.page.screenshot({ path: testInfo.outputPath("win-desktop.png"), animations: "disabled" });

    await a.page.getByRole("link", { name: "Back to lobby" }).click();
    const previous = a.page.getByRole("link", { name: /Cameron Beaumont-Sutherland · You won/ });
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
    await b.page.getByRole("button", { name: "View board" }).click();
    await expect(b.page.getByRole("button", { name: "Undo move", exact: true })).toBeDisabled();
    await a.page.reload();
    await expect(status(a.page)).toHaveText("Draw");
  } finally {
    await testInfo.attach("game-sockets", { body: JSON.stringify([a.events, b.events]), contentType: "application/json" });
    await Promise.all([a.context.close(), b.context.close(), outsider.context.close()]);
  }
});

test("practice settings, clocks, undo, keyboard, results and responsive themes", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(120_000);
  const person = await player(browser, new URL(baseURL!).origin, "Practice player");
  const page = person.page;
  const undo = page.getByRole("button", { name: "Undo move", exact: true });
  const reset = page.getByRole("button", { name: "New game", exact: true });
  const settings = () => page.getByRole("button", { name: "Settings", exact: true }).click();
  const radio = (group: string, name: string) => page.getByRole("group", { name: group, exact: true }).getByRole("radio", { name, exact: true });
  const close = async () => { await page.getByRole("dialog").getByRole("button", { name: "Close", exact: true }).first().click(); await expect(page.getByRole("dialog")).toHaveCount(0); };
  try {
    await page.goto("/games/tic-tac-toe/practice");
    await expect(board(page)).toBeVisible();
    await page.clock.install();
    await page.clock.fastForward(60000);
    await expect(page.locator('[data-player="X"] [data-clock]')).toHaveText("1:00");
    await expect(undo).toBeDisabled();
    await page.keyboard.press("1");
    await expect(cell(page, 0, "X")).toBeVisible();
    await page.clock.fastForward(5000);
    await expect(page.locator('[data-player="O"] [data-clock]')).toHaveText("0:55");
    await settings();
    await expect(page.getByRole("dialog").getByRole("button", { name: "Close", exact: true })).toBeFocused();
    await page.keyboard.press("Shift+Tab");
    await expect.poll(() => page.evaluate(() => Boolean(document.activeElement?.closest('[role="dialog"]')))).toBe(true);
    await page.clock.fastForward(20000);
    await expect(page.locator('[data-player="O"] [data-clock]')).toHaveText("0:55");
    await close();
    await expect(page.getByRole("button", { name: "Settings", exact: true })).toBeFocused();
    await page.evaluate(() => { Object.defineProperty(document, "hidden", { configurable: true, value: true }); document.dispatchEvent(new Event("visibilitychange")); });
    await page.clock.fastForward(20000);
    await expect(page.locator('[data-player="O"] [data-clock]')).toHaveText("0:55");
    await page.evaluate(() => { Object.defineProperty(document, "hidden", { configurable: true, value: false }); document.dispatchEvent(new Event("visibilitychange")); });
    await page.clock.fastForward(45000);
    await expect(page.locator('[data-player="O"] [data-clock]')).toHaveAttribute("data-low", "true");
    await page.clock.fastForward(11000);
    await expect(status(page)).toHaveText("X wins");
    await expect(page.getByRole("dialog")).toContainText("X wins");
    await page.getByRole("button", { name: "View board" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(undo).toBeDisabled();
    await page.keyboard.press("n");
    await expect(board(page).getByRole("button", { name: /Empty$/, includeHidden: true })).toHaveCount(9);
    await page.keyboard.press("1");
    await page.keyboard.press("z");
    await expect(board(page).getByRole("button", { name: /Empty$/, includeHidden: true })).toHaveCount(9);
    await settings();
    await radio("Opponent", "Computer").check();
    await radio("Difficulty", "Hard").check();
    await radio("You play as", "O").check();
    await close();
    await expect(board(page).getByRole("button", { name: /, X$/ })).toHaveCount(1);
    await expect(page.locator('[data-player="X"]')).toContainText("Hard");
    await expect(page.locator('[data-player="X"]')).not.toContainText("228");
    await expect(undo).toBeDisabled();
    const empty = board(page).getByRole("button", { name: /Empty$/, includeHidden: true }).first();
    await empty.click();
    await expect(board(page).getByRole("button", { name: /, X$/ })).toHaveCount(2);
    await undo.click();
    await expect(board(page).getByRole("button", { name: /, O$/ })).toHaveCount(0);
    await settings();
    await radio("Difficulty", "Medium").check();
    await close();
    await expect(page.locator('[data-player="X"]')).toContainText("Medium");
    await settings();
    await radio("Opponent", "Player").check();
    await close();
    await cell(page, 0).focus();
    await page.keyboard.press("ArrowRight");
    await expect(cell(page, 1)).toBeFocused();
    expect(await cell(page, 1).evaluate(e => getComputedStyle(e).outlineStyle)).toBe("solid");
    for (const index of [0, 3, 1, 4, 2]) await cell(page, index).click();
    await expect(status(page)).toHaveText("X wins");
    await expect(board(page).locator("[data-winning]")).toHaveCount(3);
    await page.getByRole("button", { name: "View board" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(undo).toBeDisabled();
    await reset.click();
    for (const index of [0, 1, 2, 4, 3, 5, 7, 6, 8]) await cell(page, index).click();
    await expect(status(page)).toHaveText("Draw");
    await page.getByRole("button", { name: "View board" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.getByRole("button", { name: "Statistics", exact: true }).click();
    const total = () => page.getByRole("dialog").getByText("Games played").locator("..");
    await expect(total()).toContainText("3");
    await page.getByRole("button", { name: "Reset", exact: true }).click();
    await expect(total()).toContainText("0");
    await close();
    await page.getByRole("button", { name: "Statistics", exact: true }).click();
    await expect(total()).toContainText("0");
    await close();
    await reset.click();
    await cell(page, 0).click();
    await cell(page, 4).click();
    await page.evaluate(() => { Object.defineProperty(document, "hidden", { configurable: true, value: true }); document.dispatchEvent(new Event("visibilitychange")); });
    const reference = await person.context.newPage();
    await reference.setContent(readFileSync(new URL("../../../docs/design/tictactoe.html", import.meta.url), "utf8"));
    await reference.locator(".cell").nth(0).click();
    await reference.locator(".cell").nth(4).click();
    for (const theme of ["light", "dark"]) {
      await settings();
      await radio("Theme", theme === "light" ? "Light" : "Dark").check();
      await close();
      await reference.evaluate(theme => document.documentElement.setAttribute("data-theme", theme), theme);
      await expect(cell(page, 4, "O").locator("svg")).toHaveCSS("color", theme === "dark" ? "rgb(240, 239, 246)" : "rgb(37, 40, 58)");
      const contrast = await page.evaluate(() => {
        const luma = (color: string) => {
          const rgb = color.match(/[\d.]+/g)!.slice(0, 3).map(Number).map(value => { const channel = value / 255; return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4; });
          return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722;
        };
        return [["text", "card"], ["text2", "card"], ["text2", "bg2"], ["on-primary", "primary"], ["on-primary", "error"]].map(([fg, bg]) => {
          const sample = document.createElement("span");
          sample.style.cssText = `color:var(--color-game-${fg});background:var(--color-game-${bg})`;
          document.body.append(sample);
          const style = getComputedStyle(sample);
          const a = luma(style.color), b = luma(style.backgroundColor);
          sample.remove();
          return { fg, bg, ratio: (Math.max(a, b) + .05) / (Math.min(a, b) + .05) };
        });
      });
      for (const pair of contrast) expect(pair.ratio, `${theme} ${pair.fg}/${pair.bg}`).toBeGreaterThanOrEqual(4.5);
      await testInfo.attach(`contrast-${theme}`, { body: JSON.stringify(contrast), contentType: "application/json" });
      await page.mouse.move(0, 0);
      await reference.mouse.move(0, 0);
      for (const width of [320, 390, 768, 1280]) {
        await page.setViewportSize({ width, height: 900 });
        await reference.setViewportSize({ width, height: 900 });
        const bounds = (await board(page).boundingBox())!;
        expect(bounds.width).toBeGreaterThan(200);
        expect(Math.abs(bounds.height - bounds.width)).toBeLessThan(2);
        expect(bounds.x).toBeGreaterThanOrEqual(0);
        expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        const header = page.locator("main header");
        const actions = header.locator("a,button");
        for (const action of await actions.all()) { const rect = (await action.boundingBox())!; expect(rect.width).toBeGreaterThanOrEqual(44); expect(rect.height).toBeGreaterThanOrEqual(44); }
        expect((await header.boundingBox())!.y).toBeLessThanOrEqual(16);
        await expect(header.getByRole("link", { name: "Back to lobby" })).toHaveText("");
        await page.screenshot({ path: testInfo.outputPath(`game-${theme}-${width}.png`), animations: "disabled" });
        await reference.screenshot({ path: testInfo.outputPath(`reference-${theme}-${width}.png`), animations: "disabled" });
      }
    }
    await reference.close();
    await page.evaluate(() => { Object.defineProperty(document, "hidden", { configurable: true, value: false }); document.dispatchEvent(new Event("visibilitychange")); });
    await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "light" });
    await settings();
    await radio("Theme", "System").check();
    await close();
    await expect(page.locator('[data-player="X"]')).toHaveCSS("background-color", "rgb(255, 255, 255)");
    await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    await expect(page.locator('[data-player="X"]')).toHaveCSS("background-color", "rgb(25, 29, 41)");
    await reset.click();
    await cell(page, 0).click();
    expect(await cell(page, 0, "X").locator("line").first().evaluate(e => getComputedStyle(e).animationName)).toBe("none");
    await settings();
    await radio("Time per player", "30 s").check();
    await radio("Opponent", "Computer").check();
    await radio("Difficulty", "Easy").check();
    await radio("Theme", "Dark").check();
    await close();
    await page.reload();
    await settings();
    await expect(radio("Time per player", "30 s")).toBeChecked();
    await expect(radio("Difficulty", "Easy")).toBeChecked();
    await expect(radio("Theme", "Dark")).toBeChecked();
    await close();
  } finally { await person.context.close(); }
});

function fixtureSQL(id: string, sql: string) {
  const container = process.env.GAME_E2E_POSTGRES_CONTAINER;
  if (!container || !/^axon-postgres-\d+$/.test(container) || !/^[0-9a-f-]{36}$/.test(id)) throw new Error("Requires an Axon dev fixture session");
  return execFileSync("docker", ["exec", container, "sh", "-c", 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -tAc "$1"', "--", sql], { encoding: "utf8" }).trim();
}

function shortenFixtureClock(id: string) {
  fixtureSQL(id, `UPDATE game.sessions SET clock=jsonb_build_object('remaining_ms',jsonb_build_array(1500,1500),'started_at',clock_timestamp()) WHERE id='${id}'; SELECT pg_notify('game_session_changed','${id}');`);
}

test("server clocks survive offline clients, timeout and an empty room", async ({ browser, baseURL }, testInfo) => {
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
    await expect(a.page.getByText("Time's up", { exact: true })).toHaveCount(1);
    await expect(a.page.locator('[data-player="O"]')).toContainText("0:00");
    await expect(a.page.locator('[data-player="X"]')).toContainText("0:02");
    await b.context.setOffline(false);
    await b.page.reload();
    await expect(status(b.page)).toHaveText("You lost");
    await expect(b.page.locator('[data-player="O"]')).toContainText("0:00");
    await a.page.getByRole("button", { name: "View board" }).click();
    await expect(a.page.getByRole("button", { name: "Undo move", exact: true })).toBeDisabled();
    await a.page.getByRole("button", { name: "New game", exact: true }).click();
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
    await a.page.getByRole("button", { name: "View board" }).click();
    await expect(a.page.getByRole("button", { name: "Undo move", exact: true })).toBeDisabled();
  } finally {
    await Promise.all([a.context.close(), b.context.close()]);
  }
});

test("quick match pairs two players into a rated game and moves both ratings", async ({ browser, baseURL }) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Quick Alpha");
  const b = await player(browser, origin, "Quick Bravo");
  try {
    for (const person of [a, b]) {
      await person.page.goto(origin + "/games/tic-tac-toe");
      await expect(person.page.getByTestId("my-rating")).toHaveText("1200");
      await person.page.getByRole("button", { name: "Start a game", exact: true }).click();
      await person.page.getByRole("button", { name: /Quick match/ }).click();
    }
    await expect(a.page).toHaveURL(/\/games\/tic-tac-toe\/[0-9a-f-]+$/, { timeout: 20_000 });
    await expect(b.page).toHaveURL(a.page.url(), { timeout: 20_000 });
    await expect(a.page.getByRole("button", { name: "Undo move", exact: true })).toHaveCount(0);
    const [first, second] = (await status(a.page).innerText()).includes("Your turn") ? [a, b] : [b, a];
    await expect(status(second.page)).toContainText("Opponent’s turn");
    await play([first.page, second.page], [0, 3, 1, 4, 2]);
    await expect(status(first.page)).toHaveText("You won");
    await expect(first.page.locator("[data-rating-delta]")).toHaveText("Rating +20");
    await expect(second.page.locator("[data-rating-delta]")).toHaveText("Rating −20");
    await first.page.getByRole("button", { name: "View board" }).click();
    await first.page.getByRole("link", { name: "Back to lobby" }).first().click();
    await expect(first.page.getByTestId("my-rating")).toHaveText("1220");
    await expect(first.page.locator("#game-history")).toContainText("+20");
  } finally {
    await a.context.close();
    await b.context.close();
  }
});

test("a player cancels a search and a waiting game, and resigns a started one", async ({ browser, baseURL }) => {
  test.setTimeout(60_000);
  const origin = new URL(baseURL!).origin;
  const a = await player(browser, origin, "Leaving Alpha");
  const b = await player(browser, origin, "Leaving Bravo");
  try {
    await a.page.goto(origin + "/games/tic-tac-toe");
    await a.page.getByRole("button", { name: "Start a game", exact: true }).click();
    await a.page.getByRole("button", { name: /Quick match/ }).click();
    await expect(a.page.getByText(/Searching ·/)).toBeVisible();
    await a.page.getByRole("button", { name: "Cancel search", exact: true }).click();
    await expect(a.page.getByText(/Searching ·/)).toHaveCount(0);
    expect((await (await a.rpc("GetMatch", { game: "tictactoe" })).json()).ticket?.status ?? "MATCH_STATUS_IDLE").toBe("MATCH_STATUS_IDLE");

    const waiting = await createGame(a.page);
    await a.page.getByRole("button", { name: "Cancel game", exact: true }).click();
    await a.page.getByRole("button", { name: "Cancel game", exact: true }).click();
    await expect(a.page).toHaveURL(/\/games\/tic-tac-toe$/);
    expect((await a.rpc("GetSession", { sessionId: waiting.split("/").at(-1) })).status()).toBe(404);

    const started = await createGame(a.page);
    await joinGame(b.page, started);
    await b.page.getByRole("button", { name: "Resign", exact: true }).click();
    await b.page.getByRole("button", { name: "Resign", exact: true }).click();
    await expect(status(b.page)).toHaveText("You resigned");
    await expect(status(a.page)).toHaveText("Opponent resigned");
  } finally {
    await a.context.close();
    await b.context.close();
  }
});
