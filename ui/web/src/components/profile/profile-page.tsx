import type { ReactNode } from "react";

import { Pane, PaneSection } from "@/components/ui/pane";

export function ProfilePage({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <Pane title={title} actions={aside} back="/profile" backLabel="Back to profile" width="narrow">
      {children}
    </Pane>
  );
}

export function ProfileSection({ id, label, aside, children }: { id: string; label: string; aside?: ReactNode; children: ReactNode }) {
  return <PaneSection id={id} title={label} aside={aside}>{children}</PaneSection>;
}
