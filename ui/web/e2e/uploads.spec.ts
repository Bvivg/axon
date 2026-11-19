import { expect, test, type Page } from "@playwright/test";

import { messageWith, openChatWith, startChat } from "./chat-support";
import { newPerson } from "./support";

async function attachFile(page: Page, name: string, size: number, trigger = "Attach"): Promise<void> {
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: trigger, exact: true }).click();
  await page.getByRole("menuitem", { name: "File" }).click();
  await (await chooser).setFiles({ name, mimeType: "application/octet-stream", buffer: Buffer.alloc(size, 7) });
}

test("a multipart file and a second file reach the other person in one captioned message", async ({ browser }) => {
  const ada = await newPerson(browser, "parts", "Ada");
  const grace = await newPerson(browser, "parts-peer", "Grace");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "sending a big one");

  const parts = new Set<string>();
  ada.page.on("request", (request) => {
    const match = /\/api\/uploads\/[0-9a-f-]{36}\/parts\/(\d+)$/.exec(request.url());
    if (match && request.method() === "PUT") parts.add(match[1]);
  });

  await attachFile(ada.page, "backup.bin", 12 * 1024 * 1024);
  const send = ada.page.getByRole("button", { name: "Send", exact: true });
  await expect(send).toBeEnabled({ timeout: 60_000 });
  expect([...parts].sort()).toEqual(["1", "2"]);
  await attachFile(ada.page, "readme.bin", 1024, "Add more");
  await ada.page.getByRole("dialog").getByLabel("Caption").fill("Backup and readme");
  await expect(send).toBeEnabled({ timeout: 60_000 });
  await send.click();

  await openChatWith(grace.page, "Ada");
  const message = messageWith(grace.page, "backup.bin");
  await expect(message.getByRole("link", { name: "Download readme.bin" })).toBeVisible();
  await expect(message).toContainText("Backup and readme");
  await expect(message).toContainText("12");
  await expect(message.locator("time")).toHaveCount(1);
  const download = message.getByRole("link", { name: "Download readme.bin" });
  const response = await grace.page.request.get((await download.getAttribute("href"))!);
  expect(response.ok()).toBeTruthy();
  expect(await response.body()).toEqual(Buffer.alloc(1024, 7));

  await grace.page.getByRole("button", { name: "Chat actions" }).click();
  await grace.page.getByRole("menuitem", { name: "Media", exact: true }).click();
  const panel = grace.page.getByRole("dialog", { name: "Media", exact: true });
  await expect(panel.getByRole("heading", { name: "No photos or videos yet" })).toBeVisible();
  await panel.getByRole("tab", { name: "Files", exact: true }).click();
  await expect(panel.getByRole("link", { name: "Download backup.bin" })).toBeVisible();
  await expect(panel.getByRole("link", { name: "Download readme.bin" })).toBeVisible();
});

test("cancelling attachments withdraws the upload and keeps the caption as a draft", async ({ browser }) => {
  const ada = await newPerson(browser, "withdraw", "Ada");
  const grace = await newPerson(browser, "withdraw-peer", "Grace");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "never mind");

  const input = ada.page.getByLabel("Message", { exact: true });
  await input.fill("draft before attaching");
  await attachFile(ada.page, "draft.bin", 64 * 1024);
  const dialog = ada.page.getByRole("dialog", { name: "1 file", exact: true });
  const caption = dialog.getByLabel("Caption");
  await expect(caption).toHaveValue("draft before attaching");
  await caption.fill("keep this draft");
  await caption.press("End");
  await caption.press("Shift+Enter");
  await expect(caption).toHaveValue("keep this draft\n");
  await ada.page.keyboard.insertText("second line");
  await expect(dialog.getByRole("button", { name: "Send", exact: true })).toBeEnabled({ timeout: 30_000 });

  const withdrawn = ada.page.waitForResponse(
    (response) => response.request().method() === "DELETE" && /\/api\/uploads\/[0-9a-f-]{36}$/.test(response.url()),
  );
  await dialog.getByRole("button", { name: "Close attachments", exact: true }).click();
  expect((await withdrawn).status()).toBe(204);
  await expect(dialog).toBeHidden();
  await expect(input).toHaveValue("keep this draft\nsecond line");
});
