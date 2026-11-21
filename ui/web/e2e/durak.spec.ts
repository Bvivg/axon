import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

import { gatewayUrl, newEmail, newUsername, password } from "./support";

type Hand = { cards?: string[]; count?: number; hidden?: boolean };
type Seen = { source: string; viewer: number; hands: Hand[]; seed: boolean };
type Person = Awaited<ReturnType<typeof person>>;

const status = (page: Page) => page.locator("[data-game-status]");
const finished = /^(You won|You lost|Draw)/;

function boards(value: unknown, source: string, userId: string, into: Seen[]) {
  if (!value || typeof value !== "object") return;
  if (Array.isArray(value)) { value.forEach(item => boards(item, source, userId, into)); return; }
  const item = value as Record<string, unknown>;
  const durak = item.durak as { hands?: Hand[] } | undefined;
  if (durak?.hands && Array.isArray(item.playerIds) && item.status === "SESSION_STATUS_PLAYING") {
    into.push({ source, viewer: (item.playerIds as string[]).indexOf(userId), hands: durak.hands, seed: JSON.stringify(item).includes("seed") });
  }
  Object.values(item).forEach(child => boards(child, source, userId, into));
}

async function person(browser: Browser, origin: string, name: string) {
  const context = await browser.newContext({ baseURL: origin });
  const headers = { Origin: origin, "Connect-Protocol-Version": "1" };
  const data = { email: newEmail("durak"), username: newUsername("durak"), displayName: name, password };
  let registered: APIResponse | undefined;
  await expect.poll(async () => {
    registered = await context.request.post(gatewayUrl + "/axon.auth.v1.AuthService/Register", { headers, data });
    return registered.status();
  }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
  expect(registered!.status()).toBe(200);
  const userId: string = (await registered!.json()).user.id;
  const page = await context.newPage();
  const seen: Seen[] = [];
  page.on("websocket", socket => socket.on("framereceived", ({ payload }) => {
    try { boards(JSON.parse(String(payload)), "socket", userId, seen); } catch { return; }
  }));
  page.on("response", response => {
    if (!response.url().includes("/axon.game.v1.GameService/")) return;
    void response.json().then(body => boards(body, response.url().split("/").pop()!, userId, seen), () => undefined);
  });
  return { context, page, userId, seen, movedAt: 0 };
}

function expectNothingLeaked(who: Person) {
  expect(who.seen.length).toBeGreaterThan(0);
  for (const view of who.seen) {
    expect(view.seed, `${view.source} carries a seed`).toBe(false);
    view.hands.forEach((hand, seat) => {
      if (seat === view.viewer) return;
      expect(hand.cards ?? [], `${view.source} reveals seat ${seat}`).toEqual([]);
      expect(hand.hidden, `${view.source} marks seat ${seat}`).toBe(true);
    });
  }
}

async function expectHiddenTable(page: Page) {
  await expect(page.getByTestId("durak-top-hand")).toHaveAttribute("data-hidden", "true");
  await expect(page.getByTestId("durak-top-hand").locator("[data-card]")).toHaveCount(0);
  const stray = await page.locator("[data-card]").evaluateAll(cards => cards.filter(card => !card.closest("[data-testid=durak-table], [data-testid=durak-stock], [data-testid=durak-hand]")).length);
  expect(stray).toBe(0);
}

async function turnOf(a: Person, b: Person) {
  let mover: Person | undefined;
  await expect.poll(async () => {
    const [left, right] = await Promise.all([status(a.page).textContent(), status(b.page).textContent()]);
    if (finished.test(left ?? "") && finished.test(right ?? "")) { mover = undefined; return "over"; }
    mover = left?.startsWith("Your move") && !left.includes("Connecting") ? a : right?.startsWith("Your move") && !right.includes("Connecting") ? b : undefined;
    return mover ? "move" : "wait";
  }, { intervals: [100], timeout: 15_000 }).not.toBe("wait");
  return mover;
}

test("two players play durak to the end and never see each other's hand", async ({ browser, baseURL }) => {
  test.setTimeout(300_000);
  const origin = new URL(baseURL!).origin;
  const a = await person(browser, origin, "Durak Alpha");
  const b = await person(browser, origin, "Durak Bravo");
  const watcher = await person(browser, origin, "Durak Watcher");
  try {
    await a.page.goto(origin + "/games");
    await a.page.getByRole("navigation", { name: "Game catalogue", exact: true }).getByRole("link", { name: "Durak lobby", exact: true }).click();
    await expect(a.page).toHaveURL(/\/games\/durak$/);
    await a.page.getByRole("button", { name: "Start a game", exact: true }).click();
    await a.page.getByRole("button", { name: /Private game/ }).click();
    await expect(a.page).toHaveURL(/\/games\/durak\/[0-9a-f-]+$/);
    await expect(status(a.page)).toContainText("Waiting for opponent");
    const id = a.page.url().split("/").pop()!;
    await b.page.goto(a.page.url());
    await b.page.getByRole("button", { name: "Join game" }).click();
    await expect(b.page.getByRole("group", { name: "Durak table" })).toBeVisible();
    await expect(a.page.getByTestId("durak-hand").locator("[data-hand-card]")).toHaveCount(6);
    await expect(b.page.getByTestId("durak-hand").locator("[data-hand-card]")).toHaveCount(6);
    await expect(a.page.getByTestId("durak-top-hand")).toHaveAttribute("data-count", "6");

    await watcher.page.goto(origin + "/games/durak");
    await watcher.page.locator(`[data-live-game="${id}"]`).click();
    await expect(watcher.page.locator("[data-watching]")).toBeVisible();
    await expect(watcher.page.getByTestId("durak-bottom-hand")).toHaveAttribute("data-hidden", "true");
    await expect(watcher.page.getByTestId("durak-hand")).toHaveCount(0);

    let took = false;
    let threw = false;
    for (let step = 0; step < 600; step++) {
      const mover = await turnOf(a, b);
      if (!mover) break;
      const page = mover.page;
      await page.waitForTimeout(Math.max(0, mover.movedAt + 300 - Date.now()));
      const text = (await status(page).textContent()) ?? "";
      const revision = await page.getByTestId("move-list").locator("li").count().catch(() => 0);
      const legal = page.locator("[data-hand-card][data-legal]");
      const canPlay = await legal.count() > 0;
      if (text.includes("defend")) {
        if (!took || !canPlay) { took = true; await page.getByRole("button", { name: "Take", exact: true }).click(); }
        else await legal.first().click();
      } else if (text.includes("· attack")) {
        await legal.first().click();
      } else if (canPlay && !threw) {
        threw = true;
        await legal.first().click();
      } else {
        await page.getByRole("button", { name: "Pass", exact: true }).click();
      }
      mover.movedAt = Date.now();
      await expect(page.getByTestId("move-list").locator("li")).toHaveCount(revision + 1);
      if (step % 10 === 0) for (const who of [a, b, watcher]) await expectHiddenTable(who.page);
    }

    await expect(status(a.page)).toHaveText(finished);
    await expect(status(b.page)).toHaveText(finished);
    await expect(watcher.page.locator("[data-spectator-status]")).not.toBeEmpty();
    const moves = a.page.getByTestId("move-list");
    await expect(moves.locator("[data-move=take]").first()).toBeAttached();
    await expect(moves.locator("[data-move=pass]").first()).toBeAttached();
    await expect(moves.locator("[data-move^=defend]").first()).toBeAttached();
    for (const who of [a, b, watcher]) expectNothingLeaked(who);
    expect(watcher.seen.every(view => view.viewer === -1)).toBe(true);

    await watcher.page.getByRole("link", { name: "Replay" }).click();
    await expect(watcher.page).toHaveURL(new RegExp(`/games/durak/${id}/replay$`));
    await watcher.page.keyboard.press("Home");
    await expect(watcher.page.locator("[data-replay-ply]")).toHaveText("Start position");
    await expect(watcher.page.locator("[data-hand-card], [data-testid=durak-top-hand] [data-card]")).not.toHaveCount(0);
  } finally {
    await a.context.close();
    await b.context.close();
    await watcher.context.close();
  }
});
