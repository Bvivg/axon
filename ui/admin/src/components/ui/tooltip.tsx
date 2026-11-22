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
      <BaseTooltip.Positioner side={side} sideOffset={sideOffset} className="z-50">
        <BaseTooltip.Popup
          className={cn(
            "flex h-[26px] items-center gap-2 rounded-md bg-primary px-2 text-xs text-primary-foreground",
            "transition-opacity duration-[120ms] ease-signal data-[ending-style]:opacity-0 data-[starting-style]:opacity-0",
            className,
          )}
          {...props}
        />
      </BaseTooltip.Positioner>
    </BaseTooltip.Portal>
  );
}
