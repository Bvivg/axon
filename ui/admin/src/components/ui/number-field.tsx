"use client";

import { NumberField as BaseNumberField } from "@base-ui/react/number-field";
import { Minus, Plus } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

const stepper = "flex w-11 shrink-0 items-center justify-center text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:bg-accent data-[disabled]:opacity-40 md:w-8";

export function NumberField({ className, id, invalid, describedBy, onBlur, ...props }: Omit<ComponentProps<typeof BaseNumberField.Root>, "className"> & { className?: string; invalid?: boolean; describedBy?: string; onBlur?: () => void }) {
  return (
    <BaseNumberField.Root id={id} className={cn("w-36", className)} {...props}>
      <BaseNumberField.Group
        className={cn(
          "flex h-11 overflow-hidden rounded-lg border border-input bg-background md:h-8 md:rounded-md",
          "transition-[border-color,box-shadow] duration-[120ms] ease-signal focus-within:border-signal focus-within:glow-signal",
          invalid && "border-destructive",
        )}
      >
        <BaseNumberField.Decrement aria-label="Decrease" className={cn(stepper, "border-r border-input")}><Minus className="size-3.5" /></BaseNumberField.Decrement>
        <BaseNumberField.Input aria-invalid={invalid || undefined} aria-describedby={describedBy} onBlur={onBlur} className="min-w-0 flex-1 bg-transparent text-center text-[16px] tabular-nums outline-none md:text-sm" />
        <BaseNumberField.Increment aria-label="Increase" className={cn(stepper, "border-l border-input")}><Plus className="size-3.5" /></BaseNumberField.Increment>
      </BaseNumberField.Group>
    </BaseNumberField.Root>
  );
}
