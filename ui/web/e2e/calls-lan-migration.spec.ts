import fs from "node:fs/promises";
import { networkInterfaces } from "node:os";
import { execFileSync } from "node:child_process";
import { createHmac } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import { newEmail, newUsername, password } from "./support";
import { startCallFromChat, startChat } from "./chat-support";
import type { PeerMedia } from "../src/lib/calls/p2p";
import type { SfuMedia } from "../src/lib/calls/livekit";
import type { CallMigration } from "../src/lib/calls/migration";

declare global { interface Window { lanMigration: { source: PeerMedia; candidate?: SfuMedia; pcs: Set<RTCPeerConnection>; samples: { time: number; phase?: string; source: boolean; target: boolean }[]; faultAt?: number; sourceProgressAt?: number; targetProgressAt?: number; stop: () => void }; } }
const origin = process.env.CALLS_TEST_ORIGIN || "https://axon.test";

test("Trusted LAN HTTPS automatically switches active P2P to SFU and releases both transports", async ({ browser }) => {
  test.skip(process.env.CALLS_LAN_MIGRATION !== "1", "Requires approved R3 LAN configuration and NET_ADMIN in the disposable browser container");
  const contexts = await Promise.all([0, 1].map(() => browser.newContext({ permissions: ["microphone", "camera"] })));
  const result: Record<string, unknown> = { origin, certificateChecks: true, physicalDevices: "not tested" };
  const rules = Object.values(networkInterfaces()).flat().filter(address => address?.family === "IPv4" && !address.internal).map(address => ["-p", "udp", "-d", address!.address, "-j", "REJECT"]);
  for (const protocol of ["udp", "tcp"]) for (const port of ["3478", "5349", "49160:49220"]) rules.push(["-p", protocol, "--dport", port, "-j", "REJECT"]);
  let blocked = false;
  const pages: Page[] = [];
  try {
    const people = [];
    for (const [index, context] of contexts.entries()) {
      const person = { email: newEmail("lan-migrate"), username: newUsername("lanmig"), name: `LAN migration ${index}`, page: await context.newPage() };
      pages.push(person.page);
      const response = await context.request.post(origin + "/api/axon.auth.v1.AuthService/Register", { headers: { Origin: origin, "Connect-Protocol-Version": "1" }, data: { email: person.email, username: person.username, displayName: person.name, password } });
      expect(response.ok()).toBe(true); people.push(person);
    }
    const [a, b] = people;
    await a.page.goto(origin + "/chat");
    await startChat(a.page, b, "LAN migration fixture");
    await b.page.goto(origin + "/calls");
    await startCallFromChat(a.page, "Video call");
    await b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages) {
      await expect.poll(() => page.evaluate(() => window.axonCallManager?.snapshot().call?.state)).toBe(3);
      await page.evaluate(() => {
        const manager = window.axonCallManager!, source = manager.snapshot().media!;
        if ("room" in source) throw new Error("Expected original native P2P");
        const evidence: Window["lanMigration"] = { source, pcs: new Set([source.pc]), samples: [], stop: () => {} };
        const sample = () => {
          const migration = (manager as unknown as { migration?: CallMigration }).migration;
          if (migration?.candidate) evidence.candidate = migration.candidate;
          evidence.candidate?.pcs.forEach(pc => evidence.pcs.add(pc));
          evidence.samples.push({ time: performance.now(), phase: manager.snapshot().call?.migration?.phase, source: source.local.getAudioTracks().some(t => t.enabled && t.readyState === "live"), target: evidence.candidate?.local.getAudioTracks().some(t => t.enabled && t.readyState === "live") ?? false });
        };
        const off = manager.subscribe(sample), timer = setInterval(sample, 10);
        let checking = false, sourcePackets = 0, sourceEnergy = 0;
        const progress = setInterval(async () => {
          if (checking) return;
          checking = true;
          try {
            let packets = 0, energy = 0;
            (await source.pc.getStats()).forEach(row => { if (row.type === "inbound-rtp" && row.kind === "audio") { packets += row.packetsReceived ?? 0; energy += row.totalAudioEnergy ?? 0; } });
            if (packets > sourcePackets && energy > sourceEnergy) evidence.sourceProgressAt = performance.now();
            sourcePackets = packets; sourceEnergy = energy;
            if (evidence.candidate && evidence.targetProgressAt === undefined) {
              let received = 0, decoded = 0;
              for (const pc of evidence.candidate.pcs) (await pc.getStats()).forEach(row => { if (row.type === "inbound-rtp" && row.kind === "audio") { received += row.packetsReceived ?? 0; decoded += row.totalAudioEnergy ?? 0; } });
              if (received > 0 && decoded > 0) evidence.targetProgressAt = performance.now();
            }
          } catch {} finally { checking = false; }
        }, 100);
        evidence.stop = () => { off(); clearInterval(timer); clearInterval(progress); sample(); };
        window.lanMigration = evidence; sample();
      });
    }
    const read = (page: Page, target: boolean) => page.evaluate(async target => {
      const m = target ? window.lanMigration.candidate! : window.lanMigration.source;
      const pcs = "pcs" in m ? m.pcs : [m.pc];
      let audio = 0, energy = 0, frames = 0;
      const selected: { local: string; remote: string; protocol: string }[] = [];
      for (const pc of pcs) {
        const report = await pc.getStats();
        report.forEach(row => {
          if (row.type === "inbound-rtp" && row.kind === "audio") { audio += row.bytesReceived ?? 0; energy += row.totalAudioEnergy ?? 0; }
          if (row.type === "inbound-rtp" && row.kind === "video") frames += row.framesDecoded ?? 0;
          if (row.type === "transport" && row.selectedCandidatePairId) { const pair = report.get(row.selectedCandidatePairId); const local = report.get(pair.localCandidateId), remote = report.get(pair.remoteCandidateId); selected.push({ local: local.candidateType, remote: remote.candidateType, protocol: local.protocol }); }
        });
      }
      return { audio, energy, frames, selected };
    }, target);
    const prove = async (target: boolean) => {
      const before = await Promise.all(pages.map(p => read(p, target)));
      for (let i = 0; i < pages.length; i++) await expect.poll(async () => { const next = await read(pages[i], target); return next.audio - before[i].audio > 1000 && next.energy - before[i].energy > 0.0001 && next.frames > before[i].frames && next.selected.length > 0; }).toBe(true);
      return { before, after: await Promise.all(pages.map(p => read(p, target))) };
    };
    result.sourceMedia = await prove(false);
    const original = await a.page.evaluate(() => { const c = window.axonCallManager!.snapshot().call!; return { id: c.id, started: c.startedAt!.seconds.toString() }; });
    result.original = original;
    for (const page of pages) await page.evaluate(() => { window.lanMigration.faultAt = performance.now(); });
    for (const rule of rules) execFileSync("iptables", ["-I", "OUTPUT", ...rule]); blocked = true;
    for (const page of pages) await expect.poll(() => page.evaluate(() => ["disconnected", "failed", "closed"].includes(window.lanMigration.source.pc.connectionState))).toBe(true);
    for (const page of pages) await expect.poll(() => page.evaluate(() => !!window.axonCallManager!.snapshot().call?.migration?.id)).toBe(true);
    for (const page of pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.migration?.phase)).toBe("completed");
    for (const page of pages) expect(await page.evaluate(() => { const c = window.axonCallManager!.snapshot().call!; return { id: c.id, started: c.startedAt!.seconds.toString() }; })).toEqual(original);
    result.targetMedia = await prove(true);
    for (const page of pages) await expect.poll(() => page.evaluate(() => { const e = window.lanMigration; return e.sourceProgressAt !== undefined && e.targetProgressAt !== undefined; })).toBe(true);
    result.timing = await Promise.all(pages.map(page => page.evaluate(() => { const e = window.lanMigration; return { sampleIntervalMs: 100, faultAt: e.faultAt, lastP2PAudioAt: e.sourceProgressAt, firstSFUAudioAt: e.targetProgressAt, decodedAudioGapMs: e.targetProgressAt! - e.sourceProgressAt! }; })));
    for (const value of result.timing as { decodedAudioGapMs: number }[]) expect(value.decodedAudioGapMs).toBeGreaterThan(0);
    const env = await fs.readFile("/evidence-sfu/media.env", "utf8");
    const values = Object.fromEntries(env.trim().split("\n").map(line => { const at = line.indexOf("="); return [line.slice(0, at), line.slice(at + 1)]; }));
    const admin = async (method: string, data: object, video: object) => {
      const head = Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url");
      const body = head + "." + Buffer.from(JSON.stringify({ iss: values.LIVEKIT_API_KEY, sub: "lan-migration-evidence", exp: Math.floor(Date.now() / 1000) + 30, video })).toString("base64url");
      const token = body + "." + createHmac("sha256", values.LIVEKIT_API_SECRET).update(body).digest("base64url");
      const response = await contexts[0].request.post(`http://livekit:7880/twirp/livekit.RoomService/${method}`, { headers: { Authorization: `Bearer ${token}` }, data });
      expect(response.ok()).toBe(true); return response.json();
    };
    result.server = await admin("ListParticipants", { room: `call_${original.id}` }, { roomAdmin: true, room: `call_${original.id}` });
    expect((result.server as { participants: unknown[] }).participants).toHaveLength(2);
    for (const page of pages) expect(await page.evaluate(() => window.lanMigration.samples.some(s => s.source && s.target))).toBe(false);
    await a.page.getByRole("button", { name: "End call", exact: true }).click();
    for (const page of pages) await expect.poll(() => page.evaluate(() => { const e = window.lanMigration; return e.source.pc.connectionState === "closed" && e.candidate?.room.state === "disconnected" && [...e.pcs].every(pc => pc.connectionState === "closed") && [...e.source.tracks, ...e.candidate.tracks].every(t => t.readyState === "ended"); })).toBe(true);
    await expect.poll(async () => ((await admin("ListRooms", {}, { roomList: true })).rooms ?? []).some((room: { name: string }) => room.name === `call_${original.id}`)).toBe(false);
    result.cleanup = await Promise.all(pages.map(page => page.evaluate(() => { const e = window.lanMigration; return { source: e.source.pc.connectionState, room: e.candidate!.room.state, pcs: [...e.pcs].map(pc => pc.connectionState), tracks: [...e.source.tracks, ...e.candidate!.tracks].map(t => ({ kind: t.kind, state: t.readyState })) }; })));
    result.passed = true;
  } finally {
    if (blocked) for (const rule of rules) execFileSync("iptables", ["-D", "OUTPUT", ...rule]);
    result.timelines = await Promise.all(pages.map(page => page.evaluate(() => { window.lanMigration?.stop(); return window.lanMigration?.samples; }).catch(() => null)));
    await fs.writeFile("/evidence/lan-migration.json", JSON.stringify(result, null, 2));
    await Promise.all(contexts.map(context => context.close()));
  }
});
