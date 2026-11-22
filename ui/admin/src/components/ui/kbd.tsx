import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Kbd({ className, ...props }: ComponentProps<"kbd">) {
  return (
    <kbd
      className={cn(
        "inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-sm border border-border bg-muted px-1 font-mono text-2xs leading-none text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}

export function KbdCombo({
  keys,
  className,
  ...props
}: ComponentProps<"span"> & { keys: string[] }) {
  return (
    <span className={cn("inline-flex items-center gap-[3px]", className)} {...props}>
      {keys.map((key, index) => (
        <Kbd key={`${index}-${key}`}>{key}</Kbd>
      ))}
    </span>
  );
}
