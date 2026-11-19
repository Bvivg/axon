import { execFileSync } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { expect, type Browser, type BrowserContext, type Page, type TestInfo } from "@playwright/test";

type Counter = { bytes: number; packets: number; frames: number; samples: number; energy: number };
type Candidate = Record<string, unknown>;
export type MediaSnapshot = {
  connectionState: string;
  iceState: string;
  gatheringState: string;
  candidates: Candidate[];
  audioIn: Counter;
  audioOut: Counter;
  videoIn: Counter;
  videoOut: Counter;
  selected: { basis: string; pair: Candidate; local: Candidate; remote: Candidate } | null;
  measuredAudio: { samples: number; energy: number; peak: number };
  audioContextState: string;
  playback: { paused: boolean; currentTime: number; userActivated: boolean };
  tracks: { id: string; kind: string; readyState: string }[];
  iceErrors: { code: number; text: string; url: string }[];
};

type Probe = {
  pc: RTCPeerConnection;
  local: MediaStream;
  remote: MediaStream;
  tracks: MediaStreamTrack[];
  pending: RTCIceCandidateInit[];
  context: AudioContext;
  analyser: AnalyserNode;
  element: HTMLMediaElement;
  audio: { samples: number; energy: number; peak: number };
  timer: ReturnType<typeof setInterval>;
  gesture: () => void;
  stopped: boolean;
  iceErrors: { code: number; text: string; url: string }[];
  setRemote: (description: RTCSessionDescriptionInit) => Promise<void>;
};

declare global {
  interface Window {
    axonSpike: Probe;
    axonForwardIce: (candidate: RTCIceCandidateInit) => Promise<void>;
  }
}

export async function evidence(info: TestInfo, value: unknown) {
  const directory = process.env.CALLS_EVIDENCE_DIR ?? "/evidence";
  const name = `${info.project.name}-${info.title.replaceAll(/[^a-z0-9]+/gi, "-")}.json`;
  await fs.mkdir(path.join(directory, "cases"), { recursive: true });
  await fs.writeFile(path.join(directory, "cases", name), JSON.stringify({
    test: info.title,
    browser: info.project.name,
    timeout: info.timeout,
    mockMedia: false,
    signaling: "diagnostic Playwright harness; not AXON WS signaling",
    room: "N/A: no business backend in spike 1.1",
    participants: "two native browser endpoints",
    slots: "N/A: no Calls tables in spike 1.1",
    push: "не проверен",
    externalNAT: "внешние NAT не проверены",
    result: value,
  }, null, 2));
}

export async function turnCredentials() {
  return JSON.parse(await fs.readFile("/evidence/.secrets/credentials.json", "utf8")) as { username: string; credential: string };
}

export function blockTurnUDP(block: boolean) {
  execFileSync("iptables", [block ? "-I" : "-D", "OUTPUT", "-p", "udp", "-d", "172.30.251.20", "--dport", "3478", "-j", "REJECT"]);
}

export async function openPeers(browser: Browser) {
  const contexts: BrowserContext[] = [];
  const profiles: string[] = [];
  const close = async () => {
    try { await Promise.all(contexts.map(context => context.close())); }
    finally { await Promise.all(profiles.map(profile => fs.rm(profile, { recursive: true, force: true }))); }
  };
  try {
    for (let i = 0; i < 2; i++) {
      if (browser.browserType().name() === "firefox") {
        const profile = await fs.mkdtemp("/tmp/axon-spike-firefox-");
        profiles.push(profile);
        execFileSync("certutil", ["-N", "--empty-password", "-d", `sql:${profile}`]);
        execFileSync("certutil", ["-A", "-d", `sql:${profile}`, "-n", "axon-spike", "-t", "C,,", "-i", "/evidence/.secrets/ca.crt"]);
        contexts.push(await browser.browserType().launchPersistentContext(profile, {
          firefoxUserPrefs: { "media.navigator.streams.fake": true, "media.navigator.permission.disabled": true },
        }));
      } else {
        contexts.push(await browser.newContext());
      }
    }
    if (browser.browserType().name() === "chromium") {
      await Promise.all(contexts.map(context => context.grantPermissions(["microphone", "camera"])));
    }
    const pages = await Promise.all(contexts.map(context => context.newPage()));
    await Promise.all(pages.map(page => page.goto("http://localhost:3000/login")));
    await Promise.all(pages.map(page => expect.poll(() => page.evaluate(() => window.isSecureContext && !!navigator.mediaDevices)).toBe(true)));
    for (let i = 0; i < pages.length; i++) {
      const other = pages[1 - i];
      await pages[i].exposeFunction("axonForwardIce", async (candidate: RTCIceCandidateInit) => {
        if (other.isClosed()) return;
        await other.evaluate(async candidate => {
          const probe = window.axonSpike;
          if (!probe || probe.stopped) return;
          if (probe.pc.remoteDescription) await probe.pc.addIceCandidate(candidate);
          else probe.pending.push(candidate);
        }, candidate);
      });
    }
    return { pages: pages as [Page, Page], close };
  } catch (error) {
    await close();
    throw error;
  }
}

