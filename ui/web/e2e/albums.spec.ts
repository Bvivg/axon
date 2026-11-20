import { expect, test, type Page } from "@playwright/test";

import { openChatWith, startChat } from "./chat-support";
import { newPerson } from "./support";

async function drawnPhotos(page: Page, count: number): Promise<Buffer[]> {
  const encoded = await page.evaluate((n) => {
    const out: string[] = [];
    for (let i = 0; i < n; i += 1) {
      const canvas = document.createElement("canvas");
      canvas.width = 480 + i * 40;
      canvas.height = 360;
      const ctx = canvas.getContext("2d")!;
      ctx.fillStyle = `hsl(${i * 70}, 70%, 50%)`;
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      out.push(canvas.toDataURL("image/jpeg", 0.9).split(",")[1]);
    }
    return out;
  }, count);
  return encoded.map((b64) => Buffer.from(b64, "base64"));
}

test("Enter sends the captioned album and file together, and the album can be paged through", async ({ browser }) => {
  const ada = await newPerson(browser, "album", "Ada");
  const grace = await newPerson(browser, "album-peer", "Grace");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "holiday pictures");
  await openChatWith(grace.page, "Ada");

  const photos = await drawnPhotos(ada.page, 4);
  const files = photos.map((buffer, i) => ({ name: `beach-${i + 1}.jpg`, mimeType: "image/jpeg", buffer }));
  const chooser = ada.page.waitForEvent("filechooser");
  await ada.page.getByRole("button", { name: "Attach" }).click();
  await ada.page.getByRole("menuitem", { name: "Photo or video" }).click();
  await (await chooser).setFiles(files.slice(0, 3));
  const dialog = ada.page.getByRole("dialog", { name: /^\d+ (media|attachments)$/ });
  await expect(dialog.getByRole("listitem")).toHaveCount(3);
  const addMore = ada.page.waitForEvent("filechooser");
  await dialog.getByRole("button", { name: "Add more" }).click();
  await ada.page.getByRole("menuitem", { name: "Photo or video" }).click();
  await (await addMore).setFiles(files.slice(3));
  await expect(dialog.getByRole("listitem")).toHaveCount(4);

  const fileChooser = ada.page.waitForEvent("filechooser");
  await dialog.getByRole("button", { name: "Add more" }).click();
  await ada.page.getByRole("menuitem", { name: "File", exact: true }).click();
  await (await fileChooser).setFiles({ name: "beach-notes.txt", mimeType: "text/plain", buffer: Buffer.from("Beach notes") });
  await expect(dialog.getByRole("listitem")).toHaveCount(5);

  const send = dialog.getByRole("button", { name: "Send", exact: true });
  await expect(send).toBeEnabled({ timeout: 60_000 });
  const caption = dialog.getByLabel("Caption");
  await caption.fill("228");
  await expect(ada.page.locator('textarea[aria-label="Message"]')).toHaveValue("");
  await caption.press("Enter");
  await expect(dialog).toBeHidden();

  const album = grace.page.getByRole("group", { name: "Album of 4" });
  const received = grace.page.getByRole("article", { name: "Message from Ada" }).filter({ has: album });
  const download = received.getByRole("link", { name: "Download beach-notes.txt" });
  await expect(received).toContainText("228");
  await expect(download).toBeVisible();
  await expect(received.locator("time")).toHaveCount(1);

  await grace.page.reload();
  await expect(received).toContainText("228");
  await expect(download).toBeVisible();

  await album.getByRole("button", { name: "Open photo 1 of 4" }).click();
  const counter = grace.page.locator(".yarl__counter");
  await expect(counter).toHaveText("1 / 4");
  await grace.page.keyboard.press("ArrowRight");
  await expect(counter).toHaveText("2 / 4");
  await grace.page.keyboard.press("Escape");
  await expect(counter).toHaveCount(0);

  await grace.page.getByRole("button", { name: "Chat actions" }).click();
  await grace.page.getByRole("menuitem", { name: "Media", exact: true }).click();
  const panel = grace.page.getByRole("dialog", { name: "Media", exact: true });
  await panel.getByRole("tab", { name: "Files", exact: true }).click();
  await expect(panel.getByRole("link", { name: "Download beach-notes.txt" })).toBeVisible();
});
