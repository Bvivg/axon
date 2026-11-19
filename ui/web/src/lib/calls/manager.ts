import { Code, ConnectError } from "@connectrpc/connect";
import type { Call, CallSocketEvent, PeerSignal } from "@/gen/axon/calls/v1/calls_pb";
import { CallMode, CallState } from "@/gen/axon/calls/v1/calls_pb";
import type { CallChannel } from "@/lib/calls/channel";
import { CallMediaError, callIssue, type CallIssue } from "@/lib/calls/call-errors";
import { PeerMedia } from "@/lib/calls/p2p";
import { CallMigration } from "@/lib/calls/migration";
import type { SfuMedia } from "@/lib/calls/livekit";
import { callsClient } from "@/lib/connect/clients";

export interface CallView {
  call?: Call;
  error?: CallIssue;
  available: boolean;
  pending: boolean;
  media?: PeerMedia | SfuMedia;
  sfuAvailable?: boolean;
  migrationAvailable?: boolean;
  resume: boolean;
  disconnected: boolean;
  mediaReconnecting?: boolean;
  waitingForDevice?: CallMode;
  starting?: "requesting" | "cancelling";
  canRetry?: boolean;
  ending?: boolean;
}

export type CallTarget = { roomId: string; group?: boolean } | { peerUserId: string };
type CaptureIntent = { requestStarted: boolean; cancelled: boolean; callId?: string };
type Termination = { callId: string; action: "decline" | "cancel" | "end" | "leave"; requestId: string; running: boolean };

export function isIncomingCall(call: Call, userID: string) {
  if (call.state === CallState.ENDED) return false;
  return call.kind === "group"
    ? call.participants.some(participant => participant.userId === userID && participant.status === "invited")
    : call.state === CallState.RINGING && call.callerId !== userID;
}

export class CallManager {
  private view: CallView = { available: false, pending: false, resume: false, disconnected: false };
  private listeners = new Set<() => void>();
  private versions = new Map<string, bigint>();
  private capture?: MediaStream;
  private captureMode = CallMode.AUDIO;
  private captureIntent?: CaptureIntent;
  private retry?: () => Promise<void>;
  private termination?: Termination;
  private closingMedia?: Promise<void>;
  private permitted = false;
  private active = false;
  private unsubscribe?: () => void;
  private historyChanged?: () => void;
  private disconnectTimer?: ReturnType<typeof setTimeout>;
  private signalBacklog: PeerSignal[] = [];
  private cleaned = new Set<string>();
  private reporting = new Set<string>();
  private ticket = 0;
  private connectingKey?: string;
  private failedGeneration?: string;
  private consentedCalls = new Set<string>();
  private migration?: CallMigration;
  private migrationTimer?: ReturnType<typeof setTimeout>;
  private recoveryTimer?: ReturnType<typeof setTimeout>;
  private migrationRequested = new Set<string>();
  private checkingDevices = false;

  constructor(readonly userID: string, readonly channel: CallChannel) {}
  snapshot = () => this.view;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private update(value: Partial<CallView>) { this.view = { ...this.view, ...value }; for (const listener of this.listeners) listener(); }

  async mount(historyChanged: () => void) {
    this.active = true;
    navigator.mediaDevices?.addEventListener("devicechange", this.devicesChanged);
    this.historyChanged = historyChanged;
    this.unsubscribe = this.channel.subscribe(event => this.receive(event));
    try {
      await this.channel.initialize(this.userID);
      if (this.active) this.update({ available: true });
      try {
        const capabilities = await callsClient.getCallCapabilities({});
        if (this.active) this.update({ sfuAvailable: capabilities.sfuEnabled, migrationAvailable: capabilities.migrationEnabled });
      } catch { if (this.active) this.update({ sfuAvailable: false }); }
    }
    catch { if (this.active) this.update({ available: false }); }
  }

  private mine(call: Call) {
    const endpoint = this.channel.endpoint;
    if (!endpoint) return false;
    return call.participants.some(p => p.userId === this.userID && p.endpointId === endpoint?.id && p.endpointEpoch === endpoint.epoch);
  }

  owns(call = this.view.call) { return !!call && this.mine(call); }

  private devicesChanged = () => { void this.restoreDevices(); };

