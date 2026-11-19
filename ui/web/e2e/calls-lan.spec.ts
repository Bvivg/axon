import fs from "node:fs/promises";
import { createHmac } from "node:crypto";
import { expect, test, type Page, type BrowserContext } from "@playwright/test";
import { newEmail, newUsername, password } from "./support";
import { startCallFromChat, startChat } from "./chat-support";
import type { SfuMedia } from "../src/lib/calls/livekit";

declare global {
  interface Window {
    lanSfuMedia: SfuMedia;
    lanSfuPCs: RTCPeerConnection[];
    lanLocalTracks: MediaStreamTrack[];
    lanTracks: MediaStreamTrack[];
  }
}

const origin = process.env.CALLS_TEST_ORIGIN || "https://192.168.2.1:3443";

test("Trusted LAN HTTPS login refresh and real two-browser call", async ({ browser }) => {
  const contexts = await Promise.all([0, 1].map(() => browser.newContext({ permissions: ["microphone", "camera"] })));
  const result: Record<string, unknown> = { origin, certificateChecks: true, physicalDevices: "not tested" };
  for (const context of contexts) {
    await context.addInitScript(() => {
      window.lanLocalTracks = [];
      const capture = MediaDevices.prototype.getUserMedia;
      MediaDevices.prototype.getUserMedia = async function (...args) {
        const stream = await Reflect.apply(capture, this, args);
        window.lanLocalTracks.push(...stream.getTracks());
        return stream;
      };
    });
  }
  const errors: string[] = [];
  for (const context of contexts) context.on("page", page => { page.on("pageerror", error => errors.push(error.message)); page.on("console", message => { if (message.type() === "error") errors.push(message.text()); }); page.on("requestfailed", request => errors.push(`${request.url()} ${request.failure()?.errorText}`)); });
  try {
    const people = [];
    for (const [index, context] of contexts.entries()) {
      const person = { email: newEmail("lan"), username: newUsername("lan"), name: `LAN ${index}`, page: await context.newPage() };
      const registered = await context.request.post(origin + "/api/axon.auth.v1.AuthService/Register", { headers: { Origin: origin, "Connect-Protocol-Version": "1" }, data: { email: person.email, username: person.username, displayName: person.name, password } });
      expect(registered.ok()).toBe(true);
      expect((await context.cookies()).find(c => c.name === "axon_refresh")).toMatchObject({ secure: true, httpOnly: true, path: "/api/axon.auth.v1.AuthService/" });
      if (process.env.CALLS_EXPECT_NO_PROBES === "1") {
        for (const route of ["/calls/probe", "/calls/spike"]) expect((await context.request.get(origin + route)).status()).toBe(404);
      }
      people.push(person);
    }
    const [a, b] = people;
    await a.page.goto(origin + "/chat");
    await startChat(a.page, b, "LAN call fixture");
    await b.page.goto(origin + "/calls");
    await expect(a.page.getByRole("button", { name: "Start call", exact: true })).toBeEnabled();
    await startCallFromChat(a.page, "Audio call");
    await b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const { page } of people) {
      await expect(page.getByRole("status").filter({ hasText: "In call" })).toBeVisible();
      await expect.poll(() => page.locator("audio[data-call-output]").evaluate((video: HTMLAudioElement) => !!video.srcObject && (video.srcObject as MediaStream).getAudioTracks().some(t => t.readyState === "live"))).toBe(true);
      const play = page.getByRole("button", { name: "Play audio", exact: true }); if (await play.isVisible()) await play.click();
    }
    result.secureContexts = await Promise.all(people.map(p => p.page.evaluate(() => isSecureContext)));
    result.playback = await Promise.all(people.map(p => p.page.locator("audio[data-call-output]").evaluate((video: HTMLAudioElement) => ({ paused: video.paused, muted: video.muted, currentTime: video.currentTime }))));
    for (const p of people) {
      const trackCounts = await p.page.evaluate(() => {
        const remote = [...document.querySelectorAll<HTMLAudioElement>("audio[data-call-output]")]
          .flatMap(audio => (audio.srcObject as MediaStream | null)?.getAudioTracks() ?? []);
        window.lanTracks = [...window.lanLocalTracks, ...remote];
        return { local: window.lanLocalTracks.length, remote: remote.length };
      });
      expect(trackCounts.local).toBeGreaterThan(0);
      expect(trackCounts.remote).toBe(1);
    }
    await a.page.getByRole("button", { name: "End call", exact: true }).click();
    for (const p of people) await expect.poll(() => p.page.evaluate(() => window.lanTracks.every(t => t.readyState === "ended"))).toBe(true);
    result.cleanup = "all saved native local/remote tracks ended"; result.passed = true;
  } finally { result.errors = errors; result.pages = await Promise.all(contexts.flatMap(c => c.pages()).map(p => p.evaluate(() => ({ url: location.href, ready: document.readyState, scripts: [...document.scripts].map(s => s.src).filter(Boolean), resources: performance.getEntriesByType("resource").map(r => r.name) })).catch(() => null))); await fs.writeFile(origin.startsWith("https:") ? "/evidence/lan-call.json" : "/evidence/localhost-call.json", JSON.stringify(result, null, 2)); await Promise.all(contexts.map(c => c.close())); }
});

