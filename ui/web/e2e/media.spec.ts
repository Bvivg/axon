import { fileURLToPath } from "node:url";

import { expect, test, type Page } from "@playwright/test";

import { chatList, goToChats, openChatWith, startChat } from "./chat-support";
import { newPerson } from "./support";

const cameraMake = "AxonTestCam";

const clip = fileURLToPath(new URL("./fixtures/clip.mov", import.meta.url));

async function drawnJpeg(page: Page): Promise<Buffer> {
  const dataUrl = await page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = 640;
    canvas.height = 480;
    const ctx = canvas.getContext("2d")!;
    ctx.fillStyle = "#1d6fe8";
    ctx.fillRect(0, 0, 640, 480);
    ctx.fillStyle = "#ffd23f";
    ctx.fillRect(40, 40, 200, 120);
    return canvas.toDataURL("image/jpeg", 0.9);
  });
  return Buffer.from(dataUrl.split(",")[1], "base64");
}

function withExif(jpeg: Buffer, make: string): Buffer {
  const text = Buffer.from(`${make}\0`, "latin1");
  const tiff = Buffer.alloc(26);
  tiff.write("II", 0, "latin1");
  tiff.writeUInt16LE(42, 2);
  tiff.writeUInt32LE(8, 4);
  tiff.writeUInt16LE(1, 8);
  tiff.writeUInt16LE(0x010f, 10);
  tiff.writeUInt16LE(2, 12);
  tiff.writeUInt32LE(text.length, 14);
  tiff.writeUInt32LE(26, 18);
  tiff.writeUInt32LE(0, 22);

  const body = Buffer.concat([Buffer.from("Exif\0\0", "latin1"), tiff, text]);
  const marker = Buffer.alloc(4);
  marker.writeUInt16BE(0xffe1, 0);
  marker.writeUInt16BE(body.length + 2, 2);

  return Buffer.concat([jpeg.subarray(0, 2), marker, body, jpeg.subarray(2)]);
}

async function attach(
  page: Page,
  item: "Photo or video" | "File",
  files: string | { name: string; mimeType: string; buffer: Buffer },
): Promise<void> {
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Attach" }).click();
  await page.getByRole("menuitem", { name: item }).click();
  await (await chooser).setFiles(files);
}

async function sendAttachments(page: Page, caption = ""): Promise<void> {
  if (caption) {
    await page.getByLabel("Message", { exact: true }).fill(caption);
  }
  const send = page.getByRole("button", { name: "Send", exact: true });
  await expect(send).toBeEnabled({ timeout: 30_000 });
  await send.click();
  await expect(page.getByRole("list", { name: "Attachments" })).toHaveCount(0);
}

test("a photo loses its camera metadata on the way and both people can open it", async ({ browser }) => {
  const ada = await newPerson(browser, "photo", "Ada");
  const grace = await newPerson(browser, "photo-peer", "Grace");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "one sec");

  const photo = withExif(await drawnJpeg(ada.page), cameraMake);
  expect(photo.includes(cameraMake)).toBe(true);

  await attach(ada.page, "Photo or video", { name: "snap.jpg", mimeType: "image/jpeg", buffer: photo });
  await expect(ada.page.getByRole("listitem", { name: "snap.jpg" })).toBeVisible();
  await sendAttachments(ada.page, "look at this");

  await expect(ada.page.getByRole("button", { name: "Open photo: look at this" })).toBeVisible();

  await goToChats(grace.page);
  await expect(chatList(grace.page).getByText("📷 look at this")).toBeVisible();
  await openChatWith(grace.page, "Ada");

  await grace.page.getByRole("button", { name: "Open photo: look at this" }).click();
  const viewer = grace.page.getByRole("dialog", { name: "Photo" });
  await expect(viewer.getByRole("img", { name: "look at this" })).toBeVisible();

  const original = await viewer.getByRole("link", { name: "Open original" }).getAttribute("href");
  const served = await grace.page.request.get(original!);
  expect(served.ok()).toBe(true);
  expect(served.headers()["content-type"]).toBe("image/jpeg");
  const bytes = await served.body();
  expect(bytes.subarray(0, 2).toString("hex")).toBe("ffd8");
  expect(bytes.includes(cameraMake)).toBe(false);

  await viewer.getByRole("button", { name: "Close" }).click();
  await expect(viewer).toHaveCount(0);

  await grace.page.getByRole("button", { name: "Open photo: look at this" }).click();
  await viewer.getByRole("img", { name: "look at this" }).click();
  await expect(viewer).toBeVisible();
  const size = grace.page.viewportSize();
  await grace.page.mouse.click(4, (size?.height ?? 600) / 2);
  await expect(viewer).toHaveCount(0);

  await ada.close();
  await grace.close();
});

test("a video is re-encoded for the web and plays in the viewer", async ({ browser }) => {
  const ada = await newPerson(browser, "video", "Ada");
  const grace = await newPerson(browser, "video-peer", "Grace");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "watch this");

  await attach(ada.page, "Photo or video", clip);
  await sendAttachments(ada.page);

  await openChatWith(grace.page, "Ada");
  await grace.page.getByRole("button", { name: "Play video" }).click();

  const viewer = grace.page.getByRole("dialog", { name: "Video" });
  const video = viewer.locator("video");
  await expect(video).toBeVisible();

  const src = await video.getAttribute("src");
  expect(src).toMatch(/\.mp4$/);
  const served = await grace.page.request.get(src!);
  expect(served.headers()["content-type"]).toBe("video/mp4");
  expect(await video.getAttribute("poster")).toMatch(/\.jpg$/);

  await ada.close();
  await grace.close();
});

test("files arrive byte for byte, voice messages play, and the Media panel keeps them apart", async ({
  browser,
}) => {
  const ada = await newPerson(browser, "files", "Ada");
  const grace = await newPerson(browser, "files-peer", "Grace");

  await startChat(ada.page, { email: grace.email, name: "Grace" }, "sending the scan");

  const scan = withExif(await drawnJpeg(ada.page), cameraMake);
  await attach(ada.page, "File", { name: "scan.jpg", mimeType: "image/jpeg", buffer: scan });
  await sendAttachments(ada.page);

  await ada.page.getByRole("button", { name: "Record voice message" }).click();
  await expect(ada.page.getByText(/Recording 0:0[1-9]/)).toBeVisible();
  await ada.page.getByRole("button", { name: "Send voice message" }).click();
  await expect(ada.page.getByRole("button", { name: "Play voice message" })).toBeVisible({ timeout: 30_000 });

  await openChatWith(grace.page, "Ada");
  const download = grace.page.getByRole("link", { name: "Download scan.jpg" });
  await expect(download).toBeVisible();
  const served = await grace.page.request.get((await download.getAttribute("href"))!);
  expect(served.headers()["content-disposition"]).toContain("attachment");
  expect(Buffer.compare(await served.body(), scan)).toBe(0);

  await expect(grace.page.getByRole("button", { name: "Play voice message" })).toBeVisible();

  await grace.page.getByRole("button", { name: "Media", exact: true }).click();
  const panel = grace.page.getByRole("dialog", { name: "Media" });
  await expect(panel.getByRole("heading", { name: "No photos or videos yet" })).toBeVisible();

  await panel.getByRole("tab", { name: "Files" }).click();
  await expect(panel.getByRole("link", { name: "Download scan.jpg" })).toBeVisible();

  await panel.getByRole("tab", { name: "Voice" }).click();
  await expect(panel.getByRole("button", { name: "Play voice message" })).toBeVisible();

  await ada.close();
  await grace.close();
});
