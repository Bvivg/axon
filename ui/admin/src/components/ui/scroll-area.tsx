"use client";

import { ScrollArea as BaseScrollArea } from "@base-ui/react/scroll-area";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

type ScrollOrientation = "vertical" | "horizontal" | "both";

export function ScrollArea({
  className,
  viewportProps,
  orientation = "vertical",
  children,
  ...props
}: Omit<ComponentProps<typeof BaseScrollArea.Root>, "className"> & {
  className?: string;
  viewportProps?: Omit<ComponentProps<typeof BaseScrollArea.Viewport>, "className"> & { className?: string };
  orientation?: ScrollOrientation;
}) {
  const { className: viewportClassName, ...viewportRest } = viewportProps ?? {};

  return (
    <BaseScrollArea.Root className={cn("relative flex min-h-0 min-w-0 flex-col overflow-hidden", className)} {...props}>
      <BaseScrollArea.Viewport
        className={cn(
          "min-h-0 w-full flex-1 rounded-[inherit] outline-none focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-signal/50",
          viewportClassName,
        )}
        {...viewportRest}
      >
        {children}
      </BaseScrollArea.Viewport>
      {orientation !== "horizontal" ? <ScrollBar orientation="vertical" /> : null}
      {orientation !== "vertical" ? <ScrollBar orientation="horizontal" /> : null}
      <BaseScrollArea.Corner />
    </BaseScrollArea.Root>
  );
}

export function ScrollBar({
  className,
  orientation = "vertical",
  ...props
}: Omit<ComponentProps<typeof BaseScrollArea.Scrollbar>, "className"> & { className?: string }) {
  return (
    <BaseScrollArea.Scrollbar
      orientation={orientation}
      className={cn(
        "z-10 flex p-0.5 opacity-0 transition-opacity delay-300 duration-200 ease-signal",
        "data-[hovering]:opacity-100 data-[hovering]:delay-0 data-[scrolling]:opacity-100 data-[scrolling]:delay-0 data-[scrolling]:duration-75",
        "data-[orientation=vertical]:w-2.5 data-[orientation=horizontal]:h-2.5 data-[orientation=horizontal]:flex-col",
        className,
      )}
      {...props}
    >
      <BaseScrollArea.Thumb
        className={cn(
          "rounded-full bg-muted-foreground/35 transition-colors hover:bg-muted-foreground/60",
          orientation === "vertical" ? "w-full" : "h-full",
        )}
      />
    </BaseScrollArea.Scrollbar>
  );
}
