import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * A failure the user has to read. role="alert" so a screen reader announces it
 * when it appears — a sign-in error that only exists visually is a sign-in
 * error some people never learn about.
 */
export function Alert({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      role="alert"
      className={cn(
        "rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive",
        className,
      )}
      {...props}
    />
  );
}
