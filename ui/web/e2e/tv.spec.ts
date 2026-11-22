import { randomUUID } from "node:crypto";
import { expect, test, type APIResponse, type Browser, type BrowserContextOptions, type Page } from "@playwright/test";
import pg from "pg";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

const cells = ["Top left", "Top center", "Top right", "Middle left", "Middle center", "Middle right", "Bottom left", "Bottom center", "Bottom right"];
const dsn = process.env.ADMIN_POSTGRES_DSN;

type Person = Awaited<ReturnType<typeof person>>;

async function person(browser: Browser, origin: string, name: string, options: BrowserContextOptions = {}) {
  const context = await browser.newContext({ baseURL: origin, ...options });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  const email = newEmail("tv");
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", {
      headers, data: { email, username: newUsername("tv"), displayName: name, password },
    });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  const { tokens } = await registered!.json();
  return {
    email,
    context,
    page: await context.newPage(),
    rpc: (method: string, data: object) => context.request.post(gatewayUrl + "/axon.game.v1.GameService/" + method, {
      headers: { ...headers, Authorization: `Bearer ${tokens.accessToken}` }, data,
    }),
  };
}

async function live(game: string, first: Person, second: Person): Promise<string> {
  const created = await first.rpc("CreateSession", { game, requestId: randomUUID() });
  expect(created.status()).toBe(200);
  const id: string = (await created.json()).session.id;
  expect((await second.rpc("JoinSession", { sessionId: id })).status()).toBe(200);
  return id;
}

const slide = (page: Page, id: string) => page.locator(`[data-tv-slide="${id}"]`);
const active = (page: Page) => page.locator("[data-tv-slide][data-active]");

async function order(page: Page): Promise<string[]> {
  return page.locator("[data-tv-slide]").evaluateAll(nodes => nodes.map(node => node.getAttribute("data-tv-slide") ?? ""));
}

async function showWithKeys(page: Page, id: string) {
  const ids = await order(page);
  const target = ids.indexOf(id);
  expect(target).toBeGreaterThanOrEqual(0);
  for (;;) {
    const current = ids.indexOf((await active(page).getAttribute("data-tv-slide")) ?? "");
    if (current === target) break;
    await page.keyboard.press(current < target ? "ArrowDown" : "ArrowUp");
    await expect(active(page)).toHaveAttribute("data-tv-slide", ids[current + (current < target ? 1 : -1)]!);
  }
  await expect(slide(page, id)).toBeInViewport({ ratio: 0.9 });
}

const grid = (page: Page) => page.getByRole("grid", { name: "Tic-Tac-Toe board", includeHidden: true });

async function play(page: Page, cell: number) {
  const button = grid(page).getByRole("button", { name: `${cells[cell]}, Empty`, exact: true, includeHidden: true });
  await expect(button).toBeEnabled();
  await button.click();
  await expect(grid(page).getByRole("button", { name: new RegExp(`^${cells[cell]}, [XO]$`), includeHidden: true })).toBeVisible();
}

