import type { ReactNode } from "react";

import { EmbedShell } from "@/components/nav/embed-shell";

export const dynamic = "force-dynamic";

export default function EmbedLayout({ children }: { children: ReactNode }) {
  return <EmbedShell>{children}</EmbedShell>;
}
