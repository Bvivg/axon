"use client";

import { Select as BaseSelect } from "@base-ui/react/select";
import { Check, ChevronsUpDown } from "lucide-react";
import { useId } from "react";

import { cn } from "@/lib/utils";

export interface SelectOption {
  value: string;
  label: string;
}

export function Select({
  value,
  options,
  onChange,
  label,
  hideLabel = false,
  className,
  disabled,
}: {
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  label: string;
  hideLabel?: boolean;
  className?: string;
  disabled?: boolean;
}) {
  const id = useId();
  const items = Object.fromEntries(options.map((option) => [option.value, option.label]));
  return (
    <div className="flex min-w-0 items-center gap-1.5">
      <span id={id} className={cn("shrink-0 text-xs text-muted-foreground", hideLabel && "sr-only")}>{label}</span>
      <BaseSelect.Root value={value} items={items} onValueChange={(next) => onChange((next as string | null) ?? "")} disabled={disabled}>
        <BaseSelect.Trigger
          aria-labelledby={id}
          className={cn(
            "inline-flex h-11 min-w-0 items-center justify-between gap-2 rounded-lg border border-input bg-background px-3 text-[16px] outline-none md:h-8 md:rounded-md md:px-2.5 md:text-sm",
            "transition-[border-color,box-shadow] duration-[120ms] ease-signal hover:border-muted-foreground focus-visible:border-signal focus-visible:glow-signal data-[popup-open]:border-signal",
            "data-[disabled]:cursor-not-allowed data-[disabled]:opacity-50",
            className,
          )}
        >
          <BaseSelect.Value className="truncate" />
          <BaseSelect.Icon className="text-muted-foreground"><ChevronsUpDown className="size-3.5" /></BaseSelect.Icon>
        </BaseSelect.Trigger>
        <BaseSelect.Portal>
          <BaseSelect.Positioner sideOffset={6} alignItemWithTrigger={false} className="z-50 outline-none">
            <BaseSelect.Popup
              className={cn(
                "max-h-[min(var(--available-height),22rem)] min-w-[var(--anchor-width)] overflow-y-auto rounded-lg border border-border bg-popover p-1 text-sm text-popover-foreground shadow-overlay outline-none",
                "origin-[var(--transform-origin)] transition-[opacity,translate] duration-150 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:translate-y-1 data-[starting-style]:opacity-0",
              )}
            >
              <BaseSelect.List>
                {options.map((option) => (
                  <BaseSelect.Item
                    key={option.value || "any"}
                    value={option.value}
                    className="flex min-h-8 cursor-default items-center gap-2 rounded-md py-1 pr-2 pl-7 outline-none select-none pointer-coarse:min-h-11 data-[highlighted]:bg-accent data-[selected]:font-medium"
                  >
                    <BaseSelect.ItemIndicator className="absolute left-2 text-signal"><Check className="size-3.5" /></BaseSelect.ItemIndicator>
                    <BaseSelect.ItemText>{option.label}</BaseSelect.ItemText>
                  </BaseSelect.Item>
                ))}
              </BaseSelect.List>
            </BaseSelect.Popup>
          </BaseSelect.Positioner>
        </BaseSelect.Portal>
      </BaseSelect.Root>
    </div>
  );
}
