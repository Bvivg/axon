import type { ReactNode } from "react";

import { AppShell } from "@/components/nav/app-shell";

export const dynamic = "force-dynamic";

export default function AppLayout({ children }: { children: ReactNode }) {
  return <AppShell callsEvidence={process.env.CALLS_E2E_PROBE === "1"}>{children}</AppShell>;
}
