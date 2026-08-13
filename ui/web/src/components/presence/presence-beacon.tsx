"use client";

import { useSession } from "@/lib/auth/session";
import { usePresenceSocket } from "@/lib/ws/use-presence-socket";

export function PresenceBeacon() {
  const { status } = useSession();

  if (status !== "authenticated") {
    return null;
  }

  return <ActivePresence />;
}

function ActivePresence() {
  usePresenceSocket();
  return null;
}