test("Axon TV streams live games of several kinds, keeps Durak hands hidden and moves on after a result", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(120_000);
  const origin = new URL(baseURL!).origin;
  const [white, black, north, south, x, o, watcher] = await Promise.all([
    person(browser, origin, "Wendy White"), person(browser, origin, "Boris Black"),
    person(browser, origin, "Nora North"), person(browser, origin, "Sam South"),
    person(browser, origin, "Xavier Televised"), person(browser, origin, "Olivia Televised"),
    person(browser, origin, "Viewer Vic"),
  ]);
  try {
    const chess = await live("chess", white, black);
    const durak = await live("durak", north, south);
    const board = await live("tictactoe", x, o);

    await watcher.page.goto("/games/tv");
    await expect(watcher.page.getByRole("heading", { name: "Axon TV" })).toBeVisible();
    await expect(slide(watcher.page, board)).toHaveAttribute("data-tv-game", "tictactoe");
    await expect(slide(watcher.page, durak)).toHaveAttribute("data-tv-game", "durak");
    await expect(slide(watcher.page, chess)).toHaveAttribute("data-tv-game", "chess");
    await showWithKeys(watcher.page, board);

    const ttt = slide(watcher.page, board);
    await expect(ttt.locator("[data-tv-live]")).toBeVisible();
    await expect(ttt.locator('[data-player="X"]')).toContainText("Xavier Televised");
    await expect(ttt.locator('[data-player="O"]')).toContainText("Olivia Televised");
    await expect(ttt.locator('[data-player="X"]')).toContainText("1200");
    await expect(ttt.locator("[data-tv-ply]")).toHaveText("Move 0");

    await x.page.goto(`/games/tic-tac-toe/${board}`);
    await o.page.goto(`/games/tic-tac-toe/${board}`);
    await play(x.page, 4);
    await expect(ttt.locator("[data-tv-ply]")).toHaveText("Move 1");
    await expect(ttt.locator(`button[aria-label="${cells[4]}, X"]`)).toHaveCount(1);
    await expect(ttt.locator("[data-tv-status]")).toHaveText("O to move");
    await expect(x.page.locator("[data-spectators]")).toHaveText("1 watching");

    await showWithKeys(watcher.page, durak);
    const cards = slide(watcher.page, durak);
    await expect(cards.locator('[data-testid="durak-top-hand"]')).toHaveAttribute("data-hidden", "true");
    await expect(cards.locator('[data-testid="durak-bottom-hand"]')).toHaveAttribute("data-hidden", "true");
    await expect(cards.locator('[data-testid="durak-top-hand"]')).toHaveAttribute("data-count", "6");
    await expect(cards.locator("[data-hand-card]")).toHaveCount(0);
    await expect(cards.locator('[data-testid="durak-hand"]')).toHaveCount(0);

    await showWithKeys(watcher.page, chess);
    await expect(slide(watcher.page, chess).locator('[data-tv-board="chess"]')).toBeVisible();
    await watcher.page.screenshot({ path: testInfo.outputPath("tv-desktop.png"), animations: "disabled" });

    const ids = await order(watcher.page);
    const at = ids.indexOf(chess);
    const feed = watcher.page.getByLabel("Live games");
    const box = (await feed.boundingBox())!;
    await watcher.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    const step = at + 1 < ids.length ? 1 : -1;
    await watcher.page.mouse.wheel(0, step * box.height);
    await expect(active(watcher.page)).toHaveAttribute("data-tv-slide", ids[at + step]!);
    await watcher.page.getByRole("button", { name: step === 1 ? "Previous game" : "Next game" }).click();
    await expect(active(watcher.page)).toHaveAttribute("data-tv-slide", chess);
    await showWithKeys(watcher.page, board);

    for (const [step, cell] of [[1, 0], [2, 1], [3, 2], [4, 7]] as const) {
      await play(step % 2 === 1 ? o.page : x.page, cell);
      await expect(ttt.locator("[data-tv-ply]")).toHaveText(`Move ${step + 1}`);
    }
    const result = ttt.locator("[data-tv-result]");
    await expect(result).toContainText("Xavier Televised won");
    await expect(ttt.locator("[data-tv-ended]")).toBeVisible();
    await result.getByRole("button", { name: "Next game" }).click();
    await expect(active(watcher.page)).not.toHaveAttribute("data-tv-slide", board);
    const after = (await order(watcher.page)).indexOf((await active(watcher.page).getAttribute("data-tv-slide"))!);
    expect(after).toBeGreaterThan((await order(watcher.page)).indexOf(board));
    await expect(active(watcher.page).locator("[data-tv-live]")).toBeVisible();
  } finally {
    await Promise.all([white, black, north, south, x, o, watcher].map(item => item.context.close()));
  }
});