  private async restoreDevices() {
    const media = this.view.media;
    if (!media || this.view.ending || this.migration?.open || this.checkingDevices) return;
    this.checkingDevices = true;
    try {
      const devices = await navigator.mediaDevices.enumerateDevices();
      for (const kind of ["audio", "video"] as const) {
        if (this.view.media !== media || this.migration?.open || ("changing" in media && media.changing)) return;
        const track = media.local.getTracks().find(item => item.kind === kind);
        if (!track) continue;
        const id = track.getSettings().deviceId;
        const type = kind === "audio" ? "audioinput" : "videoinput";
        if (track.readyState === "ended" || (id && devices.some(device => device.kind === type && device.deviceId) && !devices.some(device => device.kind === type && device.deviceId === id))) {
          await media.device(kind);
        }
      }
    } catch (error) {
      if (this.view.media === media) this.update({ error: callIssue(error, "capture") });
    } finally {
      this.checkingDevices = false;
    }
  }

  private async receive(event: CallSocketEvent) {
    if (!this.active) return false;
    if (event.errorCode) {
      if (event.errorCode === "disconnected") {
        if (!this.view.call || this.view.call.state === CallState.ENDED) return;
        this.view.media?.gate(false);
        this.update({ disconnected: true });
        if (!this.disconnectTimer) this.disconnectTimer = setTimeout(() => { void this.fail("Connection lost. Try calling again.", "disconnected"); }, 15_000);
        return;
      }
      const idle = !this.view.call || this.view.call.state === CallState.ENDED;
      if (idle && !this.view.starting && !this.view.pending) return false;
      await this.fail("Call connection is unavailable.");
      return false;
    }
    if (event.signal) {
      const source = this.migration?.open ? this.migration.source : undefined;
      if (source && event.signal.generation === source.call.generation) await source.receive(event.signal);
      else if (this.view.media && event.signal.generation === this.view.media.call.generation) await this.view.media.receive(event.signal);
      else if (this.signalBacklog.length < 128) this.signalBacklog.push(event.signal);
      else await this.fail("Connection messages exceeded the limit.");
      if (this.view.call?.id === event.signal.callId && event.signal.generation > this.view.call.generation) this.channel.resync();
      return;
    }
    if (event.snapshot) {
      if (this.disconnectTimer) clearTimeout(this.disconnectTimer);
      this.disconnectTimer = undefined;
      this.update({ disconnected: false });
      if (!event.call && this.view.call && this.view.call.state !== CallState.ENDED && !this.termination) { this.resetCaptureIntent(); await this.release(); this.update({ call: undefined }); }
    }
    if (event.call) await this.apply(event.call, event.snapshot);
    if (event.snapshot && this.termination) void this.submitTermination(this.termination);
  }

  private async apply(call: Call, snapshot = false) {
    const version = this.versions.get(call.id);
    if (version !== undefined && (version > call.version || (!snapshot && version === call.version))) return;
    this.versions.set(call.id, call.version);
    if (this.versions.size > 256) this.versions.delete(this.versions.keys().next().value!);
    this.historyChanged?.();
    const current = this.view.call;
    if (call.state === CallState.ENDED && current?.id !== call.id && !snapshot) {
      this.completeTermination(call.id);
      if (this.mine(call)) await this.cleanupAck(call);
      return;
    }
    if (current && current.id !== call.id && current.state !== CallState.ENDED) return;
    if (current?.id === call.id && current.version > call.version) return;
    if (current?.id !== call.id && this.captureIntent && !this.captureIntent.requestStarted) this.resetCaptureIntent();
    if (current?.id !== call.id) this.retry = undefined;
    this.update({ call, resume: false, ...(current?.id !== call.id ? { error: undefined, canRetry: false, mediaReconnecting: false } : {}) });
    if (call.state === CallState.ENDED) {
      if (this.captureIntent?.callId === call.id) this.resetCaptureIntent();
      this.completeTermination(call.id);
      this.consentedCalls.delete(call.id);
      navigator.serviceWorker?.controller?.postMessage({ type: "call-ended", callId: call.id });
      await this.release();
      if (this.mine(call)) await this.cleanupAck(call);
      return;
    }
    if (this.termination?.callId === call.id) return;
    if (this.captureIntent?.cancelled && call.callerId === this.userID && this.mine(call)) {
      await this.terminate(call);
      return;
    }
    if (!this.mine(call)) {
      if (this.view.media || this.capture) await this.release();
      return;
    }
    if (call.transportPhase === "waiting_cleanup") {
      if (call.predecessorCallId) return;
      const permitted = this.permitted;
      await this.release(); this.permitted = permitted;
      if (call.cleanupGeneration) await this.cleanupAck({ ...call, generation: call.cleanupGeneration });
      return;
    }
    if (await this.applyMigration(call)) return;
    if (call.state === CallState.CONNECTING || call.state === CallState.ACTIVE) {
      if (!this.permitted && !this.consentedCalls.has(call.id)) { this.update({ resume: true }); return; }
      if (this.failedGeneration === `${call.id}:${call.generation}`) return;
      if (this.mediaKey(this.view.media?.call) !== this.mediaKey(call)) {
        try { await this.connect(call); } catch { await this.mediaFailed(call, "Could not establish the call. Check device permissions and try again."); }
      }
      this.view.media?.gate(this.mediaAllowed(call));
    }
  }

