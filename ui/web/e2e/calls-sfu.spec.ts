import fs from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { expect, test, type Page } from "@playwright/test";
import type { SfuMedia } from "../src/lib/calls/livekit";
import type { PeerMedia } from "../src/lib/calls/p2p";
import { callPeers, callSQL } from "./calls-ws-support";
import { startCallFromChat, startChat } from "./chat-support";
import { admin } from "./calls-admission-support";

declare global { interface Window { axonSfuEvidence: SfuMedia; axonPriorP2P: PeerMedia; } }

async function observe(page: Page, others = 2) {
  await expect.poll(() => page.evaluate(() => {
    const view = window.axonCallManager!.snapshot();
    return view.call?.state === 3 && view.call.transport === "sfu" && view.call.participants.some(p => p.userId === window.axonCallManager!.userID && p.mediaReady);
  })).toBe(true);
  await page.evaluate(() => { const media = window.axonCallManager!.snapshot().media!; if (!("room" in media)) throw new Error("Expected a real SFU Room"); window.axonSfuEvidence = media; });
  await expect.poll(() => page.evaluate(() => window.axonSfuEvidence.room.remoteParticipants.size)).toBe(others);
}

async function stats(page: Page) {
  return page.evaluate(async () => {
    const media = window.axonSfuEvidence;
    const reports = await Promise.all(media.pcs.map(pc => pc.getStats()));
    const rows: Record<string, number | string>[] = [];
    const selected: { pair: RTCStats; local: RTCStats & { candidateType: string }; remote: RTCStats & { candidateType: string } }[] = [];
    for (const report of reports) {
      report.forEach(stat => rows.push({ ...stat }));
      report.forEach(stat => { if (stat.type === "transport" && stat.selectedCandidatePairId) { const pair = report.get(stat.selectedCandidatePairId); selected.push({ pair, local: report.get(pair.localCandidateId), remote: report.get(pair.remoteCandidateId) }); } });
    }
    const sum = (name: string, kind?: string) => rows.filter(row => row.type === "inbound-rtp" && (!kind || row.kind === kind)).reduce((n, row) => n + Number(row[name] || 0), 0);
    return { state: media.room.state, sid: await media.room.getSid(), participants: [...media.room.remoteParticipants.values()].map(p => ({ identity: p.identity, sid: p.sid })), selected, audioBytes: sum("bytesReceived", "audio"), audioPackets: sum("packetsReceived", "audio"), energy: sum("totalAudioEnergy", "audio"), frames: sum("framesDecoded", "video"), sources: rows.filter(row => row.type === "media-source" && row.kind === "audio"), inbound: rows.filter(row => row.type === "inbound-rtp" && row.kind === "audio"), tracks: [...media.tracks].map(t => ({ kind: t.kind, state: t.readyState })), playback: [...document.querySelectorAll<HTMLAudioElement>("audio[data-call-output]")].map(v => ({ paused: v.paused, muted: v.muted, time: v.currentTime })) };
  });
}

async function proveGrowth(pages: Page[], video: boolean) {
  const before = await Promise.all(pages.map(stats));
  for (let i = 0; i < pages.length; i++) {
    await expect.poll(async () => { const next = await stats(pages[i]); return next.audioBytes > before[i].audioBytes && next.audioPackets > before[i].audioPackets && next.energy > before[i].energy && (!video || next.frames > before[i].frames); }).toBe(true);
    await expect.poll(async () => { const next = await stats(pages[i]); return next.playback.length > 0 && next.playback.every(output => !output.paused && !output.muted && output.time > 0); }).toBe(true);
  }
  const after = await Promise.all(pages.map(stats));
  for (const state of after) {
    expect(state.selected.length).toBeGreaterThan(0);
    for (const pair of state.selected) { expect(pair.local.candidateType).toBe("host"); expect(pair.remote.candidateType).toBe("host"); }
    expect(state.playback.length).toBeGreaterThan(0);
    for (const output of state.playback) { expect(output.paused).toBe(false); expect(output.muted).toBe(false); expect(output.time).toBeGreaterThan(0); }
  }
  const id = await pages[0].evaluate(() => window.axonCallManager!.snapshot().call!.id);
  const server = await admin(pages[0], "ListParticipants", { room: `call_${id}` }, { roomAdmin: true, room: `call_${id}` });
  expect(server.participants).toHaveLength(pages.length);
  return { before, after, server: { room: `call_${id}`, participants: server.participants.map((p: { identity: string; sid: string; tracks: unknown[] }) => ({ identity: p.identity, sid: p.sid, tracks: p.tracks })) } };
}