test("Axon TV fits a 375 px phone and swipes one game at a time", async ({ browser, baseURL }, testInfo) => {
  test.setTimeout(90_000);
  const origin = new URL(baseURL!).origin;
  const phone = { viewport: { width: 375, height: 812 }, isMobile: true, hasTouch: true };
  const [a, b, c, d, watcher] = await Promise.all([
    person(browser, origin, "Quinn Quoridor"), person(browser, origin, "Quentin Quoridor"),
    person(browser, origin, "Cora Checkers"), person(browser, origin, "Cole Checkers"),
    person(browser, origin, "Pocket Viewer", phone),
  ]);
  try {
    const checkers = await live("checkers", c, d);
    const quoridor = await live("quoridor", a, b);
    await watcher.page.goto("/games/tv");
    await expect(slide(watcher.page, checkers)).toHaveAttribute("data-tv-game", "checkers");
    await showWithKeys(watcher.page, quoridor);
    const first = slide(watcher.page, quoridor);
    await expect(first.locator('[data-tv-board="quoridor"]')).toBeInViewport();
    await expect(first.getByRole("link", { name: "Open game" })).toBeInViewport();
    const width = await watcher.page.evaluate(() => document.documentElement.scrollWidth);
    expect(width).toBeLessThanOrEqual(375);
    const boardBox = (await first.locator('[data-tv-board="quoridor"]').boundingBox())!;
    expect(boardBox.x).toBeGreaterThanOrEqual(0);
    expect(boardBox.x + boardBox.width).toBeLessThanOrEqual(375);
    await watcher.page.screenshot({ path: testInfo.outputPath("tv-phone.png"), animations: "disabled" });

    const feed = watcher.page.getByLabel("Live games");
    const box = (await feed.boundingBox())!;
    const cdp = await watcher.context.newCDPSession(watcher.page);
    await cdp.send("Input.synthesizeScrollGesture", { x: Math.round(box.x + box.width / 2), y: Math.round(box.y + box.height * 0.7), yDistance: -Math.round(box.height * 0.4), gestureSourceType: "touch", speed: 1600, preventFling: false });
    const ids = await order(watcher.page);
    const following = ids[ids.indexOf(quoridor) + 1]!;
    await expect(active(watcher.page)).toHaveAttribute("data-tv-slide", following);
    await expect(slide(watcher.page, following)).toBeInViewport({ ratio: 0.9 });
    await expect(slide(watcher.page, following).locator("[data-tv-board]")).toBeInViewport();

    await first.getByRole("link", { name: "Open game" }).evaluate(link => link.scrollIntoView());
    await slide(watcher.page, quoridor).getByRole("link", { name: "Open game" }).click();
    await expect(watcher.page).toHaveURL(new RegExp(`/games/quoridor/${quoridor}$`));
    await expect(watcher.page.locator("[data-watching]")).toBeVisible();
  } finally {
    await Promise.all([a, b, c, d, watcher].map(item => item.context.close()));
  }
});

test("Axon TV says so when nothing is live", async ({ browser, baseURL }) => {
  const watcher = await person(browser, new URL(baseURL!).origin, "Lonely Viewer");
  try {
    await watcher.page.route("**/axon.game.v1.GameService/ListLiveSessions", async route => {
      const response = await route.fetch();
      await route.fulfill({ response, json: { sessions: [] } });
    });
    await watcher.page.goto("/games/tv");
    await expect(watcher.page.locator("[data-tv-empty]")).toContainText("No live games right now");
    await watcher.page.getByRole("link", { name: "Play a game" }).click();
    await expect(watcher.page).toHaveURL(/\/games$/);
  } finally {
    await watcher.context.close();
  }
});

test("the admin console shows Axon TV in its Play section", async ({ browser, baseURL }) => {
  test.skip(!dsn, "ADMIN_POSTGRES_DSN is not set; run via make test-web-e2e");
  const origin = new URL(baseURL!).origin;
  const [first, second, admin] = await Promise.all([person(browser, origin, "Tess Tv"), person(browser, origin, "Toby Tv"), person(browser, origin, "Console Viewer")]);
  try {
    const client = new pg.Client({ connectionString: dsn });
    await client.connect();
    try {
      await client.query("INSERT INTO user_roles (user_id, role_id) SELECT u.id, r.id FROM users u, roles r WHERE lower(u.email) = lower($1) AND r.name = 'admin' ON CONFLICT DO NOTHING", [admin.email]);
    } finally {
      await client.end();
    }
    const id = await live("tictactoe", first, second);
    await admin.page.goto("/admin/games/tv");
    const sidebar = admin.page.getByRole("navigation", { name: "Administration" });
    await expect(sidebar.getByRole("link", { name: "Axon TV", exact: true })).toHaveAttribute("aria-current", "page");
    await expect(sidebar.getByRole("link", { name: "Games", exact: true })).not.toHaveAttribute("aria-current", "page");
    const frame = admin.page.frameLocator("[data-tv-frame]");
    await expect(frame.locator(`[data-tv-slide="${id}"]`)).toBeVisible();
    await expect(frame.locator(`[data-tv-slide="${id}"]`).getByRole("link", { name: "Open game" })).toHaveAttribute("target", "_blank");
  } finally {
    await Promise.all([first, second, admin].map(item => item.context.close()));
  }
});
