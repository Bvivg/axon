import { expect, test } from "@playwright/test";

import { chatAt, expectSaid, messageWith, startCallFromChat, startChat } from "./chat-support";
import { newPerson } from "./support";

test("workspace navigation, palette focus, and persistent themes at desktop, tablet, and phone widths", async ({ browser }, testInfo) => {
  const user = await newPerson(browser, "design-workspace", "Workspace User");
  const page = user.page;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  try {
    const trigger = page.getByRole("navigation", { name: "Primary", exact: true }).getByRole("link", { name: "Chats", exact: true });
    await trigger.focus();
    await trigger.press("ControlOrMeta+k");
    const search = page.getByRole("combobox", { name: "Search commands" });
    await expect(search).toBeFocused();
    await search.press("Escape");
    await expect(trigger).toBeFocused();
    await trigger.press("ControlOrMeta+k");
    await search.fill("devices");
    await search.press("Enter");
    await expect(page).toHaveURL(/\/profile\/sessions$/);
    await page.goto("/admin");
    await expect(page).toHaveURL(/\/chat$/);

    for (const mode of ["light", "dark"] as const) {
      await page.goto("/profile/appearance");
      await page.getByRole("radio", { name: new RegExp(mode, "i") }).press("Space");
      await expect(page.locator("html")).toHaveAttribute("data-theme", mode);
      for (const width of [375, 768, 1440]) {
        await page.setViewportSize({ width, height: 850 });
        const nav = page.getByRole("navigation", { name: "Primary" });
        await expect(nav.getByRole("link")).toHaveText(["Games", "Calls", "Chats", "Profile"]);
        await expect(nav.getByRole("button", { name: "More", exact: true })).toHaveCount(0);
        await nav.getByRole("link", { name: "Chats", exact: true }).click();
        await page.keyboard.press("ControlOrMeta+k");
        await search.fill("Contacts");
        await search.press("Enter");
        await expect(nav.getByRole("link", { name: "Chats", exact: true })).toHaveAttribute("aria-current", "page");
        await expect(page.getByRole("navigation", { name: "Open pages" })).toHaveCount(0);
        await expect(page.locator("#app-content").getByRole("heading", { name: "Contacts", exact: true })).toBeVisible();
        await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
        await expect(page.locator("html")).toHaveAttribute("data-theme", mode);
        if (width === 375 || width === 1440) await page.screenshot({ path: testInfo.outputPath(`contacts-${mode}-${width}.png`), animations: "disabled" });
      }
    }
    await page.goto("/profile/appearance");
    await page.emulateMedia({ colorScheme: "dark" });
    await page.getByRole("radio", { name: /System/i }).press("Space");
    await page.reload();
    await expect(page.getByRole("radio", { name: /System/i })).toBeChecked();
    await expect(page.locator("html")).not.toHaveAttribute("data-theme", /.*/);
    const desktop = await page.context().newCDPSession(page);
    await desktop.send("Emulation.setDeviceMetricsOverride", { width: 720, height: 450, deviceScaleFactor: 2, mobile: false });
    await page.goto("/games");
    await expect(page.getByRole("heading", { name: "Games", exact: true })).toBeVisible();
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(720);
    await desktop.detach();
    expect(errors).toEqual([]);
  } finally {
    await user.close();
  }
});

test("chat restores a draft after navigation and exposes message actions by keyboard", async ({ browser }, testInfo) => {
  const author = await newPerson(browser, "design-chat", "Design Author");
  const reader = await newPerson(browser, "design-peer", "Design Peer");
  const page = author.page;
  try {
    await startChat(page, { ...reader, name: "Design Peer" }, "Design test");
    const draft = page.getByRole("textbox", { name: "Message", exact: true });
    await draft.fill("Unsent text");
    await draft.press("Shift+Enter");
    await expect(draft).toHaveValue("Unsent text\n");
    const actions = messageWith(page, "Design test").locator("xpath=..").getByRole("button", { name: "Message actions" });
    await actions.focus();
    await actions.press("Shift+F10");
    await expect(page.getByRole("menuitem", { name: "Reply" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(actions).toBeFocused();
    await page.keyboard.press("ControlOrMeta+k");
    await page.getByRole("combobox", { name: "Search commands" }).fill("Media");
    await page.getByRole("option", { name: "Media", exact: true }).click();
    await expect(page.getByRole("combobox", { name: "Choose a chat" })).toHaveCount(1);
    const photos = page.getByRole("tab", { name: "Media", exact: true });
    await photos.focus();
    await photos.press("ArrowRight");
    const files = page.getByRole("tab", { name: "Files" });
    await expect(files).toBeFocused();
    await files.press("Enter");
    await expect(files).toHaveAttribute("aria-selected", "true");
    await page.getByRole("link", { name: "Open chat", exact: true }).click();
    await expect(page).toHaveURL(chatAt(reader.username));
    await expect(draft).toHaveValue("Unsent text\n");
    await page.screenshot({ path: testInfo.outputPath("chat-desktop.png"), animations: "disabled" });
    await draft.press("Enter");
    await expectSaid(page, "You", "Unsent text");
  } finally {
    await Promise.all([author.close(), reader.close()]);
  }
});

test("call launcher handles denied capture and returns to the current chat", async ({ browser }) => {
  const author = await newPerson(browser, "design-call", "Design Caller");
  const reader = await newPerson(browser, "design-call-target", "Design Callee");
  const page = author.page;
  try {
    await page.evaluate(() => {
      navigator.mediaDevices.getUserMedia = async () => { throw new DOMException("Access denied", "NotAllowedError"); };
    });
    await startChat(page, { ...reader, name: "Design Callee" }, "Call context");
    await startCallFromChat(page, "Audio call");
    await expect(page.getByRole("alert")).toContainText(/permission|allow|denied|microphone/i);
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(page).toHaveURL(chatAt(reader.username));
    await expectSaid(page, "You", "Call context");
  } finally {
    await Promise.all([author.close(), reader.close()]);
  }
});

test("the calls socket reconnects after its lease is lost and keeps the signed-in session", async ({ browser }) => {
  const user = await newPerson(browser, "design-lease", "Calls Lease");
  const page = user.page;
  let attaches = 0;
  try {
    await page.routeWebSocket("**/ws/chat", socket => {
      const server = socket.connectToServer();
      socket.onMessage(message => {
        if (JSON.parse(String(message)).type === "call_attach" && ++attaches === 1) {
          void socket.close({ code: 4403, reason: "call lease unavailable" });
          return;
        }
        server.send(message);
      });
    });
    await page.reload();
    await expect.poll(() => attaches).toBeGreaterThanOrEqual(2);
    await expect(page.getByRole("heading", { name: "Chats", exact: true })).toBeVisible();
    await page.getByRole("navigation", { name: "Primary" }).getByRole("link", { name: "Chats" }).click();
    await expect(page.getByRole("heading", { name: "Chats", exact: true })).toBeVisible();
  } finally {
    await user.close();
  }
});