async function cleaned(pages: Page[], id: string) {
  for (const page of pages) {
    await expect.poll(() => page.evaluate(() => window.axonSfuEvidence.room.state)).toBe("disconnected");
    expect(await page.evaluate(() => [...window.axonSfuEvidence.tracks].every(t => t.readyState === "ended"))).toBe(true);
  }
  await expect.poll(() => callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${id}'`)).toBe("0");
  await expect.poll(async () => { const rooms = await admin(pages[0], "ListRooms", {}, { roomList: true }); return (rooms.rooms ?? []).some((room: { name: string }) => room.name === `call_${id}`); }).toBe(false);
  await fs.writeFile(`/evidence/cleanup-${id}.json`, JSON.stringify({ callID: id, slots: 0, serverRoomPresent: false }));
  return Promise.all(pages.map(stats));
}

test("active P2P ends before a new ad-hoc SFU group, without creating a chat", async ({ browser }) => {
  const peers = await callPeers(browser, "ui", 3);
  const pages = [peers.a.page, peers.b.page, peers.c.page];
  const result: Record<string, unknown> = { engine: browser.version(), nativeMedia: true };
  try {
    await startCallFromChat(peers.a.page, "Video call");
    await peers.b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages.slice(0, 2)) {
      await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(3);
      await page.evaluate(() => { const media = window.axonCallManager!.snapshot().media!; if ("room" in media) throw new Error("Expected native P2P"); window.axonPriorP2P = media; });
    }
    const original = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    const rooms = await callSQL("SELECT count(*) FROM chat.rooms");
    await peers.a.page.getByRole("button", { name: "People", exact: true }).click();
    await peers.a.page.getByRole("button", { name: "Add person", exact: true }).click();
    await expect(peers.a.page.getByText("Start a group call? This will end the current call.")).toBeVisible();
    await peers.a.page.getByLabel("Invite contact").selectOption(peers.c.userID);
    await peers.a.page.getByRole("button", { name: "Start group call", exact: true }).click();
    for (const page of pages.slice(0, 2)) {
      await expect.poll(() => page.evaluate(() => ({ state: window.axonPriorP2P.pc.connectionState, ended: [...window.axonPriorP2P.tracks].every(t => t.readyState === "ended") }))).toEqual({ state: "closed", ended: true });
    }
    for (const page of pages.slice(1)) await page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages) await observe(page);
    const id = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    expect(id).not.toBe(original);
    expect(await callSQL("SELECT count(*) FROM chat.rooms")).toBe(rooms);
    const privacy = await peers.c.context.request.post("http://localhost:8080/axon.calls.v1.CallsService/GetCall", { headers: { Authorization: `Bearer ${peers.c.token}`, "Connect-Protocol-Version": "1", Origin: "http://localhost:3000" }, data: { callId: original } });
    expect(privacy.status()).toBe(404);
    expect(await peers.c.page.evaluate(() => ({ room: window.axonCallManager!.snapshot().call!.roomId, source: window.axonCallManager!.snapshot().call!.predecessorCallId }))).toEqual({ room: "", source: undefined });
    for (const page of pages.slice(0, 2)) expect(await page.evaluate(() => [...window.axonSfuEvidence.tracks].every(t => !window.axonPriorP2P.tracks.has(t)))).toBe(true);
    result.original = original; result.group = id; result.media = await proveGrowth(pages, true);
    result.slots = await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${id}'`);
    expect(result.slots).toBe("3");
    await peers.a.page.getByRole("button", { name: "End for everyone", exact: true }).click();
    result.cleanup = await cleaned(pages, id);
    result.finalSlots = "0";
  } finally {
    await fs.writeFile("/evidence/sfu-replacement.json", JSON.stringify(result, null, 2));
    await peers.close();
  }
});

