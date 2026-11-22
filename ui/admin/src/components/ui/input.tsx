import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/lib/utils";

const fieldShell =
  "h-11 w-full rounded-lg border border-input bg-background px-3 text-[16px] transition-[border-color,box-shadow] duration-[120ms] ease-signal md:h-8 md:rounded-md md:px-2.5 md:text-sm";

const fieldFocus =
  "focus-visible:border-signal focus-visible:outline-none focus-visible:glow-signal aria-invalid:border-destructive";

export function Input({ className, ...props }: ComponentProps<"input">) {
  return (
    <input
      className={cn(
        fieldShell,
        fieldFocus,
        "placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export function InputGroup({
  className,
  children,
  invalid,
}: {
  className?: string;
  children: ReactNode;
  invalid?: boolean;
}) {
  return (
    <div
      className={cn(
        fieldShell,
        "flex items-center gap-2 has-[input:focus-visible]:border-signal has-[input:focus-visible]:glow-signal",
        invalid && "border-destructive",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function InputGroupInput({ className, ...props }: ComponentProps<"input">) {
  return (
    <input
      className={cn(
        "h-full min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground focus-visible:outline-none disabled:cursor-not-allowed",
        className,
      )}
      {...props}
    />
  );
}
