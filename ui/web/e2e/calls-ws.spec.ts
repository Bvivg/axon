import fs from "node:fs/promises";
import { expect, test } from "@playwright/test";
import { prepare, snapshot, assertClean, cleanup } from "./calls-spike-support";
import { callPeers, callSQL, callState, stopCallsRedis } from "./calls-ws-support";

declare global { interface Window { axonLegacyTest?: { socket: WebSocket; frames: string[] }; } }

for (const route of ["direct-audio", "relay-tls-video"]) {
  test(`Calls RPC outbox Redis activity WS ${route}`, async ({ browser }, info) => {
    const peers = await callPeers(browser);
    const pages = [peers.a.page, peers.b.page];
    const result: Record<string, unknown> = { route, engine: browser.version(), signaling: "Calls RPC → PostgreSQL outbox → UserSignalBus → Redis → existing Chat WS → ChatActivityProvider CallChannel", mockMedia: false };
    let callID = "";
    try {
      await peers.a.page.evaluate(({ token, roomID }) => {
        const socket = new WebSocket("ws://localhost:8080/ws/chat", ["axon.chat.v1", "axon.bearer." + token]);
        const frames: string[] = [];
        window.axonLegacyTest = { socket, frames };
        socket.onopen = () => socket.send(JSON.stringify({ type: "subscribe", room_id: roomID, since: 0 }));
        socket.onmessage = event => frames.push(JSON.parse(event.data).type);
        socket.onerror = () => frames.push("socket_error");
        socket.onclose = event => frames.push(`closed:${event.code}`);
      }, { token: peers.a.token, roomID: peers.roomID });
      await expect.poll(() => peers.a.page.evaluate(() => window.axonLegacyTest!.frames.includes("subscribed"))).toBe(true);
      await peers.a.page.evaluate(roomID => window.axonLegacyTest!.socket.send(JSON.stringify({ type: "send", room_id: roomID, client_id: crypto.randomUUID(), body: "Legacy socket compatibility" })), peers.roomID);
      await expect.poll(() => peers.a.page.evaluate(() => window.axonLegacyTest!.frames.includes("message") && window.axonLegacyTest!.frames.includes("ack"))).toBe(true);
      callID = await peers.a.page.evaluate(async ({ roomID, video }) => {
        const api = window.axonCallsProbe!;
        const response = await api.rpc.startCall({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), roomId: roomID, mode: video ? 2 : 1 });
        return response.call!.id;
      }, { roomID: peers.roomID, video: route.includes("video") });
      await expect.poll(() => callState(peers.b.page, callID)).toBe(1);
      await peers.b.page.evaluate(async id => {
        const api = window.axonCallsProbe!;
        await api.rpc.acceptCall({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id });
      }, callID);
      for (const page of pages) await expect.poll(() => callState(page, callID)).toBe(2);
      for (const page of pages) {
        await page.evaluate(id => {
          const api = window.axonCallsProbe!;
          const envelope = () => {
            const call = api.calls.get(id)!;
            return { callId: id, generation: call.generation, negotiationId: call.negotiationId, targetEndpointId: call.participants.find(p => p.userId !== api.userID)!.endpointId! };
          };
          window.axonForwardIce = async candidate => api.sendSignal({ ...envelope(), kind: 3, ice: { candidate: candidate.candidate ?? "", sdpMid: candidate.sdpMid ?? undefined, sdpMLineIndex: candidate.sdpMLineIndex ?? undefined, usernameFragment: candidate.usernameFragment ?? undefined } });
          api.onSignal = async signal => {
            const probe = window.axonSpike;
            if (signal.kind === 1) {
              await probe.setRemote({ type: "offer", sdp: signal.sdp! });
              await probe.pc.setLocalDescription(await probe.pc.createAnswer());
              api.sendSignal({ ...envelope(), kind: 2, sdp: probe.pc.localDescription!.sdp });
            } else if (signal.kind === 2) {
              await probe.setRemote({ type: "answer", sdp: signal.sdp! });
            } else if (signal.ice) {
              const candidate = { candidate: signal.ice.candidate, sdpMid: signal.ice.sdpMid, sdpMLineIndex: signal.ice.sdpMLineIndex, usernameFragment: signal.ice.usernameFragment };
              if (probe.pc.remoteDescription) await probe.pc.addIceCandidate(candidate);
              else probe.pending.push(candidate);
            }
          };
        }, callID);
      }
      const credentials = await Promise.all(pages.map(page => page.evaluate(async id => {
        const api = window.axonCallsProbe!, call = api.calls.get(id)!;
        const result = await api.rpc.getMediaCredentials({ endpoint: api.channel.endpoint, callId: id, generation: call.generation });
        return result.iceServers.map(server => ({ urls: server.urls.filter(url => url.startsWith("turns:")), username: server.username, credential: server.credential }));
      }, callID)));
      for (const servers of credentials) expect(servers[0].urls).toHaveLength(1);
      await Promise.all(pages.map((page, index) => prepare(page, route.includes("video"), route.startsWith("relay") ? { iceTransportPolicy: "relay", iceServers: credentials[index] } : { iceServers: [] })));
      await Promise.all(pages.map(page => page.evaluate(() => window.axonSpike.local.getTracks().forEach(track => { track.enabled = false; }))));
      await peers.a.page.evaluate(async id => {
        const api = window.axonCallsProbe!, call = api.calls.get(id)!, probe = window.axonSpike;
        await probe.pc.setLocalDescription(await probe.pc.createOffer());
        api.sendSignal({ callId: id, generation: call.generation, negotiationId: call.negotiationId, targetEndpointId: call.participants.find(p => p.userId !== api.userID)!.endpointId!, kind: 1, sdp: probe.pc.localDescription!.sdp });
      }, callID);
      for (const page of pages) await expect.poll(() => page.evaluate(() => window.axonSpike.pc.connectionState)).toBe("connected");
      for (const page of pages) await page.evaluate(async id => {
        const api = window.axonCallsProbe!, call = api.calls.get(id)!;
        await api.rpc.reportConnection({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id, generation: call.generation, ready: true });
      }, callID);
      for (const page of pages) {
        await expect.poll(() => callState(page, callID)).toBe(3);
        await page.evaluate(() => window.axonSpike.local.getTracks().forEach(track => { track.enabled = true; }));
        await page.locator("body").click({ position: { x: 2, y: 2 } });
      }
      const before = await Promise.all(pages.map(snapshot));
      for (let i = 0; i < pages.length; i++) await expect.poll(async () => {
        const next = await snapshot(pages[i]);
        return next.audioIn.bytes > before[i].audioIn.bytes && next.audioOut.bytes > before[i].audioOut.bytes && next.measuredAudio.energy > before[i].measuredAudio.energy && (!route.includes("video") || next.videoIn.frames > before[i].videoIn.frames);
      }).toBe(true);
      const after = await Promise.all(pages.map(snapshot));
      result.before = before; result.after = after;
      for (const item of after) {
        expect(item.selected).not.toBeNull();
        expect(item.selected!.local.candidateType === "relay").toBe(route.startsWith("relay"));
        expect(item.playback.paused).toBe(false);
      }
      result.call = await peers.a.page.evaluate(id => window.axonCallsProbe!.json(id), callID);
      result.slotsBeforeEnd = Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${callID}'`));
      expect(result.slotsBeforeEnd).toBe(2);
      await peers.a.page.evaluate(async id => {
        const api = window.axonCallsProbe!;
        await api.rpc.endCall({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id });
      }, callID);
      for (const page of pages) await expect.poll(() => callState(page, callID)).toBe(4);
      result.cleanup = await Promise.all(pages.map(assertClean));
      for (const page of pages) await page.evaluate(async id => {
        const api = window.axonCallsProbe!, call = api.calls.get(id)!;
        await api.rpc.acknowledgeCleanup({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id, generation: call.generation });
      }, callID);
      await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${callID}'`))).toBe(0);
      result.slotsAfterEnd = 0;
      result.room = peers.roomID;
      result.history = await peers.a.page.evaluate(async () => (await window.axonCallsProbe!.rpc.listCalls({})).calls.map(call => ({ id: call.id, state: call.state, version: call.version.toString() })));
      expect((result.history as { id: string }[]).some(call => call.id === callID)).toBe(true);
      result.legacyFrames = await peers.a.page.evaluate(() => [...window.axonLegacyTest!.frames]);
      expect((result.legacyFrames as string[]).filter(type => type.startsWith("call_"))).toHaveLength(0);
      result.protocolErrors = await Promise.all(pages.map(page => page.evaluate(() => window.axonCallsProbe!.errors)));
      expect(result.protocolErrors).toEqual([[], []]);
      result.passed = true;
    } catch (error) {
      result.error = String(error);
      result.failures = await Promise.all(pages.map(page => snapshot(page).catch(error => ({ error: String(error) }))));
      throw error;
    } finally {
      result.legacyFrames = await peers.a.page.evaluate(() => window.axonLegacyTest?.frames).catch(() => undefined);
      await peers.a.page.evaluate(() => window.axonLegacyTest?.socket.close()).catch(() => undefined);
      result.finalCleanup = await Promise.all(pages.map(page => cleanup(page).catch(() => null)));
      await fs.writeFile(`/evidence/${route}.json`, JSON.stringify({ ...result, callID, push: "не проверен", externalNAT: "внешние NAT не проверены", testTimeout: info.timeout }, null, 2));
      await peers.close();
    }
  });
}

