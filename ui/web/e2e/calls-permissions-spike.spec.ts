import { expect, test } from "@playwright/test";
import { evidence } from "./calls-spike-support";

test("microphone permission denied returns native NotAllowedError", async ({ browser }, info) => {
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    await page.goto("http://localhost:3000/login");
    const cdp = await context.newCDPSession(page);
    const { targetInfo } = await cdp.send("Target.getTargetInfo");
    await cdp.send("Browser.setPermission", {
      permission: { name: "microphone" }, setting: "denied", origin: "http://localhost:3000", browserContextId: targetInfo.browserContextId,
    });
    const result = await page.evaluate(async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
        stream.getTracks().forEach(track => track.stop());
        return { name: "unexpected-success" };
      } catch (error) {
        return { name: (error as DOMException).name, secureContext: window.isSecureContext };
      }
    });
    await evidence(info, result);
    expect(result.name).toBe("NotAllowedError");
  } finally {
    await context.close();
  }
});
