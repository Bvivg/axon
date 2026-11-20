import fs from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { expect, test, type Page } from "@playwright/test";
import type { PeerMedia } from "../src/lib/calls/p2p";
import { callPeers, callSQL, restartCallsChat } from "./calls-ws-support";
import { startCallFromChat, startChat } from "./chat-support";

declare global { interface Window { axonUIEvidence: { media: PeerMedia; context: AudioContext; analyser: AnalyserNode; energy: number; samples: number; timer: ReturnType<typeof setInterval> }; } }

async function observe(page: Page) {
  await expect.poll(() => page.evaluate(() => window.axonCallManager?.snapshot().call?.state)).toBe(3);
  await page.evaluate(async () => {
    const media = window.axonCallManager!.snapshot().media!;
    if (media.call.transport === "sfu") throw new Error("R1 evidence requires native P2P.");
    const context = new AudioContext(); await context.resume();
    const analyser = context.createAnalyser();
    const silent = context.createGain(); silent.gain.value = 0;
    context.createMediaStreamSource(media.remote).connect(analyser).connect(silent).connect(context.destination);
    const value = { media: media as PeerMedia, context, analyser, energy: 0, samples: 0, timer: 0 as unknown as ReturnType<typeof setInterval> };
    value.timer = setInterval(() => { const samples = new Float32Array(analyser.fftSize); analyser.getFloatTimeDomainData(samples); for (const sample of samples) { value.energy += sample * sample; value.samples++; } }, 25);
    window.axonUIEvidence = value;
  });
  const play = page.getByRole("button", { name: "Play audio", exact: true });
  if (await play.isVisible()) await play.click();
}

async function stats(page: Page) {
  return page.evaluate(async () => {
    const evidence = window.axonUIEvidence, report = await evidence.media.pc.getStats();
    const values: RTCStats[] = []; report.forEach(value => values.push(value));
    const rows = values as (RTCStats & { kind?: string; bytesReceived?: number; bytesSent?: number; packetsReceived?: number; framesDecoded?: number; selectedCandidatePairId?: string; localCandidateId?: string; remoteCandidateId?: string })[];
    const transport = rows.find(r => r.type === "transport" && r.selectedCandidatePairId);
    const pair = transport ? report.get(transport.selectedCandidatePairId!) : undefined;
    return {
      selected: pair ? { basis: "transport.selectedCandidatePairId", pair, local: report.get(pair.localCandidateId), remote: report.get(pair.remoteCandidateId) } : null,
      audioIn: rows.filter(r => r.type === "inbound-rtp" && r.kind === "audio").reduce((n, r) => n + (r.bytesReceived || 0), 0),
      audioOut: rows.filter(r => r.type === "outbound-rtp" && r.kind === "audio").reduce((n, r) => n + (r.bytesSent || 0), 0),
      frames: rows.filter(r => r.type === "inbound-rtp" && r.kind === "video").reduce((n, r) => n + (r.framesDecoded || 0), 0),
      energy: evidence.energy, samples: evidence.samples,
      playback: [...document.querySelectorAll<HTMLAudioElement>("audio[data-call-output]")].map(v => ({ paused: v.paused, muted: v.muted, time: v.currentTime })),
      state: window.axonCallManager!.snapshot().call?.state,
    };
  });
}

async function clean(page: Page) {
  return page.evaluate(async () => {
    const value = window.axonUIEvidence;
    clearInterval(value.timer); await value.context.close();
    return { connectionState: value.media.pc.connectionState, tracks: [...value.media.tracks].map(t => ({ kind: t.kind, readyState: t.readyState })), outputElements: [...document.querySelectorAll<HTMLMediaElement>("audio, video")].filter(v => v.srcObject).length, observerContext: value.context.state };
  });
}

