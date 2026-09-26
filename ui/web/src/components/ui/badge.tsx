import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex h-[22px] shrink-0 items-center gap-1.5 rounded-full px-2 text-xs font-medium whitespace-nowrap",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground",
        secondary: "bg-secondary",
        outline: "border border-border",
        mono: "rounded-[5px] border border-border px-1.5 font-mono text-2xs font-normal text-muted-foreground",
        signal: "bg-signal/15 text-signal",
        success: "bg-success/15",
        warning: "bg-warning/20",
        destructive: "bg-destructive/15 text-destructive",
      },
    },
    defaultVariants: { variant: "outline" },
  },
);

const dotColor = {
  default: "bg-primary-foreground",
  secondary: "bg-muted-foreground",
  outline: "bg-muted-foreground",
  mono: "bg-muted-foreground",
  signal: "bg-signal",
  success: "bg-success",
  warning: "bg-warning",
  destructive: "bg-destructive",
} as const;

export function Badge({
  className,
  variant,
  dot = false,
  children,
  ...props
}: ComponentProps<"span"> & VariantProps<typeof badgeVariants> & { dot?: boolean }) {
  return (
    <span className={cn(badgeVariants({ variant }), className)} {...props}>
      {dot ? (
        <span aria-hidden className={cn("size-1.5 rounded-full", dotColor[variant ?? "outline"])} />
      ) : null}
      {children}
    </span>
  );
}

export function CountBadge({ count, className }: { count: number; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-[18px] min-w-[18px] shrink-0 items-center justify-center rounded-full bg-foreground px-[5px] font-mono text-2xs text-background tabular-nums",
        className,
      )}
    >
      {count > 99 ? "99+" : count}
    </span>
  );
}
