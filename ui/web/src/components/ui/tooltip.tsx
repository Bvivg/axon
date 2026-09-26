"use client";

import { Tooltip as BaseTooltip } from "@base-ui/react/tooltip";
import type { ComponentProps, ReactElement } from "react";

import { cn } from "@/lib/utils";

export const Tooltip = BaseTooltip.Root;

export function TooltipTrigger({ render }: { render: ReactElement }) {
  return <BaseTooltip.Trigger render={render} />;
}

export function TooltipContent({
  className,
  side = "top",
  sideOffset = 6,
  ...props
}: ComponentProps<typeof BaseTooltip.Popup> & {
  side?: "top" | "right" | "bottom" | "left";
  sideOffset?: number;
}) {
  return (
    <BaseTooltip.Portal>
      <BaseTooltip.Positioner side={side} sideOffset={sideOffset}>
        <BaseTooltip.Popup
          className={cn(
            "z-50 rounded-md border border-border bg-card px-3 py-1.5 text-xs text-card-foreground shadow-md",
            className,
          )}
          {...props}
        />
      </BaseTooltip.Positioner>
    </BaseTooltip.Portal>
  );
}