test("Trusted LAN HTTPS real three-party SFU and complete cleanup", async ({ browser }) => {
  test.skip(process.env.CALLS_LAN_SFU !== "1", "Requires the explicitly configured LAN SFU fixture");
  const contexts = await Promise.all([0, 1, 2].map(() => browser.newContext({ permissions: ["microphone", "camera"] })));
  const result: Record<string, unknown> = { origin, certificateChecks: true, physicalDevices: "not tested" };
  try {
    const people: { context: BrowserContext; page: Page; id: string; token: string }[] = [];
    for (const [index, context] of contexts.entries()) {
      const registered = await context.request.post(origin + "/api/axon.auth.v1.AuthService/Register", { headers: { Origin: origin, "Connect-Protocol-Version": "1" }, data: { email: newEmail("lan-sfu"), username: newUsername("lansfu"), displayName: `LAN SFU ${index}`, password } });
      expect(registered.ok()).toBe(true);
      const body = await registered.json();
      people.push({ context, page: await context.newPage(), id: body.user.id, token: body.tokens.accessToken });
    }
    const [a, b, c] = people;
    const created = await a.context.request.post(origin + "/api/axon.chat.v1.ChatService/CreateGroup", { headers: { Origin: origin, "Connect-Protocol-Version": "1", Authorization: `Bearer ${a.token}` }, data: { name: "LAN SFU evidence", memberUserIds: [b.id, c.id] } });
    expect(created.ok()).toBe(true);
    for (const person of people) await person.page.goto(person === a ? `${origin}/chat#${(await created.json()).room.id}` : `${origin}/calls`);
    await startCallFromChat(a.page, "Video call");
    for (const person of [b, c]) await person.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const person of people) {
      await expect.poll(() => person.page.evaluate(() => window.axonCallManager?.snapshot().call?.state)).toBe(3);
      await person.page.evaluate(() => { const media = window.axonCallManager!.snapshot().media!; if (!("room" in media)) throw new Error("Expected actual SFU"); window.lanSfuMedia = media; window.lanSfuPCs = media.pcs; });
      await expect.poll(() => person.page.evaluate(() => window.lanSfuMedia.room.remoteParticipants.size)).toBe(2);
    }
    const read = (person: typeof a) => person.page.evaluate(async () => {
      const media = window.lanSfuMedia;
      let bytes = 0, energy = 0, frames = 0;
      const selected: { local: RTCStats; remote: RTCStats; pair: RTCStats }[] = [];
      for (const pc of media.pcs) {
        const report = await pc.getStats();
        report.forEach(row => {
          if (row.type === "inbound-rtp" && row.kind === "audio") { bytes += row.bytesReceived || 0; energy += row.totalAudioEnergy || 0; }
          if (row.type === "inbound-rtp" && row.kind === "video") frames += row.framesDecoded || 0;
          if (row.type === "transport" && row.selectedCandidatePairId) { const pair = report.get(row.selectedCandidatePairId); selected.push({ pair, local: report.get(pair.localCandidateId), remote: report.get(pair.remoteCandidateId) }); }
        });
      }
      return { bytes, energy, frames, selected, room: media.room.state, participants: [...media.room.remoteParticipants.values()].map(p => ({ identity: p.identity, sid: p.sid })) };
    });
    const before = await Promise.all(people.map(read));
    for (let i = 0; i < people.length; i++) await expect.poll(async () => { const next = await read(people[i]); return next.bytes - before[i].bytes > 1000 && next.energy - before[i].energy > 0.0001 && next.frames > before[i].frames && next.selected.length > 0; }).toBe(true);
    result.before = before; result.after = await Promise.all(people.map(read));
    const id = await a.page.evaluate(() => window.lanSfuMedia.call.id);
    result.callID = id;
    const env = await fs.readFile("/evidence-sfu/media.env", "utf8");
    const values = Object.fromEntries(env.trim().split("\n").map(line => { const at = line.indexOf("="); return [line.slice(0, at), line.slice(at + 1)]; }));
    const admin = async (method: string, data: object, grant: object) => {
      const head = Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url");
      const body = head + "." + Buffer.from(JSON.stringify({ iss: values.LIVEKIT_API_KEY, sub: "lan-evidence", exp: Math.floor(Date.now() / 1000) + 30, video: grant })).toString("base64url");
      const token = body + "." + createHmac("sha256", values.LIVEKIT_API_SECRET).update(body).digest("base64url");
      const response = await a.context.request.post(`http://livekit:7880/twirp/livekit.RoomService/${method}`, { headers: { Authorization: `Bearer ${token}` }, data });
      expect(response.ok()).toBe(true); return response.json();
    };
    const server = await admin("ListParticipants", { room: `call_${id}` }, { roomAdmin: true, room: `call_${id}` });
    expect(server.participants).toHaveLength(3);
    result.server = server;
    await a.page.getByRole("button", { name: "End for everyone", exact: true }).click();
    for (const person of people) await expect.poll(() => person.page.evaluate(() => window.lanSfuMedia.room.state === "disconnected" && [...window.lanSfuMedia.tracks].every(t => t.readyState === "ended") && window.lanSfuPCs.every(pc => pc.connectionState === "closed"))).toBe(true);
    await expect.poll(async () => ((await admin("ListRooms", {}, { roomList: true })).rooms ?? []).some((room: { name: string }) => room.name === `call_${id}`)).toBe(false);
    result.cleanup = await Promise.all(people.map(person => person.page.evaluate(() => ({ state: window.lanSfuMedia.room.state, tracks: [...window.lanSfuMedia.tracks].map(t => ({ kind: t.kind, readyState: t.readyState })), pcs: window.lanSfuPCs.map(pc => pc.connectionState) }))));
    result.passed = true;
  } finally { await fs.writeFile("/evidence/lan-sfu.json", JSON.stringify(result, null, 2)); await Promise.all(contexts.map(context => context.close())); }
});
