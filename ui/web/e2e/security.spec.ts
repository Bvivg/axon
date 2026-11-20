import { randomUUID } from "node:crypto";

import { expect, request, test, type APIRequestContext, type APIResponse } from "@playwright/test";

import { gatewayUrl, newUsername, password } from "./support";

async function rpc(context: APIRequestContext, service: string, method: string, data: object, token?: string, origin?: string) {
  return context.post(`${gatewayUrl}/axon.${service}.v1.${service[0].toUpperCase() + service.slice(1)}Service/${method}`, {
    data,
    headers: {
      "Connect-Protocol-Version": "1",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(origin ? { Origin: origin } : {}),
    },
  });
}

async function malformedGameCommand(token: string, sessionId?: string): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(`${gatewayUrl.replace(/^http/, "ws")}/ws/game`, ["axon.game.v1", `axon.bearer.${token}`]);
    const timer = setTimeout(() => { socket.close(); reject(new Error("game error response timed out")); }, 5000);
    let subscribed = !sessionId;
    socket.addEventListener("open", () => socket.send(sessionId ? JSON.stringify({ subscribe: sessionId }) : "{"));
    socket.addEventListener("error", () => { clearTimeout(timer); reject(new Error("game socket failed")); });
    socket.addEventListener("message", event => {
      const result = JSON.parse(String(event.data));
      if (!subscribed) {
        if (!result.session) { clearTimeout(timer); socket.close(); reject(new Error("game subscription failed")); return; }
        subscribed = true;
        socket.send("{");
        return;
      }
      clearTimeout(timer);
      socket.close();
      resolve(result);
    });
  });
}

test("resource authorization, private downloads and browser origin stay enforced", async ({ baseURL }) => {
  const origin = new URL(baseURL!).origin;
  const alice = await request.newContext();
  const bob = await request.newContext();
  const anonymous = await request.newContext();
  try {
    const register = async (context: APIRequestContext, label: string) => {
      const username = newUsername(label);
      let response: APIResponse | undefined;
      await expect.poll(async () => {
        response = await rpc(context, "auth", "Register", { username, email: `${username}@axon.test`, password, displayName: label }, undefined, origin);
        return response.status();
      }, { intervals: [1000], timeout: 10000 }).not.toBe(429);
      expect(response!.status()).toBe(200);
      return response!.json();
    };
    const a = await register(alice, "audit-alice");
    const b = await register(bob, "audit-bob");
    const token = a.tokens.accessToken as string;
    const other = b.tokens.accessToken as string;

    expect((await rpc(anonymous, "auth", "GetMe", {})).status()).toBe(401);
    expect((await rpc(anonymous, "auth", "GetMe", {}, token.slice(0, -8) + "invalidx")).status()).toBe(401);
    expect((await rpc(alice, "auth", "GetMe", {}, token, "https://untrusted.example")).status()).toBe(403);
    expect((await rpc(alice, "auth", "AssignRole", { userId: a.user.id, roleId: randomUUID() }, token)).status()).toBe(403);
    const push = await rpc(alice, "chat", "SubscribePush", { endpoint: "https://127.0.0.1/private", p256dh: "test", auth: "test" }, token);
    if (push.status() === 412) {
      expect((await push.json()).code).toBe("failed_precondition");
    } else {
      expect(push.status()).toBe(400);
    }

    const gameResponse = await rpc(alice, "game", "CreateSession", { game: "tictactoe", requestId: randomUUID() }, token);
    expect(gameResponse.status()).toBe(200);
    const game = (await gameResponse.json()).session;
    expect((await rpc(bob, "game", "GetSession", { sessionId: game.id }, other)).status()).toBe(404);
    expect(await malformedGameCommand(token)).toEqual({ error: "SESSION_ERROR_INVALID_COMMAND" });
    expect(await malformedGameCommand(token, game.id)).toEqual({ error: "SESSION_ERROR_INVALID_COMMAND" });

    const roomResponse = await rpc(alice, "chat", "CreateGroup", { name: "Review private group", memberUserIds: [b.user.id] }, token);
    expect(roomResponse.status()).toBe(200);
    const room = (await roomResponse.json()).room;
    expect((await rpc(bob, "chat", "RenameGroup", { roomId: room.id, name: "forbidden" }, other)).status()).toBe(403);

    const marker = Buffer.from("private-download-regression");
    const uploaded = await alice.post(`${gatewayUrl}/api/attachment?as=file`, {
      data: marker,
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/octet-stream", "X-Filename": "private.txt" },
    });
    expect(uploaded.status()).toBe(200);
    const upload = await uploaded.json();
    const key = new URL(upload.payload.url).pathname.split("/chat-attachments/")[1];
    const mediaURL = `${gatewayUrl}/api/media/${key}`;
    expect((await anonymous.get(mediaURL)).status()).toBe(401);
    expect((await bob.get(mediaURL)).status()).toBe(404);
    const download = await alice.get(mediaURL);
    expect(download.status()).toBe(200);
    expect(await download.body()).toEqual(marker);
    expect(download.headers()["cache-control"]).toContain("no-store");
    expect(download.headers()["x-content-type-options"]).toBe("nosniff");
    const range = await alice.get(mediaURL, { headers: { Range: "bytes=0-6" } });
    expect(range.status()).toBe(206);
    expect((await range.body()).toString()).toBe("private");

    const logout = await rpc(alice, "auth", "Logout", {}, token, origin);
    expect(logout.status()).toBe(200);
    expect((await alice.get(mediaURL, { headers: { Authorization: `Bearer ${token}` } })).status()).toBe(401);
  } finally {
    await Promise.all([alice.dispose(), bob.dispose(), anonymous.dispose()]);
  }
});
