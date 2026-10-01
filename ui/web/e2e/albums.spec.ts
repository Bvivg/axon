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

test("photos sent together arrive as one album that the viewer pages through", async ({ browser }) => {
  const ada = await newPerson(browser, "album", "Ada");
  const grace = await newPerson(browser, "album-peer", "Grace");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "holiday pictures");

  const photos = await drawnPhotos(ada.page, 4);
  const chooser = ada.page.waitForEvent("filechooser");
  await ada.page.getByRole("button", { name: "Attach" }).click();
  await ada.page.getByRole("menuitem", { name: "Photo or video" }).click();
  await (await chooser).setFiles(
    photos.map((buffer, i) => ({ name: `beach-${i + 1}.jpg`, mimeType: "image/jpeg", buffer })),
  );
  const send = ada.page.getByRole("button", { name: "Send", exact: true });
  await expect(send).toBeEnabled({ timeout: 60_000 });
  await ada.page.getByLabel("Message", { exact: true }).fill("the beach");
  await send.click();

  await openChatWith(grace.page, "Ada");
  const album = grace.page.getByRole("group", { name: "Album of 4" });
  await expect(album).toBeVisible();
  await expect(grace.page.getByRole("article", { name: "Message from Ada" }).filter({ has: album })).toContainText(
    "the beach",
  );

  await album.getByRole("button", { name: "Open photo 1 of 4" }).click();
  const counter = grace.page.locator(".yarl__counter");
  await expect(counter).toHaveText("1 / 4");
  await grace.page.keyboard.press("ArrowRight");
  await expect(counter).toHaveText("2 / 4");
  await grace.page.keyboard.press("Escape");
  await expect(counter).toHaveCount(0);
});
