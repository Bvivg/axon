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

async function goToChats(page: Page): Promise<void> {
  await page.getByRole("link", { name: "Chats" }).click();
  await expectLobby(page);
}

async function createRoom(page: Page, name: string): Promise<string> {
  await goToChats(page);
  await page.getByLabel("Name").fill(name);
  await page.getByRole("button", { name: "Create" }).click();

  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/);
  await expectConnected(page);

  return page.url().split("/").pop()!;
}

async function joinRoom(page: Page, roomID: string): Promise<void> {
  await goToChats(page);
  await page.getByLabel("Room ID").fill(roomID);
  await page.getByRole("button", { name: "Join" }).click();

  await expect(page).toHaveURL(new RegExp(`/chat/${roomID}$`));
  await expectConnected(page);
}

async function expectConnected(page: Page): Promise<void> {
  await expect(page.getByText(/^Connected/)).toBeVisible();
}

async function say(page: Page, body: string): Promise<void> {
  await page.getByLabel("Message").fill(body);
  await page.getByRole("button", { name: "Send" }).click();

  await expect(page.getByLabel("Message")).toHaveValue("");
}

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

  await say(ada.page, "did that reach you");
  await expectSaid(ada.page, "You", "did that reach you");
  await expectSaid(grace.page, "Ada", "did that reach you");

  await say(grace.page, "loud and clear");
  await expectSaid(grace.page, "You", "loud and clear");
  await expectSaid(ada.page, "Grace", "loud and clear");

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

  await ada.page.reload();
  await expectConnected(ada.page);
  await expectSaid(ada.page, "You", "said before the reload");

  await say(ada.page, "said after it");
  await expectSaid(ada.page, "You", "said after it");
  await expect(ada.page.locator("article")).toHaveCount(2);

  await ada.page.getByRole("link", { name: "All rooms" }).click();
  await expectLobby(ada.page);
  await expect(
    ada.page.getByRole("link", { name: /before and after/ }),
  ).toBeVisible();

  await ada.close();
});

test("a room is out of reach without a session, and reachable again after signing in", async ({
  page,
}) => {
  const email = newEmail("guarded");

  await register(page, email, "Ada");
  const roomID = await createRoom(page, "members only");

  await goToProfile(page);
  await signOut(page);

  await open(page, `/chat/${roomID}`);
  await expect(page).toHaveURL(new RegExp(`/login\\?next=%2Fchat%2F${roomID}$`));

  await submitCredentials(page, "Sign in", email);

  await expect(page).toHaveURL(new RegExp(`/chat/${roomID}$`));
  await expectConnected(page);
});
