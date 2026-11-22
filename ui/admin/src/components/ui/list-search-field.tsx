"use client";

import { Search, X } from "lucide-react";
import { useEffect, useRef, type KeyboardEventHandler } from "react";

import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupInput } from "@/components/ui/input";

export function ListSearchField({
  value, onChange, label, focus, onKeyDown, onFocus,
}: {
  value: string;
  onChange: (value: string) => void;
  label: string;
  focus?: number;
  onKeyDown?: KeyboardEventHandler<HTMLInputElement>;
  onFocus?: () => void;
}) {
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (focus !== undefined) input.current?.focus();
  }, [focus]);

  return (
    <div className="shrink-0 px-3 pb-2">
      <InputGroup className="h-9 rounded-lg border-transparent bg-muted/60 md:h-8">
        <Search aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        <InputGroupInput
          ref={input}
          type="search"
          aria-label={label}
          placeholder={label}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={onKeyDown}
          onFocus={onFocus}
          className="[&::-webkit-search-cancel-button]:appearance-none"
        />
        {value ? (
          <Button variant="ghost" size="icon-xs" aria-label="Clear search" onClick={() => {
            onChange("");
            input.current?.focus();
          }}>
            <X />
          </Button>
        ) : null}
      </InputGroup>
    </div>
  );
}
