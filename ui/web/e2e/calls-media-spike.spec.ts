import { expect, test } from "@playwright/test";
import { assertClean, blockTurnUDP, cleanup, evidence, negotiate, openPeers, prepare, snapshot, turnCredentials } from "./calls-spike-support";

const routes = [
  { name: "direct", url: null },
  { name: "relay-udp-3478", url: "turn:turn.calls.test:3478?transport=udp" },
  { name: "relay-tcp-3478", url: "turn:turn.calls.test:3478?transport=tcp" },
  { name: "relay-tls-5349-udp-blocked", url: "turns:turn.calls.test:5349?transport=tcp" },
];

for (const video of [false, true]) {
  for (const route of routes) {
    test(`${video ? "video" : "audio"} ${route.name} real media and cleanup`, async ({ browser }, info) => {
      const peers = await openPeers(browser);
      const blocked = route.name.includes("udp-blocked");
      const result: Record<string, unknown> = { requestedRoute: route.name, video, engineVersion: browser.version() };
      try {
        if (blocked) blockTurnUDP(true);
        const configuration: RTCConfiguration = route.url
          ? { iceTransportPolicy: "relay", iceServers: [{ urls: route.url, ...await turnCredentials() }] }
          : { iceServers: [] };
        await Promise.all(peers.pages.map(page => prepare(page, video, configuration)));
        await negotiate(...peers.pages);
        for (const page of peers.pages) {
          await expect.poll(() => page.evaluate(() => window.axonSpike.pc.connectionState)).toBe("connected");
          await page.locator("body").click({ position: { x: 2, y: 2 } });
        }
        const first = await Promise.all(peers.pages.map(snapshot));
        for (let i = 0; i < peers.pages.length; i++) {
          await expect.poll(async () => {
            const next = await snapshot(peers.pages[i]);
            return next.audioIn.bytes > first[i].audioIn.bytes
              && next.audioOut.bytes > first[i].audioOut.bytes
              && next.audioIn.packets > first[i].audioIn.packets
              && next.measuredAudio.energy > first[i].measuredAudio.energy
              && next.measuredAudio.samples > first[i].measuredAudio.samples
              && (!video || next.videoIn.frames > first[i].videoIn.frames && next.videoOut.frames > first[i].videoOut.frames);
          }).toBe(true);
        }
        const last = await Promise.all(peers.pages.map(snapshot));
        result.before = first;
        result.after = last;
        for (const endpoint of last) {
          expect(endpoint.selected).not.toBeNull();
          expect(endpoint.selected!.pair.state).toBe("succeeded");
          if (route.url) {
            expect(endpoint.selected!.local.candidateType).toBe("relay");
            expect(endpoint.selected!.remote.candidateType).toBe("relay");
          } else {
            expect(endpoint.selected!.local.candidateType).not.toBe("relay");
            expect(endpoint.selected!.remote.candidateType).not.toBe("relay");
          }
          expect(endpoint.playback.userActivated).toBe(true);
          expect(endpoint.playback.paused).toBe(false);
          expect(endpoint.measuredAudio.peak).toBeGreaterThan(0.001);
          if (!video) expect(endpoint.tracks.filter(track => track.kind === "video")).toHaveLength(0);
        }
        result.cleanup = await Promise.all(peers.pages.map(assertClean));
        result.recapture = await peers.pages[0].evaluate(async () => {
          const stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
          const before = stream.getTracks().map(track => track.readyState);
          stream.getTracks().forEach(track => track.stop());
          return { before, after: stream.getTracks().map(track => track.readyState) };
        });
        expect(result.recapture).toEqual({ before: ["live"], after: ["ended"] });
        result.passed = true;
      } catch (error) {
        result.error = String(error);
        result.failureSnapshots = await Promise.all(peers.pages.map(page => snapshot(page).catch(error => ({ error: String(error) }))));
        throw error;
      } finally {
        result.finalCleanup = await Promise.all(peers.pages.map(page => cleanup(page).catch(error => ({ error: String(error) }))));
        if (blocked) blockTurnUDP(false);
        await evidence(info, result);
        await peers.close();
      }
    });
  }
}

test("blocked TURN UDP reaches setup deadline and releases real capture", async ({ browser }, info) => {
  const peers = await openPeers(browser);
  const result: Record<string, unknown> = {};
  try {
    blockTurnUDP(true);
    await Promise.all(peers.pages.map(page => prepare(page, false, {
      iceTransportPolicy: "relay",
      iceServers: [],
    })));
    const credentials = await turnCredentials();
    await Promise.all(peers.pages.map(page => page.evaluate(credentials => {
      window.axonSpike.pc.setConfiguration({ iceTransportPolicy: "relay", iceServers: [{ urls: "turn:turn.calls.test:3478?transport=udp", ...credentials }] });
    }, credentials)));
    await negotiate(...peers.pages);
    await peers.pages[0].waitForTimeout(8000);
    result.snapshots = await Promise.all(peers.pages.map(snapshot));
    for (const page of peers.pages) {
      const state = await snapshot(page);
      expect(state.connectionState).not.toBe("connected");
      expect(state.selected).toBeNull();
      expect(state.audioIn.bytes).toBe(0);
    }
    result.deadlineMs = 8000;
    result.cleanup = await Promise.all(peers.pages.map(assertClean));
    result.passed = true;
  } finally {
    result.finalCleanup = await Promise.all(peers.pages.map(page => cleanup(page).catch(error => ({ error: String(error) }))));
    blockTurnUDP(false);
    await evidence(info, result);
    await peers.close();
  }
});

test("unavailable microphone device fails natively without leaking tracks", async ({ browser }, info) => {
  const peers = await openPeers(browser);
  try {
    const result = await peers.pages[0].evaluate(async () => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: { deviceId: { exact: "axon-spike-device-does-not-exist" } }, video: false });
        stream.getTracks().forEach(track => track.stop());
        return { name: "unexpected-success" };
      } catch (error) {
        return { name: (error as DOMException).name };
      }
    });
    await evidence(info, result);
    expect(["NotFoundError", "OverconstrainedError"]).toContain(result.name);
  } finally {
    await peers.close();
  }
});
