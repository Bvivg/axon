import fs from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { expect, test, type Page, type Browser } from "@playwright/test";
import type { CallMigration } from "../src/lib/calls/migration";
import type { PeerMedia } from "../src/lib/calls/p2p";
import type { SfuMedia } from "../src/lib/calls/livekit";
import { callPeers, callSQL } from "./calls-ws-support";
import { startCallFromChat } from "./chat-support";
import { admin } from "./calls-admission-support";

type Sample = { time: number; phase?: string; sourceState: string; sourceAudio: boolean; candidateAudio: boolean; outputs: number; candidateState?: string };
declare global {
  interface Window {
    axonMigrationEvidence: { source: PeerMedia; candidates: SfuMedia[]; pcs: Set<RTCPeerConnection>; samples: Sample[]; timer: ReturnType<typeof setInterval>; stop: () => void };
  }
}
const p2pRules = [["-p", "udp", "-d", "172.30.252.10", "-j", "REJECT"], ["-d", "172.30.252.20", "-j", "REJECT"]];
const sfuRules = [["-p", "udp", "-d", "172.30.252.30", "-j", "REJECT"], ["-p", "tcp", "-d", "172.30.252.30", "--dport", "7881", "-j", "REJECT"], ["-d", "172.30.252.20", "-j", "REJECT"]];
function firewall(rules: string[][], enable: boolean) { for (const rule of rules) execFileSync("iptables", [enable ? "-I" : "-D", "OUTPUT", ...rule]); }

async function active(browser: Browser) {
  const peers = await callPeers(browser, "ui");
  try {
    const pages = [peers.a.page, peers.b.page];
    await startCallFromChat(peers.a.page, "Video call");
    await peers.b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages) {
      await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(3);
      await page.evaluate(() => {
        const manager = window.axonCallManager!;
        const media = manager.snapshot().media!;
        if ("room" in media) throw new Error("Expected native P2P");
        const evidence = { source: media, candidates: [] as SfuMedia[], pcs: new Set([media.pc]), samples: [] as Sample[], timer: 0 as unknown as ReturnType<typeof setInterval>, stop: () => {} };
        window.axonMigrationEvidence = evidence;
        const sample = () => {
          const migration = (manager as unknown as { migration?: CallMigration }).migration;
          const candidate = migration?.candidate;
          if (candidate && !evidence.candidates.includes(candidate)) evidence.candidates.push(candidate);
          candidate?.pcs.forEach(pc => evidence.pcs.add(pc));
          const selected = manager.snapshot().media;
          if (selected && !("room" in selected)) evidence.pcs.add(selected.pc);
          const outputs = [...document.querySelectorAll<HTMLMediaElement>("audio,video")].filter(element => element.srcObject instanceof MediaStream && (element.srcObject as MediaStream).getAudioTracks().length && !element.paused && !element.muted).length;
          evidence.samples.push({ time: performance.now(), phase: manager.snapshot().call?.migration?.phase, sourceState: media.pc.connectionState, sourceAudio: media.local.getAudioTracks().some(t => t.enabled && t.readyState === "live"), candidateAudio: candidate?.local.getAudioTracks().some(t => t.enabled && t.readyState === "live") ?? false, outputs, candidateState: candidate?.room.state });
        };
        const unsubscribe = manager.subscribe(sample);
        evidence.timer = setInterval(sample, 10);
        evidence.stop = () => { clearInterval(evidence.timer); unsubscribe(); sample(); };
        sample();
      });
    }
    const original = await pages[0].evaluate(() => { const c = window.axonCallManager!.snapshot().call!; return { id: c.id, started: c.startedAt?.seconds.toString(), generation: c.generation.toString() }; });
    const sourceMedia = await growth(pages, false);
    return { ...peers, pages, original, sourceMedia };
  } catch (error) { await peers.close(); throw error; }
}