test("group chat starts SFU, member leaves, remaining media continues", async ({ browser }) => {
  const peers = await callPeers(browser, "ui", 3);
  const pages = [peers.a.page, peers.b.page, peers.c.page];
  const result: Record<string, unknown> = { engine: browser.version(), nativeMedia: true };
  try {
    const created = await peers.a.context.request.post("http://localhost:8080/axon.chat.v1.ChatService/CreateGroup", { headers: { Authorization: `Bearer ${peers.a.token}`, "Connect-Protocol-Version": "1", Origin: "http://localhost:3000" }, data: { name: "SFU friends", memberUserIds: [peers.b.userID, peers.c.userID] } });
    expect(created.ok()).toBe(true);
    const room = (await created.json()).room;
    await peers.a.page.goto(`http://localhost:3000/chat#${room.id}`);
    await startCallFromChat(peers.a.page, "Audio call");
    for (const page of pages.slice(1)) await page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages) await observe(page);
    const id = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    result.media = await proveGrowth(pages, false);
    await peers.c.page.getByRole("button", { name: "Leave call", exact: true }).click();
    await expect.poll(() => peers.c.page.evaluate(() => window.axonSfuEvidence.room.state)).toBe("disconnected");
    expect(await peers.c.page.evaluate(() => [...window.axonSfuEvidence.tracks].every(t => t.readyState === "ended"))).toBe(true);
    for (const page of pages.slice(0, 2)) await observe(page, 1);
    result.afterLeave = await proveGrowth(pages.slice(0, 2), false);
    await expect.poll(() => callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${id}'`)).toBe("2");
    await peers.a.page.getByRole("button", { name: "End for everyone", exact: true }).click();
    result.cleanup = await cleaned(pages, id);
  } finally {
    await fs.writeFile("/evidence/sfu-group.json", JSON.stringify(result, null, 2));
    await peers.close();
  }
});

test("P2P setup deadline falls back to SFU under the same Call ID after cleanup", async ({ browser }) => {
  const peers = await callPeers(browser, "ui");
  const pages = [peers.a.page, peers.b.page];
  const rules = [["-p", "udp", "-d", "172.30.252.10", "-j", "REJECT"], ["-d", "172.30.252.20", "-j", "REJECT"]];
  const result: Record<string, unknown> = { engine: browser.version(), nativeMedia: true, networkFault: "real iptables reject P2P direct UDP and all coturn traffic; SFU UDP remains reachable" };
  try {
    for (const rule of rules) execFileSync("iptables", ["-I", "OUTPUT", ...rule]);
    await startCallFromChat(peers.a.page, "Audio call");
    await peers.b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages) {
      await expect.poll(() => page.evaluate(() => !!window.axonCallManager!.snapshot().media)).toBe(true);
      await page.evaluate(() => {
        const media = window.axonCallManager!.snapshot().media!;
        if ("room" in media) throw new Error("Expected initial native P2P");
        if (media.call.everActive) throw new Error("Fallback must occur before activation");
        window.axonPriorP2P = media;
      });
    }
    const original = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    for (const page of pages) await page.waitForFunction(() => window.axonCallManager!.snapshot().call?.transport === "sfu");
    for (const page of pages) {
      await observe(page, 1);
      expect(await page.evaluate(() => ({ id: window.axonSfuEvidence.call.id, generation: Number(window.axonSfuEvidence.call.generation), oldConnection: window.axonPriorP2P.pc.connectionState, oldEnded: [...window.axonPriorP2P.tracks].every(t => t.readyState === "ended"), freshTracks: [...window.axonSfuEvidence.tracks].every(t => !window.axonPriorP2P.tracks.has(t)) }))).toEqual({ id: original, generation: 2, oldConnection: "closed", oldEnded: true, freshTracks: true });
    }
    result.callID = original; result.media = await proveGrowth(pages, false);
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    result.cleanup = await cleaned(pages, original);
  } finally {
    for (const rule of rules) execFileSync("iptables", ["-D", "OUTPUT", ...rule]);
    await fs.writeFile("/evidence/sfu-fallback.json", JSON.stringify(result, null, 2));
    await peers.close();
  }
});

