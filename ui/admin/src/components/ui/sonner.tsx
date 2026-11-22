"use client";

import type { CSSProperties } from "react";
import { Toaster as Sonner, type ToasterProps } from "sonner";

export function Toaster(props: ToasterProps) {
  return (
    <Sonner
      position="bottom-right"
      mobileOffset={{ bottom: "calc(6rem + env(safe-area-inset-bottom))", left: "1rem", right: "1rem" }}
      toastOptions={{ classNames: {
        toast: "axon-toast",
        title: "axon-toast-title",
        actionButton: "axon-toast-action",
      } }}
      style={
        {
          "--normal-bg": "var(--color-popover)",
          "--normal-text": "var(--color-popover-foreground)",
          "--normal-border": "var(--color-border)",
        } as CSSProperties
      }
      {...props}
    />
  );
}
