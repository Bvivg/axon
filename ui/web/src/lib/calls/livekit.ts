import { Room, RoomEvent, Track, LocalAudioTrack, LocalVideoTrack, type RemoteTrack, type RemoteParticipant, type RemoteTrackPublication } from "livekit-client";

import type { Call, PeerSignal } from "@/gen/axon/calls/v1/calls_pb";
import { apiBaseUrl } from "@/lib/connect/transport";

type Engine = { pcManager?: { publisher?: { pc: RTCPeerConnection }; subscriber?: { pc: RTCPeerConnection } } };
export type RemoteMedia = { identity: string; screen: boolean; stream: MediaStream };

export class SfuMedia {
  readonly room: Room;
  readonly tracks = new Set<MediaStreamTrack>();
  readonly remote = new MediaStream();
  readonly remotes = new Map<string, RemoteMedia>();
  private stopped = false;
  private active = false;
  private closing?: Promise<void>;
  private screen?: MediaStreamTrack;
  private publications = new Map<MediaStreamTrack, LocalAudioTrack | LocalVideoTrack>();
  private gating: Promise<void> = Promise.resolve();
  muted = false;
  cameraOff = false;

  constructor(readonly local: MediaStream, readonly call: Call, private configuration: RTCConfiguration, private changed: () => void, private connected: () => void, private reconnecting: () => void, private failed: (message: string) => void, initialScreen?: MediaStreamTrack, private prepared = false) {
    this.room = new Room({ stopLocalTrackOnUnpublish: true });
    this.cameraOff = !local.getVideoTracks().length;
    for (const track of local.getTracks()) { this.tracks.add(track); track.enabled = false; }
    if (initialScreen) { this.screen = initialScreen; this.tracks.add(initialScreen); initialScreen.enabled = false; }
    this.room.on(RoomEvent.LocalTrackPublished, publication => {
      const track = publication.track?.mediaStreamTrack;
      if (track) { this.tracks.add(track); if (this.stopped) track.stop(); }
    });
    this.room.on(RoomEvent.TrackSubscribed, (track: RemoteTrack, publication: RemoteTrackPublication, participant: RemoteParticipant) => {
      const native = track.mediaStreamTrack;
      this.tracks.add(native);
      if (this.stopped) { native.stop(); return; }
      const screen = publication.source === Track.Source.ScreenShare || publication.source === Track.Source.ScreenShareAudio;
      const key = `${participant.identity}:${screen ? "screen" : "camera"}`;
      const tile = this.remotes.get(key) ?? { identity: participant.identity, screen, stream: new MediaStream() };
      tile.stream.addTrack(native); this.remotes.set(key, tile);
      this.remote.addTrack(native); this.changed();
    });
    this.room.on(RoomEvent.TrackUnsubscribed, (track: RemoteTrack) => {
      const native = track.mediaStreamTrack;
      this.remote.removeTrack(native);
      for (const [key, tile] of this.remotes) { tile.stream.removeTrack(native); if (!tile.stream.getTracks().length) this.remotes.delete(key); }
      native.stop(); this.changed();
    });
    this.room.on(RoomEvent.TrackMuted, () => this.changed());
    this.room.on(RoomEvent.TrackUnmuted, () => this.changed());
    this.room.on(RoomEvent.ActiveSpeakersChanged, () => this.changed());
    this.room.on(RoomEvent.Reconnecting, () => { if (!this.stopped) { this.gate(false); this.reconnecting(); } });
    this.room.on(RoomEvent.Reconnected, () => { if (!this.stopped) this.connected(); });
    this.room.on(RoomEvent.Disconnected, () => { if (!this.stopped) this.failed("Media connection ended."); });
  }

  get pcs() {
    const engine = (this.room as unknown as { engine?: Engine }).engine;
    return [...new Set([engine?.pcManager?.publisher?.pc, engine?.pcManager?.subscriber?.pc].filter((pc): pc is RTCPeerConnection => !!pc))];
  }
  get pc() { return this.pcs[0]; }

  async join(path: string, token: string) {
    const url = new URL(`${apiBaseUrl.replace(/\/$/, "")}/${path.replace(/^\//, "")}`, window.location.origin);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    try {
      await this.room.connect(url.href, token, { autoSubscribe: true, rtcConfig: this.configuration });
      if (this.stopped) return;
      for (const track of this.local.getTracks()) {
        if (this.stopped) return;
        await this.publish(track, track.kind === "audio" ? Track.Source.Microphone : Track.Source.Camera);
      }
      if (!this.stopped && this.screen) {
        const track = this.screen;
        track.onended = () => { if (this.screen === track) { this.screen = undefined; void this.room.localParticipant.unpublishTrack(track, true); this.changed(); } };
        await this.publish(track, Track.Source.ScreenShare);
      }
      if (!this.stopped) this.connected();
    } catch (error) { await this.close(); throw error; }
  }