test("remove and reinvite fence old media tokens; real logout preserves the other participants", async ({ browser }) => {
  const peers = await callPeers(browser, "ui", 3);
  const pages = [peers.a.page, peers.b.page, peers.c.page];
  const result: Record<string, unknown> = { engine: browser.version(), nativeMedia: true };
  try {
    const created = await peers.a.context.request.post("http://localhost:8080/axon.chat.v1.ChatService/CreateGroup", { headers: { Authorization: `Bearer ${peers.a.token}`, "Connect-Protocol-Version": "1", Origin: "http://localhost:3000" }, data: { name: "SFU revoke", memberUserIds: [peers.b.userID, peers.c.userID] } });
    expect(created.ok()).toBe(true);
    await peers.a.page.goto(`http://localhost:3000/chat#${(await created.json()).room.id}`);
    await startCallFromChat(peers.a.page, "Audio call");
    const tokenResponse = peers.c.page.waitForResponse(response => response.url().endsWith("/axon.calls.v1.CallsService/GetSfuCredentials"));
    for (const page of pages.slice(1)) await page.getByRole("button", { name: "Accept", exact: true }).click();
    const initialToken = (await (await tokenResponse).json()).token;
    for (const page of pages) await observe(page);
    const id = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    const beforeVersion = await peers.c.page.evaluate(() => window.axonCallManager!.snapshot().call!.participants.find(p => p.userId === window.axonCallManager!.userID)!.mediaVersion!.toString());
    await peers.a.page.getByRole("button", { name: "People", exact: true }).click();
    await peers.a.page.getByRole("listitem").filter({ hasText: "Call Carol" }).getByRole("button", { name: "Remove", exact: true }).click();
    await peers.a.page.getByRole("dialog", { name: "People", exact: true }).getByRole("button", { name: "Close", exact: true }).click();
    await expect.poll(() => peers.c.page.evaluate(() => window.axonSfuEvidence.room.state)).toBe("disconnected");
    expect(await peers.c.page.evaluate(() => [...window.axonSfuEvidence.tracks].every(t => t.readyState === "ended"))).toBe(true);
    for (const page of pages.slice(0, 2)) await observe(page, 1);
    const validate = (token: string) => {
      const socket = peers.c.network.sockets.filter(item => new URL(item.url).pathname.startsWith("/livekit/")).at(-1)!;
      const url = new URL(socket.url); url.protocol = "http:"; url.pathname = "/livekit/rtc/v1/validate"; url.searchParams.set("access_token", token);
      return peers.c.context.request.get(url.href, { headers: { Origin: "http://localhost:3000" } });
    };
    expect((await validate(initialToken)).status()).toBe(403);
    await peers.a.page.getByRole("button", { name: "People", exact: true }).click();
    await peers.a.page.getByRole("button", { name: "Add person", exact: true }).click();
    await peers.a.page.getByLabel("Invite contact").selectOption(peers.c.userID);
    await peers.a.page.getByRole("button", { name: "Invite", exact: true }).click();
    const newTokenResponse = peers.c.page.waitForResponse(response => response.url().endsWith("/axon.calls.v1.CallsService/GetSfuCredentials"));
    await peers.c.page.getByRole("button", { name: "Accept", exact: true }).click();
    const nextToken = (await (await newTokenResponse).json()).token;
    for (const page of pages) await observe(page);
    const afterVersion = await peers.c.page.evaluate(() => window.axonCallManager!.snapshot().call!.participants.find(p => p.userId === window.axonCallManager!.userID)!.mediaVersion!.toString());
    expect(BigInt(afterVersion)).toBeGreaterThan(BigInt(beforeVersion));
    expect((await validate(initialToken)).status()).toBe(403);
    expect((await validate(nextToken)).status()).toBe(200);
    result.media = await proveGrowth(pages, false);
    const logout = await peers.c.context.request.post("http://localhost:8080/axon.auth.v1.AuthService/Logout", { headers: { Origin: "http://localhost:3000", "Connect-Protocol-Version": "1" }, data: {} });
    expect(logout.ok()).toBe(true);
    await expect.poll(() => peers.c.page.evaluate(() => [...window.axonSfuEvidence.tracks].every(t => t.readyState === "ended"))).toBe(true);
    expect((await validate(nextToken)).status()).toBe(403);
    for (const page of pages.slice(0, 2)) await observe(page, 1);
    await expect.poll(() => callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${id}'`)).toBe("2");
    result.remainingMedia = await proveGrowth(pages.slice(0, 2), false);
    result.versions = { beforeVersion, afterVersion }; result.revokedToken = 403;
    await peers.a.page.getByRole("button", { name: "End for everyone", exact: true }).click();
    result.cleanup = await cleaned(pages, id);
  } finally {
    await fs.writeFile("/evidence/sfu-revoke.json", JSON.stringify(result, null, 2));
    await peers.close();
  }
});

test("busy third participant preserves the active original P2P call and its media", async ({ browser }) => {
  const peers = await callPeers(browser, "ui", 4);
  const pages = [peers.a.page, peers.b.page, peers.c.page, peers.d.page];
  const result: Record<string, unknown> = { engine: browser.version(), nativeMedia: true };
  try {
    await startChat(peers.c.page, peers.d, "Busy participant fixture");
    for (const [caller, receiver] of [[peers.a, peers.b], [peers.c, peers.d]]) {
      await startCallFromChat(caller.page, "Audio call");
      await receiver.page.getByRole("button", { name: "Accept", exact: true }).click();
    }
    for (const page of pages) {
      await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(3);
      await page.evaluate(() => { const media = window.axonCallManager!.snapshot().media!; if ("room" in media) throw new Error("Expected P2P"); window.axonPriorP2P = media; });
    }
    const original = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    const bytes = (page: Page) => page.evaluate(async () => { const stats = await window.axonPriorP2P.pc.getStats(); let value = 0; stats.forEach(row => { if (row.type === "inbound-rtp" && row.kind === "audio") value += row.bytesReceived; }); return value; });
    const before = await Promise.all(pages.slice(0, 2).map(bytes));
    await peers.a.page.getByRole("button", { name: "People", exact: true }).click();
    await peers.a.page.getByRole("button", { name: "Add person", exact: true }).click();
    await peers.a.page.getByLabel("Invite contact").selectOption(peers.c.userID);
    await peers.a.page.getByRole("button", { name: "Start group call", exact: true }).click();
    await expect(peers.a.page.getByRole("dialog", { name: "Call", exact: true }).getByRole("alert")).toContainText("already in a call");
    for (let i = 0; i < 2; i++) {
      expect(await pages[i].evaluate(() => ({ id: window.axonCallManager!.snapshot().call!.id, state: window.axonCallManager!.snapshot().call!.state, sameMedia: window.axonCallManager!.snapshot().media === window.axonPriorP2P, pc: window.axonPriorP2P.pc.connectionState }))).toEqual({ id: original, state: 3, sameMedia: true, pc: "connected" });
      await expect.poll(async () => await bytes(pages[i]) > before[i]).toBe(true);
    }
    result.original = original; result.before = before; result.after = await Promise.all(pages.slice(0, 2).map(bytes));
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    await peers.c.page.getByRole("button", { name: "End call", exact: true }).click();
    for (const page of pages) await expect.poll(() => page.evaluate(() => [...window.axonPriorP2P.tracks].every(t => t.readyState === "ended"))).toBe(true);
    result.passed = true;
  } finally { await fs.writeFile("/evidence/sfu-busy.json", JSON.stringify(result, null, 2)); await peers.close(); }
});

test("original group caller can leave, decline reinvitation and later accept", async ({ browser }) => {
  const peers = await callPeers(browser, "ui", 3);
  const people = [peers.a, peers.b, peers.c], pages = people.map(person => person.page);
  const result: Record<string, unknown> = { engine: browser.version(), nativeMedia: true };
  try {
    const created = await peers.a.context.request.post("http://localhost:8080/axon.chat.v1.ChatService/CreateGroup", { headers: { Authorization: `Bearer ${peers.a.token}`, "Connect-Protocol-Version": "1", Origin: "http://localhost:3000" }, data: { name: "SFU reinvite caller", memberUserIds: [peers.b.userID, peers.c.userID] } });
    expect(created.ok()).toBe(true);
    await peers.a.page.goto(`http://localhost:3000/chat#${(await created.json()).room.id}`);
    await startCallFromChat(peers.a.page, "Audio call");
    for (const page of pages.slice(1)) await page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages) await observe(page);
    const id = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    await peers.a.page.getByRole("button", { name: "Leave call", exact: true }).click();
    await expect.poll(() => peers.a.page.evaluate(() => window.axonSfuEvidence.room.state)).toBe("disconnected");
    expect(await peers.a.page.evaluate(() => [...window.axonSfuEvidence.tracks].every(t => t.readyState === "ended"))).toBe(true);
    for (const page of pages.slice(1)) await observe(page, 1);
    await expect.poll(() => peers.b.page.evaluate(previous => { const owner = window.axonCallManager!.snapshot().call?.ownerId; return !!owner && owner !== previous; }, peers.a.userID)).toBe(true);
    const ownerID = await peers.b.page.evaluate(() => window.axonCallManager!.snapshot().call!.ownerId);
    const owner = people.find(person => person.userID === ownerID)!;
    for (const answer of ["Decline", "Accept"]) {
      await owner.page.getByRole("button", { name: "People", exact: true }).click();
      await owner.page.getByRole("button", { name: "Add person", exact: true }).click();
      await owner.page.getByLabel("Invite contact").selectOption(peers.a.userID);
      await owner.page.getByRole("button", { name: "Invite", exact: true }).click();
      await peers.a.page.getByRole("button", { name: answer, exact: true }).click();
      if (answer === "Decline") {
        await expect.poll(() => peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(4);
        expect(await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().media)).toBeUndefined();
        result.afterDecline = await proveGrowth(pages.slice(1), false);
      }
    }
    for (const page of pages) await observe(page);
    result.media = await proveGrowth(pages, false);
    await owner.page.getByRole("button", { name: "End for everyone", exact: true }).click();
    result.cleanup = await cleaned(pages, id);
  } finally {
    result.lastStats = await Promise.all(pages.map(page => stats(page).catch(() => null)));
    result.lastLocal = await Promise.all(pages.map(page => page.evaluate(() => {
      const view = window.axonCallManager!.snapshot(), m = view.media;
      if (!m || !("room" in m)) return { state: view.call?.state, present: false };
      return { state: view.call?.state, participants: view.call?.participants.map(p => ({ user: p.userId, status: p.status, ready: p.mediaReady })), muted: m.muted, native: m.local.getTracks().map(t => ({ kind: t.kind, enabled: t.enabled, muted: t.muted, ready: t.readyState })), publications: [...m.room.localParticipant.trackPublications.values()].map(p => ({ source: p.source, muted: p.isMuted, nativeEnabled: p.track?.mediaStreamTrack.enabled, nativeReady: p.track?.mediaStreamTrack.readyState })), remotes: [...m.room.remoteParticipants.values()].map(p => ({ identity: p.identity, publications: [...p.trackPublications.values()].map(t => ({ muted: t.isMuted, enabled: t.track?.mediaStreamTrack.enabled, source: t.source })) })) };
    }).catch(() => null)));
    await fs.writeFile("/evidence/sfu-reinvite-caller.json", JSON.stringify(result, null, 2)); await peers.close();
  }
});
