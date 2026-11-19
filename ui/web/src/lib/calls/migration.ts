import { MigrationReport, type Call } from "@/gen/axon/calls/v1/calls_pb";
import type { CallChannel } from "@/lib/calls/channel";
import type { PeerMedia } from "@/lib/calls/p2p";
import type { SfuMedia } from "@/lib/calls/livekit";
import { callsClient } from "@/lib/connect/clients";

type Host = {
  userID: string;
  channel: CallChannel;
  apply: (call: Call) => Promise<void>;
  select: (media: PeerMedia | SfuMedia) => void;
  changed: () => void;
  allowed: () => boolean;
  failed: (message: string) => void;
  connected: (call: Call) => void;
  reconnecting: (call: Call) => void;
};

export function silenceMedia(media: PeerMedia | SfuMedia) {
  media.gate(false);
  const streams = [media.remote, ...("remotes" in media ? [...media.remotes.values()].map(tile => tile.stream) : [])];
  for (const element of document.querySelectorAll<HTMLMediaElement>("audio,video")) {
    if (streams.includes(element.srcObject as MediaStream)) { element.muted = true; element.pause(); element.srcObject = null; }
  }
}

export class CallMigration {
  readonly id: string;
  readonly source: PeerMedia;
  candidate?: SfuMedia;
  private latest: Call;
  private stopped = false;
  private preparing = false;
  private switched = false;
  private sourceClosed = false;
  private pending = new Set<MigrationReport>();
  private sent = new Set<MigrationReport>();
  private polling?: ReturnType<typeof setInterval>;
  private checking = false;

  constructor(call: Call, source: PeerMedia, private host: Host) {
    this.id = call.migration!.id;
    this.latest = call;
    this.source = source;
  }

  get phase() { return this.latest.migration?.phase; }
  get open() { return ["preparing", "committing", "cleaning", "aborting"].includes(this.phase ?? ""); }

  handle(call: Call) {
    this.latest = call;
    if (this.stopped) return;
    const phase = call.migration?.phase;
    if (phase !== "preparing") this.clearPoll();
    if (phase === "preparing" && !this.preparing) { this.preparing = true; void this.prepare(); }
    if (phase === "committing") void this.switch();
    if (phase === "cleaning") {
      if (!this.candidate || !this.switched) { this.host.failed("Connection switch could not be restored."); return; }
      if (!this.sourceClosed) { this.sourceClosed = true; this.source.close(); }
      this.candidate.gate(this.host.allowed());
      void this.report(MigrationReport.CLEANUP);
    }
    if (phase === "aborting" || phase === "aborted") void this.abort(phase === "aborting");
    if (phase === "completed") this.candidate?.gate(this.host.allowed());
  }

  private candidateCall(): Call {
    const call = this.latest;
    const migration = call.migration!;
    return { ...call, generation: migration.targetGeneration, transport: "sfu", migration: undefined, participants: call.participants.map(p => ({ ...p, mediaVersion: migration.participants.find(peer => peer.userId === p.userId)!.mediaVersion })) };
  }

  private async prepare() {
    let capture: MediaStream | undefined;
    let screen: MediaStreamTrack | undefined;
    try {
      const migration = this.latest.migration!;
      const credentials = await callsClient.getMigrationCredentials({ endpoint: this.host.channel.endpoint, callId: this.latest.id, migrationId: this.id, generation: migration.targetGeneration });
      if (this.stopped || this.phase !== "preparing") return;
      const { SfuMedia } = await import("@/lib/calls/livekit");
      if (this.stopped || this.phase !== "preparing") return;
      if (this.source.changing) throw new Error("Media device is changing.");
      capture = this.source.local.clone();
      screen = this.source.screenTrack?.clone();
      const call = this.candidateCall();
      const candidate = new SfuMedia(capture, call, { iceServers: credentials.iceServers.map(server => ({ urls: server.urls, username: server.username, credential: server.credential })) }, () => this.host.changed(), () => {
        if (!this.open && this.switched && !this.stopped) this.host.connected(call);
      }, () => {
        if (this.open) void this.report(MigrationReport.FAILED);
        else if (!this.stopped) this.host.reconnecting(call);
      }, () => {
        if (this.open) void this.report(MigrationReport.FAILED);
        else if (!this.stopped) this.host.failed("Media connection ended.");
      }, screen, true);
      candidate.muted = this.source.muted;
      candidate.cameraOff = this.source.cameraOff || this.source.sharing;
      this.candidate = candidate;
      this.host.changed();
      await candidate.join(credentials.url, credentials.token);
      const phase = String(this.phase);
      if (this.stopped || phase !== "preparing") { if (!["committing", "cleaning", "completed"].includes(phase)) await candidate.close(); return; }
      this.polling = setInterval(() => { void this.checkReady(); }, 100);
      void this.checkReady();
    } catch {
      capture?.getTracks().forEach(track => track.stop()); screen?.stop();
      if (!this.stopped && this.open) await this.report(MigrationReport.FAILED);
    }
  }

