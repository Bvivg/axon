import type { ComponentProps } from "react";

import { Pane } from "@/components/ui/pane";

export function WorkspacePage(props: ComponentProps<typeof Pane>) {
  return <Pane {...props} />;
}
