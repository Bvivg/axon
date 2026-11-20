"use client";

import type { ComponentProps } from "react";

import { InputGroup, InputGroupInput } from "@/components/ui/input";

const usernameMaxLength = 32;

export function UsernameInput({ invalid, ...props }: ComponentProps<"input"> & { invalid?: boolean }) {
  return (
    <InputGroup invalid={invalid}>
      <span aria-hidden className="font-mono text-muted-foreground">
        @
      </span>
      <InputGroupInput
        type="text"
        autoComplete="off"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        maxLength={usernameMaxLength + 1}
        aria-invalid={invalid || undefined}
        {...props}
      />
    </InputGroup>
  );
}