  private async checkReady() {
    const candidate = this.candidate;
    if (this.checking || !candidate || this.stopped || this.phase !== "preparing") return;
    this.checking = true;
    try {
      let packets = 0;
      for (const pc of candidate.pcs) {
        if (pc.connectionState !== "connected") continue;
        const stats = await pc.getStats();
        stats.forEach(row => { if (row.type === "outbound-rtp" && row.kind === "audio") packets += row.packetsSent ?? 0; });
      }
      if (candidate.pcs.length > 0 && candidate.pcs.every(pc => pc.connectionState === "connected") && candidate.room.remoteParticipants.size === 1 && packets >= 2 && this.phase === "preparing") await this.report(MigrationReport.READY);
    } catch { if (!this.stopped && this.phase === "preparing") await this.report(MigrationReport.FAILED); }
    finally { this.checking = false; }
  }

  private async switch() {
    if (!this.candidate || this.stopped || this.switched || this.phase !== "committing") return;
    this.switched = true;
    silenceMedia(this.source);
    this.candidate.muted = this.source.muted;
    this.candidate.cameraOff = this.source.cameraOff || this.source.sharing;
    this.host.select(this.candidate);
    await this.candidate.gate(this.host.allowed());
    if (this.stopped || this.phase !== "committing") return;
    await this.report(MigrationReport.SWITCHED);
  }

  private async abort(ack: boolean) {
    if (this.sourceClosed) { this.host.failed("Connection switch could not be rolled back."); return; }
    const candidate = this.candidate;
    if (candidate) {
      silenceMedia(candidate);
      await candidate.gate(false);
      if (this.switched) { this.source.muted = candidate.muted; this.source.cameraOff = candidate.cameraOff; }
      await candidate.close();
    }
    if (this.stopped) return;
    this.switched = false;
    this.host.select(this.source);
    this.source.gate(this.host.allowed());
    if (ack && this.phase === "aborting") await this.report(MigrationReport.ABORT_CLEANUP, this.source.pc.connectionState === "connected");
  }

  private async report(report: MigrationReport, sourceHealthy = false) {
    if (this.stopped || this.sent.has(report) || this.pending.has(report)) return;
    const migration = this.latest.migration;
    const peer = migration?.participants.find(peer => peer.userId === this.host.userID);
    if (!migration || !peer) return;
    this.pending.add(report);
    try {
      const response = await callsClient.reportCallMigration({ endpoint: this.host.channel.endpoint, requestId: crypto.randomUUID(), callId: this.latest.id, migrationId: this.id, generation: migration.targetGeneration, mediaVersion: peer.mediaVersion, report, sourceHealthy });
      this.sent.add(report);
      if (!this.stopped && response.call) await this.host.apply(response.call);
    } catch { if (!this.stopped) this.host.channel.resync(); }
    finally { this.pending.delete(report); }
  }

  private clearPoll() { if (this.polling) clearInterval(this.polling); this.polling = undefined; }
  async close(keepCandidate = false, keepSource = false) {
    this.stopped = true;
    this.clearPoll();
    if (!keepCandidate) { if (this.candidate) silenceMedia(this.candidate); await this.candidate?.close(); }
    if (!keepSource) this.source.close();
  }
}
