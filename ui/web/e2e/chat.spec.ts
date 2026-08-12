import { expect, test, type Page } from "@playwright/test";

import {
  expectLobby,
  goToProfile,
  newEmail,
  newPerson,
  open,
  register,
  signOut,
  submitCredentials,
} from "./support";

/**
 * The browser half of stage 2.
 *
 * What only a browser can answer is here, and nothing else is. The Go suite
 * already drives two sockets through the gateway and proves the protocol; what
 * it cannot show is that a message crosses between two people looking at two
 * pages, that a page opened cold shows what was said before it existed, and
 * that a room is not somewhere you can simply navigate to.
 */

/**
 * Walks from wherever the nav is to the chat lobby, the way clicking the
 * Chats tab would. register() now lands on Games, not here, so this is the
 * step every scenario needs before it can open or join a room.
 */
async function goToChats(page: Page): Promise<void> {
  await page.getByRole("link", { name: "Chats" }).click();
  await expectLobby(page);
}

/** Opens a room and returns its id, which is also the invitation. */
async function createRoom(page: Page, name: string): Promise<string> {
  await goToChats(page);
  await page.getByLabel("Name").fill(name);
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/);
  await expectConnected(page);

  return page.url().split("/").pop()!;
}

/** Joins a room somebody else opened, by the id they passed on. */
async function joinRoom(page: Page, roomID: string): Promise<void> {
  await goToChats(page);
  await page.getByLabel("Room ID").fill(roomID);
  await page.getByRole("button", { name: "Join" }).click();

  await expect(page).toHaveURL(new RegExp(`/chat/${roomID}$`));
  await expectConnected(page);
}

/**
 * Waits for the socket to be up.
 *
 * Not only an assertion: the composer is disabled until the socket is open, so
 * this is also what keeps a test from typing into a box that is not listening.
 */
async function expectConnected(page: Page): Promise<void> {
  await expect(page.getByText(/^Connected/)).toBeVisible();
}

/** Says something in the room. */
async function say(page: Page, body: string): Promise<void> {
  await page.getByLabel("Message").fill(body);
  await page.getByRole("button", { name: "Send" }).click();
  // The box is cleared only once the socket has taken the message, so this is
  // the client's own acknowledgement that it went.
  await expect(page.getByLabel("Message")).toHaveValue("");
}

/** Asserts a message is on screen, attributed to the given name. */
async function expectSaid(page: Page, author: string, body: string) {
  await expect(
    page
      .locator("article")
      .filter({ hasText: body })
      .getByText(author, { exact: true }),
  ).toBeVisible();
}

test("two people in one room see each other's messages as they are sent", async ({
  browser,
}) => {
  const ada = await newPerson(browser, "ada", "Ada");
  const grace = await newPerson(browser, "grace", "Grace");

  const roomID = await createRoom(ada.page, "wire protocol");
  await joinRoom(grace.page, roomID);

  // Nothing below reloads anything. Every arrival is the socket's doing: the
  // gateway terminated it, chat published to Redis, and the instance holding
  // the other person handed it on.
  await say(ada.page, "did that reach you");
  await expectSaid(ada.page, "You", "did that reach you");
  await expectSaid(grace.page, "Ada", "did that reach you");

  await say(grace.page, "loud and clear");
  await expectSaid(grace.page, "You", "loud and clear");
  await expectSaid(ada.page, "Grace", "loud and clear");

  // The names above are the room's own: a snapshot taken when somebody joined,
  // arriving with the member list rather than on each message. Attribution
  // reading "Ada" rather than a truncated id is that list having been read.
  await expect(grace.page.getByText("2 in the room")).toBeVisible();

  await ada.close();
  await grace.close();
});

test("a page opened cold shows what was said before it existed", async ({
  browser,
}) => {
  const ada = await newPerson(browser, "history", "Ada");

  await createRoom(ada.page, "before and after");
  await say(ada.page, "said before the reload");

  // A reload is a new page with a new socket, and the socket owes a client only
  // the gap since it was last here — this one was never here. So what is on
  // screen after this came from ListMessages.
  await ada.page.reload();
  await expectConnected(ada.page);
  await expectSaid(ada.page, "You", "said before the reload");

  // And the socket carried on from there: the next message lands once, after
  // the first, rather than the history arriving a second time alongside it.
  await say(ada.page, "said after it");
  await expectSaid(ada.page, "You", "said after it");
  await expect(ada.page.locator("article")).toHaveCount(2);

  // The room is still reachable from the lobby it was opened from.
  await ada.page.getByRole("link", { name: "All rooms" }).click();
  await expectLobby(ada.page);
  await expect(
    ada.page.getByRole("link", { name: /before and after/ }),
  ).toBeVisible();

  await ada.close();
});

// The redirect is not what protects the room — the gateway refuses an
// unauthenticated call whatever the browser is showing. What it decides is
// whether somebody is looking at a page or at a bounce, and only a browser can
// answer that.
test("a room is out of reach without a session, and reachable again after signing in", async ({
  page,
}) => {
  const email = newEmail("guarded");

  await register(page, email, "Ada");
  const roomID = await createRoom(page, "members only");

  // The room page carries no link to the profile of its own, but the nav
  // shell wraps it same as every other page behind an account.
  await goToProfile(page);
  await signOut(page);

  // Typed into the address bar with no session. The address is kept, so signing
  // in resumes it instead of dropping them in the lobby wondering where they
  // were going.
  await open(page, `/chat/${roomID}`);
  await expect(page).toHaveURL(new RegExp(`/login\\?next=%2Fchat%2F${roomID}$`));

  await submitCredentials(page, "Sign in", email);

  await expect(page).toHaveURL(new RegExp(`/chat/${roomID}$`));
  await expectConnected(page);
});