async function mediaStats(page: Page, sfu: boolean, current = false) {
  return page.evaluate(async ({ useSfu, current }) => {
    const evidence = window.axonMigrationEvidence;
    const media = current ? window.axonCallManager!.snapshot().media! : useSfu ? evidence.candidates.at(-1)! : evidence.source;
    const pcs = "pcs" in media ? media.pcs : [media.pc];
    let audio = 0, packets = 0, energy = 0, frames = 0;
    const selected: { local: string; remote: string; protocol: string }[] = [];
    for (const pc of pcs) {
      const stats = await pc.getStats();
      stats.forEach(row => {
        if (row.type === "inbound-rtp" && row.kind === "audio") { audio += row.bytesReceived ?? 0; packets += row.packetsReceived ?? 0; energy += row.totalAudioEnergy ?? 0; }
        if (row.type === "inbound-rtp" && row.kind === "video") frames += row.framesDecoded ?? 0;
        if (row.type === "transport" && row.selectedCandidatePairId) { const pair = stats.get(row.selectedCandidatePairId); const local = stats.get(pair.localCandidateId), remote = stats.get(pair.remoteCandidateId); selected.push({ local: local.candidateType, remote: remote.candidateType, protocol: local.protocol }); }
      });
    }
    return { audio, packets, energy, frames, selected };
  }, { useSfu: sfu, current });
}
async function growth(pages: Page[], sfu: boolean, current = false) {
  const before = await Promise.all(pages.map(p => mediaStats(p, sfu, current)));
  for (let i = 0; i < pages.length; i++) await expect.poll(async () => { const after = await mediaStats(pages[i], sfu, current); const b = before[i]; return after.audio - b.audio > 1000 && after.packets > b.packets && after.energy - b.energy > 0.0001 && after.frames > b.frames; }).toBe(true);
  const after = await Promise.all(pages.map(p => mediaStats(p, sfu, current)));
  for (const item of after) expect(item.selected.length).toBeGreaterThan(0);
  return { before, after };
}

async function control(person: Awaited<ReturnType<typeof callPeers>>["a"], method: string, input: object) {
  const endpoint = await person.page.evaluate(() => { const e = window.axonCallManager!.channel.endpoint!; return { id: e.id, proof: e.proof, epoch: e.epoch.toString() }; });
  const response = await person.context.request.post(`http://localhost:8080/axon.calls.v1.CallsService/${method}`, { headers: { Authorization: `Bearer ${person.token}`, "Connect-Protocol-Version": "1", Origin: "http://localhost:3000" }, data: { ...input, endpoint, requestId: crypto.randomUUID() } });
  expect(response.ok(), `${method}: HTTP ${response.status()}`).toBe(true);
  return response.json();
}

async function cleanup(pages: Page[], id: string) {
  for (const page of pages) await expect.poll(() => page.evaluate(() => {
    const e = window.axonMigrationEvidence;
    return e.source.pc.connectionState === "closed" && [...e.source.tracks, ...e.candidates.flatMap(m => [...m.tracks])].every(t => t.readyState === "ended") && [...e.pcs].every(pc => pc.connectionState === "closed") && e.candidates.every(m => m.room.state === "disconnected");
  })).toBe(true);
  await expect.poll(() => callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${id}'`)).toBe("0");
  await expect.poll(async () => ((await admin(pages[0], "ListRooms", {}, { roomList: true })).rooms ?? []).some((r: { name: string }) => r.name === `call_${id}`)).toBe(false);
  return { slots: 0, roomPresent: false, clients: await Promise.all(pages.map(page => page.evaluate(() => { const e = window.axonMigrationEvidence; return { source: e.source.pc.connectionState, pcs: [...e.pcs].map(pc => pc.connectionState), tracks: [...e.source.tracks, ...e.candidates.flatMap(m => [...m.tracks])].map(t => ({ kind: t.kind, state: t.readyState })) }; }))) };
}
async function record(name: string, pages: Page[], result: Record<string, unknown>) {
  result.timelines = await Promise.all(pages.map(page => page.evaluate(() => { window.axonMigrationEvidence.stop(); return window.axonMigrationEvidence.samples; }).catch(() => [])));
  await fs.writeFile(`/evidence/migration-${name}.json`, JSON.stringify(result, null, 2));
}

test("native P2P failure automatically prepares SFU and switches the same active call without double audio", async ({ browser }) => {
  const peers = await active(browser);
  const result: Record<string, unknown> = { original: peers.original, sourceMedia: peers.sourceMedia, networkFault: "iptables rejects direct P2P UDP and all coturn; real SFU remains reachable" };
  try {
    firewall(p2pRules, true);
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonMigrationEvidence.samples.some(s => ["disconnected", "failed"].includes(s.sourceState)))).toBe(true);
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => !!window.axonCallManager!.snapshot().call?.migration?.id)).toBe(true);
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.migration?.phase)).toBe("completed");
    result.media = await growth(peers.pages, true);
    for (const page of peers.pages) {
      expect(await page.evaluate(() => { const c = window.axonCallManager!.snapshot().call!; return { id: c.id, started: c.startedAt?.seconds.toString(), generation: c.generation.toString(), transport: c.transport }; })).toEqual({ ...peers.original, generation: "2", transport: "sfu" });
      const samples = await page.evaluate(() => window.axonMigrationEvidence.samples);
      expect(samples.some(s => s.candidateState === "connected" && s.sourceState !== "closed")).toBe(true);
      expect(samples.some(s => s.phase === "preparing" && s.candidateAudio)).toBe(false);
      expect(samples.some(s => s.sourceAudio && s.candidateAudio)).toBe(false);
      expect(samples.some(s => s.outputs > 1)).toBe(false);
      expect(await page.evaluate(() => ({ pc: window.axonMigrationEvidence.source.pc.connectionState, ended: [...window.axonMigrationEvidence.source.tracks].every(t => t.readyState === "ended") }))).toEqual({ pc: "closed", ended: true });
    }
    result.server = await admin(peers.a.page, "ListParticipants", { room: `call_${peers.original.id}` }, { roomAdmin: true, room: `call_${peers.original.id}` });
    expect((result.server as { participants: unknown[] }).participants).toHaveLength(2);
    expect(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${peers.original.id}'`)).toBe("2");
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    result.cleanup = await cleanup(peers.pages, peers.original.id);
  } finally { firewall(p2pRules, false); await record("automatic", peers.pages, result); await peers.close(); }
});

