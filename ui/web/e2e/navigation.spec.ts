import { expect, test } from "@playwright/test";

import { newPerson } from "./support";

test("four sections keep their order through list, lobby, match and account navigation", async ({ browser }, testInfo) => {
  const user = await newPerson(browser, "four-sections", "Navigation User");
  const page = user.page;
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  try {
    await expect(page).toHaveURL(/\/chat$/);
    for (const width of [1440, 768, 720, 390, 320]) {
      await page.setViewportSize({ width, height: 844 });
      const nav = page.getByRole("navigation", { name: "Primary", exact: true });
      await expect(nav.getByRole("link")).toHaveCount(4);
      expect(await nav.getByRole("link").evaluateAll(links => links.map(link => link.getAttribute("aria-label")))).toEqual(["Games", "Calls", "Chats", "Profile"]);
      await nav.getByRole("link", { name: "Games", exact: true }).click();
      await expect(page).toHaveURL(/\/games$/);
      const catalogue = page.getByRole("navigation", { name: "Game catalogue", exact: true });
      await expect(catalogue).toBeVisible();
      await catalogue.getByRole("link", { name: "Tic-Tac-Toe lobby", exact: true }).click();
      await expect(page).toHaveURL(/\/games\/tic-tac-toe$/);
      await expect(page.getByRole("button", { name: "Start a game", exact: true })).toBeVisible();
      if (width <= 720) await page.getByRole("link", { name: "Back to games", exact: true }).click();
      else await nav.getByRole("link", { name: "Games", exact: true }).click();
      await expect(catalogue).toBeVisible();
      await nav.getByRole("link", { name: "Profile", exact: true }).click();
      const profileNav = page.getByRole("navigation", { name: "Profile", exact: true });
      await expect(profileNav.getByRole("link", { name: "My profile", exact: true })).toBeVisible();
      const settings = await profileNav.getByRole("link").evaluateAll(links => links.map(link => {
        const rect = link.getBoundingClientRect();
        return { x: rect.x, y: rect.y, width: rect.width };
      }));
      expect(settings.length).toBeGreaterThanOrEqual(5);
      expect(new Set(settings.map(row => row.x)).size).toBe(1);
      expect(settings.every((row, i) => !i || row.y > settings[i - 1].y)).toBe(true);
      await profileNav.getByRole("link", { name: "My profile", exact: true }).click();
      await expect(page).toHaveURL(/\/profile\?view=account$/);
      await expect(page.getByRole("heading", { name: "My profile", exact: true })).toBeVisible();
      await nav.getByRole("link", { name: "Calls", exact: true }).click();
      const launch = page.getByRole("button", { name: "New call", exact: true });
      await expect(launch).toBeEnabled();
      await launch.click();
      await expect(page.getByRole("dialog", { name: "New call", exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Cancel", exact: true }).click();
      await nav.getByRole("link", { name: "Chats", exact: true }).click();
      await expect(nav.getByRole("link", { name: "Chats", exact: true })).toHaveAttribute("aria-current", "page");
      await expect(page.getByRole("navigation", { name: "Chat tools", exact: true })).toHaveCount(0);
      await expect(page.getByRole("button", { name: "Open command palette", exact: true })).toHaveCount(0);
      expect(await page.locator(".workspace-content").evaluate(content => content.getBoundingClientRect().top)).toBe(0);
      await expect(page.getByRole("navigation", { name: "Open pages", exact: true })).toHaveCount(0);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
      if (width <= 720) {
        const glass = await page.locator(".workspace-rail").evaluate(rail => {
          const style = getComputedStyle(rail);
          const rect = rail.getBoundingClientRect();
          return { blur: style.backdropFilter, bottom: rect.bottom, height: innerHeight };
        });
        expect(glass.blur).toContain("blur(");
        expect(glass.bottom).toBeLessThan(glass.height);
        expect(glass.bottom).toBeGreaterThan(glass.height - 60);
      }
      if (width === 1440 || width === 390) await page.screenshot({ path: testInfo.outputPath(`navigation-${width}.png`), animations: "disabled" });
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole("navigation", { name: "Primary", exact: true }).getByRole("link", { name: "Games", exact: true }).click();
    await page.getByRole("link", { name: "Tic-Tac-Toe lobby", exact: true }).click();
    await page.getByRole("button", { name: "Start a game", exact: true }).click();
    await page.getByRole("button", { name: /Private game/ }).click();
    await expect(page).toHaveURL(/\/games\/tic-tac-toe\/[0-9a-f-]+$/);
    await expect(page.locator("[data-game-status]")).toContainText("Waiting for opponent");
    await page.getByRole("link", { name: "Back to lobby", exact: true }).click();
    await expect(page).toHaveURL(/\/games\/tic-tac-toe$/);
    expect(errors).toEqual([]);
  } finally {
    await user.close();
  }
});
