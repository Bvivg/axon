import { useEffect, useState } from "react";

import { useCall } from "@/components/calls/call-provider";
import { CallState } from "@/gen/axon/calls/v1/calls_pb";
import { profileName } from "@/lib/format";
import { usePublicProfiles } from "@/lib/query/people";

const callExitStatus: Record<string, string> = {
  declined: "Call declined",
  left: "You left the call",
  removed: "Removed from the call",
  cancelled: "Call cancelled",
  busy: "Line busy",
  setup_timeout: "Could not connect",
  connection_failed: "Could not connect",
  reconnect_timeout: "Call disconnected",
  endpoint_expired: "Call disconnected",
  media_unavailable: "Media unavailable",
  cleanup_timeout: "Call interrupted",
  migration_cleanup_timeout: "Call interrupted",
  replaced_by_group: "Continued as a group call",
};

export function useCallDisplay() {
  const { view, manager, incoming, elsewhere } = useCall();
  const call = view.call;
  const ids = call?.participants.map(p => p.userId).filter(id => id !== manager.userID) ?? [];
  const profiles = usePublicProfiles(ids).data;
  const name = call?.kind === "group" ? "Group call" : profileName(profiles?.get(ids[0])) || "Call";
  const active = call?.state === CallState.ACTIVE && !view.disconnected && !incoming && !elsewhere;
  const migrating = ["preparing", "committing", "cleaning", "aborting"].includes(call?.migration?.phase ?? "");
  const status = !call ? "Call unavailable"
    : call.state === CallState.ENDED ? (callExitStatus[call.endReason] ?? (call.endReason === "missed" ? (call.callerId === manager.userID ? "No answer" : "Missed call") : "Call ended"))
    : view.ending ? "Ending call…"
    : incoming ? "Incoming call"
    : elsewhere ? "Answered on another device"
    : view.disconnected || (view.mediaReconnecting && !migrating) ? "Reconnecting…"
    : call.state === CallState.RINGING ? "Calling…"
    : migrating ? (call.migration?.phase === "preparing" ? "Optimizing connection…" : "Switching connection…")
    : call.state === CallState.CONNECTING ? (call.everActive ? "Reconnecting…" : "Connecting…")
    : "In call";
  const [now, setNow] = useState(() => Date.now());
  const started = call?.state !== CallState.ENDED && !incoming && !elsewhere ? call?.startedAt?.seconds : undefined;
  useEffect(() => {
    if (started === undefined) return;
    const interval = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(interval);
  }, [started]);
  const seconds = started === undefined ? 0 : Math.max(0, Math.floor(now / 1000) - Number(started));
  const elapsed = started === undefined ? "" : `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
  const duration = call?.startedAt && call.endedAt ? Math.max(0, Number(call.endedAt.seconds - call.startedAt.seconds)) : undefined;
  return { name, status, elapsed, profiles, active, migrating, duration };
}