  private async getCapture(mode: CallMode) {
    if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia) {
      throw new CallMediaError({ kind: "unsupported", message: "Open Axon over HTTPS in a browser that supports calls." });
    }
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true, video: mode === CallMode.VIDEO });
    if (!this.active) { stream.getTracks().forEach(t => t.stop()); throw new Error("Call closed."); }
    return stream;
  }

  async start(target: CallTarget, mode: CallMode) {
    if (!this.view.available || this.view.pending || (this.view.call && this.view.call.state !== CallState.ENDED)) return;
    this.update({ call: undefined });
    await this.captureAction(mode, () => this.start(target, mode), async intent => {
      const input = { mode, endpoint: this.channel.endpoint, requestId: crypto.randomUUID() };
      const response = "peerUserId" in target
        ? await callsClient.startDirectCall({ ...input, peerUserId: target.peerUserId })
        : target.group
          ? await callsClient.startGroupCall({ ...input, roomId: target.roomId })
          : await callsClient.startCall({ ...input, roomId: target.roomId });
      const call = response.call;
      if (!call) throw new Error("Missing call");
      if (!this.active || this.captureIntent !== intent || intent.cancelled) {
        await this.terminate(call);
        return;
      }
      this.consentedCalls.add(call.id);
      await this.apply(call);
    });
  }

  async accept(audioOnly = false) {
    const call = this.view.call;
    if (!call || this.view.pending || !isIncomingCall(call, this.userID)) return;
    await this.captureAction(audioOnly ? CallMode.AUDIO : call.mode, () => this.accept(audioOnly), async intent => {
      const response = await callsClient.acceptCall({ callId: call.id, endpoint: this.channel.endpoint, requestId: crypto.randomUUID() });
      const accepted = response.call;
      if (!accepted) throw new Error("Missing call");
      if (!this.active || this.captureIntent !== intent) { await this.terminate(accepted); return; }
      this.consentedCalls.add(call.id);
      await this.apply(accepted);
    }, call.id);
  }

  async resume() {
    const call = this.view.call;
    if (!call || this.view.pending || !this.mine(call)) return;
    await this.captureAction(this.captureMode, () => this.resume(), async () => {
      this.consentedCalls.add(call.id);
      await this.connect(call);
      this.update({ resume: false });
    }, call.id);
  }

  private async captureAction(mode: CallMode, retry: () => Promise<void>, run: (intent: CaptureIntent) => Promise<void>, callId?: string) {
    const intent: CaptureIntent = { requestStarted: false, cancelled: false, callId };
    this.captureIntent = intent;
    this.retry = retry;
    this.update({ pending: true, waitingForDevice: mode, error: undefined, canRetry: false });
    try {
      const stream = await this.getCapture(mode);
      if (this.captureIntent !== intent || !this.active) { stream.getTracks().forEach(track => track.stop()); return; }
      this.capture = stream;
      this.captureMode = mode;
      this.permitted = true;
      intent.requestStarted = true;
      this.update({ waitingForDevice: undefined, starting: callId ? undefined : "requesting" });
      await run(intent);
    } catch (error) {
      if (this.captureIntent !== intent || intent.cancelled) return;
      await this.release();
      if (this.captureIntent !== intent) return;
      this.update({ error: callIssue(error, intent.requestStarted ? "control" : "capture"), canRetry: true });
    } finally {
      if (this.captureIntent === intent) {
        this.captureIntent = undefined;
        this.update({ pending: false, waitingForDevice: undefined, starting: undefined });
      }
    }
  }

  cancelCapture() {
    const intent = this.captureIntent;
    if (!intent) return;
    if (!intent.requestStarted) {
      this.resetCaptureIntent();
      return;
    }
    if (intent.callId || intent.cancelled) return;
    intent.cancelled = true;
    this.update({ starting: "cancelling" });
    const call = this.view.call;
    if (call?.callerId === this.userID && this.mine(call)) void this.terminate(call);
    else void this.release();
  }

  private resetCaptureIntent() {
    if (!this.captureIntent) return;
    this.captureIntent = undefined;
    this.retry = undefined;
    this.update({ waitingForDevice: undefined, starting: undefined, pending: false, canRetry: false });
  }

  retryAction() { if (this.view.canRetry) void this.retry?.(); }

  redial(call: Call) {
    const peer = call.participants.find(participant => participant.userId !== this.userID);
    if (call.kind !== "group" && peer) void this.start({ peerUserId: peer.userId }, call.mode);
  }

  private async connect(call: Call) {
    const key = this.mediaKey(call);
    if (this.connectingKey === key) return;
    this.connectingKey = key;
    try { await this.connectMedia(call); }
    finally { if (this.connectingKey === key) this.connectingKey = undefined; }
  }

  private mediaKey(call?: Call) {
    const participant = call?.participants.find(p => p.userId === this.userID);
    return call ? `${call.id}:${call.generation}:${participant?.endpointEpoch}:${participant?.mediaVersion}` : undefined;
  }

  private mediaAllowed(call: Call) {
    return call.state === CallState.ACTIVE && !this.view.disconnected && (call.transport !== "sfu" || !!call.participants.find(p => p.userId === this.userID)?.mediaReady);
  }

  private async connectMedia(call: Call) {
    const ticket = ++this.ticket;
    const old = this.view.media;
    const mode = old ? (old.cameraOff ? CallMode.AUDIO : CallMode.VIDEO) : this.captureMode;
    if (old) {
      await old.close();
      if (!this.active || ticket !== this.ticket) return;
      this.capture = undefined;
      this.update({ media: undefined });
    }
    const capture = this.capture ?? await this.getCapture(mode);
    if (!this.active || ticket !== this.ticket) { capture.getTracks().forEach(track => track.stop()); return; }
    this.capture = capture;
    this.captureMode = mode;
    let credentials;
    try {
      const input = { endpoint: this.channel.endpoint, callId: call.id, generation: call.generation };
      credentials = call.transport === "sfu" ? await callsClient.getSfuCredentials(input) : await callsClient.getMediaCredentials(input);
    }
    catch (error) {
      if (error instanceof ConnectError && error.code === Code.FailedPrecondition) {
        const latest = (await callsClient.getCall({ callId: call.id })).call;
        if (latest && latest.generation > call.generation) { await this.apply(latest); return; }
      }
      throw error;
    }
    if (!this.active || ticket !== this.ticket || this.view.call?.state === CallState.ENDED) { capture.getTracks().forEach(t => t.stop()); return; }
    if (call.transport === "sfu") {
      if (!("token" in credentials)) throw new Error("Media credentials unavailable.");
      const { SfuMedia } = await import("@/lib/calls/livekit");
      if (!this.active || ticket !== this.ticket || this.snapshot().call?.state === CallState.ENDED) { capture.getTracks().forEach(track => track.stop()); return; }
      const media = new SfuMedia(capture, call, { iceServers: credentials.iceServers.map(server => ({ urls: server.urls, username: server.username, credential: server.credential })) }, () => this.update({}), () => { void this.ready(call); }, () => { void this.reconnecting(call); }, message => { void this.mediaFailed(call, message); });
      media.muted = old?.muted ?? false;
      this.update({ media });
      await media.join(credentials.url, credentials.token);
      if (ticket !== this.ticket) { await media.close(); return; }
      media.gate(this.mediaAllowed(this.view.call ?? call));
      return;
    }
    const media = new PeerMedia(capture, call, this.userID, this.channel, { iceServers: credentials.iceServers.map(server => ({ urls: server.urls, username: server.username, credential: server.credential })) }, () => this.update({}), () => { void this.ready(call); }, message => { void this.mediaFailed(call, message); }, state => this.peerState(call, state));
    media.muted = old?.muted ?? false;
    this.update({ media });
    const pending = this.signalBacklog.splice(0);
    this.signalBacklog = pending.filter(signal => signal.generation > call.generation);
    for (const signal of pending.filter(signal => signal.generation === call.generation)) await media.receive(signal);
    if (ticket === this.ticket && this.view.call) media.gate(this.mediaAllowed(this.view.call));
  }

  private async ready(call: Call) {
    if (this.termination?.callId === call.id) return;
    if (this.mediaKey(this.view.call) === this.mediaKey(call)) this.update({ mediaReconnecting: false });
    const key = this.mediaKey(call)!;
    if (this.reporting.has(key)) return;
    this.reporting.add(key);
    try {
      const response = await callsClient.reportConnection({ endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, generation: call.generation, mediaVersion: this.mediaVersion(call), ready: true });
      await this.apply(response.call!);
    } catch { if (this.active && this.mediaKey(this.view.call) === this.mediaKey(call)) await this.fail("Could not confirm the connection."); }
    finally { this.reporting.delete(key); }
  }

  private async reconnecting(call: Call) {
    if (this.termination?.callId === call.id || this.mediaKey(this.view.call) !== this.mediaKey(call)) return;
    this.update({ mediaReconnecting: true });
    try {
      const response = await callsClient.reportConnection({ endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, generation: call.generation, mediaVersion: this.mediaVersion(call), reconnecting: true });
      await this.apply(response.call!);
    } catch { await this.mediaFailed(call, "Could not reconnect media."); }
  }

  private async mediaFailed(call: Call, message: string) {
    if (this.migration?.open && this.migration.source.call.id === call.id && call.transport !== "sfu") return;
    if (this.mediaKey(this.view.call) !== this.mediaKey(call)) return;
    if (!this.view.call) return;
    if (this.view.call.state === CallState.ENDED) return;
    call = this.view.call;
    if (call.transport !== "sfu" && this.canMigrate(call)) { await this.requestMigration(call); return; }
    if (call.transport !== "sfu" && !call.everActive && call.sfuFallbackAvailable) {
      this.failedGeneration = `${call.id}:${call.generation}`;
      const permitted = this.permitted;
      await this.release(); this.permitted = permitted;
      try {
        const response = await callsClient.reportConnection({ endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, generation: call.generation, mediaVersion: this.mediaVersion(call), ready: false });
        await this.apply(response.call!);
      } catch { await this.fail(message); }
      return;
    }
    if (call.transport === "sfu") {
      this.consentedCalls.delete(call.id);
      await this.release();
      try {
        const response = await callsClient.reportConnection({ endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, generation: call.generation, mediaVersion: this.mediaVersion(call), ready: false });
        await this.apply(response.call!);
      } catch { this.channel.resync(); }
      this.update({ error: { kind: "disconnected", message } });
      return;
    }
    await this.fail(message);
  }

  async addPerson(userId: string) {
    const call = this.view.call;
    if (!call || this.view.pending) return;
    await this.operation(async () => {
      const input = { endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, userId };
      if (call.kind === "group") {
        const response = await callsClient.inviteCallParticipant(input);
        await this.apply(response.call!);
      } else {
        const response = await callsClient.startGroupCallFromCall(input);
        this.consentedCalls.add(response.call!.id);
        await this.release();
        await this.cleanupAck({ ...call, state: CallState.ENDED });
        this.permitted = true;
        await this.apply(response.call!, true);
      }
    }, true);
  }

  async removePerson(userId: string) {
    const call = this.view.call;
    if (!call) return;
    await this.operation(async () => {
      const response = await callsClient.removeCallParticipant({ endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, userId });
      await this.apply(response.call!);
    }, true);
  }

  async leave() {
    const call = this.view.call;
    if (call) await this.terminate(call, "leave");
  }

  async signOut() {
    if (this.view.call?.kind === "group" && this.owns()) await this.leave();
    else await this.end();
  }

  private async cleanupAck(call: Call) {
    const mediaVersion = this.mediaVersion(call);
    const key = `${call.id}:${call.generation}:${this.channel.endpoint?.epoch}:${mediaVersion ?? 0n}`;
    if (this.cleaned.has(key)) return;
    try {
      await callsClient.acknowledgeCleanup({ endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, generation: call.generation, mediaVersion }, { timeoutMs: 2500 });
      this.cleaned.add(key);
    } catch { this.channel.resync(); }
  }

  private mediaVersion(call: Call) { return call.participants.find(p => p.userId === this.userID)?.mediaVersion; }

  async end() {
    const call = this.view.call;
    this.resetCaptureIntent();
    if (call && call.state !== CallState.ENDED) await this.terminate(call);
    else await this.release();
  }

  private async terminate(call: Call, action?: Termination["action"]) {
    if (this.view.call?.id === call.id && this.view.call.version >= call.version) call = this.view.call;
    if (call.state === CallState.ENDED) return;
    if (this.termination?.callId === call.id) { await this.submitTermination(this.termination); return; }
    const termination: Termination = {
      callId: call.id,
      action: action ?? (isIncomingCall(call, this.userID) ? "decline" : call.state === CallState.RINGING ? "cancel" : "end"),
      requestId: crypto.randomUUID(),
      running: false,
    };
    this.termination = termination;
    this.consentedCalls.delete(call.id);
    this.update({ ending: true, canRetry: false });
    await this.release();
    await this.submitTermination(termination);
  }

  private completeTermination(callId: string) {
    if (this.termination?.callId !== callId) return;
    this.termination = undefined;
    this.update({ ending: false, ...(this.view.error?.kind === "cleanup-pending" ? { error: undefined, canRetry: false } : {}) });
  }

  private async submitTermination(termination: Termination) {
    if (termination.running || this.termination !== termination) return;
    termination.running = true;
    let endInstead = false;
    try {
      const request = { endpoint: this.channel.endpoint, requestId: termination.requestId, callId: termination.callId };
      const options = { timeoutMs: 2500 };
      const response = await ({
        decline: callsClient.declineCall,
        cancel: callsClient.cancelCall,
        end: callsClient.endCall,
        leave: callsClient.leaveCall,
      }[termination.action])(request, options);
      if (response.call) await this.apply(response.call);
    } catch (error) {
      if (this.termination !== termination) return;
      if (termination.action === "cancel" && error instanceof ConnectError && [Code.AlreadyExists, Code.PermissionDenied].includes(error.code)) {
        const latest = await callsClient.getCall({ callId: termination.callId }, { timeoutMs: 2500 }).catch(() => undefined);
        const call = latest?.call;
        if (call?.state === CallState.ENDED) await this.apply(call, true);
        else if (call && call.state !== CallState.RINGING && this.mine(call) && call.callerId === this.userID) endInstead = true;
      }
      if (this.termination === termination && !endInstead) {
        this.retry = () => this.submitTermination(termination);
        this.update({ error: { kind: "cleanup-pending", message: "Call stopped on this device. Waiting for Axon to finish closing it." }, canRetry: true });
      }
    } finally {
      termination.running = false;
    }
    if (endInstead && this.termination === termination) {
      const end = { ...termination, action: "end" as const, requestId: crypto.randomUUID() };
      this.termination = end;
      await this.submitTermination(end);
    }
  }

  private async fail(message: string, kind: CallIssue["kind"] = "unavailable") { this.update({ error: { kind, message } }); await this.end(); }
  private async operation(run: () => Promise<void>, preserveMedia = false) {
    this.update({ pending: true, error: undefined });
    try { await run(); }
    catch (error) {
      if (!preserveMedia) await this.release();
      this.update({ error: callIssue(error) });
    } finally { this.update({ pending: false }); }
  }

  private async release() {
    this.ticket++;
    this.clearMigrationTimers();
    const migration = this.migration; this.migration = undefined;
    const closingMigration = migration?.close();
    const closing = this.view.media?.close();
    this.capture?.getTracks().forEach(track => track.stop());
    this.capture = undefined; this.permitted = false; this.signalBacklog = [];
    this.update({ media: undefined, resume: false, mediaReconnecting: false });
    const settled = Promise.allSettled([this.closingMedia, closing, closingMigration]).then(() => undefined);
    this.closingMedia = settled;
    await settled;
    if (this.closingMedia === settled) this.closingMedia = undefined;
  }

  private canMigrate(call: Call) {
    return !!this.view.migrationAvailable && call.transport !== "sfu" && call.kind !== "group" && call.everActive && call.state === CallState.ACTIVE && call.participants.every(p => p.supportsMigration && p.supportsSfu);
  }

  private clearMigrationTimers() {
    if (this.migrationTimer) clearTimeout(this.migrationTimer);
    if (this.recoveryTimer) clearTimeout(this.recoveryTimer);
    this.migrationTimer = undefined; this.recoveryTimer = undefined;
  }

  private peerState(call: Call, state: RTCPeerConnectionState) {
    const current = this.view.call;
    if (current?.id === call.id && current.generation === call.generation && current.everActive && !this.view.ending) {
      this.update({ mediaReconnecting: state === "disconnected" || state === "failed" || state === "connecting" });
    }
    if (state === "connected") { this.clearMigrationTimers(); return; }
    if (!current || current.id !== call.id || current.generation !== call.generation || !this.canMigrate(current) || this.migration?.open) return;
    if (state === "failed") { void this.requestMigration(current); return; }
    if (state === "disconnected" && !this.migrationTimer) this.migrationTimer = setTimeout(() => {
      this.migrationTimer = undefined;
      if (this.view.media instanceof PeerMedia && ["disconnected", "failed"].includes(this.view.media.pc.connectionState) && this.view.call?.generation === call.generation) void this.requestMigration(this.view.call);
    }, 2000);
  }

  private async requestMigration(call: Call) {
    const key = this.mediaKey(call)!;
    if (this.migration?.open) return;
    if (this.migrationRequested.has(key)) { this.armRecovery(call); return; }
    this.migrationRequested.add(key);
    if (this.migrationRequested.size > 256) this.migrationRequested.delete(this.migrationRequested.values().next().value!);
    try {
      const response = await callsClient.requestCallMigration({ endpoint: this.channel.endpoint, requestId: crypto.randomUUID(), callId: call.id, generation: call.generation });
      if (response.call) await this.apply(response.call);
    } catch {
      this.channel.resync();
      this.armRecovery(call);
    }
  }

  private armRecovery(call: Call) {
    const key = this.mediaKey(call);
    if (!this.recoveryTimer) this.recoveryTimer = setTimeout(() => {
      this.recoveryTimer = undefined;
      if (this.view.media instanceof PeerMedia && this.view.media.pc.connectionState !== "connected" && this.mediaKey(this.view.call) === key) void this.fail("Could not recover the connection.");
    }, 15_000);
  }

  private async applyMigration(call: Call) {
    const info = call.migration;
    if (this.migration && (this.migration.id !== info?.id || (info?.phase === "aborted" && this.migration.source.call.generation !== call.generation))) {
      await this.migration.close(false, this.migration.source === this.view.media && this.mediaKey(this.migration.source.call) === this.mediaKey(call));
      this.migration = undefined;
    }
    if (!info) return false;
    if (!this.migration && ["preparing", "committing", "aborting"].includes(info.phase) && this.view.media instanceof PeerMedia && this.permitted) {
      this.clearMigrationTimers();
      this.migrationRequested.add(this.mediaKey(call)!);
      this.migration = new CallMigration(call, this.view.media, {
        userID: this.userID, channel: this.channel,
        apply: latest => this.apply(latest),
        select: media => { this.capture = media.local; this.update({ media }); },
        changed: () => this.update({}), allowed: () => !!this.view.call && this.mediaAllowed(this.view.call),
        failed: message => { void this.fail(message); },
        connected: latest => { void this.ready(latest); }, reconnecting: latest => { void this.reconnecting(latest); },
      });
    }
    if (!this.migration) return false;
    this.migration.handle(call);
    if (this.migration.open) return true;
    if (info.phase === "aborted" && call.state === CallState.CONNECTING && this.view.media instanceof PeerMedia && this.view.media.pc.connectionState === "connected") void this.ready(call);
    return false;
  }

  dismiss() {
    if (!this.view.call || this.view.call.state === CallState.ENDED || !this.mine(this.view.call)) {
      this.retry = undefined;
      this.update({ call: undefined, error: undefined, canRetry: false });
    }
  }
  stop() {
    this.active = false;
    navigator.mediaDevices?.removeEventListener("devicechange", this.devicesChanged);
    this.historyChanged = undefined;
    this.unsubscribe?.();
    this.unsubscribe = undefined;
    if (this.disconnectTimer) clearTimeout(this.disconnectTimer);
    this.disconnectTimer = undefined;
    void this.signOut();
  }
}
