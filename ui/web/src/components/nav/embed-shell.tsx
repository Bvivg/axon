"use client";

import type { ReactNode } from "react";

import { LoadingDots } from "@/components/ui/status";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";

export function EmbedShell({ children }: { children: ReactNode }) {
  const { status } = useSession();
  useRequireSession();
  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-background">
      {status === "authenticated" ? children : <div aria-busy="true" className="grid flex-1 place-items-center text-muted-foreground"><LoadingDots /></div>}
    </div>
  );
}
