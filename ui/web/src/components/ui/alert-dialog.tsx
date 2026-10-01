"use client";

import { AlertDialog as BaseAlertDialog } from "@base-ui/react/alert-dialog";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export const AlertDialog = BaseAlertDialog.Root;
export const AlertDialogClose = BaseAlertDialog.Close;

export function AlertDialogContent({ className, children, ...props }: ComponentProps<typeof BaseAlertDialog.Popup>) {
  return (
    <BaseAlertDialog.Portal>
      <BaseAlertDialog.Backdrop className="fixed inset-0 z-50 bg-scrim transition-opacity duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
      <BaseAlertDialog.Popup
        className={cn(
          "fixed top-1/2 left-1/2 z-50 w-[min(calc(100vw-2rem),24rem)] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-border bg-popover p-5 text-popover-foreground shadow-overlay outline-none",
          "transition-[opacity,translate] duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:translate-y-[calc(-50%+4px)] data-[starting-style]:opacity-0",
          className,
        )}
        {...props}
      >
        {children}
      </BaseAlertDialog.Popup>
    </BaseAlertDialog.Portal>
  );
}

export function AlertDialogTitle({ className, ...props }: ComponentProps<typeof BaseAlertDialog.Title>) {
  return <BaseAlertDialog.Title className={cn("text-base font-semibold tracking-snug", className)} {...props} />;
}

export function AlertDialogDescription({ className, ...props }: ComponentProps<typeof BaseAlertDialog.Description>) {
  return <BaseAlertDialog.Description className={cn("mt-2 text-sm text-muted-foreground", className)} {...props} />;
}