export async function prepare(page: Page, video: boolean, configuration: RTCConfiguration) {
  await page.evaluate(async ({ video, configuration }) => {
    async function nativeStep<T>(name: string, operation: () => T | Promise<T>): Promise<T> {
      try { return await operation(); } catch (error) { throw new Error(`${name}: ${String(error)}`); }
    }
    const pc = await nativeStep("RTCPeerConnection", () => new RTCPeerConnection(configuration));
    const local = await nativeStep("getUserMedia", () => navigator.mediaDevices.getUserMedia({ audio: true, video }));
    const remote = await nativeStep("MediaStream", () => new MediaStream());
    const element = document.createElement(video ? "video" : "audio");
    element.controls = true;
    await nativeStep("remote srcObject", () => { element.srcObject = remote; });
    element.setAttribute("data-axon-spike", "remote-media");
    document.body.append(element);
    const context = await nativeStep("AudioContext", () => new AudioContext());
    const analyser = context.createAnalyser();
    const audio = { samples: 0, energy: 0, peak: 0 };
    let source: MediaStreamAudioSourceNode | undefined;
    const silent = context.createGain();
    silent.gain.value = 0;
    analyser.connect(silent).connect(context.destination);
    const gesture = () => {
      void context.resume().catch(error => { if (context.state !== "closed") console.error(String(error)); });
      if (remote.getTracks().length) void element.play().catch(() => undefined);
    };
    document.addEventListener("click", gesture);
    const probe: Probe = {
      pc, local, remote, context, analyser, element, audio, gesture,
      tracks: [...local.getTracks()], pending: [], stopped: false, iceErrors: [],
      timer: setInterval(() => {
        if (!source || context.state !== "running") return;
        const samples = new Float32Array(analyser.fftSize);
        analyser.getFloatTimeDomainData(samples);
        for (const sample of samples) {
          audio.samples++;
          audio.energy += sample * sample;
          audio.peak = Math.max(audio.peak, Math.abs(sample));
        }
      }, 25),
      setRemote: async description => {
        await pc.setRemoteDescription(description);
        for (const candidate of probe.pending.splice(0)) await pc.addIceCandidate(candidate);
      },
    };
    window.axonSpike = probe;
    pc.onicecandidate = event => {
      if (event.candidate && !probe.stopped) void window.axonForwardIce(event.candidate.toJSON());
    };
    pc.onicecandidateerror = event => probe.iceErrors.push({ code: event.errorCode, text: event.errorText, url: event.url });
    pc.ontrack = event => {
      remote.addTrack(event.track);
      probe.tracks.push(event.track);
      if (event.track.kind === "audio") source = context.createMediaStreamSource(new MediaStream([event.track]));
      source?.connect(analyser);
    };
    for (const track of local.getTracks()) pc.addTrack(track, local);
  }, { video, configuration });
  await page.locator("body").click({ position: { x: 2, y: 2 } });
}

export async function negotiate(first: Page, second: Page) {
  const offer = await first.evaluate(async () => {
    const pc = window.axonSpike.pc;
    await pc.setLocalDescription(await pc.createOffer());
    return pc.localDescription!.toJSON();
  });
  const answer = await second.evaluate(async offer => {
    const probe = window.axonSpike;
    await probe.setRemote(offer);
    await probe.pc.setLocalDescription(await probe.pc.createAnswer());
    return probe.pc.localDescription!.toJSON();
  }, offer);
  await first.evaluate(answer => window.axonSpike.setRemote(answer), answer);
}

