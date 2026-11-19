import { expect, test } from "@playwright/test";

import { chatList, expectSaid, openChatWith, startChat } from "./chat-support";
import { newPerson } from "./support";

test("the capsule composer inserts emoji at the cursor and hides mobile navigation only in conversations", async ({ browser }, testInfo) => {
  const author = await newPerson(browser, "capsule-author", "Composer Author");
  const peer = await newPerson(browser, "capsule-peer", "Composer Peer");
  const page = author.page;
  try {
    await page.emulateMedia({ colorScheme: "dark" });
    await startChat(page, { ...peer, name: "Composer Peer" }, "Composer ready");
    await openChatWith(peer.page, "Composer Author");
    const input = page.getByRole("textbox", { name: "Message", exact: true });
    const nav = page.getByRole("navigation", { name: "Primary", exact: true });
    const voice = page.getByRole("button", { name: "Record voice message", exact: true });
    const emoji = page.getByRole("button", { name: "Choose emoji", exact: true });
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 844 });
      if (width <= 720) {
        await expect(nav).toBeHidden();
        expect(await page.locator(".workspace-shell").evaluate(shell => getComputedStyle(shell).paddingBottom)).toBe("0px");
        const composer = await page.locator("[data-chat-composer]").boundingBox();
        expect(composer!.y + composer!.height).toBe(844);
      } else await expect(nav).toBeVisible();
      await expect(voice).toBeVisible();
      await expect(page.getByRole("button", { name: "Send", exact: true })).toHaveCount(0);
      const layout = await page.locator(".chat-composer-row").evaluate(row => {
        const box = (selector: string) => row.querySelector(selector)!.getBoundingClientRect();
        const field = box(".chat-composer-field");
        const attach = box('[aria-label="Attach"]');
        const emoji = box('[aria-label="Choose emoji"]');
        const voice = box('[aria-label="Record voice message"]');
        const input = row.querySelector("textarea")!;
        return { attachRight: attach.right, fieldLeft: field.left, fieldRight: field.right, emojiRight: emoji.right, voiceLeft: voice.left, border: getComputedStyle(input).borderWidth, shadow: getComputedStyle(input).boxShadow };
      });
      expect(layout.attachRight).toBeLessThan(layout.fieldLeft);
      expect(layout.emojiRight).toBeLessThan(layout.fieldRight);
      expect(layout.voiceLeft).toBeGreaterThan(layout.fieldRight);
      expect(layout.border).toBe("0px");
      expect(layout.shadow).toBe("none");
      if (width !== 320) await page.screenshot({ path: testInfo.outputPath(`empty-${width}.png`), animations: "disabled" });
      await input.fill("ab");
      await input.evaluate((field: HTMLTextAreaElement) => field.setSelectionRange(1, 1));
      await emoji.click();
      await page.getByRole("menuitem", { name: "Grinning face", exact: true }).click();
      await expect(input).toHaveValue("a😀b");
      await expect(input).toBeFocused();
      expect(await input.evaluate((field: HTMLTextAreaElement) => field.selectionStart)).toBe(3);
      await expect(voice).toHaveCount(0);
      await expect(page.getByRole("button", { name: "Send", exact: true })).toBeEnabled();
      if (width !== 320) await page.screenshot({ path: testInfo.outputPath(`text-${width}.png`), animations: "disabled" });
      await input.fill("");
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await emoji.click();
    await page.getByRole("menuitem", { name: "Red heart", exact: true }).click();
    await expect(input).toHaveValue("❤️");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await expect(input).toHaveValue("");
    await expectSaid(peer.page, "Composer Author", "❤️");
    await expect(voice).toBeVisible();
    await page.getByRole("button", { name: "Attach", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "File", exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await page.getByRole("link", { name: "Back to chats", exact: true }).click();
    await expect(nav).toBeVisible();
    await expect(page.getByRole("group", { name: "Filter chats", exact: true })).toHaveCount(0);
    await chatList(page).getByRole("link", { name: /Composer Peer/ }).click();
    await expect(nav).toBeHidden();
    await page.setViewportSize({ width: 1440, height: 844 });
    await expect(nav).toBeVisible();
    await page.emulateMedia({ colorScheme: "light" });
    await page.screenshot({ path: testInfo.outputPath("light-desktop.png"), animations: "disabled" });
  } finally {
    await Promise.all([author.close(), peer.close()]);
  }
});
