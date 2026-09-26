import { expect, type Page } from "@playwright/test";

import { expectLobby } from "./support";

export interface Contact {
  email: string;
  name: string;
}

export async function goToChats(page: Page): Promise<void> {
  await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Chats" }).click();
  await expectLobby(page);
}

export function chatList(page: Page) {
  return page.getByRole("complementary", { name: "Chats" });
}

export async function startChat(page: Page, contact: Contact, first: string): Promise<string> {
  await goToChats(page);
  await page.getByRole("button", { name: "New chat" }).click();

  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Search").fill(contact.email);
  await dialog.getByRole("button", { name: contact.name }).click();

  await expect(page).toHaveURL(/\/chat\/new\/[0-9a-f-]{36}$/);
  await expect(page.getByRole("heading", { name: contact.name })).toBeVisible();

  await say(page, first);
  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/);
  await expectSaid(page, "You", first);

  return page.url().split("/").pop()!;
}

export async function openChatWith(page: Page, name: string): Promise<void> {
  await goToChats(page);
  await chatList(page).getByRole("link", { name: new RegExp(name) }).click();

  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/);
  await expectReady(page);
}

export async function expectReady(page: Page): Promise<void> {
  await expect(page.getByLabel("Message", { exact: true })).toBeEnabled();
}

export async function say(page: Page, body: string): Promise<void> {
  await page.getByLabel("Message", { exact: true }).fill(body);
  await page.getByRole("button", { name: "Send", exact: true }).click();

  await expect(page.getByLabel("Message", { exact: true })).toHaveValue("");
}

export async function expectSaid(page: Page, author: string, body: string) {
  const message = page.getByRole("article").filter({ hasText: body });
  if (author === "You") {
    await expect(message).toBeVisible();
    await expect(message.getByText(body, { exact: true })).toBeVisible();
    return;
  }
  await expect(message.getByText(author, { exact: true })).toBeVisible();
}