for (const route of ["direct-audio", "relay-tls-video"]) test(`UI call ${route}, navigation, controls, End, history and redial`, async ({ browser }) => {
  const peers = await callPeers(browser, "ui");
  const pages = [peers.a.page, peers.b.page];
  const result: Record<string, unknown> = { route, engine: browser.version(), nativeMedia: true, signaling: "UI → CallManager → Calls RPC/outbox/Redis/activity WS → native PeerMedia" };
  const rules: string[][] = route.startsWith("relay") ? [["-p", "udp", "-d", "172.30.252.10", "-j", "REJECT"], ["-p", "udp", "-d", "172.30.252.20", "-j", "REJECT"]] : [];
  try {
    for (const rule of rules) execFileSync("iptables", ["-I", "OUTPUT", ...rule]);
    await startCallFromChat(peers.a.page, route.includes("video") ? "Video call" : "Audio call");
    await expect(peers.b.page.getByRole("dialog", { name: "Incoming call", exact: true })).toBeVisible();
    await peers.b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of pages) await observe(page);
    const callID = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    result.callID = callID;
    const before = await Promise.all(pages.map(stats));
    for (let i = 0; i < pages.length; i++) await expect.poll(async () => { const next = await stats(pages[i]); return next.audioIn > before[i].audioIn && next.audioOut > before[i].audioOut && next.energy > before[i].energy && (!route.includes("video") || next.frames > before[i].frames); }).toBe(true);
    result.before = before; result.after = await Promise.all(pages.map(stats));
    for (const state of result.after as Awaited<ReturnType<typeof stats>>[]) {
      expect(state.selected?.local.candidateType).toBe(route.startsWith("relay") ? "relay" : "host");
      expect(state.selected?.remote.candidateType).toBe(route.startsWith("relay") ? "relay" : "host");
      expect(state.playback).toEqual([expect.objectContaining({ paused: false, muted: false })]);
    }
    await peers.a.page.getByRole("button", { name: "Mute microphone", exact: true }).click();
    expect(await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().media!.local.getAudioTracks().every(t => !t.enabled))).toBe(true);
    await peers.a.page.getByRole("button", { name: "Unmute microphone", exact: true }).click();
    if (route.includes("video")) {
      await peers.a.page.getByRole("button", { name: "Turn camera off", exact: true }).click();
      await expect.poll(() => peers.a.page.evaluate(() => window.axonCallManager!.snapshot().media!.local.getVideoTracks().every(t => !t.enabled))).toBe(true);
      await peers.a.page.getByRole("button", { name: "Turn camera on", exact: true }).click();
      const camera = await peers.a.page.evaluate(async () => (await navigator.mediaDevices.enumerateDevices()).find(d => d.kind === "videoinput")!.label);
      await peers.a.page.getByRole("button", { name: "Call devices", exact: true }).click();
      await peers.a.page.getByRole("menuitemradio", { name: camera, exact: true }).click();
      await expect.poll(() => peers.a.page.evaluate(() => [...window.axonUIEvidence.media.tracks].filter(t => t.kind === "video" && t.readyState === "ended").length)).toBeGreaterThan(0);
      result.cameraReplacement = true;
    }
    await peers.a.page.getByRole("link", { name: "Calls", exact: true }).first().click();
    await expect(peers.a.page.getByRole("complementary", { name: "Section sidebar", exact: true }).getByRole("heading", { name: "Calls", exact: true })).toBeVisible();
    await expect(peers.a.page.getByRole("button", { name: "Return to call", exact: true })).toBeVisible();
    expect((await stats(peers.a.page)).state).toBe(3);
    result.slotsBeforeEnd = Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${callID}'`)); expect(result.slotsBeforeEnd).toBe(2);
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    for (const page of pages) await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(4);
    result.cleanup = await Promise.all(pages.map(clean));
    for (const value of result.cleanup as Awaited<ReturnType<typeof clean>>[]) { expect(value.connectionState).toBe("closed"); expect(value.tracks.every(t => t.readyState === "ended")).toBe(true); expect(value.outputElements).toBe(0); }
    await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${callID}'`))).toBe(0);
    result.slotsAfterEnd = 0; result.terminal = JSON.parse(await callSQL(`SELECT document FROM chat.calls WHERE id='${callID}'`));
    await peers.a.page.getByRole("navigation", { name: "Call history", exact: true }).getByRole("link", { name: /^All calls/ }).click();
    const historyRow = peers.a.page.locator(`[data-call-id="${callID}"]`);
    await expect(historyRow).toContainText("Completed");
    await expect(historyRow.getByRole("link", { name: /^Open chat with/ })).toBeVisible();
    await historyRow.getByRole("button", { name: "Call back", exact: true }).click();
    await expect.poll(() => peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call?.id ?? "")).not.toBe("");
    const redial = await peers.a.page.evaluate(() => {
      const call = window.axonCallManager!.snapshot().call!;
      return { id: call.id, roomId: call.roomId };
    });
    expect(redial.id).not.toBe(callID);
    expect(redial.roomId ?? "").toBe("");
    await expect(peers.b.page.getByRole("dialog", { name: "Incoming call", exact: true })).toBeVisible();
    await peers.b.page.getByRole("button", { name: "Decline", exact: true }).click();
    await expect(peers.a.page.getByRole("status").filter({ hasText: "Call declined" })).toBeVisible();
    result.redialAndDecline = true; result.passed = true;
  } finally {
    for (const rule of rules) execFileSync("iptables", ["-D", "OUTPUT", ...rule]);
    await fs.writeFile(`/evidence/ui-${route}.json`, JSON.stringify(result, null, 2));
    await peers.close();
  }
});