test("SFU preparation timeout cleans candidate and preserves the healthy original P2P", async ({ browser }) => {
  const peers = await active(browser);
  const result: Record<string, unknown> = { original: peers.original, networkFault: "iptables rejects candidate SFU TCP/UDP media, source P2P remains reachable" };
  try {
    firewall(sfuRules, true);
    await control(peers.a, "RequestCallMigration", { callId: peers.original.id, generation: "1" });
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.migration?.phase)).toBe("aborted");
    for (const page of peers.pages) expect(await page.evaluate(() => { const c = window.axonCallManager!.snapshot().call!, e = window.axonMigrationEvidence; return { state: c.state, id: c.id, generation: c.generation.toString(), source: e.source.pc.connectionState, candidates: e.candidates.length, ended: e.candidates.every(m => m.room.state === "disconnected" && [...m.tracks].every(t => t.readyState === "ended")) }; })).toEqual({ state: 3, id: peers.original.id, generation: "1", source: "connected", candidates: 1, ended: true });
    result.media = await growth(peers.pages, false);
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    result.cleanup = await cleanup(peers.pages, peers.original.id);
  } finally { firewall(sfuRules, false); await record("abort", peers.pages, result); await peers.close(); }
});

test("End during real SFU preparation fences the operation and stops both native transports", async ({ browser }) => {
  const peers = await active(browser);
  const result: Record<string, unknown> = { original: peers.original };
  try {
    firewall(sfuRules, true);
    const inputs = await peers.a.page.evaluate(async () => (await navigator.mediaDevices.enumerateDevices()).filter(d => d.kind === "audioinput" || d.kind === "videoinput").map(d => d.label).filter(Boolean));
    await peers.a.page.getByRole("button", { name: "Call devices", exact: true }).click();
    for (const label of inputs) await expect(peers.a.page.getByRole("menuitem", { name: label, exact: true })).toBeEnabled();
    await control(peers.a, "RequestCallMigration", { callId: peers.original.id, generation: "1" });
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonMigrationEvidence.candidates.length)).toBe(1);
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonMigrationEvidence.pcs.size)).toBeGreaterThan(1);
    for (const label of inputs) await expect(peers.a.page.getByRole("menuitem", { name: label, exact: true })).toBeDisabled();
    await peers.a.page.keyboard.press("Escape");
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    result.cleanup = await cleanup(peers.pages, peers.original.id);
    expect(await callSQL(`SELECT phase FROM chat.call_migrations WHERE call_id='${peers.original.id}'`)).toBe("aborted");
  } finally { firewall(sfuRules, false); await record("end-preparing", peers.pages, result); await peers.close(); }
});

