import { expect, test } from "@playwright/test";

import { expectReady, say, startChat } from "./chat-support";
import { newPerson } from "./support";

for (const [label, body] of [
  ["64 KiB ASCII", "abcdefgh".repeat(8192)],
  ["65536 Unicode characters", "я😀".repeat(32768)],
] as const) {
test(`${label} is sent once with compression and survives a reload`, async ({ browser }) => {
  test.slow();
  const ada = await newPerson(browser, "long-message", "Ada");
  const grace = await newPerson(browser, "long-peer", "Grace");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "before the long message");

  const devtools = await ada.page.context().newCDPSession(ada.page);
  await devtools.send("Network.enable");
  const chatSockets = new Set<string>();
  let compression = "";
  devtools.on("Network.webSocketCreated", ({ requestId, url }) => {
    if (url.endsWith("/ws/chat")) chatSockets.add(requestId);
  });
  devtools.on("Network.webSocketHandshakeResponseReceived", ({ requestId, response }) => {
    if (!chatSockets.has(requestId)) return;
    const header = Object.entries(response.headers).find(([name]) => name.toLowerCase() === "sec-websocket-extensions");
    compression = String(header?.[1] ?? "");
  });
  let acknowledgements = 0;
  let sends = 0;
  ada.page.on("websocket", (socket) => {
    socket.on("framesent", ({ payload }) => {
      if (JSON.parse(String(payload)).type === "send") sends += 1;
    });
    socket.on("framereceived", ({ payload }) => {
      const frame = JSON.parse(String(payload));
      if (frame.type === "ack") acknowledgements += 1;
    });
  });
  await ada.page.reload();
  await expectReady(ada.page);
  await expect.poll(() => compression).toContain("permessage-deflate");
  await ada.page.getByLabel("Message", { exact: true }).fill(body);
  await ada.page.getByRole("button", { name: "Send", exact: true }).click();
  await expect.poll(() => acknowledgements).toBe(1);
  expect(sends).toBe(1);

  const history = ada.page.waitForResponse((response) =>
    response.url().endsWith("/axon.chat.v1.ChatService/ListMessages") && response.ok(),
  );
  await ada.page.reload();
  const saved = await (await history).json();
  const parts = saved.messages
    .filter((message: { body: string }) => message.body !== "before the long message")
    .sort((a: { seq: string }, b: { seq: string }) => Number(a.seq) - Number(b.seq));
  expect(parts).toHaveLength(1);
  expect(parts[0].body).toBe(body);
  await expectReady(ada.page);
  await say(ada.page, "after the long message");
  await expect(ada.page.getByText("after the long message", { exact: true }).first()).toBeVisible();

  await ada.close();
  await grace.close();
});
}

test("an oversized draft is rejected before sending and remains in the composer", async ({ browser }) => {
  const ada = await newPerson(browser, "oversized-message", "Ada");
  const grace = await newPerson(browser, "oversized-peer", "Grace");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "before the oversized draft");
  const draft = "я".repeat(65_537);
  await ada.page.getByLabel("Message", { exact: true }).fill(draft);
  await ada.page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(ada.page.getByText("A message can contain at most 65536 characters.", { exact: true })).toBeVisible();
  await expect(ada.page.getByLabel("Message", { exact: true })).toHaveValue(draft);
  await say(ada.page, "shortened draft");
  await expect(ada.page.getByText("shortened draft", { exact: true }).first()).toBeVisible();
  await ada.close();
  await grace.close();
});

test("a socket size rejection is shown to the sender", async ({ browser }) => {
  const ada = await newPerson(browser, "rejected-message", "Ada");
  const grace = await newPerson(browser, "rejected-peer", "Grace");
  await startChat(ada.page, { email: grace.email, username: grace.username, name: "Grace" }, "before the rejection");

  await ada.page.routeWebSocket("**/ws/chat", (socket) => {
    const server = socket.connectToServer();
    socket.onMessage((message) => {
      if (JSON.parse(String(message)).type === "send") {
        socket.close({ code: 1009, reason: "message too big" });
      } else {
        server.send(message);
      }
    });
  });
  await ada.page.reload();
  await expectReady(ada.page);
  await say(ada.page, "rejected by the transport");
  const outgoing = ada.page.getByRole("article", { name: "Your message", exact: true }).filter({ hasText: "rejected by the transport" });
  await expect(outgoing.getByRole("status")).toHaveText("This message is too large to send.");
  await expect(outgoing).toContainText("rejected by the transport");

  await ada.close();
  await grace.close();
});
