import { createHash } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";

import { messageWith, openChatWith, startChat } from "./chat-support";
import { gatewayUrl, newEmail, newPerson, newUsername, password } from "./support";

const startResponse = (page: Page) => page.waitForResponse((response) => response.request().method() === "POST" && response.url().endsWith("/api/uploads"));
const attachments = (page: Page) => page.getByRole("dialog", { name: /^1 (media|file)$/ });

async function attach(page: Page, name: string, buffer: Buffer, as: "File" | "Photo or video" = "File", mimeType = "application/octet-stream") {
  await expect(attachments(page)).toBeHidden();
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Attach", exact: true }).click();
  await page.getByRole("menuitem", { name: as }).click();
  await (await chooser).setFiles({ name, mimeType, buffer });
  await expect(attachments(page).getByRole("button", { name: "Send", exact: true })).toBeEnabled({ timeout: 30_000 });
}

async function sendAttachment(page: Page) {
  const dialog = attachments(page);
  await dialog.getByRole("button", { name: "Send", exact: true }).click();
  await expect(dialog).toBeHidden();
}

test("a repeated file skips byte upload, preserves its name and is reusable only by people with access", async ({ browser }, testInfo) => {
  const ada = await newPerson(browser, "dedup", "Ada");
  const grace = await newPerson(browser, "dedup-peer", "Grace");
  const outsider = await newPerson(browser, "dedup-other", "Other");
  await startChat(ada.page, { ...grace, name: "Grace" }, "file preflight");
  await startChat(outsider.page, { ...ada, name: "Ada" }, "separate conversation");
  const body = Buffer.from(`shared bytes ${newEmail("content")}`.repeat(300));
  let parts = 0;
  let completes = 0;
  ada.page.on("request", (req) => {
    if (req.method() === "PUT" && /\/parts\/\d+$/.test(req.url())) parts += 1;
    if (req.method() === "POST" && /\/api\/uploads\/[^/]+\/complete$/.test(req.url())) completes += 1;
  });
  const initial = startResponse(ada.page);
  await attach(ada.page, "original.txt", body);
  const firstResponse = await initial;
  expect(firstResponse.request().postDataJSON().source_sha256).toBe(createHash("sha256").update(body).digest("hex"));
  expect((await firstResponse.json()).status).toBe("uploading");
  expect(parts).toBe(1);
  expect(completes).toBe(1);
  await sendAttachment(ada.page);
  const link = messageWith(ada.page, "original.txt").getByRole("link", { name: "Download original.txt" });
  await expect(link).toBeVisible();
  const downloadURL = new URL((await link.getAttribute("href"))!, ada.page.url());

  const repeat = startResponse(ada.page);
  await attach(ada.page, "renamed.txt", body);
  const reused = await (await repeat).json();
  expect(reused.status).toBe("ready");
  const storedKey = new URL(reused.upload.payload.url).pathname.split("/chat-attachments/")[1];
  expect(downloadURL.pathname).toBe(`/api/media/${storedKey}`);
  expect(reused.upload.payload.filename).toBe("renamed.txt");
  expect(parts).toBe(1);
  expect(completes).toBe(1);
  const withdrawn = ada.page.waitForResponse((response) => response.request().method() === "DELETE" && response.url().endsWith(`/api/uploads/${reused.id}`));
  await attachments(ada.page).getByRole("button", { name: "Remove renamed.txt" }).click();
  expect((await withdrawn).status()).toBe(204);
  expect((await ada.page.request.get(downloadURL.href)).status()).toBe(200);

  await openChatWith(grace.page, "Ada");
  let receivedParts = 0;
  grace.page.on("request", (req) => { if (req.method() === "PUT" && /\/parts\/\d+$/.test(req.url())) receivedParts += 1; });
  const recipient = startResponse(grace.page);
  await attach(grace.page, "received.txt", body);
  expect((await (await recipient).json()).status).toBe("ready");
  expect(receivedParts).toBe(0);
  await sendAttachment(grace.page);
  await expect(messageWith(ada.page, "received.txt")).toBeVisible();
  await ada.page.screenshot({ path: testInfo.outputPath("reused-file.png") });

  const hidden = startResponse(outsider.page);
  await attach(outsider.page, "private.txt", body);
  const unavailable = await (await hidden).json();
  expect(unavailable.status).toBe("uploading");
  expect(unavailable.upload).toBeUndefined();
  await ada.close(); await grace.close(); await outsider.close();
});