test("Active UI call reconnects and real auth revocation stops capture", async ({ browser }) => {
  const peers = await callPeers(browser, "ui");
  const result: Record<string, unknown> = {};
  try {
    await startCallFromChat(peers.a.page, "Audio call");
    await peers.b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of [peers.a.page, peers.b.page]) await observe(page);
    const before = await peers.a.page.evaluate(() => ({ callID: window.axonCallManager!.snapshot().call!.id, generation: window.axonCallManager!.snapshot().call!.generation.toString() }));
    result.before = before;
    await peers.a.page.evaluate(() => window.axonCallManager!.channel.disconnect());
    await expect.poll(() => peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.generation.toString())).not.toBe(before.generation);
    for (const page of [peers.a.page, peers.b.page]) {
      await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(3);
      const previous = await clean(page);
      expect(previous.tracks.every(t => t.readyState === "ended")).toBe(true);
      await observe(page);
    }
    result.afterReconnect = await Promise.all([peers.a.page, peers.b.page].map(stats));
    const response = await peers.a.context.request.post("http://localhost:8080/axon.auth.v1.AuthService/Logout", { headers: { Origin: "http://localhost:3000", "Connect-Protocol-Version": "1" }, data: {} });
    expect(response.ok()).toBe(true);
    await expect.poll(() => peers.a.page.evaluate(() => [...window.axonUIEvidence.media.tracks].every(t => t.readyState === "ended"))).toBe(true);
    result.revokedCleanup = await clean(peers.a.page);
    result.passed = true;
  } finally { await fs.writeFile("/evidence/ui-reconnect-revoke.json", JSON.stringify(result, null, 2)); await peers.close(); }
});

test("Two tabs cannot share endpoint ownership or both accept the call", async ({ browser }) => {
  const peers = await callPeers(browser, "ui");
  const clone = await peers.b.context.newPage();
  const result: Record<string, unknown> = {};
  try {
    const saved = await peers.b.page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith("axon.calls.endpoint."))));
    await clone.addInitScript(value => { for (const [key, item] of Object.entries(value)) sessionStorage.setItem(key, item); }, saved);
    await clone.goto("http://localhost:3000/calls");
    await expect.poll(() => clone.evaluate(() => window.axonCallManager?.snapshot().available)).toBe(true);
    const endpoints = await Promise.all([peers.b.page, clone].map(page => page.evaluate(() => window.axonCallManager!.channel.endpoint!.id)));
    expect(endpoints[0]).not.toBe(endpoints[1]); result.endpoints = endpoints;
    await startCallFromChat(peers.a.page, "Audio call");
    for (const page of [peers.b.page, clone]) await expect(page.getByRole("button", { name: "Accept", exact: true })).toBeVisible();
    await Promise.all([peers.b.page, clone].map(page => page.getByRole("button", { name: "Accept", exact: true }).click()));
    await expect.poll(() => peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call?.state)).toBe(3);
    const owners = await Promise.all([peers.b.page, clone].map(page => page.evaluate(() => ({ owns: window.axonCallManager!.owns(), media: !!window.axonCallManager!.snapshot().media }))));
    expect(owners.filter(value => value.owns && value.media)).toHaveLength(1);
    expect(owners.filter(value => !value.owns && !value.media)).toHaveLength(1);
    result.owners = owners;
    const callID = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${callID}'`))).toBe(0);
    result.slotsAfterEnd = 0; result.passed = true;
  } finally { await fs.writeFile("/evidence/ui-multi-tab.json", JSON.stringify(result, null, 2)); await clone.close(); await peers.close(); }
});

