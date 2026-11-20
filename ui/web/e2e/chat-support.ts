import { expect, type Page } from "@playwright/test";

import { expectLobby } from "./support";

export async function startCallFromChat(page: Page, mode: "Audio call" | "Video call") {
  await page.getByRole("button", { name: "Start call", exact: true }).click();
  await page.getByRole("menuitem", { name: mode, exact: true }).click();
}

export interface Contact {
  email: string;
  username: string;
  name: string;
}

export function chatAt(address: string): RegExp {
  return new RegExp(`/chat#${address}$`);
}

export async function goToChats(page: Page): Promise<void> {
  const nav = page.getByRole("navigation", { name: "Primary", exact: true });
  const back = page.getByRole("link", { name: "Back to chats", exact: true });
  await expect(nav.or(back).first()).toBeVisible();
  if (await nav.isVisible()) await nav.getByRole("link", { name: "Chats", exact: true }).click();
  else await back.click();
  await expectLobby(page);
}

export function chatList(page: Page) {
  return page.getByRole("complementary", { name: "Chats" });
}

export async function startChat(page: Page, contact: Contact, first: string): Promise<string> {
  await goToChats(page);
  await page.getByRole("button", { name: "New chat" }).click();

  const composer = chatList(page).getByRole("region", { name: "New conversation" });
  await composer.getByLabel("Search people").fill(contact.email);
  await composer.getByRole("button", { name: contact.name }).click();

  await expect(page).toHaveURL(chatAt(contact.username));
  await expect(page.getByRole("heading", { name: contact.name })).toBeVisible();

  await say(page, first);
  await expectSaid(page, "You", first);
  await expect(page).toHaveURL(chatAt(contact.username));

  const room = chatList(page).locator(`a[href="/chat#${contact.username}"]`);
  await expect(room).toHaveAttribute("data-room-id", /^[0-9a-f-]{36}$/);
  return (await room.getAttribute("data-room-id"))!;
}

export async function openChatWith(page: Page, name: string): Promise<void> {
  await goToChats(page);
  await chatList(page).getByRole("link", { name: new RegExp(name) }).click();

  await expect(page).toHaveURL(/\/chat#.+$/);
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
  const label = author === "You" ? "Your message" : `Message from ${author}`;
  const message = page.getByRole("article", { name: label, exact: true }).filter({ hasText: body });
  await expect(message).toBeVisible();
  await expect(message.getByText(body, { exact: true })).toBeVisible();
}

export function messageWith(page: Page, text: string) {
  return page.getByRole("article").filter({ hasText: text });
}

export async function openMessageActions(page: Page, text: string): Promise<void> {
  const row = messageWith(page, text).first().locator("xpath=..");
  await row.hover();
  await row.getByRole("button", { name: "Message actions" }).click();
}

export async function chooseMessageAction(page: Page, text: string, action: string): Promise<void> {
  await openMessageActions(page, text);
  await page.getByRole("menuitem", { name: action }).click();
}

export async function chooseChatAction(page: Page, action: string): Promise<void> {
  await page.getByRole("button", { name: "Chat actions" }).click();
  await page.getByRole("menuitem", { name: action }).click();
}

export function systemLine(page: Page, text: string) {
  return page.getByText(text, { exact: true });
}
