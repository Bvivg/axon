"use client";

import { create, toJson, type MessageInitShape } from "@bufbuild/protobuf";
import { useEffect } from "react";
import { useChatCallChannel } from "@/components/chat/chat-activity";
import { useSession } from "@/lib/auth/session";
import { callsClient } from "@/lib/connect/clients";
import { CallSchema, PeerSignalSchema, type Call, type PeerSignal } from "@/gen/axon/calls/v1/calls_pb";

export interface CallsProbeAPI {
  channel: ReturnType<typeof useChatCallChannel>;
  rpc: typeof callsClient;
  userID: string;
  calls: Map<string, Call>;
  seen: string[];
  snapshots: number;
  ack: boolean;
  errors: string[];
  onSignal?: (signal: PeerSignal) => Promise<void>;
  sendSignal(input: MessageInitShape<typeof PeerSignalSchema>): void;
  json(callID: string): ReturnType<typeof toJson<typeof CallSchema>> | null;
}

declare global { interface Window { axonCallsProbe?: CallsProbeAPI; } }

export function CallsProbe() {
  const channel = useChatCallChannel();
  const { user } = useSession();
  useEffect(() => {
    const api: CallsProbeAPI = {
      channel, rpc: callsClient, userID: user?.id ?? "", calls: new Map(), seen: [], snapshots: 0, ack: true, errors: [],
      sendSignal: (input) => channel.signal(create(PeerSignalSchema, input)),
      json: (id) => { const call = api.calls.get(id); return call ? toJson(CallSchema, call) : null; },
    };
    const unsubscribe = channel.subscribe(async (event) => {
      if (event.errorCode) api.errors.push(event.errorCode);
      if (event.snapshot) api.snapshots++;
      if (event.eventId) api.seen.push(event.eventId);
      if (event.call) {
        const previous = api.calls.get(event.call.id);
        if (!previous || previous.version < event.call.version) api.calls.set(event.call.id, event.call);
      }
      if (event.signal) await api.onSignal?.(event.signal);
      return api.ack;
    });
    window.axonCallsProbe = api;
    return () => { unsubscribe(); delete window.axonCallsProbe; };
  }, [channel, user?.id]);
  return null;
}