test("Native permission denial creates no call or occupied slot", async ({ browser }) => {
  const peers = await callPeers(browser, "ui");
  try {
    await peers.a.context.clearPermissions();
    const cdp = await peers.a.context.newCDPSession(peers.a.page);
    await cdp.send("Browser.setPermission", { permission: { name: "microphone" }, setting: "denied", origin: "http://localhost:3000" });
    await startCallFromChat(peers.a.page, "Audio call");
    const dialog = peers.a.page.getByRole("dialog", { name: "Could not start the call", exact: true });
    await expect(dialog.getByRole("alert")).toContainText("Allow microphone or camera access");
    await expect(dialog.getByRole("button", { name: "Try again", exact: true })).toBeEnabled();
    expect(Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE user_id='${peers.a.userID}'`))).toBe(0);
    await fs.writeFile("/evidence/ui-permission.json", JSON.stringify({ passed: true, typedPermissionMessage: true, slots: 0 }));
  } finally { await peers.close(); }
});

test("Busy attempt is recorded without disturbing an active call", async ({ browser }) => {
  const first = await callPeers(browser, "ui");
  const second = await callPeers(browser, "ui");
  try {
    await startCallFromChat(first.a.page, "Audio call");
    await first.b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of [first.a.page, first.b.page]) await observe(page);
    const activeID = await first.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    await second.a.page.goto("http://localhost:3000/chat");
    await startChat(second.a.page, first.b, "Busy call fixture");
    await startCallFromChat(second.a.page, "Audio call");
    await expect(second.a.page.getByRole("dialog", { name: "Could not start the call", exact: true }).getByRole("alert")).toContainText("already in a call");
    expect(Number(await callSQL(`SELECT count(*) FROM chat.calls WHERE caller_id='${second.a.userID}' AND state='ended' AND document->>'end_reason'='busy'`))).toBe(1);
    expect(Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${activeID}'`))).toBe(2);
    expect(await first.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.state)).toBe(3);
    await second.a.page.getByRole("dialog", { name: "Could not start the call", exact: true }).getByRole("button", { name: "Cancel", exact: true }).click();
    await second.a.page.getByRole("link", { name: "Calls", exact: true }).first().click();
    await expect(second.a.page.getByText("Outgoing · Busy", { exact: true })).toBeVisible();
    await first.a.page.getByRole("button", { name: "End call", exact: true }).click();
    await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${activeID}'`))).toBe(0);
    await fs.writeFile("/evidence/ui-busy.json", JSON.stringify({ passed: true, activeCallUnchanged: true, busyHistoryEntries: 1, slotsAfterEnd: 0 }));
  } finally { await first.close(); await second.close(); }
});

test("Active media recovers after a real Chat process restart", async ({ browser }) => {
  const peers = await callPeers(browser, "ui");
  const result: Record<string, unknown> = {};
  try {
    await startCallFromChat(peers.a.page, "Audio call");
    await peers.b.page.getByRole("button", { name: "Accept", exact: true }).click();
    for (const page of [peers.a.page, peers.b.page]) await observe(page);
    const id = await peers.a.page.evaluate(() => window.axonCallManager!.snapshot().call!.id);
    await restartCallsChat();
    for (const page of [peers.a.page, peers.b.page]) {
      await expect.poll(() => page.evaluate(() => window.axonCallManager!.snapshot().call!.generation > 1n && window.axonCallManager!.snapshot().call!.state === 3)).toBe(true);
      const previous = await clean(page); expect(previous.tracks.every(t => t.readyState === "ended")).toBe(true);
      await observe(page);
    }
    const before = await Promise.all([peers.a.page, peers.b.page].map(stats));
    for (let i = 0; i < 2; i++) await expect.poll(async () => (await stats([peers.a.page, peers.b.page][i])).audioIn > before[i].audioIn).toBe(true);
    result.stats = await Promise.all([peers.a.page, peers.b.page].map(stats));
    await peers.a.page.getByRole("button", { name: "End call", exact: true }).click();
    await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${id}'`))).toBe(0);
    result.slotsAfterEnd = 0; result.passed = true;
  } finally {
    result.finalStates = await Promise.all([peers.a.page, peers.b.page].map(page => page.evaluate(() => { const view = window.axonCallManager?.snapshot(); return { call: view?.call ? { id: view.call.id, state: view.call.state, generation: view.call.generation.toString(), version: view.call.version.toString() } : null, error: view?.error, resume: view?.resume, disconnected: view?.disconnected, pc: view?.media?.pc.connectionState, signaling: view?.media?.pc.signalingState }; })));
    await fs.writeFile("/evidence/ui-chat-restart.json", JSON.stringify(result, null, 2)); await peers.close();
  }
});
