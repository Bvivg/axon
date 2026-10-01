import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Textarea({ className, ...props }: ComponentProps<"textarea">) {
  return (
    <textarea
      className={cn(
        "field-sizing-content min-h-11 w-full resize-none rounded-lg border border-input bg-background px-3 py-2.5 text-[16px] leading-6 transition-[border-color,box-shadow] duration-[120ms] ease-signal md:min-h-8 md:rounded-md md:px-2.5 md:py-1.5 md:text-sm md:leading-5",
        "focus-visible:border-signal focus-visible:outline-none focus-visible:glow-signal aria-invalid:border-destructive",
        "placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}
