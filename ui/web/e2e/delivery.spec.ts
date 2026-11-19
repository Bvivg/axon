import { expect, test } from "@playwright/test";

import { expectReady, expectSaid, messageWith, openChatWith, say, startChat } from "./chat-support";
import { newPerson } from "./support";

test("pending bubbles become confirmed without a gap or duplicate in either acknowledgement order", async ({ browser }, testInfo) => {
  const sender = await newPerson(browser, "delivery-order", "Delivery Author");
  const receiver = await newPerson(browser, "delivery-peer", "Delivery Peer");
  const page = sender.page;
  const bodies = new Map<string, string>();
  const held: { type: "ack" | "message"; clientID: string; release: () => void }[] = [];
  try {
    await startChat(page, { ...receiver, name: "Delivery Peer" }, "Delivery baseline");
    await openChatWith(receiver.page, "Delivery Author");
    await page.routeWebSocket("**/ws/chat", socket => {
      const server = socket.connectToServer();
      socket.onMessage(raw => {
        const frame = JSON.parse(String(raw));
        if (frame.type === "send") bodies.set(frame.client_id, frame.body);
        server.send(raw);
      });
      server.onMessage(raw => {
        const frame = JSON.parse(String(raw));
        const clientID = frame.type === "ack" ? frame.client_id : frame.type === "message" ? frame.message.client_id : undefined;
        if (clientID && bodies.has(clientID) && (frame.type === "ack" || frame.type === "message")) {
          held.push({ type: frame.type, clientID, release: () => socket.send(raw) });
        } else socket.send(raw);
      });
    });
    await page.reload();
    await page.setViewportSize({ width: 390, height: 844 });
    await expectReady(page);
    for (const [body, first] of [["Waiting for acknowledgement", "ack"], ["Waiting for the message event", "message"]] as const) {
      await say(page, body);
      const bubble = page.getByRole("article", { name: "Your message", exact: true }).filter({ hasText: body });
      await expect(bubble).toHaveCount(1);
      await expect(bubble.getByRole("img", { name: "Sending", exact: true })).toBeVisible();
      await expect(bubble.getByText(/^(Sent|Read)$/)).toHaveCount(0);
      await expect(page.getByRole("list", { name: "Outgoing messages", exact: true })).toHaveCount(0);
      await expect(bubble.locator("..").getByRole("button", { name: "Message actions", exact: true })).toHaveCount(0);
      await expect.poll(() => [...bodies.values()].includes(body)).toBe(true);
      const id = [...bodies.entries()].find(([, value]) => value === body)![0];
      await expect.poll(() => held.filter(item => item.clientID === id && item.type === "ack").length).toBeGreaterThan(0);
      await expect.poll(() => held.filter(item => item.clientID === id && item.type === "message").length).toBeGreaterThan(0);
      await expectSaid(receiver.page, "Delivery Author", body);
      await page.screenshot({ path: testInfo.outputPath(`${first}-pending.png`), animations: "disabled" });
      const release = (type: "ack" | "message") => {
        for (let i = held.length - 1; i >= 0; i--) {
          if (held[i].clientID === id && held[i].type === type) held.splice(i, 1)[0].release();
        }
      };
      release(first);
      await expect(bubble).toHaveCount(1);
      await expect(bubble.getByRole("img", { name: "Sending", exact: true })).toHaveCount(0);
      await expect(bubble.getByText(/^(Sent|Read)$/)).toHaveCount(1);
      await page.screenshot({ path: testInfo.outputPath(`${first}-confirmed.png`), animations: "disabled" });
      release(first === "ack" ? "message" : "ack");
      await expect(bubble).toHaveCount(1);
      await expect(bubble.getByText(/^(Sent|Read)$/)).toHaveCount(1);
      await expect.poll(() => page.locator('[data-id^="pending-"]').count()).toBe(0);
    }
    await page.reload();
    for (const body of ["Waiting for acknowledgement", "Waiting for the message event"]) {
      await expect(messageWith(page, body)).toHaveCount(1);
    }
  } finally {
    await Promise.all([sender.close(), receiver.close()]);
  }
});

test("a disconnected pending bubble retries the same client ID and is delivered once", async ({ browser }) => {
  const sender = await newPerson(browser, "delivery-retry", "Retry Author");
  const receiver = await newPerson(browser, "retry-peer", "Retry Peer");
  const page = sender.page;
  const ids: string[] = [];
  const body = "Retry this pending bubble";
  try {
    await startChat(page, { ...receiver, name: "Retry Peer" }, "Retry baseline");
    await openChatWith(receiver.page, "Retry Author");
    await page.routeWebSocket("**/ws/chat", socket => {
      const server = socket.connectToServer();
      socket.onMessage(raw => {
        const frame = JSON.parse(String(raw));
        if (frame.type === "send" && frame.body === body) {
          ids.push(frame.client_id);
          if (ids.length === 1) {
            void socket.close({ code: 1012, reason: "Connection interrupted" });
            return;
          }
        }
        server.send(raw);
      });
    });
    await page.reload();
    await say(page, body);
    const bubble = page.getByRole("article", { name: "Your message", exact: true }).filter({ hasText: body });
    await expect(bubble.getByRole("img", { name: "Delivery unconfirmed", exact: true })).toBeVisible();
    await expect(bubble.getByText(/^(Sent|Read)$/)).toHaveCount(0);
    await bubble.getByRole("button", { name: "Retry", exact: true }).click();
    await expectSaid(receiver.page, "Retry Author", body);
    await expect(bubble).toHaveCount(1);
    await expect(bubble.getByText(/^(Sent|Read)$/)).toHaveCount(1);
    expect(ids).toHaveLength(2);
    expect(new Set(ids).size).toBe(1);
    await expect(receiver.page.getByRole("article", { name: "Message from Retry Author", exact: true }).filter({ hasText: body })).toHaveCount(1);
  } finally {
    await Promise.all([sender.close(), receiver.close()]);
  }
});
