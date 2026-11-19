"use client";

import type { ReactNode } from "react";

import { useRequireRole } from "@/lib/auth/guards";

export default function AdminLayout({ children }: { children: ReactNode }) {
  const authorized = useRequireRole("admin");

  if (!authorized) {
    return (
      <main className="flex min-h-0 flex-1 items-center justify-center">
        <p role="status" className="text-sm text-muted-foreground">Checking access…</p>
      </main>
    );
  }

  return children;
}
