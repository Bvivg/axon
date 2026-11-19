import { create, fromJson, toJson } from "@bufbuild/protobuf";

import { CallSocketEventSchema, CallSocketFrameSchema, type CallSocketEvent, type EndpointRef, type PeerSignal } from "@/gen/axon/calls/v1/calls_pb";
import { callsClient } from "@/lib/connect/clients";
import { Code, ConnectError } from "@connectrpc/connect";

type Consumer = (event: CallSocketEvent) => boolean | void | Promise<boolean | void>;
type Sender = (frame: unknown) => void;

export class CallChannel {
  private sender?: Sender;
  private reconnect?: () => void;
  private reference?: EndpointRef;
  private connected = false;
  private listeners = new Set<Consumer>();
  private heartbeat?: ReturnType<typeof setInterval>;
  private queue: Promise<void> = Promise.resolve();
  private batchAccepted = true;
  private registration?: Promise<EndpointRef | undefined>;
  private initialization?: Promise<EndpointRef | undefined>;
  private storageKey?: string;
  private unlock?: () => void;
  private lifecycle = 0;

  get endpoint() { return this.reference; }

  bind(sender: Sender, reconnect: () => void) {
    this.sender = sender;
    this.reconnect = reconnect;
    if (this.connected && this.reference && !this.heartbeat) this.attach();
  }

  initialize(userID: string) {
    const pending = this.initializeEndpoint(userID);
    this.initialization = pending;
    void pending.finally(() => { if (this.initialization === pending) this.initialization = undefined; }).catch(() => undefined);
    return pending;
  }

  private async initializeEndpoint(userID: string) {
    this.storageKey = `axon.calls.endpoint.${userID}`;
    try {
      const raw = sessionStorage.getItem(this.storageKey);
      if (raw) {
        const saved = JSON.parse(raw);
        if (saved.id && saved.proof && saved.epoch) this.reference = { $typeName: "axon.calls.v1.EndpointRef", id: saved.id, proof: saved.proof, epoch: BigInt(saved.epoch) };
      }
    } catch { this.reference = undefined; }
    if (this.reference && !(await this.claim(this.reference.id))) this.reference = undefined;
    try { return await this.register(); }
    catch (error) {
      if (!(error instanceof ConnectError) || ![Code.PermissionDenied, Code.NotFound, Code.FailedPrecondition].includes(error.code)) throw error;
      this.reference = undefined; this.unlock?.(); this.unlock = undefined;
      sessionStorage.removeItem(this.storageKey);
      return this.register();
    }
  }

  private async claim(id: string) {
    if (this.unlock) return true;
    if (!navigator.locks) return false;
    return new Promise<boolean>((resolve) => {
      void navigator.locks.request(`axon.calls.owner.${id}`, { ifAvailable: true }, async lock => {
        if (!lock) { resolve(false); return; }
        await new Promise<void>(release => { this.unlock = release; resolve(true); });
      }).catch(() => resolve(false));
    });
  }

  register() {
    if (this.registration) return this.registration;
    const lifecycle = this.lifecycle;
    const pending = (async () => {
      const supported = typeof RTCPeerConnection !== "undefined" && !!navigator.mediaDevices?.getUserMedia;
      const response = await callsClient.registerEndpoint({ resume: this.reference, supportsSfu: supported, supportsMigration: supported });
      if (lifecycle !== this.lifecycle) return undefined;
      this.reference = response.endpoint;
      if (this.reference && !this.unlock) await this.claim(this.reference.id);
      if (this.reference && this.storageKey) sessionStorage.setItem(this.storageKey, JSON.stringify({ id: this.reference.id, proof: this.reference.proof, epoch: this.reference.epoch.toString() }));
      this.attach();
      return this.reference;
    })();
    this.registration = pending;
    void pending.finally(() => { if (this.registration === pending) this.registration = undefined; }).catch(() => undefined);
    return pending;
  }

  async open() {
    this.connected = true;
    if (this.initialization) {
      try { await this.initialization; }
      catch { this.emitFailure("Endpoint initialization failed"); }
      return;
    }
    if (this.reference) {
      try { await this.register(); }
      catch { this.emitFailure("Endpoint resume failed"); }
    }
  }

  closed() {
    this.connected = false;
    if (this.heartbeat) clearInterval(this.heartbeat);
    this.heartbeat = undefined;
    this.emitFailure("Connection interrupted", "disconnected");
  }

  private attach() {
    if (!this.connected || !this.reference) return;
    this.batchAccepted = true;
    this.send("call_attach", { endpoint: this.reference });
    if (this.heartbeat) clearInterval(this.heartbeat);
    this.heartbeat = setInterval(() => this.send("call_heartbeat", {}), 10_000);
  }

  private send(type: string, value: Parameters<typeof create<typeof CallSocketFrameSchema>>[1]) {
    if (!this.connected) return;
    this.sender?.({ type, call: toJson(CallSocketFrameSchema, create(CallSocketFrameSchema, value)) });
  }

  signal(signal: PeerSignal) { this.send("call_signal", { signal }); }
  resync() { this.batchAccepted = true; this.send("call_resync", {}); }
  disconnect() { this.reconnect?.(); }

  subscribe(listener: Consumer) {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  }

  receive(raw: string) {
    let frame: { type?: string; call?: unknown };
    try { frame = JSON.parse(raw); } catch { return false; }
    if (frame.type !== "call_event") return false;
    let event: CallSocketEvent;
    try { event = fromJson(CallSocketEventSchema, frame.call as Parameters<typeof fromJson<typeof CallSocketEventSchema>>[1]); }
    catch { this.emitFailure("Invalid call event"); return true; }
    this.queue = this.queue.then(async () => {
      if (!event.errorCode && event.endpointEpoch !== this.reference?.epoch) return;
      if (event.resyncComplete) {
        if (event.hasMore && this.batchAccepted) this.resync();
        return;
      }
      let accepted = this.listeners.size > 0;
      for (const listener of this.listeners) { if (await listener(event) === false) accepted = false; }
      if (event.eventId) {
        this.batchAccepted = this.batchAccepted && accepted;
        if (accepted && event.endpointEpoch === this.reference?.epoch) this.send("call_ack", { eventId: event.eventId });
      }
    }).catch(() => { this.batchAccepted = false; });
    return true;
  }

  private emitFailure(message: string, code = "unavailable") {
    const event = create(CallSocketEventSchema, { errorCode: code, errorMessage: message });
    for (const listener of this.listeners) void Promise.resolve(listener(event)).catch(() => undefined);
  }

  dispose() {
    this.lifecycle++;
    this.closed();
    this.reference = undefined;
    this.listeners.clear();
    this.sender = undefined;
    this.reconnect = undefined;
    this.unlock?.();
    this.unlock = undefined;
  }
}
