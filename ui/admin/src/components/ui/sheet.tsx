"use client";

import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

export function Sheet({
  open,
  onOpenChange,
  title,
  children,
  footer,
  className,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
}) {
  return (
    <BaseDialog.Root open={open} onOpenChange={onOpenChange}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="fixed inset-0 z-50 bg-scrim transition-opacity duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <BaseDialog.Popup
          className={cn(
            "fixed inset-0 z-50 flex flex-col bg-popover text-popover-foreground shadow-overlay outline-none md:inset-y-2 md:right-2 md:left-auto md:w-[min(32rem,calc(100vw-1rem))] md:rounded-xl md:border md:border-border",
            "transition-[opacity,translate] duration-200 ease-signal data-[ending-style]:translate-x-4 data-[ending-style]:opacity-0 data-[starting-style]:translate-x-4 data-[starting-style]:opacity-0",
            className,
          )}
        >
          <div className="flex min-h-14 shrink-0 items-center justify-between gap-3 border-b border-line px-5">
            <BaseDialog.Title className="truncate text-base font-semibold">{title}</BaseDialog.Title>
            <BaseDialog.Close render={<Button variant="ghost" size="icon" aria-label="Close" className="pointer-coarse:size-11" />}><X /></BaseDialog.Close>
          </div>
          <ScrollArea className="min-h-0 flex-1">
            <div className="flex min-h-full flex-col p-5">{children}</div>
          </ScrollArea>
          {footer ? <div className="flex shrink-0 items-center justify-end gap-2 border-t border-line px-5 py-3">{footer}</div> : null}
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}