test("Unacked call state resyncs after reconnect and old epoch is fenced", async ({ browser }) => {
  const peers = await callPeers(browser);
  const result: Record<string, unknown> = {};
  try {
    const callID = await peers.a.page.evaluate(async roomId => {
      const api = window.axonCallsProbe!; api.ack = false;
      return (await api.rpc.startCall({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), roomId, mode: 1 })).call!.id;
    }, peers.roomID);
    await expect.poll(() => callState(peers.a.page, callID)).toBe(1);
    const before = await peers.a.page.evaluate(() => ({ epoch: window.axonCallsProbe!.channel.endpoint!.epoch.toString(), events: window.axonCallsProbe!.seen.length }));
    result.before = before;
    await peers.a.page.evaluate(() => { window.axonCallsProbe!.ack = true; window.axonCallsProbe!.channel.disconnect(); });
    await expect.poll(() => peers.a.page.evaluate(() => window.axonCallsProbe!.channel.endpoint!.epoch.toString())).not.toBe(before.epoch);
    await expect.poll(() => peers.a.page.evaluate(() => window.axonCallsProbe!.snapshots)).toBeGreaterThan(1);
    await expect.poll(() => peers.a.page.evaluate(() => window.axonCallsProbe!.seen.length)).toBeGreaterThan(before.events);
    const stale = await peers.a.page.evaluate(async ({ id, epoch }) => {
      const api = window.axonCallsProbe!;
      try { await api.rpc.cancelCall({ callId: id, requestId: crypto.randomUUID(), endpoint: { ...api.channel.endpoint!, epoch: BigInt(epoch) } }); return "unexpected-success"; }
      catch (error) { return String(error); }
    }, { id: callID, epoch: before.epoch });
    expect(stale).toContain("failed_precondition");
    result.after = await peers.a.page.evaluate(() => ({ epoch: window.axonCallsProbe!.channel.endpoint!.epoch.toString(), snapshots: window.axonCallsProbe!.snapshots, events: window.axonCallsProbe!.seen }));
    result.staleEpochResult = stale;
    result.callID = callID;
    await peers.a.page.evaluate(async id => {
      const api = window.axonCallsProbe!;
      await api.rpc.cancelCall({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id });
      const call = (await api.rpc.getCall({ callId: id })).call!;
      await api.rpc.acknowledgeCleanup({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id, generation: call.generation });
    }, callID);
    await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${callID}'`))).toBe(0);
    result.slotsAfterCleanup = 0;
    result.passed = true;
  } finally { await fs.writeFile("/evidence/resync.json", JSON.stringify(result, null, 2)); await peers.close(); }
});