  receive(_signal: PeerSignal) { return Promise.resolve(); }
  private async publish(track: MediaStreamTrack, source: Track.Source) {
    if (!this.prepared) {
      const publication = await this.room.localParticipant.publishTrack(track, { source });
      if (publication.track instanceof LocalAudioTrack || publication.track instanceof LocalVideoTrack) this.publications.set(track, publication.track);
      return;
    }
    const local = track.kind === "audio" ? new LocalAudioTrack(track, undefined, true) : new LocalVideoTrack(track, undefined, true);
    await local.mute();
    track.enabled = false;
    if (this.stopped) { track.stop(); return; }
    this.publications.set(track, local);
    await this.room.localParticipant.publishTrack(local, { source });
    await this.gate(this.active);
  }
  gate(active: boolean) {
    this.active = active && !this.stopped;
    for (const track of this.local.getAudioTracks()) track.enabled = this.active && !this.muted;
    for (const track of this.local.getVideoTracks()) track.enabled = this.active && !this.cameraOff;
    if (this.screen) this.screen.enabled = this.active;
    this.changed();
    this.gating = this.gating.then(async () => {
      for (const [native, track] of this.publications) {
        if (this.stopped || native.readyState !== "live") continue;
        const enabled = this.active && (native === this.screen || (native.kind === "audio" ? !this.muted : !this.cameraOff));
        if (enabled) await track.unmute(); else await track.mute();
        if (!this.active || this.stopped) native.enabled = false;
      }
    }).catch(() => { if (!this.stopped) this.failed("Could not update media permissions."); });
    return this.gating;
  }
  mute() { this.muted = !this.muted; this.gate(this.active); }
  async camera() {
    if (!this.local.getVideoTracks().some(track => track.readyState === "live")) {
      await this.device("video");
      if (this.stopped) return;
      this.cameraOff = false;
    } else {
      this.cameraOff = !this.cameraOff;
    }
    await this.gate(this.active);
  }

  remoteStatus(identity: string) {
    const participant = this.room.remoteParticipants.get(identity);
    return {
      muted: participant?.getTrackPublication(Track.Source.Microphone)?.isMuted === true,
      cameraOff: participant?.getTrackPublication(Track.Source.Camera)?.isMuted === true,
      speaking: this.room.activeSpeakers.some(speaker => speaker.identity === identity),
    };
  }
  get sharing() { return !!this.screen && this.screen.readyState === "live"; }
  get screenTrack() { return this.screen; }

  async device(kind: "audio" | "video", deviceId?: string) {
    const stream = await navigator.mediaDevices.getUserMedia({ [kind]: deviceId ? { deviceId: { exact: deviceId } } : true });
    const next = stream.getTracks()[0];
    stream.getTracks().forEach(track => this.tracks.add(track));
    if (this.stopped) { stream.getTracks().forEach(track => track.stop()); return; }
    next.enabled = false;
    const old = this.local.getTracks().find(track => track.kind === kind);
    const publication = old && this.publications.get(old);
    try {
      if (publication) await publication.replaceTrack(next, { userProvidedTrack: true });
      else await this.publish(next, kind === "audio" ? Track.Source.Microphone : Track.Source.Camera);
      if (this.stopped) return;
      if (old) {
        this.publications.delete(old);
        this.local.removeTrack(old);
        old.stop();
      }
      if (publication) this.publications.set(next, publication);
      this.local.addTrack(next);
      await this.gate(this.active);
    } catch (error) { next.stop(); throw error; }
    finally { if (this.stopped) next.stop(); }
  }

  async share() {
    if (this.screen) {
      const old = this.screen; this.screen = undefined;
      this.publications.delete(old);
      await this.room.localParticipant.unpublishTrack(old, true); old.stop(); this.changed(); return;
    }
    const stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: false });
    stream.getTracks().forEach(track => this.tracks.add(track));
    const track = stream.getVideoTracks()[0];
    if (this.stopped) { stream.getTracks().forEach(item => item.stop()); return; }
    this.screen = track; track.enabled = this.active;
    track.onended = () => { if (this.screen === track) { this.screen = undefined; this.publications.delete(track); void this.room.localParticipant.unpublishTrack(track, true); this.changed(); } };
    try { await this.publish(track, Track.Source.ScreenShare); await this.gate(this.active); }
    catch (error) { track.stop(); this.screen = undefined; throw error; }
    finally { if (this.stopped) track.stop(); this.changed(); }
  }

  close(): Promise<void> {
    if (this.closing) return this.closing;
    this.stopped = true;
    for (const track of this.tracks) track.stop();
    this.closing = this.room.disconnect(true).finally(() => {
      for (const track of this.tracks) track.stop();
      this.remotes.clear(); this.changed();
    });
    return this.closing;
  }
}
