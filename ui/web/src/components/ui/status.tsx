import { LoaderCircle } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function PresenceDot({
  online,
  className,
}: {
  online: boolean;
  className?: string;
}) {
  return (
    <span
      role="img"
      aria-label={online ? "Online" : "Offline"}
      className={cn(
        "absolute -right-0.5 -bottom-0.5 size-2 rounded-full ring-2 ring-background",
        online ? "bg-success" : "border-[1.5px] border-muted-foreground bg-background",
        className,
      )}
    />
  );
}

export function LiveDot({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "size-[7px] shrink-0 rounded-full bg-signal shadow-[0_0_0_3px_color-mix(in_oklch,var(--color-signal)_22%,transparent)]",
        className,
      )}
    />
  );
}

export function LoadingDots({
  className,
  label = "Loading",
  ...props
}: ComponentProps<"span"> & { label?: string }) {
  return (
    <span role="status" aria-label={label} className={cn("inline-flex gap-1", className)} {...props}>
      {[0, 150, 300].map((delay) => (
        <span
          key={delay}
          className="size-[5px] animate-dot rounded-full bg-current"
          style={{ animationDelay: `${delay}ms` }}
        />
      ))}
    </span>
  );
}

export function Spinner({ className }: { className?: string }) {
  return <LoaderCircle aria-hidden className={cn("size-3.5 animate-spin", className)} />;
}