test("a repeated photo reuses the processed object before upload", async ({ browser }) => {
  const ada = await newPerson(browser, "dedup-photo", "Ada");
  const grace = await newPerson(browser, "dedup-photo-peer", "Grace");
  await startChat(ada.page, { ...grace, name: "Grace" }, "photo preflight");
  const data = await ada.page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = 64; canvas.height = 64;
    const ctx = canvas.getContext("2d")!;
    ctx.fillStyle = "#34a8de"; ctx.fillRect(0, 0, 64, 64);
    return canvas.toDataURL("image/png");
  });
  const bytes = Buffer.from(data.split(",")[1], "base64");
  let parts = 0;
  ada.page.on("request", (req) => { if (req.method() === "PUT" && /\/parts\/\d+$/.test(req.url())) parts += 1; });
  await attach(ada.page, "first.png", bytes, "Photo or video", "image/png");
  await sendAttachment(ada.page);
  await expect(ada.page.getByRole("button", { name: "Open photo" })).toBeVisible();
  expect(parts).toBe(1);
  const repeat = startResponse(ada.page);
  await attach(ada.page, "second.png", bytes, "Photo or video", "image/png");
  expect((await (await repeat).json()).status).toBe("ready");
  expect(parts).toBe(1);
  await ada.close(); await grace.close();
});

test("a false checksum is rejected without poisoning the file catalogue", async ({ request }) => {
  const registered = await request.post(`${gatewayUrl}/axon.auth.v1.AuthService/Register`, {
    headers: { "Connect-Protocol-Version": "1" },
    data: { email: newEmail("hash-mismatch"), username: newUsername("checksum"), password },
  });
  expect(registered.status()).toBe(200);
  const account = await registered.json();
  const headers = { Authorization: `Bearer ${account.tokens.accessToken}` };
  const bytes = Buffer.from(newEmail("claimed-content"));
  const hash = createHash("sha256").update(bytes).digest("hex");
  const invalid = await request.post(`${gatewayUrl}/api/uploads`, { headers, data: { as: "file", name: "bad.txt", size: bytes.length, source_sha256: "not a hash" } });
  expect(invalid.status()).toBe(400);
  const start = await request.post(`${gatewayUrl}/api/uploads`, { headers, data: { as: "file", name: "wrong.txt", size: bytes.length, source_sha256: hash } });
  expect(start.status()).toBe(201);
  const session = await start.json();
  expect((await request.put(`${gatewayUrl}/api/uploads/${session.id}/parts/1`, { headers, data: Buffer.alloc(bytes.length, 9) })).status()).toBe(204);
  expect((await request.post(`${gatewayUrl}/api/uploads/${session.id}/complete`, { headers })).status()).toBe(202);
  let state;
  do {
    state = await (await request.get(`${gatewayUrl}/api/uploads/${session.id}?wait=1`, { headers })).json();
  } while (state.status === "processing");
  expect(state.status).toBe("failed");
  expect(state.failure).toBe("hash_mismatch");
  const genuine = await request.post(`${gatewayUrl}/api/uploads`, { headers, data: { as: "file", name: "genuine.txt", size: bytes.length, source_sha256: hash } });
  const fresh = await genuine.json();
  expect(fresh.status).toBe("uploading");
  await request.delete(`${gatewayUrl}/api/uploads/${session.id}`, { headers });
  await request.delete(`${gatewayUrl}/api/uploads/${fresh.id}`, { headers });
});
