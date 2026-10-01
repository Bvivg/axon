import { expect, request, test } from "@playwright/test";

import { goToChats, messageWith, openChatWith, startChat } from "./chat-support";
import { gatewayUrl, newPerson, password } from "./support";

async function accessTokenOf(email: string): Promise<string> {
  const api = await request.newContext();
  const resp = await api.post(`${gatewayUrl}/axon.auth.v1.AuthService/Login`, { data: { email, password } });
  expect(resp.ok()).toBe(true);
  const body = (await resp.json()) as { tokens: { accessToken: string } };
  await api.dispose();
  return body.tokens.accessToken;
}

async function sendAll(token: string, roomID: string, bodies: string[]): Promise<void> {
  const socket = new WebSocket(`${gatewayUrl.replace(/^http/, "ws")}/ws/chat`, ["axon.chat.v1", `axon.bearer.${token}`]);
  await new Promise<void>((resolve, reject) => {
    let acked = 0;
    socket.onerror = () => reject(new Error("chat socket failed"));
    socket.onopen = () => {
      for (const body of bodies) {
        socket.send(JSON.stringify({ type: "send", room_id: roomID, client_id: crypto.randomUUID(), body }));
      }
    };
    socket.onmessage = (event) => {
      const frame = JSON.parse(String(event.data)) as { type: string };
      if (frame.type === "error") reject(new Error(String(event.data)));
      if (frame.type === "ack" && ++acked === bodies.length) resolve();
    };
  });
  socket.close();
}

test("search finds a message from the start of a long chat and jumps to it", async ({ browser }) => {
  const ada = await newPerson(browser, "search", "Ada");
  const grace = await newPerson(browser, "search-peer", "Grace");

  const roomID = await startChat(ada.page, { email: grace.email, name: "Grace" }, "hello there");
  const fillers = Array.from({ length: 249 }, (_, i) => `filler ${i + 1}`);
  await sendAll(await accessTokenOf(ada.email), roomID, ["the needle sits in this haystack", ...fillers]);

  await goToChats(ada.page);
  await openChatWith(ada.page, "Grace");
  await expect(messageWith(ada.page, "filler 249")).toBeInViewport();

  await ada.page.getByRole("button", { name: "Search messages" }).click();
  const input = ada.page.getByRole("textbox", { name: "Search messages" });
  await input.fill("zzz-nothing");
  await expect(ada.page.getByText("No messages found")).toBeVisible();

  await input.fill("HAYSTACK");
  const results = ada.page.getByRole("list", { name: "Search results" });
  await expect(results.getByRole("button")).toHaveCount(1);
  await expect(results.locator("mark")).toHaveText("haystack");

  await results.getByRole("button").click();
  await expect(input).toHaveCount(0);
  await expect(messageWith(ada.page, "the needle sits in this haystack")).toBeInViewport();

  await ada.page.getByRole("button", { name: "Search messages" }).click();
  await input.press("Escape");
  await expect(input).toHaveCount(0);
  await expect(ada.page.getByRole("heading", { name: "Grace" })).toBeVisible();
});
