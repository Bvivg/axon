"use client";

import { useSession } from "@/lib/auth/session";
import { usePresenceSocket } from "@/lib/ws/use-presence-socket";

export function PresenceBeacon() {
  const { status, user } = useSession();

  if (status !== "authenticated") {
    return null;
  }

  return <ActivePresence key={user?.id} />;
}

function ActivePresence() {
  usePresenceSocket();
  return null;
}
