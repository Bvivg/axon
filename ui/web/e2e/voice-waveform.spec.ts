import { expect, test } from "@playwright/test";

import { openChatWith, startChat } from "./chat-support";
import { newPerson } from "./support";

test("voice recordings show their waveform and keep playback and keyboard seeking on desktop and mobile", async ({ browser }, testInfo) => {
  const author = await newPerson(browser, "wave-author", "Wave Author");
  const peer = await newPerson(browser, "wave-peer", "Wave Peer");
  const page = author.page;
  try {
    await page.emulateMedia({ colorScheme: "dark" });
    await startChat(page, { ...peer, name: "Wave Peer" }, "Voice waveform");
    await page.getByRole("button", { name: "Record voice message", exact: true }).click();
    await expect(page.getByText(/Recording 0:0[1-9]/)).toBeVisible();
    await page.getByRole("button", { name: "Send voice message", exact: true }).click();
    const wave = page.locator(".voice-waveform");
    await expect(wave).toHaveAttribute("data-waveform-ready", "true", { timeout: 30_000 });
    await expect(wave.locator(".voice-waveform-bar")).toHaveCount(48);
    const peaks = await wave.locator(".voice-waveform-bar").evaluateAll(bars => bars.map(bar => bar.getBoundingClientRect().height));
    expect(Math.max(...peaks)).toBeGreaterThan(20);
    const slider = page.getByRole("slider", { name: "Voice message position", exact: true });
    await slider.focus();
    await slider.press("Home");
    await slider.press("ArrowRight");
    await expect(slider).toHaveValue("100");
    await expect.poll(() => wave.locator('[data-played="true"]').count()).toBeGreaterThan(0);
    await slider.press("Home");
    await expect(slider).toHaveValue("0");
    await page.getByRole("button", { name: "Play voice message", exact: true }).click();
    const pause = page.getByRole("button", { name: "Pause voice message", exact: true });
    await expect(pause).toBeVisible();
    await expect.poll(() => page.locator("audio").evaluate((audio: HTMLAudioElement) => audio.currentTime)).toBeGreaterThan(0);
    await pause.click();
    expect(await page.locator("audio").evaluate((audio: HTMLAudioElement) => audio.paused)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath("waveform-desktop.png"), animations: "disabled" });

    await peer.page.setViewportSize({ width: 390, height: 844 });
    await openChatWith(peer.page, "Wave Author");
    const received = peer.page.locator(".voice-waveform");
    await expect(received).toHaveAttribute("data-waveform-ready", "true", { timeout: 30_000 });
    const alignment = await peer.page.locator('article[data-voice="true"]').evaluate(article => {
      const duration = article.querySelector('.voice-duration')!.getBoundingClientRect();
      const sent = article.querySelector('time')!.getBoundingClientRect();
      return Math.abs((duration.top + duration.height / 2) - (sent.top + sent.height / 2));
    });
    expect(alignment).toBeLessThan(4);
    await expect(received.locator(".voice-waveform-bar")).toHaveCount(48);
    expect(await peer.page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
    await expect(peer.page.locator("[data-sonner-toast]")).toHaveCount(0);
    await peer.page.screenshot({ path: testInfo.outputPath("waveform-mobile.png"), animations: "disabled" });

    let blocked = 0;
    await peer.page.route("**/api/media/**", route => {
      if (route.request().resourceType() === "fetch") {
        blocked++;
        return route.fulfill({ status: 503, body: "Waveform unavailable" });
      }
      return route.continue();
    });
    await peer.page.reload();
    await expect.poll(() => blocked).toBeGreaterThan(0);
    await expect(received.locator(".voice-waveform-bar")).toHaveCount(48);
    await peer.page.getByRole("button", { name: "Play voice message", exact: true }).click();
    await expect(peer.page.getByRole("button", { name: "Pause voice message", exact: true })).toBeVisible();
  } finally {
    await Promise.all([author.close(), peer.close()]);
  }
});
