"use client";

import { Popover as BasePopover } from "@base-ui/react/popover";
import type { ComponentProps, ReactElement } from "react";

import { cn } from "@/lib/utils";

export const Popover = BasePopover.Root;
export const PopoverClose = BasePopover.Close;

export function PopoverTrigger({ render, ...props }: { render: ReactElement } & Omit<ComponentProps<typeof BasePopover.Trigger>, "render">) {
  return <BasePopover.Trigger render={render} {...props} />;
}

export function PopoverContent({
  className,
  side = "bottom",
  align = "start",
  sideOffset = 6,
  ...props
}: ComponentProps<typeof BasePopover.Popup> & {
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  sideOffset?: number;
}) {
  return (
    <BasePopover.Portal>
      <BasePopover.Positioner side={side} align={align} sideOffset={sideOffset} collisionPadding={12} className="z-50 outline-none">
        <BasePopover.Popup
          className={cn(
            "w-72 max-w-[calc(100vw-1.5rem)] rounded-lg border border-border bg-popover p-3 text-sm text-popover-foreground shadow-overlay outline-none",
            "origin-[var(--transform-origin)] transition-[opacity,translate] duration-150 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:translate-y-1 data-[starting-style]:opacity-0",
            className,
          )}
          {...props}
        />
      </BasePopover.Positioner>
    </BasePopover.Portal>
  );
}
