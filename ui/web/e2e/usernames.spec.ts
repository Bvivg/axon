import { expect, test } from "@playwright/test";

import { chatAt, chatList, expectReady, expectSaid, goToChats, openChatWith, say, startChat } from "./chat-support";
import { expectHome, goToProfile, newEmail, newPerson, newUsername, open, submitCredentials } from "./support";

test("registration asks for a username and refuses one that is taken or malformed", async ({ browser }) => {
  const ada = await newPerson(browser, "taken", "Ada");

  const context = await browser.newContext();
  const page = await context.newPage();
  await open(page, "/register");

  await submitCredentials(page, "Create account", newEmail("taken-copy"), undefined, ada.username.toUpperCase());
  await expect(page.getByText("That username is taken.")).toBeVisible();
  await expect(page.getByLabel("Username")).toHaveAttribute("aria-invalid", "true");
  await expect(page).toHaveURL(/\/register$/);

  await page.getByLabel("Username").fill(`9${newUsername("digit")}`);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page.getByText("Username must start with a letter")).toBeVisible();

  await context.close();
  await ada.close();
});

test("the profile renames the username and refuses one that is taken", async ({ browser }) => {
  const ada = await newPerson(browser, "rename", "Ada");
  const grace = await newPerson(browser, "rename-peer", "Grace");

  await goToProfile(ada.page);
  const field = ada.page.getByLabel("Username");
  await expect(field).toHaveValue(ada.username);

  await field.fill(grace.username);
  await ada.page.getByRole("button", { name: "Save changes" }).click();
  await expect(ada.page.getByText("That username is taken.")).toBeVisible();

  const renamed = newUsername("renamed");
  await field.fill(`@${renamed.toUpperCase()}`);
  await ada.page.getByRole("button", { name: "Save changes" }).click();
  await expect(ada.page.getByText("Changes saved")).toBeVisible();

  await ada.page.reload();
  await expect(ada.page.getByLabel("Username")).toHaveValue(renamed);

  await ada.close();
  await grace.close();
});

test("a username in the address opens a chat with that person and keeps the address", async ({ browser }) => {
  const ada = await newPerson(browser, "address", "Ada");
  const grace = await newPerson(browser, "address-peer", "Grace");

  await open(ada.page, `/chat#${grace.username}`);
  await expect(ada.page.getByRole("heading", { name: "Grace" })).toBeVisible();

  await say(ada.page, "found you by name");
  await expectSaid(ada.page, "You", "found you by name");
  await expect(ada.page).toHaveURL(chatAt(grace.username));
  await expect(chatList(ada.page).locator(`a[href="/chat#${grace.username}"]`)).toHaveAttribute(
    "aria-current",
    "page",
  );

  await openChatWith(grace.page, "Ada");
  await expect(grace.page).toHaveURL(chatAt(ada.username));
  await expectSaid(grace.page, "Ada", "found you by name");

  await ada.page.goto(`/chat#${newUsername("nobody")}`);
  await expect(ada.page.getByText("User not found.")).toBeVisible();

  await ada.page.goto(`/chat#${ada.username}`);
  await expect(ada.page.getByText("You can't message yourself.")).toBeVisible();

  await ada.close();
  await grace.close();
});

test("old chat links and history land on the right chat", async ({ browser }) => {
  const ada = await newPerson(browser, "history-links", "Ada");
  const grace = await newPerson(browser, "history-grace", "Grace");
  const linus = await newPerson(browser, "history-linus", "Linus");

  const graceRoom = await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "hi grace");
  await startChat(ada.page, { email: linus.email, username: linus.username, name: "Linus" }, "hi linus");

  await open(ada.page, `/chat/${graceRoom}`);
  await expect(ada.page).toHaveURL(chatAt(grace.username));
  await expectReady(ada.page);
  await expectSaid(ada.page, "You", "hi grace");

  await chatList(ada.page).locator(`a[href="/chat#${linus.username}"]`).click();
  await expect(ada.page).toHaveURL(chatAt(linus.username));
  await ada.page.goto(`/chat#${graceRoom}`);
  await expect(ada.page).toHaveURL(chatAt(grace.username));

  await chatList(ada.page).locator(`a[href="/chat#${linus.username}"]`).click();
  await expect(ada.page).toHaveURL(chatAt(linus.username));
  await expectSaid(ada.page, "You", "hi linus");

  await ada.page.goBack();
  await expect(ada.page).toHaveURL(chatAt(grace.username));
  await expectSaid(ada.page, "You", "hi grace");

  await ada.page.goForward();
  await expect(ada.page).toHaveURL(chatAt(linus.username));
  await expectSaid(ada.page, "You", "hi linus");

  await ada.page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Lobby" }).click();
  await expectHome(ada.page);
  await goToChats(ada.page);

  await ada.close();
  await grace.close();
  await linus.close();
});
