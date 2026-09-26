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
  sideOffset = 6,
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
            "flex w-56 flex-col rounded-lg border border-border bg-popover p-1 text-sm text-popover-foreground shadow-overlay outline-none",
            "origin-[var(--transform-origin)] transition-[opacity,translate] duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:translate-y-1 data-[starting-style]:opacity-0",
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
  return <BaseMenu.Separator className={cn("-mx-1 my-1 h-px bg-border", className)} {...props} />;
}

const itemClass =
  "flex h-[30px] cursor-default items-center gap-2.5 rounded-[5px] px-2 outline-none select-none data-[disabled]:pointer-events-none data-[highlighted]:bg-accent data-[disabled]:opacity-50 [&>svg]:size-4 [&>svg]:shrink-0 [&>svg]:text-muted-foreground";

export function DropdownMenuItem({ className, ...props }: ComponentProps<typeof BaseMenu.Item>) {
  return <BaseMenu.Item className={cn(itemClass, className)} {...props} />;
}

export function DropdownMenuLinkItem({
  className,
  closeOnClick = true,
  ...props
}: ComponentProps<typeof BaseMenu.LinkItem>) {
  return <BaseMenu.LinkItem className={cn(itemClass, className)} closeOnClick={closeOnClick} {...props} />;
}

export function DropdownMenuRadioGroup(props: ComponentProps<typeof BaseMenu.RadioGroup>) {
  return <BaseMenu.RadioGroup {...props} />;
}

export function DropdownMenuRadioItem({ className, children, ...props }: ComponentProps<typeof BaseMenu.RadioItem>) {
  return (
    <BaseMenu.RadioItem className={cn(itemClass, "relative pl-8", className)} {...props}>
      <BaseMenu.RadioItemIndicator className="absolute left-2 flex size-3.5 items-center justify-center">
        <Check className="size-3.5" />
      </BaseMenu.RadioItemIndicator>
      {children}
    </BaseMenu.RadioItem>
  );
}