export async function snapshot(page: Page): Promise<MediaSnapshot> {
  return page.evaluate(async () => {
    const probe = window.axonSpike;
    const report = await probe.pc.getStats();
    const values: Record<string, unknown>[] = [];
    report.forEach(value => values.push(value));
    const counter = (type: string, kind: string): Counter => values
      .filter(value => value.type === type && (value.kind ?? value.mediaType) === kind && !value.isRemote)
      .reduce<Counter>((sum, value) => ({
        bytes: sum.bytes + Number(value.bytesReceived ?? value.bytesSent ?? 0),
        packets: sum.packets + Number(value.packetsReceived ?? value.packetsSent ?? 0),
        frames: sum.frames + Number(value.framesDecoded ?? value.framesEncoded ?? 0),
        samples: sum.samples + Number(value.totalSamplesReceived ?? 0),
        energy: sum.energy + Number(value.totalAudioEnergy ?? 0),
      }), { bytes: 0, packets: 0, frames: 0, samples: 0, energy: 0 });
    const transport = values.find(value => value.type === "transport" && value.selectedCandidatePairId);
    const pair = transport
      ? report.get(String(transport.selectedCandidatePairId))
      : values.find(value => value.type === "candidate-pair" && value.selected === true);
    const local = pair ? report.get(String(pair.localCandidateId)) : undefined;
    const remote = pair ? report.get(String(pair.remoteCandidateId)) : undefined;
    return {
      connectionState: probe.pc.connectionState,
      iceState: probe.pc.iceConnectionState,
      gatheringState: probe.pc.iceGatheringState,
      candidates: values.filter(value => ["local-candidate", "remote-candidate", "candidate-pair"].includes(String(value.type))),
      audioIn: counter("inbound-rtp", "audio"), audioOut: counter("outbound-rtp", "audio"),
      videoIn: counter("inbound-rtp", "video"), videoOut: counter("outbound-rtp", "video"),
      selected: pair && local && remote ? { basis: transport ? "transport.selectedCandidatePairId" : "candidate-pair.selected", pair, local, remote } : null,
      measuredAudio: { ...probe.audio },
      audioContextState: probe.context.state,
      playback: { paused: probe.element.paused, currentTime: probe.element.currentTime, userActivated: navigator.userActivation.hasBeenActive },
      tracks: probe.tracks.map(track => ({ id: track.id, kind: track.kind, readyState: track.readyState })),
      iceErrors: [...probe.iceErrors],
    };
  });
}

export async function cleanup(page: Page) {
  return page.evaluate(async () => {
    const probe = window.axonSpike;
    if (!probe) return { initialized: false };
    probe.stopped = true;
    clearInterval(probe.timer);
    document.removeEventListener("click", probe.gesture);
    probe.pc.onicecandidate = null;
    probe.pc.onicecandidateerror = null;
    probe.pc.ontrack = null;
    probe.pc.close();
    for (const track of probe.tracks) track.stop();
    probe.element.pause();
    probe.element.srcObject = null;
    probe.element.remove();
    if (probe.context.state !== "closed") await probe.context.close();
    return {
      initialized: true,
      connectionState: probe.pc.connectionState,
      iceState: probe.pc.iceConnectionState,
      audioContextState: probe.context.state,
      mediaElements: document.querySelectorAll("[data-axon-spike]").length,
      pendingICE: probe.pending.splice(0).length,
      tracks: probe.tracks.map(track => ({ id: track.id, kind: track.kind, readyState: track.readyState })),
    };
  });
}

export async function assertClean(page: Page) {
  const result = await cleanup(page);
  expect(result.initialized).toBe(true);
  expect(result.connectionState).toBe("closed");
  expect(result.audioContextState).toBe("closed");
  expect(result.mediaElements).toBe(0);
  expect(result.tracks!.length).toBeGreaterThan(0);
  expect(result.tracks!.every(track => track.readyState === "ended")).toBe(true);
  return result;
}
