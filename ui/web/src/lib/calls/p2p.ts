import { create } from "@bufbuild/protobuf";

import { CallMode, PeerSignalSchema, PeerSignalKind, type Call, type PeerSignal } from "@/gen/axon/calls/v1/calls_pb";
import type { CallChannel } from "@/lib/calls/channel";

export class PeerMedia {
  readonly pc: RTCPeerConnection;
  readonly remote = new MediaStream();
  readonly tracks = new Set<MediaStreamTrack>();
  private pending: RTCIceCandidateInit[] = [];
  private makingOffer = false;
  private ignoreOffer = false;
  private settingAnswer = false;
  private closed = false;
  private active = false;
  private signalQueue = Promise.resolve();
  private screen?: MediaStreamTrack;
  private changingSources = 0;
  muted = false;
  cameraOff = false;

  constructor(readonly local: MediaStream, readonly call: Call, private userID: string, private channel: CallChannel, configuration: RTCConfiguration, private changed: () => void, private connected: () => void, private failed: (message: string) => void, private connectionChanged?: (state: RTCPeerConnectionState) => void) {
    this.pc = new RTCPeerConnection(configuration);
    this.cameraOff = !local.getVideoTracks().length;
    local.getTracks().forEach(track => { this.tracks.add(track); track.enabled = false; this.pc.addTrack(track, local); });
    if (call.mode === CallMode.VIDEO && userID === call.callerId && this.cameraOff) this.pc.addTransceiver("video", { direction: "recvonly" });
    this.pc.onicecandidate = event => {
      if (event.candidate) this.send({ kind: PeerSignalKind.ICE, ice: { candidate: event.candidate.candidate, sdpMid: event.candidate.sdpMid ?? undefined, sdpMLineIndex: event.candidate.sdpMLineIndex ?? undefined, usernameFragment: event.candidate.usernameFragment ?? undefined } });
    };
    this.pc.ontrack = event => {
      this.tracks.add(event.track);
      this.remote.addTrack(event.track);
      this.changed();
    };
    this.pc.onconnectionstatechange = () => {
      this.connectionChanged?.(this.pc.connectionState);
      if (this.pc.connectionState === "connected") this.connected();
      if (this.pc.connectionState === "failed") this.failed("Connection failed. Try calling again.");
      if (this.pc.connectionState === "disconnected") this.pc.restartIce();
      this.changed();
    };
    this.pc.onnegotiationneeded = () => {
      if (!this.pc.localDescription && this.userID !== this.call.callerId) return;
      void this.offer().catch(() => { if (!this.closed) this.failed("Could not negotiate the call."); });
    };
    if (this.userID === this.call.callerId) queueMicrotask(() => { void this.offer().catch(() => { if (!this.closed) this.failed("Could not negotiate the call."); }); });
  }

  private send(input: Parameters<typeof create<typeof PeerSignalSchema>>[1]) {
    if (this.closed) return;
    const target = this.call.participants.find(p => p.userId !== this.userID)?.endpointId;
    if (!target) return;
    this.channel.signal(create(PeerSignalSchema, { ...input, callId: this.call.id, generation: this.call.generation, negotiationId: this.call.negotiationId, targetEndpointId: target }));
  }

  private async offer() {
    if (this.makingOffer || this.closed || this.pc.signalingState !== "stable") return;
    try {
      this.makingOffer = true;
      if (this.closed || this.pc.signalingState !== "stable") return;
      await this.pc.setLocalDescription();
      if (!this.closed) this.send({ kind: PeerSignalKind.OFFER, sdp: this.pc.localDescription!.sdp });
    } finally { this.makingOffer = false; }
  }

  receive(signal: PeerSignal) {
    this.signalQueue = this.signalQueue.then(() => this.apply(signal)).catch(() => {
      if (!this.closed) this.failed("Could not establish the call. Try again.");
    });
    return this.signalQueue;
  }

  private async apply(signal: PeerSignal) {
    if (this.closed || signal.generation !== this.call.generation || signal.negotiationId !== this.call.negotiationId) return;
    if (signal.kind === PeerSignalKind.ICE && signal.ice) {
      const ice = signal.ice;
      const candidate = { candidate: ice.candidate, sdpMid: ice.sdpMid, sdpMLineIndex: ice.sdpMLineIndex, usernameFragment: ice.usernameFragment };
      if (!this.pc.remoteDescription) {
        if (this.pending.length >= 128) throw new Error("ICE queue exceeded");
        this.pending.push(candidate);
      } else {
        try { await this.pc.addIceCandidate(candidate); } catch (error) { if (!this.ignoreOffer) throw error; }
      }
      return;
    }
    const offer = signal.kind === PeerSignalKind.OFFER;
    if (!offer && signal.kind !== PeerSignalKind.ANSWER) return;
    const collision = offer && (this.makingOffer || (this.pc.signalingState !== "stable" && !this.settingAnswer));
    const polite = this.userID.localeCompare(this.call.participants.find(p => p.userId !== this.userID)!.userId) > 0;
    this.ignoreOffer = !polite && collision;
    if (this.ignoreOffer) return;
    this.settingAnswer = !offer;
    try { await this.pc.setRemoteDescription({ type: offer ? "offer" : "answer", sdp: signal.sdp }); }
    finally { this.settingAnswer = false; }
    for (const candidate of this.pending.splice(0)) await this.pc.addIceCandidate(candidate);
    if (offer) {
      await this.pc.setLocalDescription();
      if (!this.closed) this.send({ kind: PeerSignalKind.ANSWER, sdp: this.pc.localDescription!.sdp });
    }
  }