test("Revoked real auth family cannot register an endpoint or start a call", async ({ browser }) => {
  const peers = await callPeers(browser);
  const result: Record<string, unknown> = {};
  try {
    const activeSocket = peers.a.network.sockets.filter(socket => socket.url.endsWith("/ws/chat") && !socket.closed).at(-1);
    expect(activeSocket).toBeDefined();
    const endpoint = await peers.a.page.evaluate(() => {
      const ref = window.axonCallsProbe!.channel.endpoint!;
      return { id: ref.id, epoch: ref.epoch.toString(), proof: ref.proof };
    });
    const response = await peers.a.context.request.post("http://localhost:8080/axon.auth.v1.AuthService/Logout", { headers: { Origin: "http://localhost:3000", "Connect-Protocol-Version": "1" }, data: {} });
    expect(response.ok()).toBe(true);
    await expect.poll(() => activeSocket!.closed).toBe(true);
    const headers = { Authorization: "Bearer " + peers.a.token, "Connect-Protocol-Version": "1" };
    const denied = await peers.a.context.request.post("http://localhost:8080/axon.calls.v1.CallsService/RegisterEndpoint", { headers, data: {} });
    expect(denied.status()).toBe(403);
    const body = await denied.json();
    expect(body.code).toBe("permission_denied");
    result.endpointRegistrationStatus = denied.status();
    const startDenied = await peers.a.context.request.post("http://localhost:8080/axon.calls.v1.CallsService/StartCall", {
      headers, data: { endpoint, requestId: crypto.randomUUID(), roomId: peers.roomID, mode: "CALL_MODE_AUDIO" },
    });
    expect(startDenied.status()).toBe(403);
    expect((await startDenied.json()).code).toBe("permission_denied");
    result.startCallStatus = startDenied.status();
    const credentialsDenied = await peers.a.context.request.post("http://localhost:8080/axon.calls.v1.CallsService/GetMediaCredentials", {
      headers, data: { endpoint, callId: crypto.randomUUID(), generation: "1" },
    });
    expect(credentialsDenied.status()).toBe(403);
    result.mediaCredentialsStatus = credentialsDenied.status();
    result.errorCode = body.code;
    result.socketClosedOnRevocation = true;
    result.slots = Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE user_id='${peers.a.userID}'`));
    expect(result.slots).toBe(0);
    result.passed = true;
  } finally { await fs.writeFile("/evidence/revocation.json", JSON.stringify(result, null, 2)); await peers.close(); }
});

test("Committed call outbox survives a real Redis outage", async ({ browser }) => {
  const peers = await callPeers(browser);
  const result: Record<string, unknown> = { fault: "stop/start only axon-calls-e2e Redis container" };
  let restore: (() => Promise<void>) | undefined;
  try {
    restore = await stopCallsRedis();
    result.redisStopped = true;
    const callID = await peers.a.page.evaluate(async roomId => {
      const api = window.axonCallsProbe!;
      return (await api.rpc.startCall({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), roomId, mode: 1 })).call!.id;
    }, peers.roomID);
    result.callID = callID;
    await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_outbox WHERE call_id='${callID}' AND attempts>0`))).toBeGreaterThan(0);
    result.committedEventsDuringOutage = Number(await callSQL(`SELECT count(*) FROM chat.call_outbox WHERE call_id='${callID}'`));
    expect(result.committedEventsDuringOutage).toBe(2);
    for (const page of [peers.a.page, peers.b.page]) expect(await callState(page, callID)).toBeUndefined();
    result.deliveredDuringOutage = false;
    await restore(); restore = undefined;
    result.redisRestarted = true;
    for (const page of [peers.a.page, peers.b.page]) await expect.poll(() => callState(page, callID)).toBe(1);
    result.receivedAfterRestart = await Promise.all([peers.a.page, peers.b.page].map(page => page.evaluate(() => window.axonCallsProbe!.seen)));
    await peers.a.page.evaluate(async id => {
      const api = window.axonCallsProbe!;
      await api.rpc.cancelCall({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id });
      const call = (await api.rpc.getCall({ callId: id })).call!;
      await api.rpc.acknowledgeCleanup({ endpoint: api.channel.endpoint, requestId: crypto.randomUUID(), callId: id, generation: call.generation });
    }, callID);
    await expect.poll(async () => Number(await callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${callID}'`))).toBe(0);
    result.slotsAfterCleanup = 0;
    result.passed = true;
  } finally {
    if (restore) await restore();
    await fs.writeFile("/evidence/redis-recovery.json", JSON.stringify(result, null, 2));
    await peers.close();
  }
});
