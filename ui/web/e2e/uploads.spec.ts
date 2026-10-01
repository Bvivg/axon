import { expect, test, type Page } from "@playwright/test";

import { messageWith, openChatWith, startChat } from "./chat-support";
import { newPerson } from "./support";

async function attachFile(page: Page, name: string, size: number): Promise<void> {
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Attach" }).click();
  await page.getByRole("menuitem", { name: "File" }).click();
  await (await chooser).setFiles({ name, mimeType: "application/octet-stream", buffer: Buffer.alloc(size, 7) });
}

test("a file bigger than one part goes up in parts and reaches the other person", async ({ browser }) => {
  const ada = await newPerson(browser, "parts", "Ada");
  const grace = await newPerson(browser, "parts-peer", "Grace");
  await startChat(ada.page, { email: grace.email, name: "Grace" }, "sending a big one");

  const parts = new Set<string>();
  ada.page.on("request", (request) => {
    const match = /\/api\/uploads\/[0-9a-f-]{36}\/parts\/(\d+)$/.exec(request.url());
    if (match && request.method() === "PUT") parts.add(match[1]);
  });

  await attachFile(ada.page, "backup.bin", 12 * 1024 * 1024);
  const send = ada.page.getByRole("button", { name: "Send", exact: true });
  await expect(send).toBeEnabled({ timeout: 60_000 });
  expect([...parts].sort()).toEqual(["1", "2"]);
  await send.click();

  await openChatWith(grace.page, "Ada");
  await expect(messageWith(grace.page, "backup.bin")).toBeVisible();
  await expect(messageWith(grace.page, "backup.bin")).toContainText("12");
});

test("removing an attachment before sending withdraws the upload", async ({ browser }) => {
  const ada = await newPerson(browser, "withdraw", "Ada");
  const grace = await newPerson(browser, "withdraw-peer", "Grace");
  await startChat(ada.page, { email: grace.email, name: "Grace" }, "never mind");

  await attachFile(ada.page, "draft.bin", 64 * 1024);
  await expect(ada.page.getByRole("button", { name: "Send", exact: true })).toBeEnabled({ timeout: 30_000 });

  const withdrawn = ada.page.waitForResponse(
    (response) => response.request().method() === "DELETE" && /\/api\/uploads\/[0-9a-f-]{36}$/.test(response.url()),
  );
  await ada.page.getByRole("button", { name: "Remove draft.bin" }).click();
  expect((await withdrawn).status()).toBe(204);
  await expect(ada.page.getByRole("list", { name: "Attachments" })).toHaveCount(0);
});