  gate(active: boolean) {
    this.active = active;
    for (const track of this.local.getAudioTracks()) track.enabled = active && !this.muted;
    for (const track of this.local.getVideoTracks()) track.enabled = active && !this.cameraOff;
    if (this.screen) this.screen.enabled = active;
  }

  mute() { this.muted = !this.muted; this.gate(this.active); this.changed(); }
  async camera() {
    this.changingSources++;
    try {
      if (!this.cameraOff && !this.local.getVideoTracks().some(track => track.readyState === "live")) this.cameraOff = true;
      if (this.cameraOff && !this.local.getVideoTracks().some(track => track.readyState === "live")) await this.changeDevice("video");
      if (this.closed) return;
      const off = !this.cameraOff;
      if (!this.screen) await this.routeVideo(off ? null : this.local.getVideoTracks()[0]);
      if (this.closed) return;
      this.cameraOff = off;
      this.gate(this.active);
      this.changed();
    } finally {
      this.changingSources--;
    }
  }

  private async routeVideo(track: MediaStreamTrack | null) {
    const video = this.pc.getTransceivers().find(transceiver => transceiver.receiver.track.kind === "video");
    if (video) {
      await video.sender.replaceTrack(track);
      if (!this.closed) video.direction = track ? "sendrecv" : "recvonly";
    } else if (track) {
      this.pc.addTrack(track, this.local);
    }
  }

  async device(kind: "audio" | "video", id?: string) {
    this.changingSources++;
    try { await this.changeDevice(kind, id); }
    finally { this.changingSources--; }
  }

  private async changeDevice(kind: "audio" | "video", id?: string) {
    const capture = await navigator.mediaDevices.getUserMedia({ [kind]: id ? { deviceId: { exact: id } } : true });
    const track = capture.getTracks()[0];
    this.tracks.add(track);
    if (this.closed) { track.stop(); return; }
    track.enabled = false;
    const sender = this.pc.getSenders().find(sender => sender.track?.kind === kind);
    try {
      if (kind === "video") {
        if (!this.screen && !this.cameraOff) await this.routeVideo(track);
      } else {
        if (sender) await sender.replaceTrack(track);
        else this.pc.addTrack(track, this.local);
      }
    }
    catch (error) { track.stop(); throw error; }
    if (this.closed) { track.stop(); return; }
    for (const old of this.local.getTracks().filter(t => t.kind === kind)) { old.stop(); this.local.removeTrack(old); }
    this.local.addTrack(track); this.gate(this.active); this.changed();
  }

  async share() {
    this.changingSources++;
    try { await this.changeScreen(); }
    finally { this.changingSources--; }
  }

  private async changeScreen() {
    if (this.screen) {
      const previous = this.screen; this.screen = undefined;
      await this.routeVideo(this.cameraOff ? null : this.local.getVideoTracks()[0] ?? null);
      previous.onended = null; previous.stop(); this.changed(); return;
    }
    const stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false });
    const track = stream.getVideoTracks()[0]; this.tracks.add(track);
    if (this.closed) { track.stop(); return; }
    if (this.call.mode !== CallMode.VIDEO) { track.stop(); throw new Error("Screen sharing requires a video call."); }
    track.enabled = false;
    try { await this.routeVideo(track); }
    catch (error) { track.stop(); throw error; }
    if (this.closed) { track.stop(); return; }
    this.screen = track; track.enabled = this.active;
    track.onended = () => { void this.share().catch(() => this.failed("Screen sharing stopped.")); };
    this.changed();
  }

  get sharing() { return !!this.screen; }
  get screenTrack() { return this.screen; }
  get changing() { return this.changingSources > 0; }

  close() {
    this.closed = true;
    this.pc.onicecandidate = null; this.pc.onnegotiationneeded = null; this.pc.ontrack = null; this.pc.onconnectionstatechange = null;
    for (const track of this.tracks) { track.onended = null; track.stop(); }
    this.pending = [];
    this.pc.close();
  }
}