test("End wins while one real client has switched and a late switch report is rejected", async ({ browser }) => {
  const peers = await active(browser);
  const result: Record<string, unknown> = { original: peers.original };
  let unblock: () => void = () => {};
  const held = new Promise<void>(resolve => { unblock = resolve; });
  let switchHeld = false;
  try {
    await peers.b.page.route("**/axon.calls.v1.CallsService/ReportCallMigration", async route => {
      if (route.request().postDataJSON().report === "MIGRATION_REPORT_SWITCHED") { switchHeld = true; await held; }
      await route.continue();
    });
    await control(peers.a, "RequestCallMigration", { callId: peers.original.id, generation: "1" });
    await expect.poll(() => switchHeld).toBe(true);
    expect(await callSQL(`SELECT document->'migration'->>'phase' FROM chat.calls WHERE id='${peers.original.id}'`)).toBe("committing");
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    await expect.poll(() => callSQL(`SELECT state FROM chat.calls WHERE id='${peers.original.id}'`)).toBe("ended");
    const late = peers.b.page.waitForResponse(response => response.url().endsWith("/ReportCallMigration") && response.request().postDataJSON().report === "MIGRATION_REPORT_SWITCHED");
    unblock();
    result.lateSwitchStatus = (await late).status();
    expect(result.lateSwitchStatus).toBe(400);
    result.cleanup = await cleanup(peers.pages, peers.original.id);
    expect(await callSQL(`SELECT state FROM chat.calls WHERE id='${peers.original.id}'`)).toBe("ended");
  } finally { unblock(); await record("end-committing", peers.pages, result); await peers.close(); }
});

test("missing old-transport cleanup acknowledgement ends a committed migration", async ({ browser }) => {
  const peers = await active(browser);
  const result: Record<string, unknown> = { original: peers.original, networkFault: "cleanup reports withheld on the real HTTP transport" };
  let unblock: () => void = () => {};
  const held = new Promise<void>(resolve => { unblock = resolve; });
  try {
    for (const page of peers.pages) await page.route("**/axon.calls.v1.CallsService/ReportCallMigration", async route => {
      if (route.request().postDataJSON().report === "MIGRATION_REPORT_CLEANUP") await held;
      await route.continue();
    });
    await control(peers.a, "RequestCallMigration", { callId: peers.original.id, generation: "1" });
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(4);
    unblock();
    result.cleanup = await cleanup(peers.pages, peers.original.id);
    expect(await callSQL(`SELECT document->>'end_reason' FROM chat.calls WHERE id='${peers.original.id}'`)).toBe("migration_cleanup_timeout");
  } finally { unblock(); await record("cleanup-timeout", peers.pages, result); await peers.close(); }
});

test("committed SFU preserves mute and camera settings, reconnects and obeys real session revocation", async ({ browser }) => {
  const peers = await active(browser);
  const result: Record<string, unknown> = { original: peers.original };
  try {
    await peers.a.page.getByRole("button", { name: "Mute microphone", exact: true }).click();
    await peers.a.page.getByRole("button", { name: "Turn camera off", exact: true }).click();
    const credentials = peers.a.page.waitForResponse(response => response.url().endsWith("/GetMigrationCredentials"));
    await control(peers.a, "RequestCallMigration", { callId: peers.original.id, generation: "1" });
    const token = (await (await credentials).json()).token;
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.migration?.phase)).toBe("completed");
    result.permissions = await peers.a.page.evaluate(() => { const m = window.axonMigrationEvidence.candidates.at(-1)!; return { muted: m.muted, cameraOff: m.cameraOff, audioEnabled: m.local.getAudioTracks().some(t => t.enabled), cameraEnabled: m.local.getVideoTracks().some(t => t.enabled) }; });
    expect(result.permissions).toEqual({ muted: true, cameraOff: true, audioEnabled: false, cameraEnabled: false });
    await peers.a.page.getByRole("button", { name: "Unmute microphone", exact: true }).click();
    await peers.a.page.getByRole("button", { name: "Turn camera on", exact: true }).click();
    result.media = await growth(peers.pages, true);
    const sockets = peers.a.network.opened;
    await peers.a.page.evaluate(() => window.axonMigrationEvidence.candidates.at(-1)!.room.simulateScenario("full-reconnect"));
    await expect.poll(() => peers.a.network.opened).toBeGreaterThan(sockets);
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(3);
    result.reconnectedMedia = await growth(peers.pages, true);
    const revokeAt = Date.now();
    const logout = await peers.a.context.request.post("http://localhost:8080/axon.auth.v1.AuthService/Logout", { headers: { Origin: "http://localhost:3000", "Connect-Protocol-Version": "1" }, data: {} });
    expect(logout.ok()).toBe(true);
    result.cleanup = await cleanup(peers.pages, peers.original.id);
    result.revokeCleanupObservedMs = Date.now() - revokeAt;
    const denied = [];
    for (const path of ["/rtc", "/rtc/validate", "/rtc/v1", "/rtc/v1/validate"]) {
      const response = await peers.b.context.request.get(`http://localhost:8080/livekit${path}?access_token=${encodeURIComponent(token)}`, { headers: { Origin: "http://localhost:3000" } });
      expect(response.status()).toBe(403); denied.push({ path, status: response.status() });
    }
    result.denied = denied;
  } finally { await record("revoke", peers.pages, result); await peers.close(); }
});

