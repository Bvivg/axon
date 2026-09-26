"use client";

import { Check } from "lucide-react";
import { Menu as BaseMenu } from "@base-ui/react/menu";
import type { ComponentProps, ReactElement } from "react";

import { cn } from "@/lib/utils";

export const DropdownMenu = BaseMenu.Root;

export function DropdownMenuTrigger({ render }: { render: ReactElement }) {
  return <BaseMenu.Trigger render={render} />;
}

export function DropdownMenuContent({
  className,
  side = "bottom",
  align = "start",
  sideOffset = 8,
  ...props
}: ComponentProps<typeof BaseMenu.Popup> & {
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  sideOffset?: number;
}) {
  return (
    <BaseMenu.Portal>
      <BaseMenu.Positioner side={side} align={align} sideOffset={sideOffset} className="z-50 outline-none">
        <BaseMenu.Popup
          className={cn(
            "w-56 rounded-md border border-border bg-card p-1 text-card-foreground shadow-md outline-none",
            className,
          )}
          {...props}
        />
      </BaseMenu.Positioner>
    </BaseMenu.Portal>
  );
}

export function DropdownMenuLabel({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("px-2 py-1.5", className)} {...props} />;
}

export function DropdownMenuSeparator({ className, ...props }: ComponentProps<typeof BaseMenu.Separator>) {
  return <BaseMenu.Separator className={cn("my-1 h-px bg-border", className)} {...props} />;
}

export function DropdownMenuItem({ className, ...props }: ComponentProps<typeof BaseMenu.Item>) {
  return (
    <BaseMenu.Item
      className={cn(
        "flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none hover:bg-muted focus:bg-muted data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&>svg]:size-4 [&>svg]:shrink-0",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuLinkItem({ className, ...props }: ComponentProps<typeof BaseMenu.LinkItem>) {
  return (
    <BaseMenu.LinkItem
      className={cn(
        "flex cursor-default items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none hover:bg-muted focus:bg-muted [&>svg]:size-4 [&>svg]:shrink-0",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuRadioGroup(props: ComponentProps<typeof BaseMenu.RadioGroup>) {
  return <BaseMenu.RadioGroup {...props} />;
}

export function DropdownMenuRadioItem({ className, children, ...props }: ComponentProps<typeof BaseMenu.RadioItem>) {
  return (
    <BaseMenu.RadioItem
      className={cn(
        "relative flex cursor-default items-center gap-2 rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none hover:bg-muted focus:bg-muted data-[disabled]:pointer-events-none data-[disabled]:opacity-50 [&>svg]:size-4 [&>svg]:shrink-0",
        className,
      )}
      {...props}
    >
      <BaseMenu.RadioItemIndicator className="absolute left-2 flex size-3.5 items-center justify-center">
        <Check className="size-3.5" />
      </BaseMenu.RadioItemIndicator>
      {children}
    </BaseMenu.RadioItem>
  );
}
