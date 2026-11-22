"use client";

import { Checkbox as BaseCheckbox } from "@base-ui/react/checkbox";
import { Check, Minus } from "lucide-react";
import { useId, type ComponentProps, type ReactNode } from "react";

import { cn } from "@/lib/utils";

export function Checkbox({ className, ...props }: Omit<ComponentProps<typeof BaseCheckbox.Root>, "className"> & { className?: string }) {
  return (
    <BaseCheckbox.Root
      className={cn(
        "peer relative inline-flex size-4 shrink-0 items-center justify-center rounded-[5px] border border-input bg-background text-signal-foreground outline-none",
        "transition-[background-color,border-color,box-shadow] duration-[120ms] ease-signal",
        "hover:border-muted-foreground focus-visible:border-signal focus-visible:glow-signal",
        "data-[checked]:border-signal data-[checked]:bg-signal data-[indeterminate]:border-signal data-[indeterminate]:bg-signal",
        "data-[disabled]:cursor-not-allowed data-[disabled]:opacity-40",
        "before:absolute before:-inset-3.5 before:content-[''] pointer-coarse:size-5",
        className,
      )}
      {...props}
    >
      <BaseCheckbox.Indicator
        className="flex items-center justify-center"
        render={(indicatorProps, state) => (
          <span {...indicatorProps}>
            {state.indeterminate ? <Minus className="size-3" strokeWidth={3} /> : <Check className="size-3" strokeWidth={3} />}
          </span>
        )}
      />
    </BaseCheckbox.Root>
  );
}

export function CheckboxField({ label, hint, leading, className, ...props }: Omit<ComponentProps<typeof Checkbox>, "children"> & { label: string; hint?: string; leading?: ReactNode }) {
  const id = useId();
  return (
    <label className={cn("flex min-h-9 cursor-pointer items-center gap-2.5 text-sm pointer-coarse:min-h-11 has-[[data-disabled]]:cursor-not-allowed", className)}>
      <Checkbox aria-labelledby={`${id}-label`} aria-describedby={hint ? `${id}-hint` : undefined} {...props} />
      {leading}
      <span className="flex min-w-0 flex-col">
        <span id={`${id}-label`}>{label}</span>
        {hint ? <span id={`${id}-hint`} className="text-xs text-muted-foreground">{hint}</span> : null}
      </span>
    </label>
  );
}