test("endpoint resume aborts a prepared candidate and uses a fresh P2P generation", async ({ browser }) => {
  const peers = await active(browser);
  const result: Record<string, unknown> = { original: peers.original };
  try {
    firewall(sfuRules, true);
    const credentials = peers.a.page.waitForResponse(response => response.url().endsWith("/GetMigrationCredentials"));
    await control(peers.a, "RequestCallMigration", { callId: peers.original.id, generation: "1" });
    const token = (await (await credentials).json()).token;
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonMigrationEvidence.candidates.length)).toBe(1);
    await peers.a.page.evaluate(() => window.axonCallManager!.channel.disconnect());
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => { const c = window.axonCallManager!.snapshot().call; return c?.state === 3 && c.generation === 3n && c.migration?.phase === "aborted"; })).toBe(true);
    for (const page of peers.pages) expect(await page.evaluate(() => { const e = window.axonMigrationEvidence; return e.source.pc.connectionState === "closed" && e.candidates.every(m => m.room.state === "disconnected" && [...m.tracks].every(t => t.readyState === "ended")); })).toBe(true);
    result.media = await growth(peers.pages, false, true);
    const denied = await peers.b.context.request.get(`http://localhost:8080/livekit/rtc/v1/validate?access_token=${encodeURIComponent(token)}`, { headers: { Origin: "http://localhost:3000" } });
    expect(denied.status()).toBe(403); result.oldCandidateStatus = denied.status();
    for (const page of peers.pages) await page.evaluate(() => { const m = window.axonCallManager!.snapshot().media!; if (!("room" in m)) for (const track of m.tracks) window.axonMigrationEvidence.source.tracks.add(track); });
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    result.cleanup = await cleanup(peers.pages, peers.original.id);
  } finally { firewall(sfuRules, false); await record("resume", peers.pages, result); await peers.close(); }
});

test.describe("P2P recovery after a completed rollback", () => {
  let peers: Awaited<ReturnType<typeof active>>;
  let sourceBlocked = false;
  let candidateBlocked = false;
  test.beforeAll(async ({ browser }) => {
    peers = await active(browser);
    firewall(sfuRules, true); candidateBlocked = true;
    await control(peers.a, "RequestCallMigration", { callId: peers.original.id, generation: "1" });
    for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.migration?.phase)).toBe("aborted");
    firewall(sfuRules, false); candidateBlocked = false;
  });
  test.afterAll(async () => {
    if (candidateBlocked) firewall(sfuRules, false);
    if (sourceBlocked) firewall(p2pRules, false);
    await peers?.close();
  });
  test("a later real P2P failure ends within bounded recovery without a second automatic migration", async () => {
    const result: Record<string, unknown> = { original: peers.original, networkFault: "P2P failed again after a healthy rollback; SFU now reachable" };
    try {
      const ended = Promise.race(peers.pages.map(page => page.waitForResponse(response => response.url().endsWith("/axon.calls.v1.CallsService/EndCall"))));
      firewall(p2pRules, true); sourceBlocked = true;
      for (const page of peers.pages) await expect.poll(() => page.evaluate(() => window.axonMigrationEvidence.samples.some(s => ["disconnected", "failed"].includes(s.sourceState)))).toBe(true);
      await expect.poll(() => peers.a.page.evaluate(() => !!(window.axonCallManager as unknown as { recoveryTimer?: unknown }).recoveryTimer)).toBe(true);
      const armedAt = Date.now();
      expect(await callSQL(`SELECT count(*) FROM chat.call_migrations WHERE call_id='${peers.original.id}'`)).toBe("1");
      expect((await ended).ok()).toBe(true);
      result.recoveryObservedMs = Date.now() - armedAt;
      expect(Number(result.recoveryObservedMs)).toBeLessThan(16_000);
      result.cleanup = await cleanup(peers.pages, peers.original.id);
      expect(await callSQL(`SELECT count(*) FROM chat.call_migrations WHERE call_id='${peers.original.id}'`)).toBe("1");
    } finally { await record("bounded-recovery", peers.pages, result); }
  });
});
