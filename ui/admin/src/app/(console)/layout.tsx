"use client";

import { useEffect, type ReactNode } from "react";

import { ConsoleShell } from "@/components/shell/console-shell";
import { ErrorCard } from "@/components/ui/error-card";
import { AccessStatus, useAdminAccess } from "@/lib/admin/access";
import { leaveToApp, useRequireSession } from "@/lib/auth/guards";

export default function ConsoleLayout({ children }: { children: ReactNode }) {
  const access = useAdminAccess();
  const allowed = access.staff || access.admin;
  useRequireSession();

  useEffect(() => {
    if (access.status === AccessStatus.Ready && !allowed) leaveToApp();
  }, [access.status, allowed]);

  if (access.status === AccessStatus.Error) {
    return (
      <main className="flex min-h-dvh items-center justify-center p-4">
        <ErrorCard title="Could not check your access" error={access.error} onRetry={access.refetch} className="w-full max-w-md" />
      </main>
    );
  }

  if (access.status !== AccessStatus.Ready || !allowed) {
    return (
      <main className="flex min-h-dvh items-center justify-center">
        <p role="status" className="text-sm text-muted-foreground">Checking access…</p>
      </main>
    );
  }

  return <ConsoleShell>{children}</ConsoleShell>;
}
