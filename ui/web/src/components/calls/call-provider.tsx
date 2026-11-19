"use client";

import { useQueryClient } from "@tanstack/react-query";
import { usePathname } from "next/navigation";
import { createContext, useContext, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { useChatCallChannel } from "@/components/chat/chat-activity";
import { CallAudioRenderer } from "@/components/calls/call-audio-renderer";
import { useSounds } from "@/components/nav/sound-provider";
import { CallState } from "@/gen/axon/calls/v1/calls_pb";
import { useSession } from "@/lib/auth/session";
import { onBeforeSignOut } from "@/lib/auth/before-sign-out";
import { CallManager, isIncomingCall, type CallView } from "@/lib/calls/manager";
import { coalesceCallHistory } from "@/lib/query/calls";
import { useChatAddress } from "@/lib/chat/address";
import { useCallWakeLock } from "@/components/calls/use-call-wake-lock";

const Context = createContext<{
  manager: CallManager;
  view: CallView;
  expanded: boolean;
  expandedResult: boolean;
  incoming: boolean;
  elsewhere: boolean;
  keepScreenOn: boolean;
  setKeepScreenOn: (enabled: boolean) => void;
  screenHeld: boolean;
  shortcuts: boolean;
  setShortcuts: (enabled: boolean) => void;
  expand: () => void;
  minimize: () => void;
} | null>(null);
declare global { interface Window { axonCallManager?: CallManager; } }

export function CallProvider({ children, evidence = false }: { children: ReactNode; evidence?: boolean }) {
  const channel = useChatCallChannel();
  const { user } = useSession();
  const client = useQueryClient();
  const pathname = usePathname();
  const address = useChatAddress();
  const probe = pathname === "/calls/probe";
  const [manager] = useState(() => new CallManager(user!.id, channel));
  const view = useSyncExternalStore(manager.subscribe, manager.snapshot, manager.snapshot);
  const call = view.call;
  const route = `${pathname}#${address}`;
  const callId = call?.id;
  const [presentation, setPresentation] = useState({ route, callId, expanded: true, seenActive: false });
  if (presentation.route !== route || presentation.callId !== callId) {
    setPresentation({ route, callId, expanded: presentation.callId !== callId, seenActive: false });
  }
  const incoming = !!call && isIncomingCall(call, manager.userID);
  const elsewhere = !!call && !incoming && call.state !== CallState.RINGING && call.state !== CallState.ENDED && !manager.owns();
  const expanded = !!call && call.state !== CallState.ENDED && !incoming && !elsewhere && presentation.expanded;
  const [screenCallId, setScreenCallId] = useState<string>();
  const keepScreenOn = !!callId && screenCallId === callId;
  const screenHeld = useCallWakeLock(keepScreenOn && call?.state === CallState.ACTIVE && !!view.media && !elsewhere && !view.ending);
  const [shortcuts, setShortcuts] = useState(false);
  const { player, calls: callSounds } = useSounds();
  const owned = manager.owns();
  const outgoing = owned && call?.callerId === manager.userID && call.state === CallState.RINGING && call.transportPhase !== "waiting_cleanup";
  const connected = owned && !incoming && call?.state === CallState.ACTIVE && !!view.media && !view.disconnected
    && !view.mediaReconnecting && !view.ending;
  const ended = call?.state === CallState.ENDED;
  const ring = !view.pending && !view.ending && !view.disconnected
    ? incoming ? "incoming" : outgoing ? "outgoing" : undefined
    : undefined;
  const heard = useRef<{ id: string; connected: boolean; ended: boolean } | undefined>(undefined);
  if (expanded && connected && !presentation.seenActive && presentation.route === route && presentation.callId === callId) {
    setPresentation({ ...presentation, seenActive: true });
  }
  const expandedResult = ended && presentation.expanded && presentation.seenActive && presentation.callId === callId && presentation.route === route;
  useEffect(() => {
    player.ring(callSounds ? ring : undefined);
    if (!callId) { heard.current = undefined; return; }
    if (ended && heard.current?.id === callId && !heard.current.ended) {
      heard.current.ended = true;
      if (callSounds) player.call("ended");
    } else if (!ended && (owned || incoming)) {
      if (heard.current?.id !== callId || (heard.current.ended && incoming)) heard.current = { id: callId, connected: false, ended: false };
      if (connected && !heard.current.connected) {
        heard.current.connected = true;
        if (callSounds) player.call("connected");
      }
    }
  }, [callSounds, player, ring, callId, owned, incoming, connected, ended]);
  useEffect(() => {
    if (probe) return;
    const history = coalesceCallHistory(client);
    void manager.mount(history.changed);
    if (evidence) window.axonCallManager = manager;
    const unregister = onBeforeSignOut(() => manager.signOut());
    return () => {
      history.dispose();
      unregister();
      manager.stop();
      delete window.axonCallManager;
    };
  }, [client, manager, probe, evidence]);
  return (
    <Context value={{ manager, view, expanded, expandedResult, incoming, elsewhere, keepScreenOn, screenHeld,
      setKeepScreenOn: enabled => setScreenCallId(enabled ? callId : undefined),
      shortcuts, setShortcuts,
      expand: () => setPresentation({ route, callId, expanded: true, seenActive: presentation.seenActive }),
      minimize: () => setPresentation({ route, callId, expanded: false, seenActive: presentation.seenActive }),
    }}>
      <CallAudioRenderer call={call} media={view.media} enabled={call?.state === CallState.ACTIVE && !view.disconnected && !elsewhere}>
        {children}
      </CallAudioRenderer>
    </Context>
  );
}

export function useCall() {
  const value = useContext(Context);
  if (!value) throw new Error("Calls require a signed-in session");
  return value;
}
